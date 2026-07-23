package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	aiworker "lpicto/backend/internal/ai"
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

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg.FFmpegHWAccel = video.ResolveHWAccel(rootCtx, cfg.FFmpegHWAccel, cfg.FFmpegHWDevice, logger)
	cfg.Log(logger)

	store, err := storage.NewWithRootsAndCache(cfg.PhotoRoots, cfg.DataRoot, cfg.CacheRoot)
	if err != nil {
		logger.Error("storage init failed", "error", err)
		os.Exit(1)
	}
	sources := storage.NewSourceHealth(store, 15*time.Second, cfg.RedisURL)

	database, err := db.Open(rootCtx, cfg.DatabaseURL, cfg.MigrationsDir)
	if err != nil {
		logger.Error("database init failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	eventBus := events.NewBus()
	statusStore, err := scanner.NewRedisStatusStore(rootCtx, cfg.RedisURL)
	if err != nil {
		logger.Error("redis status init failed", "error", err)
		os.Exit(1)
	}
	thumbProcessor := thumb.Processor{
		DB: database, Store: store, ThumbLongEdge: cfg.ThumbLongEdge, PreviewLongEdge: cfg.PreviewLongEdge,
		PreviewQuality: cfg.PreviewQuality, Events: eventBus, Logger: logger, Sources: sources,
	}
	queue, err := jobs.NewRedis(rootCtx, logger, cfg.RedisURL, thumbProcessor.Handle, jobs.ResourcePolicy{
		MaxActive:          cfg.BackgroundMaxActive,
		LoadTarget:         cfg.BackgroundLoadTarget,
		MinFreeMemoryBytes: uint64(cfg.BackgroundMinFreeMB) * 1024 * 1024,
		StartSpacing:       cfg.BackgroundStartGap,
	})
	if err != nil {
		logger.Error("redis queue init failed", "error", err)
		os.Exit(1)
	}

	scan := &scanner.Scanner{
		DB: database, Store: store, Extractor: media.NewExtractor(), Jobs: queue,
		Events: eventBus, StatusReporter: statusStore, VideoProxyEnabled: cfg.VideoProxyEnabled, ScanWorkers: cfg.ScanWorkers, Logger: logger, Sources: sources,
	}
	queue.SetScanHandler(scanTaskHandler(scan))
	queue.SetAIHandler((&aiworker.Processor{DB: database, BaseURL: cfg.AIURL, Logger: logger, Sources: sources}).Handle)

	role := ""
	if len(os.Args) > 1 {
		role = os.Args[1]
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = strings.ToLower(strings.TrimSpace(os.Getenv("APP_ROLE")))
	}
	if role == "" {
		role = "api"
	}
	switch role {
	case "worker":
		runWorker(rootCtx, cfg, database, queue, scan, sources, logger)
		<-rootCtx.Done()
	case "all":
		runWorker(rootCtx, cfg, database, queue, scan, sources, logger)
		handler := api.NewServer(cfg, database, store, scan, queue, eventBus, logger)
		if err := api.Start(rootCtx, cfg.HTTPAddr, handler, logger); err != nil {
			logger.Error("server stopped with error", "error", err)
			os.Exit(1)
		}
	default:
		remote := scanner.RemoteController{DB: database, Jobs: queue, StatusStore: statusStore}
		handler := api.NewServer(cfg, database, store, remote, queue, nil, logger)
		if err := api.Start(rootCtx, cfg.HTTPAddr, handler, logger); err != nil {
			logger.Error("server stopped with error", "error", err)
			os.Exit(1)
		}
	}
}

func runWorker(ctx context.Context, cfg config.Config, database *db.DB, queue *jobs.Manager, scan *scanner.Scanner, sources *storage.SourceHealth, logger *slog.Logger) {
	scan.Start(ctx)
	if cfg.EnableFSWatch {
		scan.StartWatcher(ctx, 3*time.Second)
	}
	scan.StartPeriodicCount(ctx, cfg.FileCountScanInterval)
	queue.ResetRuntimeState(ctx)
	if err := database.ResetAIProcessing(ctx); err != nil {
		logger.Warn("reset interrupted AI work failed", "error", err)
	}
	if err := database.ResetBackgroundVideoProxyWork(ctx); err != nil {
		logger.Warn("reset background video proxy work failed", "error", err)
	}
	enqueuePendingWork(ctx, database, queue, logger)
	queue.Start(ctx, jobs.WorkerConfig{
		Image:       cfg.ThumbWorkers,
		VideoPoster: cfg.VideoPosterWorkers,
		AI:          1,
	})
	go enqueueAIBackfill(ctx, database, queue, sources, logger)
	go monitorSourceRecovery(ctx, scan, sources, logger)
	aiworker.StartHealthMonitor(ctx, database, cfg.AIURL, logger)
}

func monitorSourceRecovery(ctx context.Context, scan *scanner.Scanner, sources *storage.SourceHealth, logger *slog.Logger) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	wasUnavailable := sourceUnavailable(sources)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			unavailable := sourceUnavailable(sources)
			if wasUnavailable && !unavailable {
				result := scan.RequestMetadataScan("storage_recovered")
				if result.Accepted && logger != nil {
					logger.Info("storage recovered; scheduled one metadata scan")
				}
			}
			wasUnavailable = unavailable
		}
	}
}

func sourceUnavailable(sources *storage.SourceHealth) bool {
	if sources == nil {
		return false
	}
	for _, status := range sources.Statuses() {
		if !status.Available {
			return true
		}
	}
	return false
}

func enqueueAIBackfill(ctx context.Context, database *db.DB, queue *jobs.Manager, sources *storage.SourceHealth, logger *slog.Logger) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		settings, settingsErr := database.GetAISettings(ctx)
		if settingsErr != nil {
			logger.Warn("load AI settings failed", "error", settingsErr)
		} else if settings.AutoAnalyze || settings.ManualRun {
			items, err := database.AIBackfillBatch(ctx, 1000)
			if err != nil {
				logger.Warn("load AI backfill failed", "error", err)
			} else {
				if len(items) > 0 {
					_ = database.EnsureSystemTaskRunning(ctx, "ai_analysis")
				}
				for _, item := range items {
					if sources != nil {
						if available, _ := sources.AvailableForRel(item.RelPath); !available {
							continue
						}
					}
					if err := database.EnsureAIQueued(ctx, item.AssetID, item.CacheKey, false); err == nil {
						queue.Enqueue(jobs.Task{Type: "ai_analyze", AssetID: item.AssetID, Priority: 100})
					}
				}
				if settings.ManualRun && len(items) == 0 {
					if _, err := database.SetAIManualRun(ctx, false); err != nil {
						logger.Warn("finish manual AI run failed", "error", err)
					}
				}
				stats := queue.Stats()
				if len(items) == 0 && stats.AIQueued+stats.ActiveAI == 0 {
					state, stateErr := database.SystemTaskState(ctx, "ai_analysis")
					if stateErr == nil && state != nil && state.Status == "running" {
						_ = database.FinishSystemTask(ctx, "ai_analysis", "success", "AI 分析已完成")
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func scanTaskHandler(scan *scanner.Scanner) jobs.Handler {
	return func(ctx context.Context, task jobs.Task) error {
		_ = ctx
		switch task.Type {
		case "scan", "scan_metadata":
			scan.RequestMetadataScan(defaultReason(task.Reason, "manual"))
		case "scan_roots":
			scan.RequestMetadataScanRoots(defaultReason(task.Reason, "manual"), task.Roots)
		case "scan_metadata_paths":
			scan.RequestMetadataScanPaths(defaultReason(task.Reason, "fsnotify"), task.Roots, task.Paths)
		case "scan_count":
			if len(task.Roots) > 0 {
				scan.RequestCountScanRoots(defaultReason(task.Reason, "count"), task.Roots)
			} else {
				scan.RequestCountScan(defaultReason(task.Reason, "count"))
			}
		case "scan_rebuild", "thumb_rebuild":
			if len(task.Roots) > 0 {
				scan.RequestThumbnailRebuildRoots(defaultReason(task.Reason, "thumb_rebuild"), task.Roots)
			} else {
				scan.RequestThumbnailRebuild(defaultReason(task.Reason, "thumb_rebuild"))
			}
		case "thumb_continue":
			if len(task.Roots) > 0 {
				scan.RequestThumbnailContinueRoots(defaultReason(task.Reason, "thumb_continue"), task.Roots)
			} else {
				scan.RequestThumbnailContinue(defaultReason(task.Reason, "thumb_continue"))
			}
		case "scan_stop":
			scan.RequestStop()
		}
		return nil
	}
}

func enqueuePendingWork(ctx context.Context, database *db.DB, queue *jobs.Manager, logger *slog.Logger) {
	items, err := database.PendingWork(ctx)
	if err != nil {
		logger.Warn("load pending work failed", "error", err)
		return
	}
	for _, item := range items {
		queue.Enqueue(jobs.Task{Type: item.Type, AssetID: item.AssetID})
	}
}

func defaultReason(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
