package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	aiworker "lpicto/backend/internal/ai"
	"lpicto/backend/internal/api"
	"lpicto/backend/internal/cachepolicy"
	"lpicto/backend/internal/config"
	"lpicto/backend/internal/db"
	"lpicto/backend/internal/debugcontrol"
	"lpicto/backend/internal/events"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/media"
	"lpicto/backend/internal/model"
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
	cfg.StoryboardWorkers = config.ResolveStoryboardWorkers(cfg.StoryboardWorkers, cfg.FFmpegHWAccel)
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
	debugSettings, err := database.GetDebugSettings(rootCtx)
	if err != nil {
		logger.Error("load debug settings failed", "error", err)
		os.Exit(1)
	}
	debugcontrol.Apply(debugSettings.ExternalFileAccessPaused, debugSettings.BackgroundProcessingPaused)

	eventBus := events.NewBus()
	cachePolicy := cachepolicy.New(store.CacheRoot, database)
	go func() {
		reconcileCtx, cancel := context.WithTimeout(rootCtx, 10*time.Minute)
		defer cancel()
		if err := cachePolicy.Reconcile(reconcileCtx); err != nil {
			logger.Warn("reconcile cache registry failed", "error", err)
		}
	}()
	statusStore, err := scanner.NewRedisStatusStore(rootCtx, cfg.RedisURL)
	if err != nil {
		logger.Error("redis status init failed", "error", err)
		os.Exit(1)
	}
	thumbProcessor := thumb.Processor{
		DB: database, Store: store, ThumbLongEdge: cfg.ThumbLongEdge, PreviewLongEdge: cfg.PreviewLongEdge,
		PreviewQuality: cfg.PreviewQuality, Events: eventBus, Logger: logger, Sources: sources, CachePolicy: cachePolicy,
		FFmpegHWAccel: cfg.FFmpegHWAccel, FFmpegHWDevice: cfg.FFmpegHWDevice, FFmpegHWFallback: cfg.FFmpegHWFallback,
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
	aiStager := &aiworker.Stager{DB: database, Store: store, Policy: cachePolicy}
	queue.SetAIHandler((&aiworker.Processor{DB: database, BaseURL: cfg.AIURL, ExternalToken: cfg.ExternalAIToken, Logger: logger, Sources: sources, Stager: aiStager}).Handle)

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
		runWorker(rootCtx, cfg, database, queue, scan, sources, aiStager, logger)
		<-rootCtx.Done()
	case "all":
		runWorker(rootCtx, cfg, database, queue, scan, sources, aiStager, logger)
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

func runWorker(ctx context.Context, cfg config.Config, database *db.DB, queue *jobs.Manager, scan *scanner.Scanner, sources *storage.SourceHealth, aiStager *aiworker.Stager, logger *slog.Logger) {
	scan.Start(ctx)
	if cfg.EnableFSWatch {
		scan.StartWatcher(ctx, 3*time.Second)
	}
	scan.StartPeriodicCount(ctx, cfg.FileCountScanInterval)
	queue.ResetRuntimeState(ctx)
	if aiStager != nil {
		if err := aiStager.CleanupInterrupted(ctx); err != nil {
			logger.Warn("cleanup interrupted AI staging failed", "error", err)
		}
	}
	if err := queue.ClearAIQueue(ctx); err != nil {
		logger.Warn("clear stale AI queue before staged backfill failed", "error", err)
	}
	if err := database.ResetAIProcessing(ctx); err != nil {
		logger.Warn("reset interrupted AI work failed", "error", err)
	}
	if err := database.ResetBackgroundVideoProxyWork(ctx); err != nil {
		logger.Warn("reset background video proxy work failed", "error", err)
	}
	if !debugcontrol.BackgroundProcessingPaused() {
		enqueuePendingWork(ctx, database, queue, logger)
	}
	queue.Start(ctx, jobs.WorkerConfig{
		Image:       cfg.ThumbWorkers,
		VideoPoster: cfg.VideoPosterWorkers,
		Storyboard:  cfg.StoryboardWorkers,
		AI:          2,
	})
	go enqueueAIBackfill(ctx, database, queue, sources, aiStager, logger)
	go superviseAutomaticTasks(ctx, database, queue, logger)
	go monitorSourceRecovery(ctx, database, scan, sources, logger)
	aiworker.StartHealthMonitor(ctx, database, cfg.AIURL, cfg.ExternalAIToken, logger)
}

func superviseAutomaticTasks(ctx context.Context, database *db.DB, queue *jobs.Manager, logger *slog.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		health := queue.ExecutorHealth(ctx)
		status, message := "success", "任务执行器正常"
		if !health.Healthy {
			status = "failed"
			message = fmt.Sprintf("队列中有 %d 项，但 %d 秒内没有执行器取出任务", health.Queued, health.StalledSeconds)
			if logger != nil {
				logger.Error("task executor stalled", "queued", health.Queued, "stalledSeconds", health.StalledSeconds)
			}
		} else if health.Active > 0 {
			message = fmt.Sprintf("%d 个任务正在处理，%d 项等待", health.Active, health.Queued)
		} else if health.BlockedReason != "" {
			status = "pending"
			message = automaticBlockerMessage(health.BlockedReason, health.Queued)
		} else if health.Queued > 0 {
			status = "pending"
			message = fmt.Sprintf("%d 项正在等待执行器调度", health.Queued)
		}
		_ = database.BeginSystemTask(ctx, db.SystemTaskExecutorHealth)
		_ = database.FinishSystemTask(ctx, db.SystemTaskExecutorHealth, status, message)

		stats := queue.Stats()
		backgroundQueued := stats.ThumbQueued + stats.PreviewQueued + stats.VideoPosterQueued + stats.StoryboardQueued
		backgroundActive := stats.ActiveThumb + stats.ActivePreview + stats.ActiveVideoPoster + stats.ActiveStoryboard
		if backgroundQueued+backgroundActive == 0 && !debugcontrol.BackgroundProcessingPaused() {
			enqueuePendingWork(ctx, database, queue, logger)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func automaticBlockerMessage(reason string, queued int) string {
	switch reason {
	case "media_scan":
		return fmt.Sprintf("媒体扫描优先，%d 项自动等待", queued)
	case "playback", "foreground":
		return fmt.Sprintf("当前播放优先，%d 项自动等待", queued)
	case "storyboard":
		return fmt.Sprintf("进度预览图优先，%d 项自动等待", queued)
	case "load":
		return fmt.Sprintf("系统负载较高，%d 项自动等待", queued)
	case "memory":
		return fmt.Sprintf("可用内存不足，%d 项自动等待", queued)
	case "debug_pause":
		return fmt.Sprintf("调试开关已暂停后台处理，%d 项等待", queued)
	default:
		return fmt.Sprintf("%d 项自动等待", queued)
	}
}

func monitorSourceRecovery(ctx context.Context, database *db.DB, scan *scanner.Scanner, sources *storage.SourceHealth, logger *slog.Logger) {
	// Healthy storage is intentionally not polled so an idle NAS can sleep.
	// Once a source has been marked unavailable, probe once per minute until it
	// recovers, then run the normal media scan to refresh both data and task UI.
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if debugcontrol.ExternalFileAccessPaused() {
				continue
			}
			if !storageRecoveryNeeded(ctx, database, sources) {
				continue
			}
			statuses := sources.Statuses()
			if !sourceStatusesUnavailable(statuses) {
				result := scan.RequestMediaScan("storage_recovered")
				if result.Accepted && logger != nil {
					logger.Info("storage recovered; scheduled one media scan")
				}
			}
		}
	}
}

func storageRecoveryNeeded(ctx context.Context, database *db.DB, sources *storage.SourceHealth) bool {
	if sourceUnavailable(sources) {
		return true
	}
	if database == nil {
		return false
	}
	run, err := database.LastScanRunForTask(ctx, "media_scan")
	return err == nil && run != nil && run.LastError != nil && strings.Contains(*run.LastError, "存储不可达")
}

func sourceUnavailable(sources *storage.SourceHealth) bool {
	if sources == nil {
		return false
	}
	return sourceStatusesUnavailable(sources.CachedStatuses())
}

func sourceStatusesUnavailable(statuses []storage.SourceHealthStatus) bool {
	for _, status := range statuses {
		if !status.Available {
			return true
		}
	}
	return false
}

func enqueueAIBackfill(ctx context.Context, database *db.DB, queue *jobs.Manager, sources *storage.SourceHealth, stager *aiworker.Stager, logger *slog.Logger) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if debugcontrol.BackgroundProcessingPaused() {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				continue
			}
		}
		settings, settingsErr := database.GetAISettings(ctx)
		if settingsErr != nil {
			logger.Warn("load AI settings failed", "error", settingsErr)
		} else if settings.AutoAnalyze || settings.ManualRun {
			if active, activeErr := queue.MediaCachePriorityActive(ctx); activeErr == nil && active {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					continue
				}
			}
			items, err := database.AIBackfillBatch(ctx, 1000)
			if err != nil {
				logger.Warn("load AI backfill failed", "error", err)
			} else {
				if len(items) > 0 {
					_ = database.EnsureSystemTaskRunning(ctx, "ai_analysis")
				}
				type stagedBackfillItem struct {
					item  db.AIBackfillItem
					stage *db.AIStage
				}
				stagedItems := make([]stagedBackfillItem, 0, len(items))
				stagedReady := 0
				for _, item := range items {
					stage, stageErr := database.AIStageForAsset(ctx, item.AssetID, item.CacheKey)
					if stageErr != nil {
						logger.Warn("load AI staging failed", "assetID", item.AssetID, "error", stageErr)
						continue
					}
					if stage != nil && stage.State == "ready" {
						stagedReady++
					}
					stagedItems = append(stagedItems, stagedBackfillItem{item: item, stage: stage})
				}
				queueStats := queue.Stats()
				prepareBudget := 0
				if stagedReady == 0 && queueStats.AIQueued+queueStats.ActiveAI == 0 {
					prepareBudget = aiworker.StageBatchLimit
				}
				var preparedBytes int64
				preparedCount := 0
				var stageBatchID int64
				var stageBatchErr error
				if prepareBudget > 0 {
					stageBatchID, _ = database.BeginSourceIOBatch(ctx, "ai_stage_batch", 100)
				}
				for _, stagedItem := range stagedItems {
					item, stage := stagedItem.item, stagedItem.stage
					enabled, enabledErr := database.AIExecutionEnabled(ctx)
					if enabledErr != nil {
						stageBatchErr = enabledErr
						break
					}
					if !enabled {
						break
					}
					if (stage == nil || stage.State != "ready") && stager != nil && prepareBudget > 0 && preparedBytes < aiworker.StageBatchMaxBytes {
						if sources != nil {
							if available, _ := sources.CachedAvailableForRel(item.RelPath); !available {
								continue
							}
						}
						if jobs.MediaScanPriorityActive() {
							stageBatchErr = jobs.ErrMediaScanPriority
							break
						}
						if active, _ := queue.PlaybackPriorityActive(ctx); active {
							break
						}
						asset, assetErr := database.GetAsset(ctx, item.AssetID)
						if assetErr == nil {
							var stageErr error
							stage, stageErr = prepareAIStageWithPlayback(ctx, queue, stager, asset)
							if stageErr != nil {
								if errors.Is(stageErr, jobs.ErrPlaybackPriority) || ctx.Err() != nil {
									stageBatchErr = jobs.ErrPlaybackPriority
									break
								}
								logger.Warn("prepare AI staging failed", "assetID", item.AssetID, "error", stageErr)
								sourceUnavailable := storage.IsSourceUnavailable(stageErr)
								if sources != nil {
									libraryRoot, _ := database.ScanLibraryRootForPath(ctx, item.RelPath)
									sourceUnavailable = sources.AssetReadErrorIsSourceUnavailable(item.RelPath, stageErr, libraryRoot)
								}
								if sourceUnavailable {
									stageBatchErr = stageErr
									break
								}
								_ = database.EnsureAIQueued(ctx, item.AssetID, item.CacheKey, false)
								_, _ = database.MarkAIProcessing(ctx, item.AssetID, item.CacheKey)
								_, _ = database.MarkAIFailed(ctx, item.AssetID, item.CacheKey, "AI 输入准备失败："+stageErr.Error())
								prepareBudget--
								continue
							}
							enabled, enabledErr = database.AIExecutionEnabled(ctx)
							if enabledErr != nil || !enabled {
								stager.Remove(context.Background(), stage)
								if enabledErr != nil {
									stageBatchErr = enabledErr
								}
								break
							}
							preparedBytes += stage.SizeBytes
							preparedCount++
							prepareBudget--
						}
					}
					if stage == nil || stage.State != "ready" {
						continue
					}
					if err := database.EnsureAIQueued(ctx, item.AssetID, item.CacheKey, false); err == nil {
						queue.Enqueue(jobs.Task{Type: "ai_analyze", AssetID: item.AssetID, Priority: 100})
					}
				}
				if stageBatchID > 0 {
					state, message := "success", ""
					if errors.Is(stageBatchErr, jobs.ErrMediaScanPriority) {
						state, message = "preempted", "媒体扫描已抢占 AI 输入准备"
					} else if errors.Is(stageBatchErr, jobs.ErrPlaybackPriority) {
						state, message = "preempted", "当前媒体播放已抢占 NAS 读取"
					} else if stageBatchErr != nil {
						state, message = "failed", stageBatchErr.Error()
					}
					_ = database.FinishSourceIOBatch(context.Background(), stageBatchID, state, preparedCount, preparedBytes, message)
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

func prepareAIStageWithPlayback(ctx context.Context, queue *jobs.Manager, stager *aiworker.Stager, asset model.Asset) (*db.AIStage, error) {
	if debugcontrol.BackgroundProcessingPaused() {
		return nil, debugcontrol.ErrBackgroundProcessingPaused
	}
	stageCtx, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			if debugcontrol.BackgroundProcessingPaused() {
				cancel(debugcontrol.ErrBackgroundProcessingPaused)
				return
			}
			if jobs.MediaScanPriorityActive() {
				cancel(jobs.ErrMediaScanPriority)
				return
			}
			active, _ := queue.PlaybackPriorityActive(stageCtx)
			if active {
				cancel(jobs.ErrPlaybackPriority)
				return
			}
			select {
			case <-stageCtx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
	stage, err := stager.Prepare(stageCtx, asset)
	close(done)
	if cause := context.Cause(stageCtx); cause != nil {
		return nil, cause
	}
	cancel(nil)
	return stage, err
}

func scanTaskHandler(scan *scanner.Scanner) jobs.Handler {
	return func(ctx context.Context, task jobs.Task) error {
		_ = ctx
		switch task.Type {
		case "scan", "scan_metadata":
			reason := defaultReason(task.Reason, "manual")
			if len(task.Roots) > 0 {
				scan.RequestMetadataScanRoots(reason, task.Roots)
			} else {
				scan.RequestMetadataScan(reason)
			}
		case "scan_media":
			reason := defaultReason(task.Reason, "task:media_scan")
			if strings.HasPrefix(reason, "fsnotify_remote_recovery") && len(task.Roots) > 0 {
				scan.RequestMediaScanNestedRoots(reason, task.Roots)
			} else if len(task.Roots) > 0 {
				scan.RequestMediaScanRoots(reason, task.Roots)
			} else {
				scan.RequestMediaScan(reason)
			}
		case "scan_reconcile":
			if len(task.Roots) > 0 {
				scan.RequestReconcileScanRoots(defaultReason(task.Reason, "manual_reconcile"), task.Roots)
			} else {
				scan.RequestReconcileScan(defaultReason(task.Reason, "manual_reconcile"))
			}
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
	if debugcontrol.BackgroundProcessingPaused() {
		return
	}
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
