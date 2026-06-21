package api

import (
	"lpicto/backend/internal/db"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/scanner"
)

type ErrorResponse struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type AssetDTO struct {
	ID                int64    `json:"id"`
	Filename          string   `json:"filename"`
	RelPath           string   `json:"relPath"`
	ParentRelPath     string   `json:"parentRelPath"`
	MediaType         string   `json:"mediaType"`
	MimeType          *string  `json:"mimeType"`
	Size              int64    `json:"size"`
	Mtime             int64    `json:"mtime"`
	Width             *int     `json:"width"`
	Height            *int     `json:"height"`
	Duration          *float64 `json:"duration"`
	TakenAt           *int64   `json:"takenAt"`
	TimelineAt        int64    `json:"timelineAt"`
	ImportedAt        int64    `json:"importedAt"`
	CacheKey          string   `json:"cacheKey"`
	BrowserPlayable   bool     `json:"browserPlayable"`
	ThumbStatus       string   `json:"thumbStatus"`
	PreviewStatus     string   `json:"previewStatus"`
	VideoPosterStatus string   `json:"videoPosterStatus"`
	VideoProxyStatus  string   `json:"videoProxyStatus"`
	Rotation          int      `json:"rotation"`
}

type FolderDTO struct {
	ID                  int64   `json:"id"`
	RelPath             string  `json:"relPath"`
	Name                string  `json:"name"`
	ParentRelPath       *string `json:"parentRelPath"`
	Depth               int     `json:"depth"`
	AssetCount          int     `json:"assetCount"`
	RecursiveAssetCount int     `json:"recursiveAssetCount"`
	CoverAssetID        *int64  `json:"coverAssetId"`
}

type ScanRunDTO struct {
	ID            int64   `json:"id"`
	Status        string  `json:"status"`
	StartedAt     int64   `json:"startedAt"`
	FinishedAt    *int64  `json:"finishedAt"`
	TotalSeen     int     `json:"totalSeen"`
	AssetsAdded   int     `json:"assetsAdded"`
	AssetsUpdated int     `json:"assetsUpdated"`
	AssetsDeleted int     `json:"assetsDeleted"`
	Errors        int     `json:"errors"`
	LastError     *string `json:"lastError"`
}

type ScanStatusDTO struct {
	Running   bool            `json:"running"`
	LastStart int64           `json:"lastStart"`
	LastRun   *ScanRunDTO     `json:"lastRun"`
	Progress  ScanProgressDTO `json:"progress"`
}

type ScanProgressDTO struct {
	State           string                         `json:"state"`
	RequestedAction string                         `json:"requestedAction"`
	Task            string                         `json:"task"`
	Reason          string                         `json:"reason"`
	Phase           string                         `json:"phase"`
	Roots           []string                       `json:"roots"`
	CurrentRoot     string                         `json:"currentRoot"`
	CurrentRelPath  string                         `json:"currentRelPath"`
	DiscoveredFiles int                            `json:"discoveredFiles"`
	TotalFiles      int                            `json:"totalFiles"`
	ScannedFiles    int                            `json:"scannedFiles"`
	TotalSeen       int                            `json:"totalSeen"`
	AssetsAdded     int                            `json:"assetsAdded"`
	AssetsUpdated   int                            `json:"assetsUpdated"`
	AssetsDeleted   int                            `json:"assetsDeleted"`
	Errors          int                            `json:"errors"`
	RootStats       map[string]ScanRootProgressDTO `json:"rootStats,omitempty"`
}

type ScanRootProgressDTO struct {
	DiscoveredFiles int  `json:"discoveredFiles"`
	TotalFiles      int  `json:"totalFiles"`
	ScannedFiles    int  `json:"scannedFiles"`
	TotalSeen       int  `json:"totalSeen"`
	Finished        bool `json:"finished"`
}

type WorkStatusCountsDTO struct {
	Total       int `json:"total"`
	Ready       int `json:"ready"`
	Pending     int `json:"pending"`
	Processing  int `json:"processing"`
	Error       int `json:"error"`
	NotRequired int `json:"notRequired"`
}

type QueueStatsDTO struct {
	ImageQueued       int `json:"imageQueued"`
	ImageCap          int `json:"imageCap"`
	ThumbQueued       int `json:"thumbQueued"`
	ThumbCap          int `json:"thumbCap"`
	PreviewQueued     int `json:"previewQueued"`
	PreviewCap        int `json:"previewCap"`
	VideoPosterQueued int `json:"videoPosterQueued"`
	VideoPosterCap    int `json:"videoPosterCap"`
	VideoProxyQueued  int `json:"videoProxyQueued"`
	VideoProxyCap     int `json:"videoProxyCap"`
	VideoQueued       int `json:"videoQueued"`
	VideoCap          int `json:"videoCap"`
	ActiveThumb       int `json:"activeThumb"`
	ActiveTranscode   int `json:"activeTranscode"`
}

type CacheStatsDTO struct {
	SizeBytes  int64 `json:"sizeBytes"`
	FileCount  int   `json:"fileCount"`
	UpdatedAt  int64 `json:"updatedAt"`
	Refreshing bool  `json:"refreshing"`
}

type ProcessingProgressDTO struct {
	AssetTotal  int                 `json:"assetTotal"`
	ImageTotal  int                 `json:"imageTotal"`
	VideoTotal  int                 `json:"videoTotal"`
	Thumb       WorkStatusCountsDTO `json:"thumb"`
	Transcode   WorkStatusCountsDTO `json:"transcode"`
	Preview     WorkStatusCountsDTO `json:"preview"`
	VideoPoster WorkStatusCountsDTO `json:"videoPoster"`
	VideoProxy  WorkStatusCountsDTO `json:"videoProxy"`
	Queue       QueueStatsDTO       `json:"queue"`
	Cache       CacheStatsDTO       `json:"cache"`
	Active      bool                `json:"active"`
	UpdatedAt   int64               `json:"updatedAt"`
	Refreshing  bool                `json:"refreshing"`
}

type CleanupStatusDTO struct {
	Running   bool   `json:"running"`
	Status    string `json:"status"`
	LastError string `json:"lastError"`
	UpdatedAt int64  `json:"updatedAt"`
}

type ScanFolderDTO struct {
	RelPath       string  `json:"relPath"`
	Name          string  `json:"name"`
	ParentRelPath *string `json:"parentRelPath"`
	Depth         int     `json:"depth"`
	Exists        bool    `json:"exists"`
}

type ScanLibraryDTO struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Folders  []ScanFolderDTO        `json:"folders"`
	Exists   bool                   `json:"exists"`
	Progress ScanLibraryProgressDTO `json:"progress"`
}

type ScanLibraryProgressDTO struct {
	AssetTotal      int                 `json:"assetTotal"`
	DiscoveredFiles int                 `json:"discoveredFiles"`
	DiscoveredAt    *int64              `json:"discoveredAt"`
	ScannedFiles    int                 `json:"scannedFiles"`
	UnscannedFiles  int                 `json:"unscannedFiles"`
	Thumb           WorkStatusCountsDTO `json:"thumb"`
	Transcode       WorkStatusCountsDTO `json:"transcode"`
	Active          bool                `json:"active"`
}

type SourceFolderDTO struct {
	RelPath       string  `json:"relPath"`
	Name          string  `json:"name"`
	ParentRelPath *string `json:"parentRelPath"`
	Depth         int     `json:"depth"`
	Selected      bool    `json:"selected"`
	Included      bool    `json:"included"`
}

type TimelineGroupDTO struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Start        int64  `json:"start"`
	End          int64  `json:"end"`
	Count        int    `json:"count"`
	CoverAssetID *int64 `json:"coverAssetId"`
}

type PageDTO[T any] struct {
	Items    []T  `json:"items"`
	Page     int  `json:"page"`
	PageSize int  `json:"pageSize"`
	HasMore  bool `json:"hasMore"`
}

type NeighborsDTO struct {
	Current  AssetDTO   `json:"current"`
	Previous []AssetDTO `json:"previous"`
	Next     []AssetDTO `json:"next"`
}

type LibraryAnchorDTO struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Kind     string  `json:"kind"`
	Page     int     `json:"page"`
	Position float64 `json:"position"`
	Value    int64   `json:"value"`
}

type AssetPreferenceDTO struct {
	AssetID   int64 `json:"assetId"`
	Rotation  int   `json:"rotation"`
	UpdatedAt int64 `json:"updatedAt"`
}

type AlbumDTO struct {
	ID                int64            `json:"id"`
	Name              string           `json:"name"`
	GroupID           *int64           `json:"groupId"`
	MediaTypeFilter   string           `json:"mediaTypeFilter"`
	OrientationFilter string           `json:"orientationFilter"`
	AssetCount        int              `json:"assetCount"`
	CoverAssetID      *int64           `json:"coverAssetId"`
	CreatedAt         int64            `json:"createdAt"`
	UpdatedAt         int64            `json:"updatedAt"`
	Sources           []AlbumSourceDTO `json:"sources"`
}

type AlbumGroupDTO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type AlbumSourceDTO struct {
	ID                int64  `json:"id"`
	SourceType        string `json:"sourceType"`
	RelPath           string `json:"relPath"`
	Recursive         bool   `json:"recursive"`
	MediaTypeFilter   string `json:"mediaTypeFilter"`
	OrientationFilter string `json:"orientationFilter"`
}

func assetDTO(asset model.Asset) AssetDTO {
	return AssetDTO{
		ID:                asset.ID,
		Filename:          asset.Filename,
		RelPath:           asset.RelPath,
		ParentRelPath:     asset.ParentRelPath,
		MediaType:         asset.MediaType,
		MimeType:          asset.MimeType,
		Size:              asset.Size,
		Mtime:             asset.Mtime,
		Width:             asset.Width,
		Height:            asset.Height,
		Duration:          asset.Duration,
		TakenAt:           asset.TakenAt,
		TimelineAt:        asset.TimelineAt,
		ImportedAt:        asset.ImportedAt,
		CacheKey:          asset.CacheKey,
		BrowserPlayable:   asset.BrowserPlayable,
		ThumbStatus:       asset.ThumbStatus,
		PreviewStatus:     asset.PreviewStatus,
		VideoPosterStatus: asset.VideoPosterStatus,
		VideoProxyStatus:  asset.VideoProxyStatus,
		Rotation:          asset.Rotation,
	}
}

func assetDTOs(assets []model.Asset) []AssetDTO {
	result := make([]AssetDTO, 0, len(assets))
	for _, asset := range assets {
		result = append(result, assetDTO(asset))
	}
	return result
}

func folderDTO(folder model.Folder) FolderDTO {
	return FolderDTO{
		ID:                  folder.ID,
		RelPath:             folder.RelPath,
		Name:                folder.Name,
		ParentRelPath:       folder.ParentRelPath,
		Depth:               folder.Depth,
		AssetCount:          folder.AssetCount,
		RecursiveAssetCount: folder.RecursiveAssetCount,
		CoverAssetID:        folder.CoverAssetID,
	}
}

func folderDTOs(folders []model.Folder) []FolderDTO {
	result := make([]FolderDTO, 0, len(folders))
	for _, folder := range folders {
		result = append(result, folderDTO(folder))
	}
	return result
}

func scanRunDTO(run model.ScanRun) ScanRunDTO {
	return ScanRunDTO{
		ID:            run.ID,
		Status:        run.Status,
		StartedAt:     run.StartedAt,
		FinishedAt:    run.FinishedAt,
		TotalSeen:     run.TotalSeen,
		AssetsAdded:   run.AssetsAdded,
		AssetsUpdated: run.AssetsUpdated,
		AssetsDeleted: run.AssetsDeleted,
		Errors:        run.Errors,
		LastError:     run.LastError,
	}
}

func scanRunDTOs(runs []model.ScanRun) []ScanRunDTO {
	result := make([]ScanRunDTO, 0, len(runs))
	for _, run := range runs {
		result = append(result, scanRunDTO(run))
	}
	return result
}

func scanProgressDTO(progress scanner.Progress) ScanProgressDTO {
	roots := progress.Roots
	if roots == nil {
		roots = []string{}
	}
	var rootStats map[string]ScanRootProgressDTO
	if len(progress.RootStats) > 0 {
		rootStats = make(map[string]ScanRootProgressDTO, len(progress.RootStats))
		for root, stat := range progress.RootStats {
			rootStats[root] = ScanRootProgressDTO{
				DiscoveredFiles: stat.DiscoveredFiles,
				TotalFiles:      stat.TotalFiles,
				ScannedFiles:    stat.ScannedFiles,
				TotalSeen:       stat.TotalSeen,
				Finished:        stat.Finished,
			}
		}
	}
	return ScanProgressDTO{
		State:           progress.State,
		RequestedAction: progress.RequestedAction,
		Task:            progress.Task,
		Reason:          progress.Reason,
		Phase:           progress.Phase,
		Roots:           roots,
		CurrentRoot:     progress.CurrentRoot,
		CurrentRelPath:  progress.CurrentRelPath,
		DiscoveredFiles: progress.DiscoveredFiles,
		TotalFiles:      progress.TotalFiles,
		ScannedFiles:    progress.ScannedFiles,
		TotalSeen:       progress.TotalSeen,
		AssetsAdded:     progress.AssetsAdded,
		AssetsUpdated:   progress.AssetsUpdated,
		AssetsDeleted:   progress.AssetsDeleted,
		Errors:          progress.Errors,
		RootStats:       rootStats,
	}
}

func workStatusCountsDTO(counts db.WorkStatusCounts) WorkStatusCountsDTO {
	return WorkStatusCountsDTO{
		Total:       counts.Total,
		Ready:       counts.Ready,
		Pending:     counts.Pending,
		Processing:  counts.Processing,
		Error:       counts.Error,
		NotRequired: counts.NotRequired,
	}
}

func processingProgressDTO(progress db.ProcessingProgress, queue jobs.QueueStats, cache CacheStatsDTO, updatedAt int64, refreshing bool) ProcessingProgressDTO {
	active := queue.ActiveThumb+queue.ActiveTranscode+queue.ThumbQueued+queue.PreviewQueued+queue.VideoProxyQueued > 0
	return ProcessingProgressDTO{
		AssetTotal:  progress.AssetTotal,
		ImageTotal:  progress.ImageTotal,
		VideoTotal:  progress.VideoTotal,
		Thumb:       workStatusCountsDTO(progress.Thumb),
		Transcode:   workStatusCountsDTO(progress.Transcode),
		Preview:     workStatusCountsDTO(progress.Preview),
		VideoPoster: workStatusCountsDTO(progress.VideoPoster),
		VideoProxy:  workStatusCountsDTO(progress.VideoProxy),
		Queue: QueueStatsDTO{
			ImageQueued:       queue.ImageQueued,
			ImageCap:          queue.ImageCap,
			ThumbQueued:       queue.ThumbQueued,
			ThumbCap:          queue.ThumbCap,
			PreviewQueued:     queue.PreviewQueued,
			PreviewCap:        queue.PreviewCap,
			VideoPosterQueued: queue.VideoPosterQueued,
			VideoPosterCap:    queue.VideoPosterCap,
			VideoProxyQueued:  queue.VideoProxyQueued,
			VideoProxyCap:     queue.VideoProxyCap,
			VideoQueued:       queue.VideoQueued,
			VideoCap:          queue.VideoCap,
			ActiveThumb:       queue.ActiveThumb,
			ActiveTranscode:   queue.ActiveTranscode,
		},
		Cache:      cache,
		Active:     active,
		UpdatedAt:  updatedAt,
		Refreshing: refreshing,
	}
}

func timelineGroupDTO(group model.TimelineGroup) TimelineGroupDTO {
	return TimelineGroupDTO{
		Key:          group.Key,
		Label:        group.Label,
		Start:        group.Start,
		End:          group.End,
		Count:        group.Count,
		CoverAssetID: group.CoverAssetID,
	}
}

func timelineGroupDTOs(groups []model.TimelineGroup) []TimelineGroupDTO {
	result := make([]TimelineGroupDTO, 0, len(groups))
	for _, group := range groups {
		result = append(result, timelineGroupDTO(group))
	}
	return result
}

func libraryAnchorDTOs(anchors []db.LibraryAnchor) []LibraryAnchorDTO {
	result := make([]LibraryAnchorDTO, 0, len(anchors))
	for _, anchor := range anchors {
		result = append(result, LibraryAnchorDTO{
			Key:      anchor.Key,
			Label:    anchor.Label,
			Kind:     anchor.Kind,
			Page:     anchor.Page,
			Position: anchor.Position,
			Value:    anchor.Value,
		})
	}
	return result
}

func assetPreferenceDTO(pref model.AssetPreference) AssetPreferenceDTO {
	return AssetPreferenceDTO{AssetID: pref.AssetID, Rotation: pref.Rotation, UpdatedAt: pref.UpdatedAt}
}

func albumDTO(album model.Album) AlbumDTO {
	return AlbumDTO{
		ID:                album.ID,
		Name:              album.Name,
		GroupID:           album.GroupID,
		MediaTypeFilter:   album.MediaTypeFilter,
		OrientationFilter: album.OrientationFilter,
		AssetCount:        album.AssetCount,
		CoverAssetID:      album.CoverAssetID,
		CreatedAt:         album.CreatedAt,
		UpdatedAt:         album.UpdatedAt,
		Sources:           albumSourceDTOs(album.Sources),
	}
}

func albumDTOs(albums []model.Album) []AlbumDTO {
	result := make([]AlbumDTO, 0, len(albums))
	for _, album := range albums {
		result = append(result, albumDTO(album))
	}
	return result
}

func albumGroupDTO(group model.AlbumGroup) AlbumGroupDTO {
	return AlbumGroupDTO{ID: group.ID, Name: group.Name, CreatedAt: group.CreatedAt, UpdatedAt: group.UpdatedAt}
}

func albumGroupDTOs(groups []model.AlbumGroup) []AlbumGroupDTO {
	result := make([]AlbumGroupDTO, 0, len(groups))
	for _, group := range groups {
		result = append(result, albumGroupDTO(group))
	}
	return result
}

func albumSourceDTOs(sources []model.AlbumSource) []AlbumSourceDTO {
	result := make([]AlbumSourceDTO, 0, len(sources))
	for _, source := range sources {
		result = append(result, AlbumSourceDTO{
			ID:                source.ID,
			SourceType:        source.SourceType,
			RelPath:           source.RelPath,
			Recursive:         source.Recursive,
			MediaTypeFilter:   source.MediaTypeFilter,
			OrientationFilter: source.OrientationFilter,
		})
	}
	return result
}
