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
	"github.com/go-chi/chi/v5/middleware"

	"lpicto/backend/internal/config"
	"lpicto/backend/internal/db"
	"lpicto/backend/internal/events"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/scanner"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

type Server struct {
	cfg          config.Config
	db           *db.DB
	store        storage.Store
	scanner      ScanController
	jobs         *jobs.Manager
	events       *events.Bus
	logger       *slog.Logger
	sourceHealth *storage.SourceHealth

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
	cleanupCancel context.CancelFunc

	sourceDirMu    sync.Mutex
	sourceDirCache map[string]sourceDirCacheEntry

	libraryCountsMu         sync.Mutex
	libraryCountsKey        string
	libraryCounts           map[string]scanLibraryProgressStats
	libraryCountsAt         time.Time
	libraryCountsRefreshing bool

	videoProxyMu               sync.Mutex
	videoProxyStates           map[string]*videoProxyRuntime
	videoSegmentStates         map[string]*videoSegmentRuntime
	videoSegmentIgnoreEditList map[string]bool
	videoSegmentSequence       uint64
	videoProxySlots            chan struct{}

	folderRefreshSem chan struct{}

	staticDir string
}

type ScanController interface {
	RequestScan(reason string) scanner.CommandResult
	RequestScanRoots(reason string, roots []string) scanner.CommandResult
	RequestRebuild(reason string) scanner.CommandResult
	RequestCountScan(reason string) scanner.CommandResult
	RequestCountScanRoots(reason string, roots []string) scanner.CommandResult
	RequestMetadataScan(reason string) scanner.CommandResult
	RequestMetadataScanRoots(reason string, roots []string) scanner.CommandResult
	RequestMetadataScanPaths(reason string, roots []string, paths []string) scanner.CommandResult
	RequestThumbnailContinue(reason string) scanner.CommandResult
	RequestThumbnailContinueRoots(reason string, roots []string) scanner.CommandResult
	RequestThumbnailRebuild(reason string) scanner.CommandResult
	RequestThumbnailRebuildRoots(reason string, roots []string) scanner.CommandResult
	RequestStop() scanner.CommandResult
	Status(context.Context) (scanner.Status, error)
}

func NewServer(cfg config.Config, database *db.DB, store storage.Store, scan ScanController, queue *jobs.Manager, bus *events.Bus, logger *slog.Logger) http.Handler {
	liveVideoProxyMaxActive := cfg.LiveVideoProxyMaxActive
	if liveVideoProxyMaxActive < 1 {
		liveVideoProxyMaxActive = 1
	}
	s := &Server{
		cfg:                        cfg,
		db:                         database,
		store:                      store,
		scanner:                    scan,
		jobs:                       queue,
		events:                     bus,
		logger:                     logger,
		sourceHealth:               storage.NewSourceHealth(store, 15*time.Second, cfg.RedisURL),
		videoProxyStates:           map[string]*videoProxyRuntime{},
		videoSegmentStates:         map[string]*videoSegmentRuntime{},
		videoSegmentIgnoreEditList: map[string]bool{},
		videoProxySlots:            make(chan struct{}, liveVideoProxyMaxActive),
		folderRefreshSem:           make(chan struct{}, 4),
		staticDir:                  findStaticDir(cfg.StaticDir),
	}
	if _, cached, err := database.GetSystemCollectionCounts(context.Background()); err != nil {
		if logger != nil {
			logger.Warn("read system collection count cache during startup failed", "error", err)
		}
	} else if !cached {
		if _, err := database.RefreshSystemCollectionCounts(context.Background()); err != nil && logger != nil {
			logger.Warn("initialize system collection count cache failed", "error", err)
		}
	}
	s.startVideoProxySweeper()
	s.startCacheCleanupScheduler()
	s.startStorageHealthScheduler()
	r := chi.NewRouter()
	r.Use(requestLogger(logger))
	r.Use(middleware.Compress(5))
	r.Use(foregroundActivity)
	r.Get("/api/health", s.health)
	r.Get("/api/storage/status", s.storageStatus)
	r.Get("/api/config/public", s.publicConfig)
	r.Get("/api/events", s.eventStream)
	r.Post("/api/scan", s.triggerScan)
	r.Post("/api/scan/count", s.countScan)
	r.Post("/api/scan/metadata", s.metadataScan)
	r.Post("/api/scan/pause", s.pauseScan)
	r.Post("/api/scan/rebuild", s.rebuildScan)
	r.Post("/api/scan/thumbnails/rebuild", s.thumbnailRebuildScan)
	r.Post("/api/scan/thumbnails/continue", s.thumbnailContinueScan)
	r.Get("/api/scan/status", s.scanStatus)
	r.Get("/api/scan/runs", s.scanRuns)
	r.Get("/api/settings/progress", s.settingsProgress)
	r.Get("/api/settings/activity", s.settingsActivity)
	r.Get("/api/settings/tasks", s.systemTasks)
	r.Post("/api/settings/tasks/{id}/run", s.runSystemTask)
	r.Post("/api/settings/tasks/{id}/stop", s.stopSystemTask)
	r.Get("/api/settings/video-proxy", s.videoProxySettings)
	r.Put("/api/settings/video-proxy", s.updateVideoProxySettings)
	r.Get("/api/settings/libraries", s.scanLibraries)
	r.Post("/api/settings/libraries", s.addScanLibrary)
	r.Put("/api/settings/libraries/{id}", s.updateScanLibrary)
	r.Delete("/api/settings/libraries/{id}", s.removeScanLibrary)
	r.Put("/api/settings/libraries/{id}/ai-focus", s.updateScanLibraryAIFocus)
	r.Post("/api/settings/libraries/{id}/ai/reindex", s.reindexScanLibraryAI)
	r.Post("/api/settings/libraries/{id}/scan", s.scanLibrary)
	r.Post("/api/settings/libraries/{id}/scan/count", s.countScanLibrary)
	r.Post("/api/settings/libraries/{id}/scan/metadata", s.metadataScanLibrary)
	r.Post("/api/settings/libraries/{id}/thumbnails/rebuild", s.thumbnailRebuildLibrary)
	r.Post("/api/settings/libraries/{id}/thumbnails/continue", s.thumbnailContinueLibrary)
	r.Get("/api/settings/scan-folders", s.scanFolders)
	r.Post("/api/settings/scan-folders", s.addScanFolder)
	r.Delete("/api/settings/scan-folders", s.removeScanFolder)
	r.Get("/api/source-folders", s.sourceFolders)
	r.Get("/api/tags", s.tags)
	r.Post("/api/tags", s.createTag)
	r.Put("/api/tags/{id}", s.updateTag)
	r.Delete("/api/tags/{id}", s.deleteTag)
	r.Post("/api/tags/merge", s.mergeTags)
	r.Get("/api/collections", s.collections)
	r.Post("/api/collections", s.createCollection)
	r.Put("/api/collections/{id}", s.updateCollection)
	r.Delete("/api/collections/{id}", s.deleteCollection)
	r.Get("/api/collections/{id}/assets", s.collectionAssets)
	r.Get("/api/collections/{id}/anchors", s.collectionAnchors)
	r.Get("/api/ai/status", s.aiStatus)
	r.Get("/api/ai/settings", s.aiSettings)
	r.Put("/api/ai/settings", s.updateAISettings)
	r.Post("/api/ai/run", s.runAIManually)
	r.Post("/api/ai/stop", s.stopAIManually)
	r.Post("/api/ai/reindex", s.reindexAI)
	r.Post("/api/ai/retry-failed", s.retryFailedAI)
	r.Get("/api/ai/tags", s.aiTags)
	r.Get("/api/duplicates", s.duplicateGroups)
	r.Get("/api/duplicates/selection", s.duplicateSelection)
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
	r.Post("/api/albums/{id}/assets", s.addAlbumAssets)
	r.Delete("/api/albums/{id}/assets", s.removeAlbumAssets)
	r.Get("/api/library/assets", s.libraryAssets)
	r.Get("/api/library/anchors", s.libraryAnchors)
	r.Get("/api/library/nfo-options", s.libraryNFOOptions)
	r.Get("/api/folders", s.folders)
	r.Get("/api/folders/tree", s.folderTree)
	r.Get("/api/folders/by-path", s.folderByPath)
	r.Get("/api/folders/{id}", s.folder)
	r.Get("/api/folders/{id}/assets", s.folderAssets)
	r.Get("/api/folders/{id}/anchors", s.folderAnchors)
	r.Get("/api/assets/{id}", s.asset)
	r.Get("/api/assets/{id}/ai", s.assetAI)
	r.Post("/api/assets/{id}/ai/reanalyze", s.reanalyzeAssetAI)
	r.Post("/api/assets/batch/tags", s.batchAddTag)
	r.Post("/api/assets/batch/rating", s.batchSetRating)
	r.Post("/api/assets/batch/rotation", s.batchSetRotation)
	r.Post("/api/assets/batch/album", s.batchAddToAlbum)
	r.Post("/api/assets/batch/hide", s.batchHide)
	r.Post("/api/assets/batch/unhide", s.batchUnhide)
	r.Post("/api/assets/batch/delete", s.batchDelete)
	r.Get("/api/assets/{id}/delete-plan", s.assetDeletePlan)
	r.Post("/api/assets/{id}/delete", s.deleteAsset)
	r.Get("/api/assets/{id}/tags", s.assetTags)
	r.Post("/api/assets/{id}/tags", s.addAssetTag)
	r.Delete("/api/assets/{id}/tags", s.removeAssetTag)
	r.Get("/api/assets/{id}/preferences", s.assetPreferences)
	r.Put("/api/assets/{id}/preferences", s.updateAssetPreferences)
	r.Get("/api/assets/{id}/sidecars", s.assetSidecars)
	r.Get("/api/assets/{id}/subtitles/{subtitleID}", s.assetSubtitle)
	r.Get("/api/assets/{id}/neighbors", s.neighbors)
	r.Get("/api/assets/{id}/position", s.assetPosition)
	r.Get("/api/assets/{id}/thumb", s.thumb)
	r.Get("/api/assets/{id}/preview", s.preview)
	r.Get("/api/assets/{id}/original", s.original)
	r.Head("/api/assets/{id}/original", s.original)
	r.Get("/api/assets/{id}/video", s.video)
	r.Head("/api/assets/{id}/video", s.video)
	r.Get("/api/assets/{id}/video-poster", s.videoPoster)
	r.Get("/api/assets/{id}/hls/playlist.m3u8", s.videoHLSPlaylist)
	r.Get("/api/assets/{id}/hls/status", s.videoHLSStatus)
	r.Post("/api/assets/{id}/hls/prewarm", s.videoHLSPrewarm)
	r.Post("/api/assets/{id}/hls/session/stop", s.videoHLSSessionStop)
	r.Get("/api/assets/{id}/hls/segments/{segment}", s.videoHLSSegment)
	r.Get("/api/assets/{id}/video-proxy/status", s.videoProxyStatus)
	r.Post("/api/assets/{id}/video-proxy/keepalive", s.videoProxyKeepalive)
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
	if isStreamingAssetRequest(path) {
		return false
	}
	return strings.HasPrefix(path, "/api/library/") ||
		strings.HasPrefix(path, "/api/albums") ||
		strings.HasPrefix(path, "/api/collections") ||
		strings.HasPrefix(path, "/api/tags") ||
		strings.HasPrefix(path, "/api/duplicates") ||
		strings.HasPrefix(path, "/api/folders") ||
		strings.HasPrefix(path, "/api/assets/") ||
		strings.HasPrefix(path, "/api/ai/") ||
		strings.HasPrefix(path, "/api/cache/")
}

func isStreamingAssetRequest(path string) bool {
	return strings.HasSuffix(path, "/video") || strings.HasSuffix(path, "/video-proxy") || strings.Contains(path, "/hls/")
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) storageStatus(w http.ResponseWriter, r *http.Request) {
	statuses := s.sourceHealth.Statuses()
	for _, status := range statuses {
		if relPath, err := s.db.SourceHealthSample(r.Context(), status.RootID); err == nil {
			s.sourceHealth.ProbeAsset(relPath)
		}
	}
	statuses = s.sourceHealth.Statuses()
	available := true
	for _, status := range statuses {
		if !status.Available {
			available = false
			break
		}
	}
	message := ""
	if !available {
		message = "存储不可达，扫描、缩略图和 AI 分析已暂停；已有媒体和文件夹缓存仍可浏览。"
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": available, "message": message, "roots": statuses})
}

func scanCommandResponse(result scanner.CommandResult) map[string]any {
	return map[string]any{
		"accepted": result.Accepted,
		"started":  result.Started,
		"paused":   result.Paused,
		"state":    result.State,
	}
}

func (s *Server) publicConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"pageSizeDefault":         s.cfg.PageSizeDefault,
		"pageSizeMax":             s.cfg.PageSizeMax,
		"thumbLongEdge":           s.cfg.ThumbLongEdge,
		"previewLongEdge":         s.cfg.PreviewLongEdge,
		"videoProxyEnabled":       s.cfg.VideoProxyEnabled,
		"liveVideoProxyMaxActive": s.cfg.LiveVideoProxyMaxActive,
		"videoProxyMaxHeight":     s.cfg.VideoProxyMaxHeight,
		"videoSegmentSeconds":     s.cfg.VideoSegmentSeconds,
		"videoPreloadSegments":    s.cfg.VideoPreloadSegments,
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
		var data []byte
		var err error
		switch event.Type {
		case "asset_ready":
			asset, ok := event.Payload.(model.Asset)
			if !ok {
				continue
			}
			data, err = json.Marshal(assetDTO(asset))
		case "scan_status":
			status, ok := event.Payload.(scanner.Status)
			if !ok {
				continue
			}
			var lastRun *ScanRunDTO
			if status.LastRun != nil {
				dto := scanRunDTO(*status.LastRun)
				lastRun = &dto
			}
			data, err = json.Marshal(ScanStatusDTO{
				Running: status.Running, LastStart: status.LastStart, LastRun: lastRun, Progress: scanProgressDTO(status.Progress),
			})
		case "asset_deleted":
			asset, ok := event.Payload.(model.Asset)
			if !ok {
				continue
			}
			data, err = json.Marshal(map[string]any{
				"id":       asset.ID,
				"relPath":  asset.RelPath,
				"cacheKey": asset.CacheKey,
			})
		default:
			continue
		}
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "event: %s\n", event.Type)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

func (s *Server) triggerScan(w http.ResponseWriter, r *http.Request) {
	result := s.scanner.RequestMetadataScan("manual")
	writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
}

func (s *Server) countScan(w http.ResponseWriter, r *http.Request) {
	result := s.scanner.RequestCountScan("count")
	writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
}

func (s *Server) metadataScan(w http.ResponseWriter, r *http.Request) {
	result := s.scanner.RequestMetadataScan("metadata")
	writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
}

func (s *Server) rebuildScan(w http.ResponseWriter, r *http.Request) {
	s.thumbnailRebuildScan(w, r)
}

func (s *Server) thumbnailRebuildScan(w http.ResponseWriter, r *http.Request) {
	if !boolQuery(r, "force", false) {
		writeError(w, http.StatusBadRequest, "confirm_required", "强制重建需要确认")
		return
	}
	result := s.scanner.RequestThumbnailRebuild("thumb_rebuild")
	writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
}

func (s *Server) thumbnailContinueScan(w http.ResponseWriter, r *http.Request) {
	result := s.scanner.RequestThumbnailContinue("thumb_continue")
	writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
}

func (s *Server) pauseScan(w http.ResponseWriter, r *http.Request) {
	result := s.scanner.RequestStop()
	writeJSON(w, http.StatusOK, scanCommandResponse(result))
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

func (s *Server) libraryAssets(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	page, pageSize := s.page(r, s.cfg.PageSizeDefault)
	opts := s.libraryAssetOptions(r, page, pageSize)
	if albumID := int64QueryPtr(r, "albumId"); albumID != nil {
		assets, err := s.db.ListAlbumAssets(r.Context(), *albumID, opts)
		s.recordFilterTiming(w, r, started)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "album_not_found", "相册不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "library_assets_failed", "读取图库失败")
			return
		}
		items, err := s.listAssetDTOs(r, assets.Items)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "library_assets_failed", "读取 AI 摘要失败")
			return
		}
		writeJSON(w, http.StatusOK, PageDTO[AssetDTO]{Items: items, Page: assets.Page, PageSize: assets.PageSize, HasMore: assets.HasMore})
		return
	}
	assets, err := s.db.SearchAssets(r.Context(), opts)
	s.recordFilterTiming(w, r, started)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "library_assets_failed", "读取图库失败")
		return
	}
	items, err := s.listAssetDTOs(r, assets.Items)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "library_assets_failed", "读取 AI 摘要失败")
		return
	}
	writeJSON(w, http.StatusOK, PageDTO[AssetDTO]{Items: items, Page: assets.Page, PageSize: assets.PageSize, HasMore: assets.HasMore})
}

func (s *Server) libraryAnchors(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	_, pageSize := s.page(r, s.cfg.PageSizeDefault)
	opts := s.libraryAssetOptions(r, 1, pageSize)
	var anchorResult db.LibraryAnchorResult
	var err error
	if albumID := int64QueryPtr(r, "albumId"); albumID != nil {
		anchorResult, err = s.db.AlbumAnchors(r.Context(), *albumID, opts)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "album_not_found", "相册不存在")
			return
		}
	} else {
		anchorResult, err = s.db.SearchAnchors(r.Context(), opts)
	}
	s.recordFilterTiming(w, r, started)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "library_anchors_failed", "读取图库索引失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": libraryAnchorDTOs(anchorResult.Items), "total": anchorResult.Total})
}

func (s *Server) libraryNFOOptions(w http.ResponseWriter, r *http.Request) {
	field := safeNFOOptionField(r.URL.Query().Get("field"))
	if field == "" {
		writeError(w, http.StatusBadRequest, "nfo_field_invalid", "NFO 字段不支持")
		return
	}
	limit := intQuery(r, "limit", 40)
	if limit < 1 {
		limit = 40
	}
	if limit > 100 {
		limit = 100
	}
	items, err := s.db.NFOOptions(r.Context(), db.NFOOptionOptions{
		Field:       field,
		Query:       strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:       limit,
		VisibleOnly: visibleOnly(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "nfo_options_failed", "读取 NFO 选项失败")
		return
	}
	if items == nil {
		items = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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

func (s *Server) folderByPath(w http.ResponseWriter, r *http.Request) {
	rel, err := storage.NormalizeRelPath(r.URL.Query().Get("relPath"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "文件夹路径无效")
		return
	}
	folder, err := s.db.GetFolderByRel(r.Context(), rel)
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
	started := time.Now()
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	page, pageSize := s.page(r, s.cfg.PageSizeDefault)
	opts := db.AssetListOptions{
		Page: page, PageSize: pageSize, Sort: safeSort(r.URL.Query().Get("sort")), Group: safeGroup(r.URL.Query().Get("group")),
		Query: strings.TrimSpace(r.URL.Query().Get("q")), Recursive: boolQuery(r, "recursive", false), VisibleOnly: visibleOnly(r), Rating: ratingQueryPtr(r, "rating"),
		Orientation: searchOrientation(r), CombinedTags: combinedTagsQuery(r),
	}
	assets, err := s.db.ListFolderAssets(r.Context(), id, opts)
	s.recordFilterTiming(w, r, started)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "folder_not_found", "文件夹不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "folder_assets_failed", "读取文件夹资源失败")
		return
	}
	items, err := s.listAssetDTOs(r, assets.Items)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "folder_assets_failed", "读取 AI 摘要失败")
		return
	}
	writeJSON(w, http.StatusOK, PageDTO[AssetDTO]{Items: items, Page: assets.Page, PageSize: assets.PageSize, HasMore: assets.HasMore})
}

func (s *Server) listAssetDTOs(r *http.Request, assets []model.Asset) ([]AssetDTO, error) {
	if !boolQuery(r, "includeAiSummary", false) {
		return assetDTOs(assets), nil
	}
	ids := make([]int64, 0, len(assets))
	for _, asset := range assets {
		ids = append(ids, asset.ID)
	}
	summaries, err := s.db.AssetAISummaries(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	manualTags, err := s.db.AssetTagsByAssetIDs(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	return assetDTOsWithListSummaries(assets, summaries, manualTags), nil
}

func (s *Server) folderAnchors(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	_, pageSize := s.page(r, s.cfg.PageSizeDefault)
	opts := db.AssetListOptions{
		PageSize: pageSize, Sort: safeSort(r.URL.Query().Get("sort")), Group: safeGroup(r.URL.Query().Get("group")),
		Query: strings.TrimSpace(r.URL.Query().Get("q")), Recursive: boolQuery(r, "recursive", false), VisibleOnly: visibleOnly(r), Rating: ratingQueryPtr(r, "rating"),
		Orientation: searchOrientation(r), CombinedTags: combinedTagsQuery(r),
	}
	anchorResult, err := s.db.FolderAnchors(r.Context(), id, opts)
	s.recordFilterTiming(w, r, started)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "folder_not_found", "文件夹不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "folder_anchors_failed", "读取文件夹索引失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": libraryAnchorDTOs(anchorResult.Items), "total": anchorResult.Total})
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
	if contextName != "folder" && contextName != "album" && contextName != "collection" {
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
	albumID := int64QueryPtr(r, "albumId")
	opts := db.NeighborOptions{
		Context: contextName, AssetID: id, Type: typeFilter, Sort: safeSort(r.URL.Query().Get("sort")), Group: safeGroup(r.URL.Query().Get("group")),
		Query: strings.TrimSpace(r.URL.Query().Get("q")), FolderID: folderID,
		CombinedQuery: strings.TrimSpace(r.URL.Query().Get("combinedQuery")),
		From:          int64QueryPtr(r, "from"), To: int64QueryPtr(r, "to"), Limit: 5, Recursive: boolQuery(r, "recursive", false),
		NFOQuery:      strings.TrimSpace(r.URL.Query().Get("nfo")),
		NFOActor:      strings.TrimSpace(r.URL.Query().Get("nfoActor")),
		NFOID:         strings.TrimSpace(r.URL.Query().Get("nfoId")),
		NFOTag:        strings.TrimSpace(r.URL.Query().Get("nfoTag")),
		ManualTag:     strings.TrimSpace(r.URL.Query().Get("manualTag")),
		CombinedTag:   strings.TrimSpace(r.URL.Query().Get("combinedTag")),
		CombinedTags:  combinedTagsQuery(r),
		AIDescription: strings.TrimSpace(r.URL.Query().Get("aiDescription")),
		AITag:         strings.TrimSpace(r.URL.Query().Get("aiTag")),
		NFOTitle:      strings.TrimSpace(r.URL.Query().Get("nfoTitle")),
		NFOYear:       strings.TrimSpace(r.URL.Query().Get("nfoYear")),
		MinWidth:      intQueryPtr(r, "widthMin"), MaxWidth: intQueryPtr(r, "widthMax"),
		MinHeight: intQueryPtr(r, "heightMin"), MaxHeight: intQueryPtr(r, "heightMax"),
		MatchAnyAxis: dimensionMode(r) == "both",
		MinDuration:  float64QueryPtr(r, "durationMin"), MaxDuration: float64QueryPtr(r, "durationMax"),
		MinSize: int64QueryPtr(r, "sizeMin"), MaxSize: int64QueryPtr(r, "sizeMax"),
		Orientation:     searchOrientation(r),
		Rating:          ratingQueryPtr(r, "rating"),
		AlbumUnassigned: albumUnassignedQuery(r),
		AlbumIDs:        int64ListQuery(r, "albumIds"),
		VisibleOnly:     visibleOnly(r),
	}
	var result db.Neighbors
	var err error
	if contextName == "collection" {
		collectionID := strings.TrimSpace(r.URL.Query().Get("collectionId"))
		if collectionID == "" {
			writeError(w, http.StatusBadRequest, "collection_required", "集合 ID 缺失")
			return
		}
		result, err = s.collectionNeighborsForAsset(r.Context(), collectionID, id, s.collectionAssetOptions(r, 1, s.cfg.PageSizeDefault), opts.Limit)
	} else if contextName == "album" || albumID != nil {
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

func (s *Server) assetPosition(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	contextName := r.URL.Query().Get("context")
	if contextName != "folder" && contextName != "album" && contextName != "collection" {
		contextName = "library"
	}
	_, pageSize := s.page(r, s.cfg.PageSizeDefault)
	typeFilter := safeType(r.URL.Query().Get("type"))
	if typeFilter == "all" {
		typeFilter = ""
	}
	albumID := int64QueryPtr(r, "albumId")
	var result db.AssetPosition
	var err error
	switch contextName {
	case "folder":
		folderID := int64QueryPtr(r, "folderId")
		if folderID == nil {
			writeError(w, http.StatusBadRequest, "folder_required", "文件夹 ID 缺失")
			return
		}
		result, err = s.db.FolderAssetPosition(r.Context(), *folderID, id, db.AssetListOptions{
			PageSize: pageSize, Sort: safeSort(r.URL.Query().Get("sort")), Group: safeGroup(r.URL.Query().Get("group")),
			Query: strings.TrimSpace(r.URL.Query().Get("q")), Recursive: boolQuery(r, "recursive", false), VisibleOnly: visibleOnly(r), Rating: ratingQueryPtr(r, "rating"),
			ManualTag: strings.TrimSpace(r.URL.Query().Get("manualTag")), Orientation: searchOrientation(r),
		})
	case "album":
		if albumID == nil {
			writeError(w, http.StatusBadRequest, "album_required", "相册 ID 缺失")
			return
		}
		result, err = s.db.AlbumAssetPosition(r.Context(), *albumID, id, db.AssetListOptions{
			PageSize: pageSize, Type: typeFilter, Sort: safeSort(r.URL.Query().Get("sort")), Group: safeGroup(r.URL.Query().Get("group")),
			Query: strings.TrimSpace(r.URL.Query().Get("q")), VisibleOnly: visibleOnly(r), Rating: ratingQueryPtr(r, "rating"),
			ManualTag: strings.TrimSpace(r.URL.Query().Get("manualTag")), Orientation: searchOrientation(r),
		})
	case "collection":
		collectionID := strings.TrimSpace(r.URL.Query().Get("collectionId"))
		if collectionID == "" {
			writeError(w, http.StatusBadRequest, "collection_required", "集合 ID 缺失")
			return
		}
		result, err = s.collectionPositionForAsset(r.Context(), collectionID, id, s.collectionAssetOptions(r, 1, pageSize))
	default:
		opts := s.libraryAssetOptions(r, 1, pageSize)
		if albumID != nil {
			result, err = s.db.AlbumAssetPosition(r.Context(), *albumID, id, opts)
		} else {
			result, err = s.db.AssetPosition(r.Context(), id, opts, true)
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "asset_position_not_found", "资源不在当前列表")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_position_failed", "读取资源位置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"index": result.Index, "page": result.Page, "position": result.Position, "total": result.Total,
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
	file, err := os.Open(path)
	if err != nil {
		if s.recoverMissingThumbCache(r.Context(), cacheKey) {
			serveMissingThumbPlaceholder(w, cacheKey)
			return
		}
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+cacheKey+`"`)
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func (s *Server) recoverMissingThumbCache(ctx context.Context, cacheKey string) bool {
	asset, ok := s.assetByCacheKey(ctx, cacheKey)
	if !ok {
		return false
	}
	if available, _ := s.sourceHealth.AvailableForRel(asset.RelPath); !available {
		return false
	}
	if err := s.db.SetAssetWorkStatus(ctx, asset.ID, "thumb_status", model.StatusPending, nil); err != nil {
		if s.logger != nil {
			s.logger.Warn("reset missing thumb cache failed", "assetID", asset.ID, "cacheKey", cacheKey, "error", err)
		}
	} else if s.logger != nil {
		s.logger.Info("missing thumb cache reset", "assetID", asset.ID, "cacheKey", cacheKey)
	}
	if s.jobs != nil {
		s.jobs.Enqueue(jobs.Task{Type: "thumb", AssetID: asset.ID})
	}
	return true
}

func serveMissingThumbPlaceholder(w http.ResponseWriter, cacheKey string) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", `W/"missing-thumb-`+cacheKey+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`))
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	s.serveCache(w, r, "previews", "webp", "image/webp", "preview")
}

func (s *Server) videoPoster(w http.ResponseWriter, r *http.Request) {
	s.serveCache(w, r, "thumbs", "webp", "image/webp", "thumb")
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
	s.serveVideoAsset(w, r, asset)
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
	file, err := os.Open(path)
	if err != nil {
		_ = taskType
		writeError(w, http.StatusNotFound, "cache_not_ready", "缓存尚未生成")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "cache_not_ready", "缓存尚未生成")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+asset.CacheKey+`"`)
	if kind == "video-proxies" {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("X-Accel-Buffering", "no")
	}
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
}

func (s *Server) serveOriginalAsset(w http.ResponseWriter, r *http.Request, asset model.Asset) {
	s.serveOriginalAssetFile(w, r, asset, false)
}

func (s *Server) serveVideoAsset(w http.ResponseWriter, r *http.Request, asset model.Asset) {
	s.serveOriginalAssetFile(w, r, asset, false)
}

func (s *Server) serveOriginalAssetFile(w http.ResponseWriter, r *http.Request, asset model.Asset, _ bool) {
	path, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资源不存在")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusServiceUnavailable, "source_unavailable", "源文件暂时不可用")
			return
		}
		writeError(w, http.StatusNotFound, "asset_not_found", "资源不存在")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusServiceUnavailable, "source_unavailable", "源文件暂时不可用")
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
	if asset.MediaType == model.MediaTypeVideo {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("X-Accel-Buffering", "no")
	}
	http.ServeContent(w, r, asset.Filename, info.ModTime(), file)
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "not_found", "接口不存在")
		return
	}
	staticDir := s.staticDir
	cleanPath := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
	if cleanPath == "." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) || cleanPath == ".." {
		serveStaticFile(w, r, filepath.Join(staticDir, "index.html"), false)
		return
	}
	target := filepath.Join(staticDir, cleanPath)
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		serveStaticFile(w, r, target, strings.HasPrefix(filepath.ToSlash(cleanPath), "assets/"))
		return
	}
	serveStaticFile(w, r, filepath.Join(staticDir, "index.html"), false)
}

func serveStaticFile(w http.ResponseWriter, r *http.Request, path string, immutable bool) {
	if immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFile(w, r, path)
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
	return strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("visible")), "ready")
}

func (s *Server) libraryAssetOptions(r *http.Request, page int, pageSize int) db.AssetListOptions {
	typeFilter := safeType(r.URL.Query().Get("type"))
	if typeFilter == "all" {
		typeFilter = ""
	}
	return db.AssetListOptions{
		Page: page, PageSize: pageSize, Type: typeFilter, Sort: safeSort(r.URL.Query().Get("sort")), Group: safeGroup(r.URL.Query().Get("group")),
		Query:         strings.TrimSpace(r.URL.Query().Get("q")),
		CombinedQuery: strings.TrimSpace(r.URL.Query().Get("combinedQuery")),
		NFOQuery:      strings.TrimSpace(r.URL.Query().Get("nfo")),
		NFOActor:      strings.TrimSpace(r.URL.Query().Get("nfoActor")),
		NFOID:         strings.TrimSpace(r.URL.Query().Get("nfoId")),
		NFOTag:        strings.TrimSpace(r.URL.Query().Get("nfoTag")),
		ManualTag:     strings.TrimSpace(r.URL.Query().Get("manualTag")),
		CombinedTag:   strings.TrimSpace(r.URL.Query().Get("combinedTag")),
		CombinedTags:  combinedTagsQuery(r),
		AIDescription: strings.TrimSpace(r.URL.Query().Get("aiDescription")),
		AITag:         strings.TrimSpace(r.URL.Query().Get("aiTag")),
		NFOTitle:      strings.TrimSpace(r.URL.Query().Get("nfoTitle")),
		NFOYear:       strings.TrimSpace(r.URL.Query().Get("nfoYear")),
		From:          int64QueryPtr(r, "from"), To: int64QueryPtr(r, "to"), VisibleOnly: visibleOnly(r),
		MinWidth: intQueryPtr(r, "widthMin"), MaxWidth: intQueryPtr(r, "widthMax"),
		MinHeight: intQueryPtr(r, "heightMin"), MaxHeight: intQueryPtr(r, "heightMax"),
		MatchAnyAxis: dimensionMode(r) == "both",
		MinDuration:  float64QueryPtr(r, "durationMin"), MaxDuration: float64QueryPtr(r, "durationMax"),
		MinSize: int64QueryPtr(r, "sizeMin"), MaxSize: int64QueryPtr(r, "sizeMax"),
		Orientation:     searchOrientation(r),
		Rating:          ratingQueryPtr(r, "rating"),
		AlbumUnassigned: albumUnassignedQuery(r),
		AlbumIDs:        int64ListQuery(r, "albumIds"),
	}
}

func searchOrientation(r *http.Request) string {
	orientation := safeOrientation(r.URL.Query().Get("orientation"))
	if orientation == "all" {
		return ""
	}
	return orientation
}

func dimensionMode(r *http.Request) string {
	if searchOrientation(r) != "" {
		return "axis"
	}
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("dimensionMode")), "both") {
		return "both"
	}
	return "axis"
}

func safeNFOOptionField(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "actor":
		return "actor"
	case "id":
		return "id"
	case "tag":
		return "tag"
	case "title":
		return "title"
	case "year":
		return "year"
	default:
		return ""
	}
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

func (s *Server) assetByCacheKey(ctx context.Context, cacheKey string) (model.Asset, bool) {
	asset, err := s.db.GetAssetByCacheKey(ctx, cacheKey)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Asset{}, false
	}
	if err != nil {
		s.logger.Warn("read asset by cache key failed", "cacheKey", cacheKey, "error", err)
		return model.Asset{}, false
	}
	return asset, true
}

func (s *Server) deleteAssetIfSourceMissing(ctx context.Context, asset model.Asset, reason string) bool {
	missing, err := s.assetSourceMissing(asset)
	if err != nil {
		s.logger.Warn("check asset source failed", "assetID", asset.ID, "relPath", asset.RelPath, "reason", reason, "error", err)
		return false
	}
	if !missing {
		return false
	}
	return s.markDeletedAsset(ctx, asset, reason)
}

func (s *Server) assetSourceMissing(asset model.Asset) (bool, error) {
	root, _, err := s.store.RootForRel(asset.RelPath)
	if err != nil {
		return false, err
	}
	if !sourceRootAvailable(root.Path) {
		return false, nil
	}
	path, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func sourceRootAvailable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (s *Server) markDeletedAsset(ctx context.Context, asset model.Asset, reason string) bool {
	deleted, err := s.db.MarkDeletedWithCache(ctx, asset.RelPath, util.UnixNow())
	if err != nil {
		s.logger.Warn("mark missing source asset deleted failed", "assetID", asset.ID, "relPath", asset.RelPath, "reason", reason, "error", err)
		return false
	}
	if deleted == nil {
		return true
	}
	s.removeDeletedAssetCaches([]db.DeletedAsset{*deleted})
	s.invalidateProcessingProgress()
	if s.events != nil {
		s.events.Publish(events.Event{Type: "asset_deleted", Payload: asset})
	}
	go func() {
		s.folderRefreshSem <- struct{}{}
		defer func() { <-s.folderRefreshSem }()
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("folder refresh goroutine panicked", "panic", r, "assetID", asset.ID)
			}
		}()
		if err := s.db.RefreshFolders(context.Background()); err != nil {
			s.logger.Warn("refresh folders after asset deletion failed", "assetID", asset.ID, "relPath", asset.RelPath, "reason", reason, "error", err)
		}
	}()
	return true
}

func (s *Server) invalidateProcessingProgress() {
	s.progressMu.Lock()
	s.progressStatsAt = time.Time{}
	s.progressMu.Unlock()
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
	candidates := []string{preferred, "frontend/dist", filepath.Join("LPicto", "frontend", "dist"), "/app/frontend/dist"}
	for _, candidate := range candidates {
		if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return preferred
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/events") || strings.HasPrefix(r.URL.Path, "/api/assets/") && (strings.Contains(r.URL.Path, "/thumb") || strings.Contains(r.URL.Path, "/preview") || strings.Contains(r.URL.Path, "/original") || strings.Contains(r.URL.Path, "/video")) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Info("http", "method", r.Method, "path", r.URL.Path, "elapsed", time.Since(start).String())
		})
	}
}

func (s *Server) recordFilterTiming(w http.ResponseWriter, r *http.Request, started time.Time) {
	elapsed := time.Since(started)
	w.Header().Set("Server-Timing", fmt.Sprintf("db;dur=%.3f", float64(elapsed.Microseconds())/1000))
	if elapsed < 500*time.Millisecond {
		return
	}
	keys := make([]string, 0, len(r.URL.Query()))
	for key := range r.URL.Query() {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	s.logger.Warn("slow media filter", "path", r.URL.Path, "elapsed", elapsed.String(), "filter_keys", strings.Join(keys, ","))
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
