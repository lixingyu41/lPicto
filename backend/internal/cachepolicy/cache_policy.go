package cachepolicy

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"lpicto/backend/internal/db"
)

const (
	DefaultCacheMaxBytes     int64 = 25 << 30
	DefaultCacheMinFreeBytes int64 = 40 << 30
)

type CacheUsage struct {
	TotalBytes int64
	FileCount  int
	FreeBytes  int64
	ByKind     map[string]int64
	UpdatedAt  int64
}

type Manager struct {
	root     string
	db       *db.DB
	maxBytes int64
	minFree  int64
	mu       sync.Mutex
	pinned   map[string]int
}

func New(root string, database *db.DB) *Manager {
	return &Manager{
		root: filepath.Clean(root), db: database,
		maxBytes: DefaultCacheMaxBytes, minFree: DefaultCacheMinFreeBytes,
		pinned: map[string]int{},
	}
}

func (p *Manager) MaxBytes() int64     { return p.maxBytes }
func (p *Manager) MinFreeBytes() int64 { return p.minFree }

func (p *Manager) Pin(path string) func() {
	path = filepath.Clean(path)
	p.mu.Lock()
	p.pinned[path]++
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		if p.pinned[path] <= 1 {
			delete(p.pinned, path)
		} else {
			p.pinned[path]--
		}
		p.mu.Unlock()
	}
}

func (p *Manager) isPinned(path string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pinned[filepath.Clean(path)] > 0
}

func (p *Manager) Register(ctx context.Context, kind, cacheKey, path string, assetID *int64, pinFor time.Duration) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	rel, err := filepath.Rel(p.root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return
	}
	var pinnedUntil *int64
	if pinFor > 0 {
		value := time.Now().Add(pinFor).Unix()
		pinnedUntil = &value
	}
	if p.db != nil {
		_ = p.db.UpsertCacheEntry(ctx, db.CacheEntry{
			CacheKey: cacheKey, Kind: kind, RelativePath: filepath.ToSlash(rel),
			AssetID: assetID, SizeBytes: info.Size(), PinnedUntil: pinnedUntil, State: "ready",
		})
	}
}

func (p *Manager) Touch(ctx context.Context, kind, cacheKey, path string) {
	rel, err := filepath.Rel(p.root, path)
	if err != nil || strings.HasPrefix(rel, "..") || p.db == nil {
		return
	}
	_ = p.db.TouchCacheEntry(ctx, kind, cacheKey, filepath.ToSlash(rel))
}

func (p *Manager) Usage() CacheUsage {
	usage := CacheUsage{ByKind: map[string]int64{}, UpdatedAt: time.Now().Unix()}
	_ = filepath.WalkDir(p.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(p.root, path)
		if err != nil {
			return nil
		}
		kind := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		usage.TotalBytes += info.Size()
		usage.FileCount++
		usage.ByKind[kind] += info.Size()
		return nil
	})
	usage.FreeBytes = diskFreeBytes(p.root)
	return usage
}

func (p *Manager) Reconcile(ctx context.Context) error {
	if p.db == nil {
		return nil
	}
	return filepath.WalkDir(p.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(p.root, path)
		if err != nil {
			return nil
		}
		parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
		if len(parts) < 2 {
			return nil
		}
		base := strings.SplitN(entry.Name(), ".", 2)[0]
		cacheKey := base
		if len(cacheKey) > 20 {
			cacheKey = cacheKey[:20]
		}
		return p.db.UpsertCacheEntry(ctx, db.CacheEntry{
			CacheKey: cacheKey, Kind: parts[0], RelativePath: filepath.ToSlash(rel),
			SizeBytes: info.Size(), State: "ready",
		})
	})
}

func (p *Manager) EnsureCapacity(ctx context.Context, reserve int64) (CacheUsage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	usage := p.Usage()
	target := p.maxBytes - reserve
	if target < 0 {
		target = 0
	}
	required := usage.TotalBytes - target
	if freeRequired := p.minFree + reserve - usage.FreeBytes; freeRequired > required {
		required = freeRequired
	}
	if required <= 0 {
		return usage, nil
	}
	type candidate struct {
		path string
		kind string
		size int64
		mod  time.Time
	}
	var candidates []candidate
	evictionRank := map[string]int{
		"ai-staging": 0, "video-chunks": 1, "video-proxies": 1, "originals": 2, "previews": 2,
		"thumbs": 10, "video-posters": 10,
	}
	_ = filepath.WalkDir(p.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || p.pinned[path] > 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(p.root, path)
		kind := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		age := time.Since(info.ModTime())
		if kind == "ai-staging" && age < 24*time.Hour {
			return nil
		}
		if (kind == "video-chunks" || kind == "video-proxies" || kind == "originals" || kind == "previews") && age < 10*time.Minute {
			return nil
		}
		if evictionRank[kind] >= 10 && !strings.Contains(entry.Name(), ".tmp") {
			return nil
		}
		candidates = append(candidates, candidate{path: path, kind: kind, size: info.Size(), mod: info.ModTime()})
		return nil
	})
	sort.Slice(candidates, func(i, j int) bool {
		ri, rj := evictionRank[candidates[i].kind], evictionRank[candidates[j].kind]
		if ri != rj {
			return ri < rj
		}
		return candidates[i].mod.Before(candidates[j].mod)
	})
	var released int64
	for _, item := range candidates {
		if released >= required {
			break
		}
		if err := os.Remove(item.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
		released += item.size
		if p.db != nil {
			rel, _ := filepath.Rel(p.root, item.path)
			_ = p.db.DeleteCacheEntry(ctx, item.kind, strings.SplitN(filepath.Base(item.path), ".", 2)[0], filepath.ToSlash(rel))
		}
	}
	usage = p.Usage()
	if usage.TotalBytes+reserve > p.maxBytes || usage.FreeBytes-reserve < p.minFree {
		return usage, errors.New("本地缓存容量不足，已停止新的后台缓存写入")
	}
	return usage, nil
}
