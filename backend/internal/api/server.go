package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"lpicto/backend/internal/config"
	"lpicto/backend/internal/db"
	"lpicto/backend/internal/events"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/scanner"
	"lpicto/backend/internal/storage"
)

type Server struct {
	cfg     config.Config
	db      *db.DB
	store   storage.Store
	scanner *scanner.Scanner
	jobs    *jobs.Manager
	events  *events.Bus
	logger  *slog.Logger

	cacheMu         sync.Mutex
	cacheStats      CacheStatsDTO
	cacheStatsAt    time.Time
	cacheRefreshing bool

	progressMu         sync.Mutex
	progressStats      db.ProcessingProgress
	progressStatsAt    time.Time
	progressRefreshing bool

	cleanupMu     sync.Mutex
	cleanupStatus CleanupStatusDTO
}

func NewServer(cfg config.Config, database *db.DB, store storage.Store, scan *scanner.Scanner, queue *jobs.Manager, bus *events.Bus, logger *slog.Logger) http.Handler {
	s := &Server{cfg: cfg, db: database, store: store, scanner: scan, jobs: queue, events: bus, logger: logger}
	r := chi.NewRouter()
	r.Use(foregroundActivity)
	r.Get("/api/health", s.health)
	r.Get("/api/config/public", s.publicConfig)
	r.Get("/api/events", s.eventStream)
	r.Post("/api/scan", s.triggerScan)
	r.Post("/api/scan/pause", s.pauseScan)
	r.Post("/api/scan/rebuild", s.rebuildScan)
	r.Get("/api/scan/status", s.scanStatus)
	r.Get("/api/scan/runs", s.scanRuns)
	r.Get("/api/settings/progress", s.settingsProgress)
	r.Get("/api/settings/activity", s.settingsActivity)
	r.Get("/api/settings/libraries", s.scanLibraries)
	r.Post("/api/settings/libraries", s.addScanLibrary)
	r.Put("/api/settings/libraries/{id}", s.updateScanLibrary)
	r.Delete("/api/settings/libraries/{id}", s.removeScanLibrary)
	r.Post("/api/settings/libraries/{id}/scan", s.scanLibrary)
	r.Get("/api/settings/scan-folders", s.scanFolders)
	r.Post("/api/settings/scan-folders", s.addScanFolder)
	r.Delete("/api/settings/scan-folders", s.removeScanFolder)
	r.Get("/api/source-folders", s.sourceFolders)
	r.Get("/api/album-groups", s.albumGroups)
	r.Post("/api/album-groups", s.createAlbumGroup)
	r.Get("/api/albums", s.albums)
	r.Post("/api/albums", s.createAlbum)
	r.Get("/api/albums/source-folders", s.albumSourceFolders)
	r.Get("/api/albums/{id}", s.album)
	r.Put("/api/albums/{id}", s.updateAlbum)
	r.Delete("/api/albums/{id}", s.deleteAlbum)
	r.Post("/api/albums/{id}/refresh", s.refreshAlbum)
	r.Get("/api/albums/{id}/anchors", s.albumAnchors)
	r.Get("/api/albums/{id}/assets", s.albumAssets)
	r.Get("/api/library/assets", s.libraryAssets)
	r.Get("/api/library/anchors", s.libraryAnchors)
	r.Get("/api/search/assets", s.searchAssets)
	r.Get("/api/folders", s.folders)
	r.Get("/api/folders/tree", s.folderTree)
	r.Get("/api/folders/{id}", s.folder)
	r.Get("/api/folders/{id}/assets", s.folderAssets)
	r.Get("/api/assets/{id}", s.asset)
	r.Get("/api/assets/{id}/preferences", s.assetPreferences)
	r.Put("/api/assets/{id}/preferences", s.updateAssetPreferences)
	r.Get("/api/assets/{id}/sidecars", s.assetSidecars)
	r.Get("/api/assets/{id}/subtitles/{subtitleID}", s.assetSubtitle)
	r.Get("/api/assets/{id}/neighbors", s.neighbors)
	r.Get("/api/assets/{id}/thumb", s.thumb)
	r.Get("/api/assets/{id}/preview", s.preview)
	r.Get("/api/assets/{id}/original", s.original)
	r.Get("/api/assets/{id}/video", s.video)
	r.Get("/api/assets/{id}/video-poster", s.videoPoster)
	r.Get("/api/assets/{id}/video-proxy", s.videoProxy)
	r.Get("/api/cache/thumbs/{name}", s.cacheThumb)
	r.NotFound(s.static)
	return r
}

func foregroundActivity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isForegroundRequest(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		done := jobs.EnterForeground()
		defer done()
		next.ServeHTTP(w, r)
	})
}

func isForegroundRequest(path string) bool {
	return strings.HasPrefix(path, "/api/library/") ||
		strings.HasPrefix(path, "/api/albums") ||
		strings.HasPrefix(path, "/api/search/") ||
		strings.HasPrefix(path, "/api/folders") ||
		strings.HasPrefix(path, "/api/assets/") ||
		strings.HasPrefix(path, "/api/cache/")
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) publicConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"pageSizeDefault":     s.cfg.PageSizeDefault,
		"pageSizeMax":         s.cfg.PageSizeMax,
		"thumbLongEdge":       s.cfg.ThumbLongEdge,
		"previewLongEdge":     s.cfg.PreviewLongEdge,
		"videoProxyEnabled":   s.cfg.VideoProxyEnabled,
		"videoProxyMaxHeight": s.cfg.VideoProxyMaxHeight,
	})
}

func (s *Server) eventStream(w http.ResponseWriter, r *http.Request) {
	if s.events == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "events_unsupported", "事件流不可用")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := s.events.Subscribe(r.Context())
	for event := range ch {
		if event.Type != "asset_ready" {
			continue
		}
		asset, ok := event.Payload.(model.Asset)
		if !ok {
			continue
		}
		data, err := json.Marshal(assetDTO(asset))
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "event: %s\n", event.Type)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

func (s *Server) triggerScan(w http.ResponseWriter, r *http.Request) {
	started := s.scanner.Trigger("manual")
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": started})
}

func (s *Server) rebuildScan(w http.ResponseWriter, r *http.Request) {
	if !boolQuery(r, "force", false) {
		writeError(w, http.StatusBadRequest, "confirm_required", "强制重建需要确认")
		return
	}
	started := s.scanner.TriggerRebuild("rebuild")
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": started})
}

func (s *Server) pauseScan(w http.ResponseWriter, r *http.Request) {
	paused := s.scanner.Pause()
	writeJSON(w, http.StatusOK, map[string]bool{"paused": paused})
}

func (s *Server) scanStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.scanner.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_status_failed", "读取扫描状态失败")
		return
	}
	var lastRun *ScanRunDTO
	if status.LastRun != nil {
		dto := scanRunDTO(*status.LastRun)
		lastRun = &dto
	}
	writeJSON(w, http.StatusOK, ScanStatusDTO{
		Running: status.Running, LastStart: status.LastStart, LastRun: lastRun, Progress: scanProgressDTO(status.Progress),
	})
}

func (s *Server) scanRuns(w http.ResponseWriter, r *http.Request) {
	page, pageSize := s.page(r, 20)
	runs, err := s.db.RecentScanRuns(r.Context(), page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_runs_failed", "读取扫描记录失败")
		return
	}
	writeJSON(w, http.StatusOK, PageDTO[ScanRunDTO]{
		Items: scanRunDTOs(runs.Items), Page: runs.Page, PageSize: runs.PageSize, HasMore: runs.HasMore,
	})
}

func (s *Server) timelineGroups(w http.ResponseWriter, r *http.Request) {
	page, pageSize := s.page(r, 24)
	unit := r.URL.Query().Get("unit")
	if unit != "year" && unit != "day" {
		unit = "month"
	}
	groups, err := s.db.TimelineGroups(r.Context(), unit, page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "timeline_groups_failed", "读取时间线失败")
		return
	}
	writeJSON(w, http.StatusOK, PageDTO[TimelineGroupDTO]{
		Items: timelineGroupDTOs(groups.Items), Page: groups.Page, PageSize: groups.PageSize, HasMore: groups.HasMore,
	})
}

func (s *Server) timelineAssets(w http.ResponseWriter, r *http.Request) {
	page, pageSize := s.page(r, s.cfg.PageSizeDefault)
	opts := db.AssetListOptions{Page: page, PageSize: pageSize, From: int64QueryPtr(r, "from"), To: int64QueryPtr(r, "to")}
	assets, err := s.db.ListTimelineAssets(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "timeline_assets_failed", "读取时间线资源失败")
		return
	}
	writeJSON(w, http.StatusOK, PageDTO[AssetDTO]{
		Items: assetDTOs(assets.Items), Page: assets.Page, PageSize: assets.PageSize, HasMore: assets.HasMore,
	})
}

func (s *Server) libraryAssets(w http.ResponseWriter, r *http.Request) {
	page, pageSize := s.page(r, s.cfg.PageSizeDefault)
	typeFilter := safeType(r.URL.Query().Get("type"))
	if typeFilter == "all" {
		typeFilter = ""
	}
	opts := db.AssetListOptions{
		Page: page, PageSize: pageSize, Type: typeFilter, Sort: safeSort(r.URL.Query().Get("sort")),
		Query: strings.TrimSpace(r.URL.Query().Get("q")), VisibleOnly: visibleOnly(r),
	}
	assets, err := s.db.ListLibraryAssets(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "library_assets_failed", "读取图库失败")
		return
	}
	writeJSON(w, http.StatusOK, PageDTO[AssetDTO]{
		Items: assetDTOs(assets.Items), Page: assets.Page, PageSize: assets.PageSize, HasMore: assets.HasMore,
	})
}

func (s *Server) libraryAnchors(w http.ResponseWriter, r *http.Request) {
	_, pageSize := s.page(r, s.cfg.PageSizeDefault)
	typeFilter := safeType(r.URL.Query().Get("type"))
	if typeFilter == "all" {
		typeFilter = ""
	}
	anchorResult, err := s.db.LibraryAnchors(r.Context(), db.AssetListOptions{
		PageSize:    pageSize,
		Type:        typeFilter,
		Sort:        safeSort(r.URL.Query().Get("sort")),
		Query:       strings.TrimSpace(r.URL.Query().Get("q")),
		VisibleOnly: visibleOnly(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "library_anchors_failed", "读取图库索引失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": libraryAnchorDTOs(anchorResult.Items), "total": anchorResult.Total})
}

func (s *Server) searchAssets(w http.ResponseWriter, r *http.Request) {
	page, pageSize := s.page(r, s.cfg.PageSizeDefault)
	opts := s.searchAssetOptions(r, page, pageSize)
	assets, err := s.db.SearchAssets(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search_assets_failed", "搜索资源失败")
		return
	}
	writeJSON(w, http.StatusOK, PageDTO[AssetDTO]{
		Items: assetDTOs(assets.Items), Page: assets.Page, PageSize: assets.PageSize, HasMore: assets.HasMore,
	})
}

func (s *Server) folders(w http.ResponseWriter, r *http.Request) {
	parentID := int64(intQuery(r, "parentId", 0))
	folders, err := s.db.ListFolders(r.Context(), parentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "folders_failed", "读取文件夹失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": folderDTOs(folders)})
}

func (s *Server) folderTree(w http.ResponseWriter, r *http.Request) {
	roots, _, err := s.db.GetScanFolders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "folder_tree_failed", "读取文件夹树失败")
		return
	}
	if err := s.ensureFolderRoots(r.Context(), roots); err != nil {
		writeError(w, http.StatusInternalServerError, "folder_tree_failed", "读取文件夹树失败")
		return
	}
	folders, err := s.db.FolderTreeWithRoots(r.Context(), roots)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "folder_tree_failed", "读取文件夹树失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": folderDTOs(folders)})
}

func (s *Server) folder(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	folder, err := s.db.GetFolder(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "folder_not_found", "文件夹不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "folder_failed", "读取文件夹失败")
		return
	}
	writeJSON(w, http.StatusOK, folderDTO(folder))
}

func (s *Server) folderAssets(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	page, pageSize := s.page(r, s.cfg.PageSizeDefault)
	opts := db.AssetListOptions{
		Page: page, PageSize: pageSize, Sort: safeSort(r.URL.Query().Get("sort")),
		Query: strings.TrimSpace(r.URL.Query().Get("q")), Recursive: boolQuery(r, "recursive", false), VisibleOnly: visibleOnly(r),
	}
	assets, err := s.db.ListFolderAssets(r.Context(), id, opts)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "folder_not_found", "文件夹不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "folder_assets_failed", "读取文件夹资源失败")
		return
	}
	writeJSON(w, http.StatusOK, PageDTO[AssetDTO]{
		Items: assetDTOs(assets.Items), Page: assets.Page, PageSize: assets.PageSize, HasMore: assets.HasMore,
	})
}

func (s *Server) asset(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, assetDTO(asset))
}

func (s *Server) neighbors(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	contextName := r.URL.Query().Get("context")
	if contextName != "folder" && contextName != "album" && contextName != "search" {
		contextName = "library"
	}
	typeFilter := safeType(r.URL.Query().Get("type"))
	if typeFilter == "all" {
		typeFilter = ""
	}
	var folderID *int64
	if v := int64QueryPtr(r, "folderId"); v != nil {
		folderID = v
	}
	opts := db.NeighborOptions{
		Context: contextName, AssetID: id, Type: typeFilter, Sort: safeSort(r.URL.Query().Get("sort")),
		Query: strings.TrimSpace(r.URL.Query().Get("q")), FolderID: folderID,
		From: int64QueryPtr(r, "from"), To: int64QueryPtr(r, "to"), Limit: 5, Recursive: boolQuery(r, "recursive", false),
		NFOQuery: strings.TrimSpace(r.URL.Query().Get("nfo")),
		MinWidth: intQueryPtr(r, "widthMin"), MaxWidth: intQueryPtr(r, "widthMax"),
		MinHeight: intQueryPtr(r, "heightMin"), MaxHeight: intQueryPtr(r, "heightMax"),
		MinDuration: float64QueryPtr(r, "durationMin"), MaxDuration: float64QueryPtr(r, "durationMax"),
		MinSize: int64QueryPtr(r, "sizeMin"), MaxSize: int64QueryPtr(r, "sizeMax"),
		Orientation: searchOrientation(r),
	}
	var result db.Neighbors
	var err error
	if contextName == "album" {
		albumID := int64QueryPtr(r, "albumId")
		if albumID == nil {
			writeError(w, http.StatusBadRequest, "album_required", "相册 ID 缺失")
			return
		}
		result, err = s.db.AlbumNeighbors(r.Context(), *albumID, opts)
	} else {
		result, err = s.db.Neighbors(r.Context(), opts)
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "asset_not_found", "资源不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "neighbors_failed", "读取相邻资源失败")
		return
	}
	writeJSON(w, http.StatusOK, NeighborsDTO{
		Current: assetDTO(result.Current), Previous: assetDTOs(result.Previous), Next: assetDTOs(result.Next),
	})
}

func (s *Server) thumb(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	s.serveCacheAsset(w, r, asset, "thumbs", "webp", "image/webp", "thumb")
}

func (s *Server) cacheThumb(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !strings.HasSuffix(name, ".webp") {
		http.NotFound(w, r)
		return
	}
	cacheKey := strings.TrimSuffix(name, ".webp")
	if !validCacheKey(cacheKey) {
		http.NotFound(w, r)
		return
	}
	path, err := s.store.CacheFilePath("thumbs", cacheKey, "webp")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+cacheKey+`"`)
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	s.serveCache(w, r, "previews", "webp", "image/webp", "preview")
}

func (s *Server) videoPoster(w http.ResponseWriter, r *http.Request) {
	s.serveCache(w, r, "thumbs", "webp", "image/webp", "thumb")
}

func (s *Server) videoProxy(w http.ResponseWriter, r *http.Request) {
	s.serveCache(w, r, "video-proxies", "mp4", "video/mp4", "video_proxy")
}

func (s *Server) original(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	s.serveOriginalAsset(w, r, asset)
}

func (s *Server) video(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType != model.MediaTypeVideo {
		writeError(w, http.StatusBadRequest, "not_video", "资源不是视频")
		return
	}
	s.serveOriginalAsset(w, r, asset)
}

func (s *Server) serveCache(w http.ResponseWriter, r *http.Request, kind string, ext string, contentType string, taskType string) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	s.serveCacheAsset(w, r, asset, kind, ext, contentType, taskType)
}

func (s *Server) serveCacheAsset(w http.ResponseWriter, r *http.Request, asset model.Asset, kind string, ext string, contentType string, taskType string) {
	path, err := s.store.CachePath(kind, asset.CacheKey, ext)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cache_path_failed", "读取缓存失败")
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		_ = taskType
		writeError(w, http.StatusNotFound, "cache_not_ready", "缓存尚未生成")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cache_open_failed", "读取缓存失败")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+asset.CacheKey+`"`)
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
}

func (s *Server) serveOriginalAsset(w http.ResponseWriter, r *http.Request, asset model.Asset) {
	path, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资源不存在")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资源不存在")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "asset_not_found", "资源不存在")
		return
	}
	if asset.MimeType != nil && *asset.MimeType != "" {
		w.Header().Set("Content-Type", *asset.MimeType)
	} else if mt := mime.TypeByExtension("." + asset.Ext); mt != "" {
		w.Header().Set("Content-Type", mt)
	}
	w.Header().Set("ETag", fmt.Sprintf(`W/"asset-%d-%s"`, asset.ID, asset.CacheKey))
	w.Header().Set("Content-Disposition", contentDisposition(asset.Filename))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, asset.Filename, info.ModTime(), file)
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "not_found", "接口不存在")
		return
	}
	staticDir := findStaticDir(s.cfg.StaticDir)
	cleanPath := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
	if cleanPath == "." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) || cleanPath == ".." {
		http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
		return
	}
	target := filepath.Join(staticDir, cleanPath)
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		http.ServeFile(w, r, target)
		return
	}
	http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
}

func (s *Server) page(r *http.Request, fallback int) (int, int) {
	return ClampPage(intQuery(r, "page", 1), intQuery(r, "pageSize", fallback), fallback, s.cfg.PageSizeMax)
}

func boolQuery(r *http.Request, key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func visibleOnly(r *http.Request) bool {
	return strings.ToLower(strings.TrimSpace(r.URL.Query().Get("visible"))) != "all"
}

func (s *Server) searchAssetOptions(r *http.Request, page int, pageSize int) db.AssetListOptions {
	typeFilter := safeType(r.URL.Query().Get("type"))
	if typeFilter == "all" {
		typeFilter = ""
	}
	return db.AssetListOptions{
		Page: page, PageSize: pageSize, Type: typeFilter, Sort: safeSort(r.URL.Query().Get("sort")),
		Query: strings.TrimSpace(r.URL.Query().Get("q")), NFOQuery: strings.TrimSpace(r.URL.Query().Get("nfo")),
		From: int64QueryPtr(r, "from"), To: int64QueryPtr(r, "to"), VisibleOnly: visibleOnly(r),
		MinWidth: intQueryPtr(r, "widthMin"), MaxWidth: intQueryPtr(r, "widthMax"),
		MinHeight: intQueryPtr(r, "heightMin"), MaxHeight: intQueryPtr(r, "heightMax"),
		MinDuration: float64QueryPtr(r, "durationMin"), MaxDuration: float64QueryPtr(r, "durationMax"),
		MinSize: int64QueryPtr(r, "sizeMin"), MaxSize: int64QueryPtr(r, "sizeMax"),
		Orientation: searchOrientation(r),
	}
}

func searchOrientation(r *http.Request) string {
	orientation := safeOrientation(r.URL.Query().Get("orientation"))
	if orientation == "all" {
		return ""
	}
	return orientation
}

func validCacheKey(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

func (s *Server) ensureFolderRoots(ctx context.Context, roots []string) error {
	for _, root := range roots {
		if root == "" {
			if err := s.db.EnsureFolder(ctx, ""); err != nil {
				return err
			}
			continue
		}
		ancestors := storage.AncestorFolders(root + "/.scan-root")
		sort.Slice(ancestors, func(i, j int) bool { return storage.FolderDepth(ancestors[i]) < storage.FolderDepth(ancestors[j]) })
		for _, rel := range ancestors {
			if err := s.db.EnsureFolder(ctx, rel); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) idParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 0 {
		writeError(w, http.StatusBadRequest, "invalid_id", "ID 无效")
		return 0, false
	}
	return id, true
}

func (s *Server) assetByParam(w http.ResponseWriter, r *http.Request) (model.Asset, bool) {
	id, ok := s.idParam(w, r)
	if !ok {
		return model.Asset{}, false
	}
	asset, err := s.db.GetAsset(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "asset_not_found", "资源不存在")
		return model.Asset{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_failed", "读取资源失败")
		return model.Asset{}, false
	}
	return asset, true
}

func contentDisposition(filename string) string {
	safe := strings.ReplaceAll(filename, `"`, "")
	if safe == "" {
		safe = "asset"
	}
	return `inline; filename="` + safe + `"; filename*=UTF-8''` + urlPathEscape(filename)
}

func urlPathEscape(value string) string {
	clean := strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(value)
	return url.PathEscape(clean)
}

func findStaticDir(preferred string) string {
	candidates := []string{preferred, "frontend/dist", filepath.Join("..", "frontend", "dist"), filepath.Join("LPicto", "frontend", "dist"), "/app/frontend/dist"}
	for _, candidate := range candidates {
		if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return preferred
}

func Start(ctx context.Context, addr string, handler http.Handler, logger *slog.Logger) error {
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	errs := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", addr)
		errs <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
