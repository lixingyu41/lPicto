package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lpicto/backend/internal/api"
	"lpicto/backend/internal/config"
	"lpicto/backend/internal/db"
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

	store, err := storage.New(cfg.PhotoRoot, cfg.DataRoot)
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

	thumbProcessor := thumb.Processor{
		DB: database, Store: store, ThumbLongEdge: cfg.ThumbLongEdge, PreviewLongEdge: cfg.PreviewLongEdge,
		PreviewQuality: cfg.PreviewQuality, Logger: logger,
	}
	videoProcessor := video.Processor{
		DB: database, Store: store, ProxyEnabled: cfg.VideoProxyEnabled, ProxyMaxHeight: cfg.VideoProxyMaxHeight,
		ProxyCRF: cfg.VideoProxyCRF, HWAccel: cfg.FFmpegHWAccel, HWDevice: cfg.FFmpegHWDevice,
		HWFallback: cfg.FFmpegHWFallback, Logger: logger,
	}
	queue := jobs.New(logger, thumbProcessor.Handle, videoProcessor.Handle)
	queue.Start(rootCtx, cfg.ThumbWorkers, cfg.VideoWorkers)

	scan := &scanner.Scanner{
		DB: database, Store: store, Extractor: media.NewExtractor(), Jobs: queue,
		VideoProxyEnabled: cfg.VideoProxyEnabled, Logger: logger,
	}
	recoverPending(rootCtx, database, queue, cfg.VideoProxyEnabled, logger)
	scan.StartPeriodic(rootCtx, cfg.ScanInterval)
	if cfg.EnableFSWatch {
		scan.StartWatcher(rootCtx, 2*time.Second)
	}

	handler := api.NewServer(cfg, database, store, scan, queue, logger)
	go func() {
		time.Sleep(200 * time.Millisecond)
		scan.Trigger("startup")
	}()
	if err := api.Start(rootCtx, cfg.HTTPAddr, handler, logger); err != nil {
		logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}

func recoverPending(ctx context.Context, database *db.DB, queue *jobs.Manager, proxyEnabled bool, logger *slog.Logger) {
	items, err := database.PendingWork(ctx, proxyEnabled)
	if err != nil {
		logger.Warn("recover pending jobs failed", "error", err)
		return
	}
	for _, item := range items {
		queue.Enqueue(jobs.Task{Type: item.Type, AssetID: item.AssetID})
	}
	logger.Info("recovered pending jobs", "count", len(items))
}
