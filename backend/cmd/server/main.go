package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"lpicto/backend/internal/api"
	"lpicto/backend/internal/config"
	"lpicto/backend/internal/db"
	"lpicto/backend/internal/events"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/media"
	"lpicto/backend/internal/scanner"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/thumb"
	"lpicto/backend/internal/video"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config failed", "error", err)
		os.Exit(1)
	}
	cfg.Log(logger)

	store, err := storage.NewWithRoots(cfg.PhotoRoots, cfg.DataRoot)
	if err != nil {
		logger.Error("storage init failed", "error", err)
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(rootCtx, cfg.DBPath(), cfg.MigrationsDir)
	if err != nil {
		logger.Error("database init failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	eventBus := events.NewBus()
	thumbProcessor := thumb.Processor{
		DB: database, Store: store, ThumbLongEdge: cfg.ThumbLongEdge, PreviewLongEdge: cfg.PreviewLongEdge,
		PreviewQuality: cfg.PreviewQuality, Events: eventBus, Logger: logger,
	}
	videoProcessor := video.Processor{
		DB: database, Store: store, ProxyEnabled: cfg.VideoProxyEnabled, ProxyMaxHeight: cfg.VideoProxyMaxHeight,
		ProxyCRF: cfg.VideoProxyCRF, HWAccel: video.ResolveHWAccel(rootCtx, cfg.FFmpegHWAccel, logger), HWDevice: cfg.FFmpegHWDevice,
		HWFallback: cfg.FFmpegHWFallback, Logger: logger,
	}
	queue := jobs.New(logger, thumbProcessor.Handle, videoProcessor.Handle, jobs.ResourcePolicy{
		MaxActive:          cfg.BackgroundMaxActive,
		LoadTarget:         cfg.BackgroundLoadTarget,
		MinFreeMemoryBytes: uint64(cfg.BackgroundMinFreeMB) * 1024 * 1024,
		StartSpacing:       cfg.BackgroundStartGap,
	})
	queue.Start(rootCtx, jobs.WorkerConfig{
		Image:       cfg.ThumbWorkers,
		VideoPoster: cfg.VideoPosterWorkers,
		VideoProxy:  cfg.VideoProxyWorkers,
	})

	scan := &scanner.Scanner{
		DB: database, Store: store, Extractor: media.NewExtractor(), Jobs: queue,
		VideoProxyEnabled: cfg.VideoProxyEnabled, ScanWorkers: cfg.ScanWorkers, Logger: logger,
	}

	handler := api.NewServer(cfg, database, store, scan, queue, eventBus, logger)
	if err := api.Start(rootCtx, cfg.HTTPAddr, handler, logger); err != nil {
		logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}
