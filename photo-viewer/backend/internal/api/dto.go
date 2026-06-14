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
	Reason         string `json:"reason"`
	CurrentRoot    string `json:"currentRoot"`
	CurrentRelPath string `json:"currentRelPath"`
	TotalSeen      int    `json:"totalSeen"`
	AssetsAdded    int    `json:"assetsAdded"`
	AssetsUpdated  int    `json:"assetsUpdated"`
	AssetsDeleted  int    `json:"assetsDeleted"`
	Errors         int    `json:"errors"`
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
	ThumbQueued int `json:"thumbQueued"`
	ThumbCap    int `json:"thumbCap"`
	VideoQueued int `json:"videoQueued"`
	VideoCap    int `json:"videoCap"`
}

type ProcessingProgressDTO struct {
	AssetTotal  int                 `json:"assetTotal"`
	ImageTotal  int                 `json:"imageTotal"`
	VideoTotal  int                 `json:"videoTotal"`
	Thumb       WorkStatusCountsDTO `json:"thumb"`
	Preview     WorkStatusCountsDTO `json:"preview"`
	VideoPoster WorkStatusCountsDTO `json:"videoPoster"`
	VideoProxy  WorkStatusCountsDTO `json:"videoProxy"`
	Queue       QueueStatsDTO       `json:"queue"`
}

type ScanFolderDTO struct {
	RelPath       string  `json:"relPath"`
	Name          string  `json:"name"`
	ParentRelPath *string `json:"parentRelPath"`
	Depth         int     `json:"depth"`
	Exists        bool    `json:"exists"`
}

type ScanLibraryDTO struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Folders []ScanFolderDTO `json:"folders"`
	Exists  bool            `json:"exists"`
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
	MediaTypeFilter   string           `json:"mediaTypeFilter"`
	OrientationFilter string           `json:"orientationFilter"`
	AssetCount        int              `json:"assetCount"`
	CoverAssetID      *int64           `json:"coverAssetId"`
	CreatedAt         int64            `json:"createdAt"`
	UpdatedAt         int64            `json:"updatedAt"`
	Sources           []AlbumSourceDTO `json:"sources"`
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
	return ScanProgressDTO{
		Reason:         progress.Reason,
		CurrentRoot:    progress.CurrentRoot,
		CurrentRelPath: progress.CurrentRelPath,
		TotalSeen:      progress.TotalSeen,
		AssetsAdded:    progress.AssetsAdded,
		AssetsUpdated:  progress.AssetsUpdated,
		AssetsDeleted:  progress.AssetsDeleted,
		Errors:         progress.Errors,
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

func processingProgressDTO(progress db.ProcessingProgress, queue jobs.QueueStats) ProcessingProgressDTO {
	return ProcessingProgressDTO{
		AssetTotal:  progress.AssetTotal,
		ImageTotal:  progress.ImageTotal,
		VideoTotal:  progress.VideoTotal,
		Thumb:       workStatusCountsDTO(progress.Thumb),
		Preview:     workStatusCountsDTO(progress.Preview),
		VideoPoster: workStatusCountsDTO(progress.VideoPoster),
		VideoProxy:  workStatusCountsDTO(progress.VideoProxy),
		Queue: QueueStatsDTO{
			ThumbQueued: queue.ThumbQueued,
			ThumbCap:    queue.ThumbCap,
			VideoQueued: queue.VideoQueued,
			VideoCap:    queue.VideoCap,
		},
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
