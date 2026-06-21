package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"lpicto/backend/internal/storage"
)

type Config struct {
	PhotoRoot                    string               `json:"photoRoot"`
	PhotoRoots                   []storage.RootConfig `json:"photoRoots"`
	DataRoot                     string               `json:"dataRoot"`
	CacheRoot                    string               `json:"cacheRoot"`
	DatabaseURL                  string               `json:"-"`
	RedisURL                     string               `json:"-"`
	HTTPAddr                     string               `json:"httpAddr"`
	ScanInterval                 time.Duration        `json:"-"`
	ScanIntervalMinutes          int                  `json:"scanIntervalMinutes"`
	FileCountScanInterval        time.Duration        `json:"-"`
	FileCountScanIntervalMinutes int                  `json:"fileCountScanIntervalMinutes"`
	ScanWorkers                  int                  `json:"scanWorkers"`
	ThumbWorkers                 int                  `json:"thumbWorkers"`
	VideoWorkers                 int                  `json:"videoWorkers"`
	VideoPosterWorkers           int                  `json:"videoPosterWorkers"`
	VideoProxyWorkers            int                  `json:"videoProxyWorkers"`
	BackgroundMaxActive          int                  `json:"backgroundMaxActive"`
	BackgroundLoadTarget         float64              `json:"backgroundLoadTarget"`
	BackgroundMinFreeMB          int                  `json:"backgroundMinFreeMb"`
	BackgroundStartGap           time.Duration        `json:"-"`
	BackgroundStartGapMS         int                  `json:"backgroundStartGapMs"`
	PageSizeDefault              int                  `json:"pageSizeDefault"`
	PageSizeMax                  int                  `json:"pageSizeMax"`
	EnableFSWatch                bool                 `json:"enableFsWatch"`
	ThumbLongEdge                int                  `json:"thumbLongEdge"`
	PreviewLongEdge              int                  `json:"previewLongEdge"`
	PreviewQuality               int                  `json:"previewQuality"`
	VideoProxyEnabled            bool                 `json:"videoProxyEnabled"`
	VideoProxyMaxHeight          int                  `json:"videoProxyMaxHeight"`
	VideoProxyCRF                int                  `json:"videoProxyCrf"`
	FFmpegHWAccel                string               `json:"ffmpegHwAccel"`
	FFmpegHWDevice               string               `json:"ffmpegHwDevice"`
	FFmpegHWFallback             bool                 `json:"ffmpegHwFallback"`
	StaticDir                    string               `json:"-"`
	MigrationsDir                string               `json:"-"`
}

func Load() (Config, error) {
	scanMinutes := intEnv("SCAN_INTERVAL_MINUTES", 0)
	fileCountScanMinutes := intEnv("FILE_COUNT_SCAN_INTERVAL_MINUTES", 10)
	photoRoot := stringEnv("MEDIA_ROOT", "/Media")
	photoRoots, err := photoRootsEnv("PHOTO_ROOTS", photoRoot)
	if err != nil {
		return Config{}, err
	}
	cpus := runtime.NumCPU()
	scanWorkers := intEnv("SCAN_WORKERS", boundedInt((cpus+1)/2, 1, 8))
	thumbWorkers := intEnv("THUMB_WORKERS", boundedInt(cpus, 2, 8))
	videoWorkersOverride := intEnv("VIDEO_WORKERS", 0)
	videoPosterDefault := boundedInt((cpus+1)/2, 1, 4)
	videoProxyDefault := 1
	if videoWorkersOverride > 0 {
		videoPosterDefault = videoWorkersOverride
		videoProxyDefault = maxInt(1, videoWorkersOverride/2)
	}
	videoPosterWorkers := intEnv("VIDEO_POSTER_WORKERS", videoPosterDefault)
	videoProxyWorkers := intEnv("VIDEO_PROXY_WORKERS", videoProxyDefault)
	videoWorkers := videoPosterWorkers + videoProxyWorkers
	backgroundMaxActive := intEnv("BACKGROUND_MAX_ACTIVE", boundedInt(cpus, 2, 8))
	backgroundStartGapMS := intEnv("BACKGROUND_START_SPACING_MS", 50)
	cfg := Config{
		PhotoRoot:                    photoRoot,
		PhotoRoots:                   photoRoots,
		DataRoot:                     stringEnv("DATA_ROOT", "/data"),
		CacheRoot:                    stringEnv("CACHE_ROOT", "/cache"),
		DatabaseURL:                  stringEnv("DATABASE_URL", "postgres://media:media@postgres:5432/media?sslmode=disable"),
		RedisURL:                     stringEnv("REDIS_URL", "redis://redis:6379/0"),
		HTTPAddr:                     stringEnv("HTTP_ADDR", ":8080"),
		ScanIntervalMinutes:          scanMinutes,
		ScanInterval:                 time.Duration(scanMinutes) * time.Minute,
		FileCountScanIntervalMinutes: fileCountScanMinutes,
		FileCountScanInterval:        time.Duration(fileCountScanMinutes) * time.Minute,
		ScanWorkers:                  scanWorkers,
		ThumbWorkers:                 thumbWorkers,
		VideoWorkers:                 videoWorkers,
		VideoPosterWorkers:           videoPosterWorkers,
		VideoProxyWorkers:            videoProxyWorkers,
		BackgroundMaxActive:          backgroundMaxActive,
		BackgroundLoadTarget:         floatEnv("BACKGROUND_LOAD_TARGET", float64(maxInt(cpus*2, backgroundMaxActive))),
		BackgroundMinFreeMB:          intEnv("BACKGROUND_MIN_FREE_MB", 512),
		BackgroundStartGapMS:         backgroundStartGapMS,
		BackgroundStartGap:           time.Duration(backgroundStartGapMS) * time.Millisecond,
		PageSizeDefault:              intEnv("PAGE_SIZE_DEFAULT", 100),
		PageSizeMax:                  intEnv("PAGE_SIZE_MAX", 500),
		EnableFSWatch:                boolEnv("ENABLE_FS_WATCH", true),
		ThumbLongEdge:                intEnv("THUMB_LONG_EDGE", 320),
		PreviewLongEdge:              intEnv("PREVIEW_LONG_EDGE", 2560),
		PreviewQuality:               intEnv("PREVIEW_QUALITY", 82),
		VideoProxyEnabled:            boolEnv("VIDEO_PROXY_ENABLED", true),
		VideoProxyMaxHeight:          intEnv("VIDEO_PROXY_MAX_HEIGHT", 1080),
		VideoProxyCRF:                intEnv("VIDEO_PROXY_CRF", 23),
		FFmpegHWAccel:                hwAccelEnv("FFMPEG_HWACCEL", "none"),
		FFmpegHWDevice:               stringEnv("FFMPEG_HWACCEL_DEVICE", ""),
		FFmpegHWFallback:             boolEnv("FFMPEG_HWACCEL_FALLBACK", true),
		StaticDir:                    stringEnv("STATIC_DIR", "frontend/dist"),
		MigrationsDir:                stringEnv("MIGRATIONS_DIR", "migrations"),
	}
	if cfg.PageSizeDefault < 1 {
		cfg.PageSizeDefault = 100
	}
	if cfg.PageSizeMax < cfg.PageSizeDefault {
		cfg.PageSizeMax = cfg.PageSizeDefault
	}
	if cfg.ScanWorkers < 1 {
		cfg.ScanWorkers = 1
	}
	if cfg.ThumbWorkers < 1 {
		cfg.ThumbWorkers = 1
	}
	if cfg.VideoWorkers < 1 {
		cfg.VideoWorkers = 1
	}
	if cfg.VideoPosterWorkers < 1 {
		cfg.VideoPosterWorkers = 1
	}
	if cfg.VideoProxyWorkers < 1 {
		cfg.VideoProxyWorkers = 1
	}
	if cfg.BackgroundMaxActive < 1 {
		cfg.BackgroundMaxActive = 1
	}
	if cfg.BackgroundLoadTarget <= 0 {
		cfg.BackgroundLoadTarget = float64(cfg.BackgroundMaxActive)
	}
	if cfg.BackgroundMinFreeMB < 0 {
		cfg.BackgroundMinFreeMB = 0
	}
	if cfg.BackgroundStartGapMS < 0 {
		cfg.BackgroundStartGapMS = 0
		cfg.BackgroundStartGap = 0
	}
	if err := cfg.preparePaths(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Log(logger *slog.Logger) {
	logger.Info("effective config",
		"photoRoot", c.PhotoRoot,
		"photoRoots", c.PhotoRoots,
		"dataRoot", c.DataRoot,
		"cacheRoot", c.CacheRoot,
		"httpAddr", c.HTTPAddr,
		"scanIntervalMinutes", c.ScanIntervalMinutes,
		"fileCountScanIntervalMinutes", c.FileCountScanIntervalMinutes,
		"scanWorkers", c.ScanWorkers,
		"thumbWorkers", c.ThumbWorkers,
		"videoWorkers", c.VideoWorkers,
		"videoPosterWorkers", c.VideoPosterWorkers,
		"videoProxyWorkers", c.VideoProxyWorkers,
		"backgroundMaxActive", c.BackgroundMaxActive,
		"backgroundLoadTarget", c.BackgroundLoadTarget,
		"backgroundMinFreeMB", c.BackgroundMinFreeMB,
		"backgroundStartGapMS", c.BackgroundStartGapMS,
		"pageSizeDefault", c.PageSizeDefault,
		"pageSizeMax", c.PageSizeMax,
		"enableFsWatch", c.EnableFSWatch,
		"thumbLongEdge", c.ThumbLongEdge,
		"previewLongEdge", c.PreviewLongEdge,
		"previewQuality", c.PreviewQuality,
		"videoProxyEnabled", c.VideoProxyEnabled,
		"videoProxyMaxHeight", c.VideoProxyMaxHeight,
		"videoProxyCrf", c.VideoProxyCRF,
		"ffmpegHwAccel", c.FFmpegHWAccel,
		"ffmpegHwDevice", c.FFmpegHWDevice,
		"ffmpegHwFallback", c.FFmpegHWFallback,
	)
}

func (c Config) DBPath() string {
	return c.DatabaseURL
}

func (c Config) CacheDir() string {
	return c.CacheRoot
}

func (c Config) preparePaths() error {
	for _, root := range c.PhotoRoots {
		photoInfo, err := os.Stat(root.Path)
		if err != nil {
			return fmt.Errorf("photo root %s is not accessible: %w", root.Path, err)
		}
		if !photoInfo.IsDir() {
			return fmt.Errorf("photo root %s is not a directory", root.Path)
		}
	}
	if err := os.MkdirAll(c.DataRoot, 0o755); err != nil {
		return fmt.Errorf("create DATA_ROOT: %w", err)
	}
	for _, rel := range []string{
		"thumbs",
		"previews",
		"video-posters",
		"video-proxies",
	} {
		if err := os.MkdirAll(filepath.Join(c.CacheRoot, rel), 0o755); err != nil {
			return fmt.Errorf("create cache dir %s: %w", rel, err)
		}
	}
	return nil
}

func stringEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func intEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func floatEnv(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolEnv(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "":
		return fallback
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func photoRootsEnv(key string, legacyRoot string) ([]storage.RootConfig, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return []storage.RootConfig{{Path: legacyRoot}}, nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == '\n'
	})
	roots := make([]storage.RootConfig, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, rootPath, ok := strings.Cut(part, "=")
		if !ok {
			rootPath = part
			name = filepath.Base(filepath.Clean(rootPath))
		}
		id, err := storage.NormalizeRootID(name)
		if err != nil || id == "" {
			return nil, fmt.Errorf("invalid PHOTO_ROOTS entry %q", part)
		}
		roots = append(roots, storage.RootConfig{ID: id, Path: strings.TrimSpace(rootPath)})
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("PHOTO_ROOTS is empty")
	}
	return roots, nil
}

func hwAccelEnv(key, fallback string) string {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "":
		return fallback
	case "none", "auto", "cuda", "vaapi", "qsv", "dxva2", "d3d11va", "videotoolbox":
		return value
	default:
		return fallback
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boundedInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
