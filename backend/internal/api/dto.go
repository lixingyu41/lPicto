package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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
	ID                int64         `json:"id"`
	Filename          string        `json:"filename"`
	DisplayTitle      string        `json:"displayTitle"`
	FilenameSortKey   string        `json:"filenameSortKey"`
	RelPath           string        `json:"relPath"`
	ParentRelPath     string        `json:"parentRelPath"`
	MediaType         string        `json:"mediaType"`
	MimeType          *string       `json:"mimeType"`
	Size              int64         `json:"size"`
	Mtime             int64         `json:"mtime"`
	Width             *int          `json:"width"`
	Height            *int          `json:"height"`
	Duration          *float64      `json:"duration"`
	TakenAt           *int64        `json:"takenAt"`
	TimelineAt        int64         `json:"timelineAt"`
	ImportedAt        int64         `json:"importedAt"`
	CacheKey          string        `json:"cacheKey"`
	LastPlayedAt      *int64        `json:"lastPlayedAt,omitempty"`
	BrowserPlayable   bool          `json:"browserPlayable"`
	ThumbStatus       string        `json:"thumbStatus"`
	PreviewStatus     string        `json:"previewStatus"`
	VideoPosterStatus string        `json:"videoPosterStatus"`
	VideoProxyStatus  string        `json:"videoProxyStatus"`
	Rotation          int           `json:"rotation"`
	Rating            int           `json:"rating"`
	Hidden            bool          `json:"hidden"`
	SHA256            *string       `json:"sha256"`
	HasSubtitle       bool          `json:"hasSubtitle"`
	HasDanmaku        bool          `json:"hasDanmaku"`
	FPS               *float64      `json:"fps"`
	VideoCodec        *string       `json:"videoCodec"`
	AudioCodec        *string       `json:"audioCodec"`
	Container         *string       `json:"container"`
	VideoBitrate      *int64        `json:"videoBitrate"`
	AudioBitrate      *int64        `json:"audioBitrate"`
	OverallBitrate    *int64        `json:"overallBitrate"`
	AIDescription     *string       `json:"aiDescription,omitempty"`
	AITags            []db.AITag    `json:"aiTags,omitempty"`
	Palette           []db.AIColor  `json:"palette,omitempty"`
	ManualTags        []AssetTagDTO `json:"manualTags,omitempty"`
}

type AssetDeleteEntryDTO struct {
	RelPath string `json:"relPath"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Size    int64  `json:"size"`
	Reason  string `json:"reason"`
	IsMedia bool   `json:"isMedia"`
}

type AssetDeletePlanDTO struct {
	Asset          AssetDTO              `json:"asset"`
	Mode           string                `json:"mode"`
	Token          string                `json:"token"`
	CanDelete      bool                  `json:"canDelete"`
	Files          []AssetDeleteEntryDTO `json:"files"`
	Folder         *AssetDeleteEntryDTO  `json:"folder"`
	FolderContents []AssetDeleteEntryDTO `json:"folderContents"`
	Warnings       []string              `json:"warnings"`
	Blockers       []string              `json:"blockers"`
}

type AssetDeleteConfirmRequest struct {
	Token string `json:"token"`
}

type AssetDeleteFailureDTO struct {
	RelPath string `json:"relPath"`
	Message string `json:"message"`
}

type AssetDeleteResultDTO struct {
	Deleted         bool                    `json:"deleted"`
	DeletedAssetIDs []int64                 `json:"deletedAssetIds"`
	Failures        []AssetDeleteFailureDTO `json:"failures"`
	Plan            *AssetDeletePlanDTO     `json:"plan,omitempty"`
}

type AssetDeleteConflictDTO struct {
	Stale bool               `json:"stale"`
	Plan  AssetDeletePlanDTO `json:"plan"`
}

type VideoProxyRuntimeDTO struct {
	AssetID      int64   `json:"assetId"`
	Required     bool    `json:"required"`
	Cached       bool    `json:"cached"`
	Transcoding  bool    `json:"transcoding"`
	Queued       bool    `json:"queued"`
	Active       bool    `json:"active"`
	Status       string  `json:"status"`
	Progress     float64 `json:"progress"`
	SecondsDone  float64 `json:"secondsDone"`
	Duration     float64 `json:"duration"`
	Bytes        int64   `json:"bytes"`
	ExpiresAt    int64   `json:"expiresAt"`
	Error        string  `json:"error"`
	UpdatedAt    int64   `json:"updatedAt"`
	LeaseUntil   int64   `json:"leaseUntil"`
	CacheTTL     int64   `json:"cacheTtl"`
	KeepaliveTTL int64   `json:"keepaliveTtl"`
	RuntimeKey   string  `json:"runtimeKey"`
	StartSeconds float64 `json:"startSeconds"`
	ClientID     string  `json:"clientId"`
	SessionID    string  `json:"sessionId"`
	SessionState string  `json:"sessionState"`
	ActiveUsers  int     `json:"activeUsers"`
	PlayingUsers int     `json:"playingUsers"`
	Command      string  `json:"command"`
	Message      string  `json:"message"`
	ServerTime   int64   `json:"serverTime"`
}

type VideoSegmentStatusDTO struct {
	AssetID             int64   `json:"assetId"`
	SessionID           string  `json:"sessionId"`
	SegmentIndex        int     `json:"segmentIndex"`
	State               string  `json:"state"`
	Status              string  `json:"status"`
	Cached              bool    `json:"cached"`
	Transcoding         bool    `json:"transcoding"`
	Queued              bool    `json:"queued"`
	Progress            float64 `json:"progress"`
	SecondsDone         float64 `json:"secondsDone"`
	Duration            float64 `json:"duration"`
	Bytes               int64   `json:"bytes"`
	CachedBytes         int64   `json:"cachedBytes"`
	CachedSegments      int     `json:"cachedSegments"`
	SegmentCount        int     `json:"segmentCount"`
	EstimatedTotalBytes int64   `json:"estimatedTotalBytes"`
	SourceBytes         int64   `json:"sourceBytes"`
	Error               string  `json:"error"`
	Message             string  `json:"message"`
	UpdatedAt           int64   `json:"updatedAt"`
	ServerTime          int64   `json:"serverTime"`
}

type VideoProxySettingsDTO struct {
	CacheTTLSeconds int64 `json:"cacheTtlSeconds"`
	MaxCacheBytes   int64 `json:"maxCacheBytes"`
}

type VideoProxyHeartbeatRequest struct {
	ClientID     string  `json:"clientId"`
	SessionID    string  `json:"sessionId"`
	State        string  `json:"state"`
	CurrentTime  float64 `json:"currentTime"`
	PlaybackRate float64 `json:"playbackRate"`
	WantsStream  bool    `json:"wantsStream"`
	Hidden       bool    `json:"hidden"`
}

type AssetTagDTO struct {
	AssetID   int64  `json:"assetId"`
	Tag       string `json:"tag"`
	CreatedAt int64  `json:"createdAt"`
}

type TagSummaryDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	AssetCount int    `json:"assetCount"`
	CreatedAt  int64  `json:"createdAt"`
}

type CollectionDTO struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Kind       string          `json:"kind"`
	SystemKind string          `json:"systemKind,omitempty"`
	AssetCount int             `json:"assetCount"`
	Rule       json.RawMessage `json:"rule,omitempty"`
	CreatedAt  int64           `json:"createdAt"`
	UpdatedAt  int64           `json:"updatedAt"`
}

type BatchOperationResultDTO struct {
	UpdatedAssetIDs []int64                 `json:"updatedAssetIds"`
	DeletedAssetIDs []int64                 `json:"deletedAssetIds,omitempty"`
	Failures        []AssetDeleteFailureDTO `json:"failures"`
}

type DuplicateGroupDTO struct {
	Key    string     `json:"key"`
	Size   int64      `json:"size"`
	SHA256 string     `json:"sha256"`
	Items  []AssetDTO `json:"items"`
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
	ActivePreview     int `json:"activePreview"`
	ActiveVideoPoster int `json:"activeVideoPoster"`
	ActiveTranscode   int `json:"activeTranscode"`
}

type CacheStatsDTO struct {
	SizeBytes        int64            `json:"sizeBytes"`
	CacheBytes       int64            `json:"cacheBytes"`
	DatabaseBytes    int64            `json:"databaseBytes"`
	FileCount        int              `json:"fileCount"`
	UpdatedAt        int64            `json:"updatedAt"`
	Refreshing       bool             `json:"refreshing"`
	MaxBytes         int64            `json:"maxBytes"`
	MinFreeBytes     int64            `json:"minFreeBytes"`
	FreeBytes        int64            `json:"freeBytes"`
	ReclaimableBytes int64            `json:"reclaimableBytes"`
	ByKind           map[string]int64 `json:"byKind"`
}

type ProcessingProgressDTO struct {
	AssetTotal  int                 `json:"assetTotal"`
	ImageTotal  int                 `json:"imageTotal"`
	VideoTotal  int                 `json:"videoTotal"`
	AudioTotal  int                 `json:"audioTotal"`
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
	AIFocus  string                 `json:"aiFocus"`
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
	VideoProxy      WorkStatusCountsDTO `json:"videoProxy"`
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
	Rating    int   `json:"rating"`
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
	mediaDetails := parseAssetMediaDetails(asset.MetadataJSON)
	return AssetDTO{
		ID:                asset.ID,
		Filename:          asset.Filename,
		DisplayTitle:      assetDisplayTitle(asset),
		FilenameSortKey:   asset.FilenameSortKey,
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
		LastPlayedAt:      asset.LastPlayedAt,
		BrowserPlayable:   asset.BrowserPlayable,
		ThumbStatus:       asset.ThumbStatus,
		PreviewStatus:     asset.PreviewStatus,
		VideoPosterStatus: asset.VideoPosterStatus,
		VideoProxyStatus:  asset.VideoProxyStatus,
		Rotation:          asset.Rotation,
		Rating:            asset.Rating,
		Hidden:            asset.Hidden,
		SHA256:            asset.SHA256,
		HasSubtitle:       asset.HasSubtitle,
		HasDanmaku:        asset.HasDanmaku,
		FPS:               mediaDetails.FPS,
		VideoCodec:        mediaDetails.VideoCodec,
		AudioCodec:        mediaDetails.AudioCodec,
		Container:         mediaDetails.Container,
		VideoBitrate:      mediaDetails.VideoBitrate,
		AudioBitrate:      mediaDetails.AudioBitrate,
		OverallBitrate:    mediaDetails.OverallBitrate,
	}
}

type assetMediaDetails struct {
	FPS            *float64
	VideoCodec     *string
	AudioCodec     *string
	Container      *string
	VideoBitrate   *int64
	AudioBitrate   *int64
	OverallBitrate *int64
}

type assetProbeMetadata struct {
	Streams []struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Profile      string `json:"profile"`
		BitRate      string `json:"bit_rate"`
		AvgFrameRate string `json:"avg_frame_rate"`
		RFrameRate   string `json:"r_frame_rate"`
	} `json:"streams"`
	Format struct {
		FormatName string            `json:"format_name"`
		BitRate    string            `json:"bit_rate"`
		Tags       map[string]string `json:"tags"`
	} `json:"format"`
}

func assetDisplayTitle(asset model.Asset) string {
	if title := embeddedMediaTitle(asset.MetadataJSON); title != "" {
		return title
	}
	if title := nfoMediaTitle(asset.NFOJSON); title != "" {
		return title
	}
	return asset.Filename
}

func embeddedMediaTitle(raw *string) string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return ""
	}
	var probe assetProbeMetadata
	if json.Unmarshal([]byte(*raw), &probe) == nil {
		if title := mapValueFold(probe.Format.Tags, "title"); title != "" {
			return title
		}
	}
	var images []map[string]any
	if json.Unmarshal([]byte(*raw), &images) == nil && len(images) > 0 {
		for _, key := range []string{"Title", "ObjectName", "XPTitle", "Headline"} {
			for field, value := range images[0] {
				if strings.EqualFold(field, key) {
					if title := cleanDisplayTitle(fmt.Sprint(value)); title != "" {
						return title
					}
				}
			}
		}
	}
	return ""
}

func nfoMediaTitle(raw *string) string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return ""
	}
	var value struct {
		Fields map[string]string `json:"fields"`
		Groups []struct {
			Items []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"items"`
		} `json:"groups"`
	}
	if json.Unmarshal([]byte(*raw), &value) != nil {
		return ""
	}
	for _, group := range value.Groups {
		for _, item := range group.Items {
			if strings.EqualFold(item.Key, "title") {
				if title := cleanDisplayTitle(item.Value); title != "" {
					return title
				}
			}
		}
	}
	for _, key := range []string{"标题", "title", "原名", "originaltitle"} {
		if title := mapValueFold(value.Fields, key); title != "" {
			return title
		}
	}
	return ""
}

func mapValueFold(values map[string]string, key string) string {
	for current, value := range values {
		if strings.EqualFold(current, key) {
			return cleanDisplayTitle(value)
		}
	}
	return ""
}

func cleanDisplayTitle(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func parseAssetMediaDetails(raw *string) assetMediaDetails {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return assetMediaDetails{}
	}
	var probe assetProbeMetadata
	if err := json.Unmarshal([]byte(*raw), &probe); err != nil {
		return assetMediaDetails{}
	}
	details := assetMediaDetails{
		Container:      nonEmptyStringPtr(probe.Format.FormatName),
		OverallBitrate: positiveInt64Ptr(probe.Format.BitRate),
	}
	hasAudio := false
	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			if details.VideoCodec == nil {
				details.VideoCodec = nonEmptyStringPtr(codecLabel(stream.CodecName, stream.Profile))
				details.VideoBitrate = positiveInt64Ptr(stream.BitRate)
				details.FPS = frameRatePtr(stream.AvgFrameRate)
				if details.FPS == nil {
					details.FPS = frameRatePtr(stream.RFrameRate)
				}
			}
		case "audio":
			hasAudio = true
			if details.AudioCodec == nil {
				details.AudioCodec = nonEmptyStringPtr(codecLabel(stream.CodecName, stream.Profile))
				details.AudioBitrate = positiveInt64Ptr(stream.BitRate)
			}
		}
	}
	if details.OverallBitrate != nil {
		streamBitrate := int64(0)
		if details.VideoBitrate != nil {
			streamBitrate += *details.VideoBitrate
		}
		if details.AudioBitrate != nil {
			streamBitrate += *details.AudioBitrate
		}
		if streamBitrate > 0 && streamBitrate*4 < *details.OverallBitrate {
			details.VideoBitrate = nil
			details.AudioBitrate = nil
		}
	}
	if details.OverallBitrate != nil {
		switch {
		case details.VideoBitrate == nil && details.AudioBitrate != nil && *details.OverallBitrate > *details.AudioBitrate:
			value := *details.OverallBitrate - *details.AudioBitrate
			details.VideoBitrate = &value
		case details.AudioBitrate == nil && details.VideoBitrate != nil && *details.OverallBitrate > *details.VideoBitrate:
			value := *details.OverallBitrate - *details.VideoBitrate
			details.AudioBitrate = &value
		case details.VideoBitrate == nil && !hasAudio:
			value := *details.OverallBitrate
			details.VideoBitrate = &value
		}
	}
	return details
}

func codecLabel(codec, profile string) string {
	codec = strings.TrimSpace(codec)
	profile = strings.TrimSpace(profile)
	if codec == "" || profile == "" || strings.EqualFold(codec, profile) {
		return codec
	}
	return codec + " (" + profile + ")"
}

func nonEmptyStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func positiveInt64Ptr(value string) *int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed <= 0 {
		return nil
	}
	return &parsed
}

func frameRatePtr(value string) *float64 {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) == 0 || len(parts) > 2 {
		return nil
	}
	numerator, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || numerator <= 0 {
		return nil
	}
	result := numerator
	if len(parts) == 2 {
		denominator, err := strconv.ParseFloat(parts[1], 64)
		if err != nil || denominator <= 0 {
			return nil
		}
		result /= denominator
	}
	return &result
}

func assetDTOs(assets []model.Asset) []AssetDTO {
	result := make([]AssetDTO, 0, len(assets))
	for _, asset := range assets {
		result = append(result, assetDTO(asset))
	}
	return result
}

func assetDTOsWithListSummaries(assets []model.Asset, summaries map[int64]db.AISummary, manualTags map[int64][]db.AssetTag) []AssetDTO {
	result := assetDTOs(assets)
	for i := range result {
		if summary, ok := summaries[result[i].ID]; ok {
			description := summary.Description
			result[i].AIDescription = &description
			result[i].AITags = summary.Tags
			result[i].Palette = summary.Palette
		}
		result[i].ManualTags = assetTagDTOs(result[i].ID, manualTags[result[i].ID])
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
	active := queue.ActiveThumb+queue.ActivePreview+queue.ActiveVideoPoster+queue.ActiveTranscode+queue.ThumbQueued+queue.PreviewQueued+queue.VideoPosterQueued+queue.VideoProxyQueued > 0
	return ProcessingProgressDTO{
		AssetTotal:  progress.AssetTotal,
		ImageTotal:  progress.ImageTotal,
		VideoTotal:  progress.VideoTotal,
		AudioTotal:  progress.AudioTotal,
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
			ActivePreview:     queue.ActivePreview,
			ActiveVideoPoster: queue.ActiveVideoPoster,
			ActiveTranscode:   queue.ActiveTranscode,
		},
		Cache:      cache,
		Active:     active,
		UpdatedAt:  updatedAt,
		Refreshing: refreshing,
	}
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
	return AssetPreferenceDTO{AssetID: pref.AssetID, Rotation: pref.Rotation, Rating: pref.Rating, UpdatedAt: pref.UpdatedAt}
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

func collectionDTO(item model.Collection) CollectionDTO {
	var raw json.RawMessage
	if item.RuleJSON != nil && *item.RuleJSON != "" {
		raw = json.RawMessage(*item.RuleJSON)
	}
	return CollectionDTO{
		ID:         item.ID,
		Name:       item.Name,
		Kind:       item.Kind,
		SystemKind: item.SystemKind,
		AssetCount: item.AssetCount,
		Rule:       raw,
		CreatedAt:  item.CreatedAt,
		UpdatedAt:  item.UpdatedAt,
	}
}

func collectionDTOs(items []model.Collection) []CollectionDTO {
	result := make([]CollectionDTO, 0, len(items))
	for _, item := range items {
		result = append(result, collectionDTO(item))
	}
	return result
}

func duplicateGroupDTOs(groups []model.DuplicateGroup) []DuplicateGroupDTO {
	result := make([]DuplicateGroupDTO, 0, len(groups))
	for _, group := range groups {
		result = append(result, DuplicateGroupDTO{
			Key: group.Key, Size: group.Size, SHA256: group.SHA256, Items: assetDTOs(group.Items),
		})
	}
	return result
}
