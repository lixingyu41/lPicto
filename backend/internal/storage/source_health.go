package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

// SourceHealth is a short-lived, process-local availability cache for media roots.
// A directory read is intentional: a mounted NFS directory can stat successfully
// while reads from the backing NAS are already failing.
type SourceHealth struct {
	store Store
	ttl   time.Duration

	mu      sync.Mutex
	entries map[string]SourceHealthStatus
	samples map[string]string
	redis   *redis.Client
}

type SourceHealthStatus struct {
	RootID    string `json:"rootId"`
	Available bool   `json:"available"`
	Message   string `json:"message,omitempty"`
	CheckedAt int64  `json:"checkedAt"`
}

const sourceProbeTimeout = 2 * time.Second

func NewSourceHealth(store Store, ttl time.Duration, redisURLs ...string) *SourceHealth {
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	health := &SourceHealth{store: store, ttl: ttl, entries: make(map[string]SourceHealthStatus, len(store.Roots)), samples: make(map[string]string, len(store.Roots))}
	if len(redisURLs) > 0 && strings.TrimSpace(redisURLs[0]) != "" {
		if options, err := redis.ParseURL(redisURLs[0]); err == nil {
			health.redis = redis.NewClient(options)
		}
	}
	return health
}

func (h *SourceHealth) AvailableForRel(rel string) (bool, SourceHealthStatus) {
	if h == nil {
		return true, SourceHealthStatus{Available: true}
	}
	root, err := h.probeRootForRel(rel)
	if err != nil {
		return false, SourceHealthStatus{Available: false, Message: err.Error(), CheckedAt: time.Now().Unix()}
	}
	return h.AvailableRoot(root)
}

func (h *SourceHealth) AvailableRoot(root Root) (bool, SourceHealthStatus) {
	if h == nil {
		return true, SourceHealthStatus{RootID: root.ID, Available: true}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if global, ok := h.globalUnavailable(root.ID); ok {
		h.entries[root.ID] = global
		return false, global
	}
	if current, ok := h.entries[root.ID]; ok && time.Since(time.Unix(current.CheckedAt, 0)) < h.ttl {
		return current.Available, current
	}
	status := probeSourceRoot(root)
	if sample := h.samples[root.ID]; status.Available && sample != "" {
		status = probeSourceFile(root, sample)
	}
	h.entries[root.ID] = status
	return status.Available, status
}

func (h *SourceHealth) CachedAvailableForRel(rel string) (bool, SourceHealthStatus) {
	if h == nil {
		return true, SourceHealthStatus{Available: true}
	}
	root, err := h.probeRootForRel(rel)
	if err != nil {
		return false, SourceHealthStatus{Available: false, Message: err.Error()}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if global, ok := h.globalUnavailable(root.ID); ok {
		return false, global
	}
	if current, ok := h.entries[root.ID]; ok {
		return current.Available, current
	}
	return true, SourceHealthStatus{RootID: root.ID, Available: true, Message: "尚未在读取窗口验证"}
}

func (h *SourceHealth) MarkUnavailableForRel(rel string, err error) {
	if h == nil || err == nil {
		return
	}
	root, rootErr := h.probeRootForRel(rel)
	if rootErr != nil {
		return
	}
	h.mu.Lock()
	h.entries[root.ID] = SourceHealthStatus{RootID: root.ID, Available: false, Message: err.Error(), CheckedAt: time.Now().Unix()}
	h.mu.Unlock()
	h.publishUnavailable(root.ID, err.Error())
}

// RecordSourceError turns a confirmed source-read error into a root-level pause.
// The failed file becomes the next recovery probe, so cached directory entries
// cannot make an unavailable NFS mount appear healthy again.
func (h *SourceHealth) RecordSourceError(rel string, err error) {
	if h == nil || !IsSourceUnavailable(err) {
		return
	}
	root, rootErr := h.probeRootForRel(rel)
	if rootErr != nil {
		return
	}
	path, pathErr := h.store.PhotoPath(rel)
	if pathErr != nil {
		return
	}
	h.mu.Lock()
	h.samples[root.ID] = path
	h.entries[root.ID] = SourceHealthStatus{RootID: root.ID, Available: false, Message: err.Error(), CheckedAt: time.Now().Unix()}
	h.mu.Unlock()
	h.publishUnavailable(root.ID, err.Error())
}

// AssetReadErrorIsSourceUnavailable distinguishes a missing or unreadable
// individual media file from an unavailable storage root. Confirmed source
// failures remain pending instead of being recorded as permanent media errors.
func (h *SourceHealth) AssetReadErrorIsSourceUnavailable(rel string, readErr error, libraryRoots ...string) bool {
	if h == nil || readErr == nil {
		return false
	}
	root, err := h.probeRootForRel(rel)
	if err != nil {
		return false
	}
	probeRoot := root
	if len(libraryRoots) > 0 && strings.TrimSpace(libraryRoots[0]) != "" {
		if libraryPath, pathErr := h.store.PhotoPath(libraryRoots[0]); pathErr == nil {
			probeRoot.Path = libraryPath
		}
	}
	rootStatus := probeSourceRoot(probeRoot)
	if !rootStatus.Available {
		rootStatus.RootID = root.ID
		h.mu.Lock()
		delete(h.samples, root.ID)
		h.entries[root.ID] = rootStatus
		h.mu.Unlock()
		h.publishUnavailable(root.ID, rootStatus.Message)
		return true
	}
	if !IsSourceUnavailable(readErr) {
		return false
	}
	path, err := h.store.PhotoPath(rel)
	if err == nil {
		status := probeSourceFile(root, path)
		if !status.Available && IsSourceUnavailablePath(status.Message) {
			h.mu.Lock()
			h.samples[root.ID] = path
			h.entries[root.ID] = status
			h.mu.Unlock()
			h.publishUnavailable(root.ID, status.Message)
		}
	}
	return true
}

// ProbeAsset checks a stored media file. Missing files do not make a storage
// root unhealthy; transport and NFS errors do.
func (h *SourceHealth) ProbeAsset(rel string) {
	if h == nil {
		return
	}
	root, err := h.probeRootForRel(rel)
	if err != nil {
		return
	}
	path, err := h.store.PhotoPath(rel)
	if err != nil {
		return
	}
	status := probeSourceFile(root, path)
	if status.Available || IsSourceUnavailablePath(status.Message) {
		h.mu.Lock()
		h.samples[root.ID] = path
		h.entries[root.ID] = status
		h.mu.Unlock()
		if status.Available {
			h.clearUnavailable(root.ID)
		} else {
			h.publishUnavailable(root.ID, status.Message)
		}
	}
}

func (h *SourceHealth) Statuses() []SourceHealthStatus {
	if h == nil {
		return nil
	}
	roots := h.probeRoots()
	statuses := make([]SourceHealthStatus, 0, len(roots))
	for _, root := range roots {
		_, status := h.AvailableRoot(root)
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].RootID < statuses[j].RootID })
	return statuses
}

// CachedStatuses reports the last known state without touching source paths.
// Use this for UI polling and idle monitors so an NFS disk can remain asleep.
func (h *SourceHealth) CachedStatuses() []SourceHealthStatus {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	statuses := make([]SourceHealthStatus, 0, len(h.entries)+len(h.store.Roots))
	seen := map[string]struct{}{}
	for _, status := range h.entries {
		statuses = append(statuses, status)
		seen[status.RootID] = struct{}{}
	}
	for _, root := range h.store.Roots {
		if _, ok := seen[root.ID]; ok {
			continue
		}
		statuses = append(statuses, SourceHealthStatus{
			RootID: root.ID, Available: true, Message: "尚未在读取窗口验证",
		})
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].RootID < statuses[j].RootID })
	return statuses
}

func (h *SourceHealth) probeRootForRel(rel string) (Root, error) {
	root, childRel, err := h.store.RootForRel(rel)
	if err != nil || h.store.HasVirtualRoot() || childRel == "" {
		return root, err
	}
	segment := strings.SplitN(filepath.ToSlash(childRel), "/", 2)[0]
	if segment == "" {
		return root, nil
	}
	root.ID = segment
	root.Path = filepath.Join(root.Path, filepath.FromSlash(segment))
	return root, nil
}

func (h *SourceHealth) probeRoots() []Root {
	if h.store.HasVirtualRoot() {
		return append([]Root(nil), h.store.Roots...)
	}
	base := h.store.Roots[0]
	entries, err := os.ReadDir(base.Path)
	if err != nil {
		return []Root{base}
	}
	roots := make([]Root, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			roots = append(roots, Root{ID: entry.Name(), Path: filepath.Join(base.Path, entry.Name())})
		}
	}
	if len(roots) == 0 {
		return []Root{base}
	}
	return roots
}

func probeSourceRoot(root Root) SourceHealthStatus {
	status := SourceHealthStatus{RootID: root.ID, Available: true, CheckedAt: time.Now().Unix()}
	result := make(chan error, 1)
	go func() {
		dir, err := os.Open(root.Path)
		if err == nil {
			_, err = dir.Readdirnames(1)
			if err == io.EOF {
				err = nil
			}
			_ = dir.Close()
		}
		result <- err
	}()
	var err error
	select {
	case err = <-result:
	case <-time.After(sourceProbeTimeout):
		err = errors.New("source probe timed out")
	}
	if err != nil {
		status.Available = false
		status.Message = err.Error()
	}
	return status
}

func probeSourceFile(root Root, path string) SourceHealthStatus {
	status := SourceHealthStatus{RootID: root.ID, Available: true, CheckedAt: time.Now().Unix()}
	result := make(chan error, 1)
	go func() {
		file, err := os.Open(path)
		if err == nil {
			var one [1]byte
			_, err = file.Read(one[:])
			if err == io.EOF {
				err = nil
			}
			_ = file.Close()
		}
		result <- err
	}()
	var err error
	select {
	case err = <-result:
	case <-time.After(sourceProbeTimeout):
		err = errors.New("source probe timed out")
	}
	if errors.Is(err, os.ErrNotExist) {
		// The filesystem answered the lookup. The root is healthy; this is a
		// genuine missing media file and reconciliation must be allowed to run.
		return status
	}
	if err != nil {
		status.Available = false
		status.Message = err.Error()
	}
	return status
}

func IsSourceUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENXIO) || errors.Is(err, syscall.EIO) || errors.Is(err, syscall.ESTALE) || errors.Is(err, syscall.ETIMEDOUT) || errors.Is(err, syscall.ENOTCONN) {
		return true
	}
	return IsSourceUnavailablePath(err.Error())
}

func IsSourceUnavailablePath(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "no such device") || strings.Contains(message, "stale file handle") || strings.Contains(message, "input/output error") || strings.Contains(message, "transport endpoint is not connected") || strings.Contains(message, "operation not permitted") || strings.Contains(message, "source probe timed out") || strings.Contains(message, "source read timed out") || strings.Contains(message, "nfs")
}

func (h *SourceHealth) globalUnavailable(rootID string) (SourceHealthStatus, bool) {
	if h.redis == nil || rootID == "" {
		return SourceHealthStatus{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	raw, err := h.redis.Get(ctx, sourceHealthRedisKey(rootID)).Result()
	if err != nil {
		return SourceHealthStatus{}, false
	}
	var status SourceHealthStatus
	if json.Unmarshal([]byte(raw), &status) != nil || status.Available {
		return SourceHealthStatus{}, false
	}
	return status, true
}

func (h *SourceHealth) publishUnavailable(rootID string, message string) {
	if h.redis == nil || rootID == "" {
		return
	}
	status := SourceHealthStatus{RootID: rootID, Available: false, Message: message, CheckedAt: time.Now().Unix()}
	raw, err := json.Marshal(status)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = h.redis.Set(ctx, sourceHealthRedisKey(rootID), raw, 45*time.Second).Err()
}

func (h *SourceHealth) clearUnavailable(rootID string) {
	if h.redis == nil || rootID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = h.redis.Del(ctx, sourceHealthRedisKey(rootID)).Err()
}

func sourceHealthRedisKey(rootID string) string { return "lpicto:source-health:" + rootID }
