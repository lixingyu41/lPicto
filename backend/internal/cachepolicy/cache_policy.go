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
	state    *sharedState
}

type sharedState struct {
	mu     sync.Mutex
	pinned map[string]int
}

var sharedStates sync.Map

type CleanupResult struct {
	DeletedFiles  int
	ReleasedBytes int64
}

type evictionCandidate struct {
	path     string
	kind     string
	cacheKey string
	size     int64
	mod      time.Time
	dir      bool
	rank     int
}

func New(root string, database *db.DB) *Manager {
	cleanRoot := filepath.Clean(root)
	value, _ := sharedStates.LoadOrStore(cleanRoot, &sharedState{pinned: map[string]int{}})
	return &Manager{
		root: cleanRoot, db: database,
		maxBytes: DefaultCacheMaxBytes, minFree: DefaultCacheMinFreeBytes,
		state: value.(*sharedState),
	}
}

func (p *Manager) MaxBytes() int64     { return p.maxBytes }
func (p *Manager) MinFreeBytes() int64 { return p.minFree }

func (p *Manager) Pin(path string) func() {
	path = filepath.Clean(path)
	p.state.mu.Lock()
	p.state.pinned[path]++
	p.state.mu.Unlock()
	return func() {
		p.state.mu.Lock()
		if p.state.pinned[path] <= 1 {
			delete(p.state.pinned, path)
		} else {
			p.state.pinned[path]--
		}
		p.state.mu.Unlock()
	}
}

func (p *Manager) pathPinnedLocked(path string) bool {
	path = filepath.Clean(path)
	for pinnedPath, count := range p.state.pinned {
		if count <= 0 {
			continue
		}
		pinnedPath = filepath.Clean(pinnedPath)
		if path == pinnedPath || strings.HasPrefix(path, pinnedPath+string(filepath.Separator)) || strings.HasPrefix(pinnedPath, path+string(filepath.Separator)) {
			return true
		}
	}
	return false
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
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
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
	candidates := p.evictionCandidatesLocked(30*time.Second, p.cacheAccessTimes(ctx))
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return candidates[i].mod.Before(candidates[j].mod)
	})
	var released int64
	for _, item := range candidates {
		if released >= required {
			break
		}
		if err := p.removeCandidate(ctx, item); err != nil {
			continue
		}
		released += item.size
	}
	usage = p.Usage()
	if usage.TotalBytes+reserve > p.maxBytes || usage.FreeBytes-reserve < p.minFree {
		return usage, errors.New("本地缓存容量不足，已停止新的后台缓存写入")
	}
	return usage, nil
}

// Clear removes every reclaimable cache that is not currently being read or
// written by this process. Completed thumbnails, video posters and progress previews are always
// retained. A short grace period protects newly-created partial outputs.
func (p *Manager) Clear(ctx context.Context) (CleanupResult, error) {
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	candidates := p.evictionCandidatesLocked(30*time.Second, p.cacheAccessTimes(ctx))
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return candidates[i].mod.Before(candidates[j].mod)
	})
	var result CleanupResult
	for _, item := range candidates {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := p.removeCandidate(ctx, item); err != nil {
			continue
		}
		result.DeletedFiles++
		result.ReleasedBytes += item.size
	}
	p.removeEmptyDirectories()
	return result, nil
}

// CleanupAbandoned removes stale partial outputs even when the cache is below
// its size limit. Completed cache files are left to the regular LRU policy.
func (p *Manager) CleanupAbandoned(ctx context.Context, olderThan time.Duration) (CleanupResult, error) {
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	cutoff := time.Now().Add(-olderThan)
	var result CleanupResult
	err := filepath.WalkDir(p.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || p.pathPinnedLocked(path) {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if !strings.Contains(name, ".tmp") && !strings.Contains(name, ".part") && !strings.Contains(name, ".partial") {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			return nil
		}
		rel, _ := filepath.Rel(p.root, path)
		kind := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		item := evictionCandidate{path: path, kind: kind, cacheKey: cacheKeyFromPath(path), size: info.Size(), mod: info.ModTime(), rank: 0}
		if err := p.removeCandidate(ctx, item); err == nil {
			result.DeletedFiles++
			result.ReleasedBytes += item.size
		}
		return ctx.Err()
	})
	p.removeEmptyDirectories()
	return result, err
}

func (p *Manager) evictionCandidatesLocked(activeGrace time.Duration, accessTimes map[string]time.Time) []evictionCandidate {
	var candidates []evictionCandidate
	now := time.Now()
	_ = filepath.WalkDir(p.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == p.root {
			return nil
		}
		rel, err := filepath.Rel(p.root, path)
		if err != nil {
			return nil
		}
		kind := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if entry.IsDir() {
			if kind != "ai-staging" || !strings.HasSuffix(entry.Name(), ".stage.d") {
				return nil
			}
			if p.pathPinnedLocked(path) {
				return filepath.SkipDir
			}
			size, mod := directoryUsage(path)
			candidates = append(candidates, evictionCandidate{path: path, kind: kind, cacheKey: strings.TrimSuffix(entry.Name(), ".stage.d"), size: size, mod: mod, dir: true, rank: 0})
			return filepath.SkipDir
		}
		if p.pathPinnedLocked(path) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		transient := strings.Contains(name, ".tmp") || strings.Contains(name, ".part") || strings.Contains(name, ".partial")
		if (kind == "thumbs" || kind == "video-posters" || kind == "storyboards") && !transient {
			return nil
		}
		if transient && activeGrace > 0 && now.Sub(info.ModTime()) < activeGrace {
			return nil
		}
		lastAccessed := info.ModTime()
		if accessedAt, ok := accessTimes[kind+"\x00"+filepath.ToSlash(rel)]; ok {
			lastAccessed = accessedAt
		}
		candidates = append(candidates, evictionCandidate{
			path: path, kind: kind, cacheKey: cacheKeyFromPath(path), size: info.Size(), mod: lastAccessed, rank: evictionRank(kind, transient),
		})
		return nil
	})
	return candidates
}

func (p *Manager) removeCandidate(ctx context.Context, item evictionCandidate) error {
	var err error
	if item.dir {
		err = os.RemoveAll(item.path)
	} else {
		err = os.Remove(item.path)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if p.db != nil {
		if item.kind == "ai-staging" && item.cacheKey != "" {
			_ = p.db.DeleteAIStageByCacheKey(ctx, item.cacheKey)
			_ = p.db.DeleteCacheEntriesByCacheKey(ctx, item.kind, item.cacheKey)
		} else {
			rel, _ := filepath.Rel(p.root, item.path)
			_ = p.db.DeleteCacheEntryByPath(ctx, item.kind, filepath.ToSlash(rel))
		}
	}
	return nil
}

func (p *Manager) removeEmptyDirectories() {
	var directories []string
	_ = filepath.WalkDir(p.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && entry.IsDir() && path != p.root {
			directories = append(directories, path)
		}
		return nil
	})
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if !p.pathPinnedLocked(directory) {
			_ = os.Remove(directory)
		}
	}
}

func evictionRank(kind string, transient bool) int {
	if transient || kind == "ai-staging" {
		return 0
	}
	return 1
}

func (p *Manager) cacheAccessTimes(ctx context.Context) map[string]time.Time {
	result := make(map[string]time.Time)
	if p.db == nil {
		return result
	}
	items, err := p.db.CacheEntryAccessTimes(ctx)
	if err != nil {
		return result
	}
	for key, value := range items {
		result[key] = time.Unix(value, 0)
	}
	return result
}

func cacheKeyFromPath(path string) string {
	base := filepath.Base(path)
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	if len(base) > 20 {
		base = base[:20]
	}
	return base
}

func directoryUsage(root string) (int64, time.Time) {
	var size int64
	var newest time.Time
	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			size += info.Size()
			if info.ModTime().After(newest) {
				newest = info.ModTime()
			}
		}
		return nil
	})
	return size, newest
}
