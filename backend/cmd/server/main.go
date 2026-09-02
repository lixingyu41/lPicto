package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
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
	aiProcessor := &aiworker.Processor{DB: database, BaseURL: cfg.AIURL, ExternalToken: cfg.ExternalAIToken, Logger: logger, Sources: sources, Stager: aiStager}
	queue.SetAIHandler(aiProcessor.Handle)

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
		runWorker(rootCtx, cfg, database, queue, scan, sources, aiStager, aiProcessor, logger)
		<-rootCtx.Done()
	case "all":
		runWorker(rootCtx, cfg, database, queue, scan, sources, aiStager, aiProcessor, logger)
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

func runWorker(ctx context.Context, cfg config.Config, database *db.DB, queue *jobs.Manager, scan *scanner.Scanner, sources *storage.SourceHealth, aiStager *aiworker.Stager, aiProcessor *aiworker.Processor, logger *slog.Logger) {
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
	go maintainExternalAIPrefetch(ctx, database, queue, aiProcessor, logger)
	go superviseAutomaticTasks(ctx, database, queue, logger)
	go monitorSourceRecovery(ctx, database, scan, sources, logger)
	go repairVideoDisplayMetadataDimensions(ctx, database, logger)
	aiworker.StartHealthMonitor(ctx, database, cfg.AIURL, cfg.ExternalAIToken, logger)
}

func repairVideoDisplayMetadataDimensions(ctx context.Context, database *db.DB, logger *slog.Logger) {
	updated, err := database.RepairVideoDisplayMetadataDimensions(ctx)
	if err != nil {
		logger.Warn("repair video display metadata dimensions failed", "error", err)
		return
	}
	if updated > 0 {
		logger.Info("repaired video display metadata dimensions", "count", updated)
	}
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
	ticker := time.NewTicker(2 * time.Second)
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
			items, err := database.AIBackfillBatch(ctx, 128)
			if err != nil {
				logger.Warn("load AI backfill failed", "error", err)
			} else {
				if len(items) > 0 {
					_ = database.EnsureSystemTaskRunning(ctx, "ai_analysis")
				}
				stages, stageQueryErr := database.AIStagesForBackfill(ctx, items)
				if stageQueryErr != nil {
					logger.Warn("load AI staging failed", "error", stageQueryErr)
					stages = make(map[int64]*db.AIStage)
				}
				queueStats := queue.Stats()
				readyCount, readyBytes, stageStatsErr := database.AIStageStats(ctx)
				if stageStatsErr != nil {
					logger.Warn("load AI staging stats failed", "error", stageStatsErr)
				}
				enqueueBudget := aiworker.StageBatchLimit - queueStats.AIQueued - queueStats.ActiveAI
				if enqueueBudget < 0 {
					enqueueBudget = 0
				}
				prepareBudget := 0
				if stageStatsErr == nil && readyCount+queueStats.ActiveAI <= aiworker.StageLowWater {
					prepareBudget = aiworker.StageBatchLimit - readyCount - queueStats.ActiveAI
					if prepareBudget > aiworker.StageBatchLimit {
						prepareBudget = aiworker.StageBatchLimit
					}
				}
				var preparedBytes int64
				preparedCount := 0
				var stageBatchID int64
				var stageBatchErr error
				if prepareBudget > 0 {
					stageBatchID, _ = database.BeginSourceIOBatch(ctx, "ai_stage_batch", prepareBudget)
				}
				enqueueStage := func(item db.AIBackfillItem, stage *db.AIStage) {
					if enqueueBudget <= 0 {
						return
					}
					if err := database.EnsureAIQueued(ctx, item.AssetID, item.CacheKey, false); err == nil {
						queue.Enqueue(jobs.Task{Type: "ai_analyze", AssetID: item.AssetID, Priority: 100})
						enqueueBudget--
					}
				}
				type stageCandidate struct{ item db.AIBackfillItem }
				candidates := make([]stageCandidate, 0, prepareBudget)
				for _, item := range items {
					stage := stages[item.AssetID]
					if stage != nil && stage.State == "ready" {
						enqueueStage(item, stage)
						continue
					}
					if stager == nil || len(candidates) >= prepareBudget {
						continue
					}
					if sources != nil {
						if available, _ := sources.CachedAvailableForRel(item.RelPath); !available {
							continue
						}
					}
					candidates = append(candidates, stageCandidate{item: item})
				}
				if len(candidates) > 0 {
					type stageResult struct {
						item  db.AIBackfillItem
						stage *db.AIStage
						err   error
					}
					batchCtx, cancelBatch := context.WithCancelCause(ctx)
					work := make(chan stageCandidate)
					results := make(chan stageResult)
					var workers sync.WaitGroup
					for range min(2, len(candidates)) {
						workers.Add(1)
						go func() {
							defer workers.Done()
							for candidate := range work {
								asset, assetErr := database.GetAsset(batchCtx, candidate.item.AssetID)
								if assetErr != nil {
									results <- stageResult{item: candidate.item, err: assetErr}
									continue
								}
								stage, stageErr := prepareAIStageWithPlayback(batchCtx, queue, stager, asset)
								results <- stageResult{item: candidate.item, stage: stage, err: stageErr}
							}
						}()
					}
					go func() {
						defer close(work)
						for _, candidate := range candidates {
							select {
							case work <- candidate:
							case <-batchCtx.Done():
								return
							}
						}
					}()
					go func() {
						workers.Wait()
						close(results)
					}()
					for result := range results {
						if result.err != nil {
							if errors.Is(result.err, context.Canceled) || errors.Is(result.err, jobs.ErrTaskStopped) || errors.Is(result.err, jobs.ErrPlaybackPriority) || errors.Is(result.err, jobs.ErrMediaScanPriority) || errors.Is(result.err, jobs.ErrMediaCachePriority) || ctx.Err() != nil {
								if stageBatchErr == nil {
									stageBatchErr = result.err
								}
								cancelBatch(result.err)
								continue
							}
							logger.Warn("prepare AI staging failed", "assetID", result.item.AssetID, "error", result.err)
							sourceUnavailable := storage.IsSourceUnavailable(result.err)
							if sources != nil {
								libraryRoot, _ := database.ScanLibraryRootForPath(ctx, result.item.RelPath)
								sourceUnavailable = sources.AssetReadErrorIsSourceUnavailable(result.item.RelPath, result.err, libraryRoot)
							}
							if sourceUnavailable {
								if stageBatchErr == nil {
									stageBatchErr = result.err
								}
								cancelBatch(result.err)
								continue
							}
							_ = database.EnsureAIQueued(ctx, result.item.AssetID, result.item.CacheKey, false)
							_, _ = database.MarkAIProcessing(ctx, result.item.AssetID, result.item.CacheKey)
							_, _ = database.MarkAIFailed(ctx, result.item.AssetID, result.item.CacheKey, "AI 输入准备失败："+result.err.Error())
							continue
						}
						enabled, enabledErr := database.AIExecutionEnabled(ctx)
						if enabledErr != nil || !enabled {
							stager.Remove(context.Background(), result.stage)
							if enabledErr != nil && stageBatchErr == nil {
								stageBatchErr = enabledErr
							}
							cancelBatch(context.Canceled)
							continue
						}
						if readyBytes+preparedBytes+result.stage.SizeBytes > aiworker.StageBatchMaxBytes {
							stager.Remove(context.Background(), result.stage)
							continue
						}
						preparedBytes += result.stage.SizeBytes
						preparedCount++
						enqueueStage(result.item, result.stage)
					}
					cancelBatch(nil)
				}
				if stageBatchID > 0 {
					state, message := "success", ""
					if errors.Is(stageBatchErr, jobs.ErrMediaScanPriority) {
						state, message = "preempted", "媒体扫描已抢占 AI 输入准备"
					} else if errors.Is(stageBatchErr, jobs.ErrMediaCachePriority) {
						state, message = "preempted", "缩略图或视频缓存已抢占 AI 输入准备"
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

func maintainExternalAIPrefetch(ctx context.Context, database *db.DB, queue *jobs.Manager, processor *aiworker.Processor, logger *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		settings, err := database.GetAISettings(ctx)
		if err == nil && (settings.AutoAnalyze || settings.ManualRun) && !debugcontrol.BackgroundProcessingPaused() {
			mediaCacheActive, _ := queue.MediaCachePriorityActive(ctx)
			playbackActive, _ := queue.PlaybackPriorityActive(ctx)
			if !jobs.MediaScanPriorityActive() && !mediaCacheActive && !playbackActive {
				nodes, nodeErr := aiworker.ComputeNodes(settings, processor.BaseURL, processor.ExternalToken)
				node, hasExternal := processor.ExternalNode(nodes)
				if nodeErr == nil && hasExternal {
					prefetchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
					status, statusErr := processor.PrefetchStatus(prefetchCtx, node)
					cancel()
					if statusErr == nil {
						items, itemsErr := database.AIBackfillBatch(ctx, 64)
						if itemsErr == nil {
							stages, stagesErr := database.AIStagesForBackfill(ctx, items)
							if stagesErr == nil {
								eligible := make(map[string]struct{}, len(items))
								for _, item := range items {
									stage := stages[item.AssetID]
									if stage != nil && stage.State == "ready" {
										eligible[item.CacheKey] = struct{}{}
									}
								}
								present := make(map[string]struct{}, len(status.CacheKeys))
								for _, key := range status.CacheKeys {
									if _, ok := eligible[key]; ok {
										present[key] = struct{}{}
										continue
									}
									discardCtx, discardCancel := context.WithTimeout(ctx, 500*time.Millisecond)
									_ = processor.DiscardPrefetchedBundleAt(discardCtx, node, key)
									discardCancel()
								}
								for _, item := range items {
									if len(present) >= status.Capacity {
										break
									}
									stage := stages[item.AssetID]
									if stage == nil || stage.State != "ready" {
										continue
									}
									if _, exists := present[item.CacheKey]; exists {
										continue
									}
									if enabled, enabledErr := database.AIExecutionEnabled(ctx); enabledErr != nil || !enabled {
										break
									}
									uploadCtx, uploadCancel := context.WithTimeout(ctx, 2*time.Minute)
									uploadErr := processor.PrefetchStage(uploadCtx, node, item.AssetID, item.CacheKey, stage)
									uploadCancel()
									if uploadErr != nil {
										if logger != nil {
											logger.Debug("prefetch external AI input failed", "assetID", item.AssetID, "error", uploadErr)
										}
										break
									}
									present[item.CacheKey] = struct{}{}
								}
							}
						}
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
	if jobs.MediaScanPriorityActive() {
		return nil, jobs.ErrMediaScanPriority
	}
	if active, _ := queue.MediaCachePriorityActive(ctx); active {
		return nil, jobs.ErrMediaCachePriority
	}
	if active, _ := queue.PlaybackPriorityActive(ctx); active {
		return nil, jobs.ErrPlaybackPriority
	}
	if enabled, err := stager.DB.AIExecutionEnabled(ctx); err != nil {
		return nil, err
	} else if !enabled {
		return nil, jobs.ErrTaskStopped
	}
	stageCtx, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			enabled, enabledErr := stager.DB.AIExecutionEnabled(stageCtx)
			if enabledErr == nil && !enabled {
				cancel(jobs.ErrTaskStopped)
				return
			}
			if debugcontrol.BackgroundProcessingPaused() {
				cancel(debugcontrol.ErrBackgroundProcessingPaused)
				return
			}
			if jobs.MediaScanPriorityActive() {
				cancel(jobs.ErrMediaScanPriority)
				return
			}
			mediaCacheActive, mediaCacheErr := queue.MediaCachePriorityActive(stageCtx)
			if mediaCacheErr == nil && mediaCacheActive {
				cancel(jobs.ErrMediaCachePriority)
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
