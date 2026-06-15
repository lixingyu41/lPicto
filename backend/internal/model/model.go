package model

type Asset struct {
	ID                int64
	RelPath           string
	ParentRelPath     string
	Filename          string
	Ext               string
	MediaType         string
	MimeType          *string
	Size              int64
	Mtime             int64
	Width             *int
	Height            *int
	Duration          *float64
	TakenAt           *int64
	ImportedAt        int64
	TimelineAt        int64
	CacheKey          string
	BrowserPlayable   bool
	ScanStatus        string
	ThumbStatus       string
	PreviewStatus     string
	VideoPosterStatus string
	VideoProxyStatus  string
	Rotation          int
	MetadataJSON      *string
	NFOJSON           *string
	NFOSearchText     *string
	Error             *string
	DeletedAt         *int64
	CreatedAt         int64
	UpdatedAt         int64
}

type Folder struct {
	ID                  int64
	RelPath             string
	Name                string
	ParentRelPath       *string
	Depth               int
	AssetCount          int
	RecursiveAssetCount int
	CoverAssetID        *int64
	UpdatedAt           int64
}

type ScanRun struct {
	ID            int64
	Status        string
	StartedAt     int64
	FinishedAt    *int64
	TotalSeen     int
	AssetsAdded   int
	AssetsUpdated int
	AssetsDeleted int
	Errors        int
	LastError     *string
}

type TimelineGroup struct {
	Key          string
	Label        string
	Start        int64
	End          int64
	Count        int
	CoverAssetID *int64
}

type AssetPreference struct {
	AssetID   int64
	Rotation  int
	UpdatedAt int64
}

type Album struct {
	ID                int64
	Name              string
	GroupID           *int64
	MediaTypeFilter   string
	OrientationFilter string
	AssetCount        int
	CoverAssetID      *int64
	CreatedAt         int64
	UpdatedAt         int64
	Sources           []AlbumSource
}

type AlbumGroup struct {
	ID        int64
	Name      string
	CreatedAt int64
	UpdatedAt int64
}

type AlbumSource struct {
	ID                int64
	AlbumID           int64
	SourceType        string
	RelPath           string
	Recursive         bool
	MediaTypeFilter   string
	OrientationFilter string
	CreatedAt         int64
}

type Page[T any] struct {
	Items    []T
	Page     int
	PageSize int
	HasMore  bool
}

const (
	MediaTypeImage = "image"
	MediaTypeVideo = "video"

	StatusPending     = "pending"
	StatusProcessing  = "processing"
	StatusReady       = "ready"
	StatusError       = "error"
	StatusNotRequired = "not_required"
	StatusOK          = "ok"
)
