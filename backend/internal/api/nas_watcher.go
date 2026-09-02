package api

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"lpicto/backend/internal/debugcontrol"
	"lpicto/backend/internal/media"
	"lpicto/backend/internal/storage"
)

const nasWatcherEventLimit = 1000

type nasWatcherIntegration struct {
	mu             sync.RWMutex
	enabled        bool
	offlineAfter   time.Duration
	firstSeenAt    int64
	lastSeenAt     int64
	lastEventAt    int64
	instanceID     string
	remoteAddress  string
	acceptedEvents int64
}

type nasWatcherEventRequest struct {
	InstanceID string            `json:"instanceId"`
	Events     []nasWatcherEvent `json:"events"`
}

type nasWatcherEvent struct {
	Root      string `json:"root"`
	Path      string `json:"path"`
	Operation string `json:"operation"`
}

type nasWatcherStatusDTO struct {
	Enabled        bool   `json:"enabled"`
	Connected      bool   `json:"connected"`
	InstanceID     string `json:"instanceId,omitempty"`
	RemoteAddress  string `json:"remoteAddress,omitempty"`
	FirstSeenAt    int64  `json:"firstSeenAt,omitempty"`
	LastSeenAt     int64  `json:"lastSeenAt,omitempty"`
	LastEventAt    int64  `json:"lastEventAt,omitempty"`
	AcceptedEvents int64  `json:"acceptedEvents"`
	Message        string `json:"message"`
}

func newNASWatcherIntegration(enabled bool, offlineAfter time.Duration) *nasWatcherIntegration {
	if offlineAfter <= 0 {
		offlineAfter = 90 * time.Second
	}
	return &nasWatcherIntegration{enabled: enabled, offlineAfter: offlineAfter}
}

func (n *nasWatcherIntegration) observe(instanceID, remoteAddress string, eventCount int) {
	if n == nil {
		return
	}
	now := time.Now().Unix()
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.firstSeenAt == 0 {
		n.firstSeenAt = now
	}
	n.lastSeenAt = now
	if eventCount > 0 {
		n.lastEventAt = now
		n.acceptedEvents += int64(eventCount)
	}
	n.instanceID = strings.TrimSpace(instanceID)
	n.remoteAddress = remoteAddress
}

func (n *nasWatcherIntegration) status() nasWatcherStatusDTO {
	if n == nil {
		return nasWatcherStatusDTO{Message: "NAS 实时监听未配置"}
	}
	n.mu.RLock()
	result := nasWatcherStatusDTO{
		Enabled: n.enabled, InstanceID: n.instanceID, RemoteAddress: n.remoteAddress,
		FirstSeenAt: n.firstSeenAt, LastSeenAt: n.lastSeenAt, LastEventAt: n.lastEventAt,
		AcceptedEvents: n.acceptedEvents,
	}
	offlineAfter := n.offlineAfter
	n.mu.RUnlock()
	result.Connected = result.Enabled && result.LastSeenAt > 0 && time.Since(time.Unix(result.LastSeenAt, 0)) <= offlineAfter
	switch {
	case !result.Enabled:
		result.Message = "NAS 实时监听未配置；手动扫描和计划扫描正常可用"
	case result.Connected:
		result.Message = "飞牛 NAS 监听容器已连接"
	default:
		result.Message = "飞牛 NAS 监听容器未连接；实时发现暂停，其他功能不受影响"
	}
	return result
}

func (s *Server) nasWatcherEvents(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(s.cfg.NASWatcherToken) == "" {
		writeError(w, http.StatusServiceUnavailable, "nas_watcher_disabled", "NAS 实时监听未配置")
		return
	}
	provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	expected := s.cfg.NASWatcherToken
	if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		writeError(w, http.StatusUnauthorized, "nas_watcher_unauthorized", "NAS 监听器认证失败")
		return
	}
	if debugcontrol.BackgroundProcessingPaused() {
		s.nasWatcher.observe("", remoteHost(r.RemoteAddr), 0)
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": 0, "ignored": 0, "paused": true})
		return
	}
	var payload nasWatcherEventRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "nas_watcher_invalid", "NAS 监听事件格式无效")
		return
	}
	if len(payload.Events) > nasWatcherEventLimit {
		writeError(w, http.StatusRequestEntityTooLarge, "nas_watcher_too_many_events", "单次最多接收 1000 个监听事件")
		return
	}

	upserts := map[string]struct{}{}
	removes := map[string]struct{}{}
	removeTrees := map[string]struct{}{}
	roots := map[string]struct{}{}
	recoveryRoots := map[string]struct{}{}
	ignored := 0
	for _, event := range payload.Events {
		operation := strings.ToLower(strings.TrimSpace(event.Operation))
		if operation == "recover_root" {
			mappedRoot, ok := s.mapNASWatcherRoot(event.Root)
			if !ok {
				ignored++
				continue
			}
			recoveryRoots[mappedRoot] = struct{}{}
			continue
		}
		if (operation == "upsert" || operation == "remove") && !nasWatcherNestedFile(event.Path) {
			ignored++
			continue
		}
		mappedRoot, fullPath, ok := s.mapNASWatcherPath(event.Root, event.Path)
		if !ok {
			ignored++
			continue
		}
		switch operation {
		case "upsert":
			if media.DetectByPath(fullPath).OK {
				upserts[fullPath] = struct{}{}
				roots[mappedRoot] = struct{}{}
			} else {
				ignored++
			}
		case "remove":
			removes[fullPath] = struct{}{}
		case "remove_tree":
			removeTrees[fullPath] = struct{}{}
		default:
			ignored++
		}
	}

	removePaths := sortedKeys(removes)
	markedMissing := 0
	if len(removePaths) > 0 {
		marked, err := s.db.MarkMissingRelPaths(r.Context(), removePaths)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "nas_watcher_remove_failed", "标记已移走媒体失败")
			return
		}
		markedMissing += marked
	}
	for _, relPath := range sortedKeys(removeTrees) {
		marked, err := s.db.MarkMissingUnder(r.Context(), relPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "nas_watcher_remove_failed", "标记已移走文件夹失败")
			return
		}
		markedMissing += marked
	}
	if markedMissing > 0 {
		if _, err := s.db.RefreshSystemCollectionCounts(r.Context()); err != nil && s.logger != nil {
			s.logger.Warn("refresh system collection counts after NAS remove event failed", "error", err)
		}
	}
	upsertPaths := sortedKeys(upserts)
	if len(recoveryRoots) > 0 {
		result := s.scanner.RequestMediaScanNestedRoots("fsnotify_remote_recovery", sortedKeys(recoveryRoots))
		if !result.Accepted {
			writeError(w, http.StatusServiceUnavailable, "nas_watcher_scan_busy", "媒体扫描队列暂时无法接收监听恢复任务")
			return
		}
	}
	if len(upsertPaths) > 0 {
		result := s.scanner.RequestMetadataScanPaths("fsnotify_remote", sortedKeys(roots), upsertPaths)
		if !result.Accepted {
			writeError(w, http.StatusServiceUnavailable, "nas_watcher_scan_busy", "媒体扫描队列暂时无法接收监听事件")
			return
		}
	}
	accepted := len(upsertPaths) + len(removePaths) + len(removeTrees) + len(recoveryRoots)
	s.nasWatcher.observe(payload.InstanceID, remoteHost(r.RemoteAddr), accepted)
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": accepted, "ignored": ignored, "markedMissing": markedMissing})
}

func nasWatcherNestedFile(relPath string) bool {
	relPath, err := storage.NormalizeRelPath(relPath)
	return err == nil && strings.Contains(relPath, "/")
}

func (s *Server) nasWatcherStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.nasWatcher.status())
}

func (s *Server) mapNASWatcherPath(rootName, relPath string) (string, string, bool) {
	mappedRoot, ok := s.mapNASWatcherRoot(rootName)
	if !ok {
		return "", "", false
	}
	relPath, err := storage.NormalizeRelPath(relPath)
	if err != nil || relPath == "" {
		return "", "", false
	}
	fullPath, err := storage.NormalizeRelPath(path.Join(mappedRoot, relPath))
	if err != nil || (fullPath != mappedRoot && !strings.HasPrefix(fullPath, mappedRoot+"/")) {
		return "", "", false
	}
	if _, err := s.store.PhotoPath(fullPath); err != nil {
		return "", "", false
	}
	return mappedRoot, fullPath, true
}

func (s *Server) mapNASWatcherRoot(rootName string) (string, bool) {
	rootName = strings.ToUpper(strings.TrimSpace(rootName))
	mappedRoot, ok := s.cfg.NASWatcherRoots[rootName]
	if !ok {
		return "", false
	}
	mappedRoot, err := storage.NormalizeRelPath(mappedRoot)
	if err != nil || mappedRoot == "" {
		return "", false
	}
	if _, err := s.store.PhotoPath(mappedRoot); err != nil {
		return "", false
	}
	return mappedRoot, true
}

func remoteHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *Server) nasWatcherSystemTask() SystemTaskDTO {
	status := s.nasWatcher.status()
	item := SystemTaskDTO{
		ID: "nas_realtime_watcher", Name: "NAS 实时监听",
		Description: "接收飞牛 NAS 的文件完成与移动事件，并按精确路径触发媒体入库",
		Schedule:    "持续监听（可选）", Message: status.Message,
		Actions: []SystemTaskActionDTO{}, Failures: []SystemTaskFailureDTO{},
	}
	switch {
	case !status.Enabled:
		item.Status = "skipped"
	case status.Connected:
		item.Status = "success"
		item.Succeeded = boolValue(true)
		item.LastStartedAt = int64Value(status.FirstSeenAt)
	default:
		item.Status = "stopped"
		item.Succeeded = boolValue(false)
		item.LastStartedAt = int64Value(status.FirstSeenAt)
		item.LastFinishedAt = int64Value(status.LastSeenAt)
	}
	item.Processed = int(status.AcceptedEvents)
	return item
}

func int64Value(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}
