package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/scanner"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

type scanFolderRequest struct {
	RelPath string `json:"relPath"`
}

type scanLibraryRequest struct {
	Name     string   `json:"name"`
	RelPaths []string `json:"relPaths"`
}

func (s *Server) settingsProgress(w http.ResponseWriter, r *http.Request) {
	progress, updatedAt, refreshing := s.cachedProcessingProgress()
	cache := s.cachedCacheStats()
	writeJSON(w, http.StatusOK, processingProgressDTO(progress, s.jobs.Stats(), cache, updatedAt, refreshing))
}

func (s *Server) settingsActivity(w http.ResponseWriter, r *http.Request) {
	status, err := s.scanner.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "activity_failed", "读取活动状态失败")
		return
	}
	var lastRun *ScanRunDTO
	if status.LastRun != nil {
		dto := scanRunDTO(*status.LastRun)
		lastRun = &dto
	}
	progress, updatedAt, refreshing := s.cachedProcessingProgress()
	s.cleanupMu.Lock()
	cleanup := s.cleanupStatus
	s.cleanupMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"scan": ScanStatusDTO{
			Running: status.Running, LastStart: status.LastStart, LastRun: lastRun, Progress: scanProgressDTO(status.Progress),
		},
		"progress": processingProgressDTO(progress, s.jobs.Stats(), s.cachedCacheStats(), updatedAt, refreshing),
		"cleanup":  cleanup,
	})
}

func (s *Server) cachedProcessingProgress() (db.ProcessingProgress, int64, bool) {
	const ttl = 5 * time.Second
	now := time.Now()
	s.progressMu.Lock()
	progress := s.progressStats
	updatedAt := int64(0)
	if !s.progressStatsAt.IsZero() {
		updatedAt = s.progressStatsAt.Unix()
	}
	stale := s.progressStatsAt.IsZero() || now.Sub(s.progressStatsAt) > ttl
	refreshing := s.progressRefreshing
	if stale && !s.progressRefreshing {
		s.progressRefreshing = true
		refreshing = true
		go s.refreshProcessingProgress()
	}
	s.progressMu.Unlock()
	return progress, updatedAt, refreshing
}

func (s *Server) refreshProcessingProgress() {
	progress, err := s.db.ProcessingProgress(context.Background())
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if err == nil {
		s.progressStats = progress
		s.progressStatsAt = time.Now()
	}
	s.progressRefreshing = false
}

func (s *Server) cachedCacheStats() CacheStatsDTO {
	const ttl = 5 * time.Second
	now := time.Now()
	s.cacheMu.Lock()
	stats := s.cacheStats
	stale := s.cacheStatsAt.IsZero() || now.Sub(s.cacheStatsAt) > ttl
	if s.cacheRefreshing {
		stats.Refreshing = true
		s.cacheMu.Unlock()
		return stats
	}
	if stale {
		s.cacheRefreshing = true
		stats.Refreshing = true
		s.cacheMu.Unlock()
		go s.refreshCacheStats()
		return stats
	}
	s.cacheMu.Unlock()
	return stats
}

func (s *Server) refreshCacheStats() {
	stats := computeCacheStats(filepath.Join(s.store.DataRoot, "cache"))
	stats.UpdatedAt = time.Now().Unix()
	s.cacheMu.Lock()
	s.cacheStats = stats
	s.cacheStatsAt = time.Now()
	s.cacheRefreshing = false
	s.cacheMu.Unlock()
}

func computeCacheStats(root string) CacheStatsDTO {
	var stats CacheStatsDTO
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		stats.FileCount++
		stats.SizeBytes += info.Size()
		return nil
	})
	if err != nil {
		return CacheStatsDTO{}
	}
	return stats
}

func (s *Server) scanLibraries(w http.ResponseWriter, r *http.Request) {
	libraries, configured, err := s.db.GetScanLibraries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_libraries_failed", "读取来源失败")
		return
	}
	status, err := s.scanner.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_status_failed", "读取扫描状态失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": configured,
		"items":      s.scanLibraryDTOs(r.Context(), libraries, status),
	})
}

func (s *Server) addScanLibrary(w http.ResponseWriter, r *http.Request) {
	var payload scanLibraryRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_name", "来源名称不能为空")
		return
	}
	roots, err := s.validSourceRoots(payload.RelPaths)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}
	libraries, library, err := s.db.AddScanLibrary(r.Context(), name, roots)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_library_add_failed", "添加来源失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": true,
		"items":      s.scanLibraryDTOs(r.Context(), libraries, scanner.Status{}),
		"library":    s.scanLibraryDTO(r.Context(), library, scanner.Status{}),
		"started":    false,
	})
}

func (s *Server) updateScanLibrary(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "来源 ID 无效")
		return
	}
	var payload scanLibraryRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_name", "来源名称不能为空")
		return
	}
	roots, err := s.validSourceRoots(payload.RelPaths)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}
	libraries, library, err := s.db.UpdateScanLibrary(r.Context(), id, name, roots)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "scan_library_not_found", "来源不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_library_update_failed", "更新来源失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": true,
		"items":      s.scanLibraryDTOs(r.Context(), libraries, scanner.Status{}),
		"library":    s.scanLibraryDTO(r.Context(), library, scanner.Status{}),
		"started":    false,
	})
}

func (s *Server) removeScanLibrary(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "来源 ID 无效")
		return
	}
	libraries, err := s.db.RemoveScanLibrary(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_library_remove_failed", "删除来源失败")
		return
	}
	started := false
	cleanupQueued := false
	if len(libraries) == 0 {
		cleanupQueued = s.startClearAllAssetsCleanup()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured":    true,
		"items":         s.scanLibraryDTOs(r.Context(), libraries, scanner.Status{}),
		"started":       started,
		"cleanupQueued": cleanupQueued,
	})
}

func (s *Server) startClearAllAssetsCleanup() bool {
	s.cleanupMu.Lock()
	if s.cleanupStatus.Running {
		s.cleanupMu.Unlock()
		return false
	}
	s.cleanupStatus = CleanupStatusDTO{Running: true, Status: "running", UpdatedAt: util.UnixNow()}
	s.cleanupMu.Unlock()
	go func() {
		deleted, err := s.db.MarkAllDeletedWithCache(context.Background(), util.UnixNow())
		if err == nil {
			s.removeDeletedAssetCaches(deleted)
			err = s.db.RefreshFolders(context.Background())
		}
		s.cleanupMu.Lock()
		defer s.cleanupMu.Unlock()
		s.cleanupStatus.Running = false
		s.cleanupStatus.UpdatedAt = util.UnixNow()
		if err != nil {
			s.cleanupStatus.Status = "error"
			s.cleanupStatus.LastError = "清理资源失败"
			s.logger.Warn("cleanup after library removal failed", "error", err)
			return
		}
		s.cleanupStatus.Status = "done"
		s.cleanupStatus.LastError = ""
	}()
	return true
}

func (s *Server) removeDeletedAssetCaches(items []db.DeletedAsset) {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.CacheKey == "" {
			continue
		}
		if _, ok := seen[item.CacheKey]; ok {
			continue
		}
		seen[item.CacheKey] = struct{}{}
		if err := s.store.RemoveCache(item.CacheKey); err != nil {
			s.logger.Warn("remove cache after asset deletion failed", "relPath", item.RelPath, "cacheKey", item.CacheKey, "error", err)
		}
	}
	s.cacheMu.Lock()
	s.cacheStatsAt = time.Time{}
	s.cacheMu.Unlock()
}

func (s *Server) scanLibrary(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "来源 ID 无效")
		return
	}
	library, err := s.db.FindScanLibrary(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "scan_library_not_found", "来源不存在")
		return
	}
	started := s.scanner.TriggerRoots("library:"+library.Name, library.Roots)
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": started})
}

func (s *Server) scanFolders(w http.ResponseWriter, r *http.Request) {
	folders, configured, err := s.db.GetScanFolders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_folders_failed", "读取扫描文件夹失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": configured,
		"items":      s.scanFolderDTOs(folders),
	})
}

func (s *Server) addScanFolder(w http.ResponseWriter, r *http.Request) {
	var payload scanFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	rel, err := storage.NormalizeRelPath(payload.RelPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "文件夹路径无效")
		return
	}
	if !s.sourceDirExists(rel) {
		writeError(w, http.StatusBadRequest, "folder_not_found", "文件夹不存在或不可访问")
		return
	}
	folders, err := s.db.AddScanFolder(r.Context(), rel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_folder_add_failed", "添加扫描文件夹失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.scanFolderDTOs(folders)})
}

func (s *Server) validSourceRoots(relPaths []string) ([]string, error) {
	if len(relPaths) == 0 {
		return nil, errors.New("至少选择一个文件夹")
	}
	roots := make([]string, 0, len(relPaths))
	for _, raw := range relPaths {
		rel, err := storage.NormalizeRelPath(raw)
		if err != nil {
			return nil, errors.New("文件夹路径无效")
		}
		if !s.sourceDirExists(rel) {
			return nil, errors.New("文件夹不存在或不可访问")
		}
		roots = append(roots, rel)
	}
	normalized, err := db.NormalizeScanFolders(roots)
	if err != nil {
		return nil, errors.New("文件夹路径无效")
	}
	if len(normalized) == 0 {
		return nil, errors.New("至少选择一个文件夹")
	}
	return normalized, nil
}

func (s *Server) removeScanFolder(w http.ResponseWriter, r *http.Request) {
	rel, err := storage.NormalizeRelPath(r.URL.Query().Get("relPath"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "文件夹路径无效")
		return
	}
	folders, err := s.db.RemoveScanFolder(r.Context(), rel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_folder_remove_failed", "移除扫描文件夹失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.scanFolderDTOs(folders)})
}

func (s *Server) sourceFolders(w http.ResponseWriter, r *http.Request) {
	parentRel, err := storage.NormalizeRelPath(r.URL.Query().Get("parentRelPath"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "文件夹路径无效")
		return
	}
	folders, err := s.sourcePickerRoots(r.Context(), strings.TrimSpace(r.URL.Query().Get("excludeLibraryId")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_folders_failed", "读取扫描文件夹失败")
		return
	}
	if parentRel == "" && s.store.HasVirtualRoot() {
		items := make([]SourceFolderDTO, 0, len(s.store.Roots))
		for _, rel := range s.store.RootRelPaths() {
			items = append(items, s.sourceFolderDTO(rel, folders))
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		writeJSON(w, http.StatusOK, map[string]any{
			"current": s.sourceFolderDTO(parentRel, folders),
			"items":   items,
			"warning": "",
		})
		return
	}
	parentPath, err := s.store.PhotoPath(parentRel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "文件夹路径无效")
		return
	}
	entries, err := util.ReadDirPartial(parentPath)
	warning := ""
	if err != nil && len(entries) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"current": s.sourceFolderDTO(parentRel, folders),
			"items":   []SourceFolderDTO{},
			"warning": "源目录读取失败",
		})
		return
	}
	if err != nil {
		warning = "源目录部分读取失败"
	}
	items := make([]SourceFolderDTO, 0, len(entries))
	for _, entry := range entries {
		childRel := joinRel(parentRel, entry.Name())
		childPath, err := s.store.PhotoPath(childRel)
		if err != nil {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			inside, _, err := s.store.SymlinkTargetWithinRoot(childPath)
			if err != nil || !inside {
				continue
			}
		}
		info, err := os.Stat(childPath)
		if err != nil || !info.IsDir() {
			continue
		}
		items = append(items, s.sourceFolderDTO(childRel, folders))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{
		"current": s.sourceFolderDTO(parentRel, folders),
		"items":   items,
		"warning": warning,
	})
}

func (s *Server) sourcePickerRoots(ctx context.Context, excludeLibraryID string) ([]string, error) {
	if excludeLibraryID == "" {
		folders, _, err := s.db.GetScanFolders(ctx)
		return folders, err
	}
	libraries, _, err := s.db.GetScanLibraries(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]db.ScanLibrary, 0, len(libraries))
	for _, library := range libraries {
		if library.ID != excludeLibraryID {
			filtered = append(filtered, library)
		}
	}
	return db.ScanRoots(filtered), nil
}

func (s *Server) scanFolderDTOs(folders []string) []ScanFolderDTO {
	items := make([]ScanFolderDTO, 0, len(folders))
	for _, rel := range folders {
		parent := parentPtr(rel)
		items = append(items, ScanFolderDTO{
			RelPath: rel, Name: storage.FolderName(rel), ParentRelPath: parent,
			Depth: storage.FolderDepth(rel), Exists: s.sourceDirExists(rel),
		})
	}
	return items
}

func (s *Server) scanLibraryDTOs(ctx context.Context, libraries []db.ScanLibrary, status scanner.Status) []ScanLibraryDTO {
	items := make([]ScanLibraryDTO, 0, len(libraries))
	for _, library := range libraries {
		items = append(items, s.scanLibraryDTO(ctx, library, status))
	}
	return items
}

func (s *Server) scanLibraryDTO(ctx context.Context, library db.ScanLibrary, status scanner.Status) ScanLibraryDTO {
	folders := s.scanFolderDTOs(library.Roots)
	exists := len(folders) > 0
	for _, folder := range folders {
		if !folder.Exists {
			exists = false
			break
		}
	}
	progress, err := s.db.ProcessingProgressForRoots(ctx, library.Roots)
	if err != nil {
		s.logger.Warn("load library progress failed", "libraryID", library.ID, "error", err)
	}
	return ScanLibraryDTO{
		ID:       library.ID,
		Name:     library.Name,
		Folders:  folders,
		Exists:   exists,
		Progress: scanLibraryProgressDTO(library, progress, status),
	}
}

func scanLibraryProgressDTO(library db.ScanLibrary, progress db.ProcessingProgress, status scanner.Status) ScanLibraryProgressDTO {
	active := status.Running && scanRootsSame(library.Roots, status.Progress.Roots)
	scannedFiles := progress.AssetTotal
	unscannedFiles := 0
	if active {
		scannedFiles = status.Progress.ScannedFiles
		unscannedFiles = status.Progress.TotalFiles - status.Progress.ScannedFiles
		if unscannedFiles < 0 {
			unscannedFiles = 0
		}
	}
	return ScanLibraryProgressDTO{
		AssetTotal:     progress.AssetTotal,
		ScannedFiles:   scannedFiles,
		UnscannedFiles: unscannedFiles,
		Thumb:          workStatusCountsDTO(progress.Thumb),
		Transcode:      workStatusCountsDTO(progress.Transcode),
		Active:         active,
	}
}

func scanRootsSame(a []string, b []string) bool {
	left, err := db.NormalizeScanFolders(a)
	if err != nil {
		return false
	}
	right, err := db.NormalizeScanFolders(b)
	if err != nil {
		return false
	}
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *Server) sourceFolderDTO(rel string, folders []string) SourceFolderDTO {
	selected := false
	for _, folder := range folders {
		if folder == rel {
			selected = true
			break
		}
	}
	return SourceFolderDTO{
		RelPath: rel, Name: storage.FolderName(rel), ParentRelPath: parentPtr(rel),
		Depth: storage.FolderDepth(rel), Selected: selected, Included: db.AssetInScanFolders(rel, folders),
	}
}

func (s *Server) sourceDirExists(rel string) bool {
	if rel == "" && s.store.HasVirtualRoot() {
		return true
	}
	fullPath, err := s.store.PhotoPath(rel)
	if err != nil {
		return false
	}
	linkInfo, err := os.Lstat(fullPath)
	if err != nil {
		return false
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		inside, _, err := s.store.SymlinkTargetWithinRoot(fullPath)
		if err != nil || !inside {
			return false
		}
	}
	info, err := os.Stat(fullPath)
	return err == nil && info.IsDir()
}

func joinRel(parent string, name string) string {
	if parent == "" {
		return path.Clean(name)
	}
	return path.Clean(parent + "/" + name)
}

func parentPtr(rel string) *string {
	if rel == "" {
		return nil
	}
	parent := storage.ParentRelPath(rel)
	return &parent
}
