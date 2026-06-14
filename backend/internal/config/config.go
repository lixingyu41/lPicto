package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	PhotoRoot           string        `json:"photoRoot"`
	DataRoot            string        `json:"dataRoot"`
	HTTPAddr            string        `json:"httpAddr"`
	ScanInterval        time.Duration `json:"-"`
	ScanIntervalMinutes int           `json:"scanIntervalMinutes"`
	ThumbWorkers        int           `json:"thumbWorkers"`
	VideoWorkers        int           `json:"videoWorkers"`
	PageSizeDefault     int           `json:"pageSizeDefault"`
	PageSizeMax         int           `json:"pageSizeMax"`
	EnableFSWatch       bool          `json:"enableFsWatch"`
	ThumbLongEdge       int           `json:"thumbLongEdge"`
	PreviewLongEdge     int           `json:"previewLongEdge"`
	PreviewQuality      int           `json:"previewQuality"`
	VideoProxyEnabled   bool          `json:"videoProxyEnabled"`
	VideoProxyMaxHeight int           `json:"videoProxyMaxHeight"`
	VideoProxyCRF       int           `json:"videoProxyCrf"`
	FFmpegHWAccel       string        `json:"ffmpegHwAccel"`
	FFmpegHWDevice      string        `json:"ffmpegHwDevice"`
	FFmpegHWFallback    bool          `json:"ffmpegHwFallback"`
	StaticDir           string        `json:"-"`
	MigrationsDir       string        `json:"-"`
}

func Load() (Config, error) {
	scanMinutes := intEnv("SCAN_INTERVAL_MINUTES", 30)
	cfg := Config{
		PhotoRoot:           stringEnv("PHOTO_ROOT", "/photos"),
		DataRoot:            stringEnv("DATA_ROOT", "/data"),
		HTTPAddr:            stringEnv("HTTP_ADDR", ":8080"),
		ScanIntervalMinutes: scanMinutes,
		ScanInterval:        time.Duration(scanMinutes) * time.Minute,
		ThumbWorkers:        intEnv("THUMB_WORKERS", 2),
		VideoWorkers:        intEnv("VIDEO_WORKERS", 1),
		PageSizeDefault:     intEnv("PAGE_SIZE_DEFAULT", 100),
		PageSizeMax:         intEnv("PAGE_SIZE_MAX", 500),
		EnableFSWatch:       boolEnv("ENABLE_FS_WATCH", true),
		ThumbLongEdge:       intEnv("THUMB_LONG_EDGE", 320),
		PreviewLongEdge:     intEnv("PREVIEW_LONG_EDGE", 2560),
		PreviewQuality:      intEnv("PREVIEW_QUALITY", 82),
		VideoProxyEnabled:   boolEnv("VIDEO_PROXY_ENABLED", true),
		VideoProxyMaxHeight: intEnv("VIDEO_PROXY_MAX_HEIGHT", 1080),
		VideoProxyCRF:       intEnv("VIDEO_PROXY_CRF", 23),
		FFmpegHWAccel:       hwAccelEnv("FFMPEG_HWACCEL", "none"),
		FFmpegHWDevice:      stringEnv("FFMPEG_HWACCEL_DEVICE", ""),
		FFmpegHWFallback:    boolEnv("FFMPEG_HWACCEL_FALLBACK", true),
		StaticDir:           stringEnv("STATIC_DIR", "frontend/dist"),
		MigrationsDir:       stringEnv("MIGRATIONS_DIR", "migrations"),
	}
	if cfg.PageSizeDefault < 1 {
		cfg.PageSizeDefault = 100
	}
	if cfg.PageSizeMax < cfg.PageSizeDefault {
		cfg.PageSizeMax = cfg.PageSizeDefault
	}
	if cfg.ThumbWorkers < 1 {
		cfg.ThumbWorkers = 1
	}
	if cfg.VideoWorkers < 1 {
		cfg.VideoWorkers = 1
	}
	if err := cfg.preparePaths(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Log(logger *slog.Logger) {
	logger.Info("effective config",
		"photoRoot", c.PhotoRoot,
		"dataRoot", c.DataRoot,
		"httpAddr", c.HTTPAddr,
		"scanIntervalMinutes", c.ScanIntervalMinutes,
		"thumbWorkers", c.ThumbWorkers,
		"videoWorkers", c.VideoWorkers,
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
	return filepath.Join(c.DataRoot, "lpicto.db")
}

func (c Config) CacheRoot() string {
	return filepath.Join(c.DataRoot, "cache")
}

func (c Config) preparePaths() error {
	photoInfo, err := os.Stat(c.PhotoRoot)
	if err != nil {
		return fmt.Errorf("PHOTO_ROOT is not accessible: %w", err)
	}
	if !photoInfo.IsDir() {
		return fmt.Errorf("PHOTO_ROOT is not a directory")
	}
	if err := os.MkdirAll(c.DataRoot, 0o755); err != nil {
		return fmt.Errorf("create DATA_ROOT: %w", err)
	}
	for _, rel := range []string{
		filepath.Join("cache", "thumbs"),
		filepath.Join("cache", "previews"),
		filepath.Join("cache", "video-posters"),
		filepath.Join("cache", "video-proxies"),
	} {
		if err := os.MkdirAll(filepath.Join(c.DataRoot, rel), 0o755); err != nil {
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
