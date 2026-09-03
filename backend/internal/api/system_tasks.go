package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/scanner"
)

type SystemTaskProgressDTO struct {
	Total      int `json:"total"`
	Completed  int `json:"completed"`
	Queued     int `json:"queued"`
	Pending    int `json:"pending"`
	Processing int `json:"processing"`
	Failed     int `json:"failed"`
}

type SystemTaskActionDTO struct {
	ID                   string `json:"id"`
	Label                string `json:"label"`
	Kind                 string `json:"kind"`
	Enabled              bool   `json:"enabled"`
	RequiresConfirmation bool   `json:"requiresConfirmation"`
}

type SystemTaskFailureDTO struct {
	AssetID int64  `json:"assetId,omitempty"`
	Path    string `json:"path"`
	Reason  string `json:"reason"`
}

type SystemTaskDTO struct {
	ID                    string                 `json:"id"`
	Name                  string                 `json:"name"`
	Description           string                 `json:"description"`
	Schedule              string                 `json:"schedule"`
	Status                string                 `json:"status"`
	Succeeded             *bool                  `json:"succeeded"`
	LastStartedAt         *int64                 `json:"lastStartedAt"`
	LastFinishedAt        *int64                 `json:"lastFinishedAt"`
	NextRunAt             *int64                 `json:"nextRunAt"`
	DurationSeconds       *int64                 `json:"durationSeconds"`
	AverageSecondsPerItem *float64               `json:"averageSecondsPerItem"`
	Message               string                 `json:"message"`
	BlockedReason         string                 `json:"blockedReason,omitempty"`
	LastError             string                 `json:"lastError"`
	Processed             int                    `json:"processed"`
	FailedCount           int64                  `json:"failedCount"`
	CanRetry              bool                   `json:"canRetry"`
	SupportsScope         bool                   `json:"supportsScope"`
	Progress              *SystemTaskProgressDTO `json:"progress"`
	Actions               []SystemTaskActionDTO  `json:"actions"`
	Failures              []SystemTaskFailureDTO `json:"failures"`
}

func (s *Server) systemTasks(w http.ResponseWriter, r *http.Request) {
	mediaScanRun, err := s.db.LastScanRunForTask(r.Context(), "media_scan")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tasks_failed", "读取媒体扫描任务失败")
		return
	}
	reconcileRun, err := s.db.LastScanRunForTask(r.Context(), "reconcile")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tasks_failed", "读取图库对账任务失败")
		return
	}
	progress, err := s.db.ProcessingProgress(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tasks_failed", "读取媒体处理进度失败")
		return
	}
	thumbActivity, _ := s.db.LatestMediaJobTask(r.Context(), "thumb")
	previewActivity, _ := s.db.LatestMediaJobTask(r.Context(), "preview")
	posterActivity, _ := s.db.LatestMediaJobTask(r.Context(), "video_poster")
	storyboardActivity, _ := s.db.LatestMediaJobTask(r.Context(), "storyboard")
	aiHealth, _ := s.db.SystemTaskState(r.Context(), db.SystemTaskAIHealth)
	storageHealth, _ := s.db.SystemTaskState(r.Context(), taskStorageHealth)
	cacheCleanup, _ := s.db.SystemTaskState(r.Context(), taskCacheCleanup)
	duplicateScanState, _ := s.db.SystemTaskState(r.Context(), taskDuplicateScan)
	sourceIO, _ := s.db.LatestSourceIOBatch(r.Context())
	thumbnailControl, _ := s.db.SystemTaskState(r.Context(), "thumbnail_creation")
	previewControl, _ := s.db.SystemTaskState(r.Context(), "preview_creation")
	posterControl, _ := s.db.SystemTaskState(r.Context(), "video_poster_creation")
	storyboardControl, _ := s.db.SystemTaskState(r.Context(), "storyboard_creation")
	aiControl, _ := s.db.SystemTaskState(r.Context(), "ai_analysis")
	aiStatus, err := s.db.AIStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tasks_failed", "读取 AI 分析任务失败")
		return
	}
	aiSettings, _ := s.db.GetAISettings(r.Context())
	aiActivity, _ := s.db.AIActivity(r.Context())
	mediaAverages, _ := s.db.MediaJobAverageSecondsPerItem(r.Context(), []string{"thumb", "preview", "video_poster", "storyboard"})
	aiAverage, _ := s.db.AIAverageSecondsPerItem(r.Context())
	queue := jobs.QueueStats{}
	executorHealth := jobs.ExecutorHealth{Healthy: true}
	if s.jobs != nil {
		queue = s.jobs.Stats()
		executorHealth = s.jobs.ExecutorHealth(r.Context())
	}
	scanStatus, _ := s.scanner.Status(r.Context())
	duplicateTotal, duplicateCompleted, err := s.db.DuplicateHashProgress(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tasks_failed", "读取重复文件扫描进度失败")
		return
	}

	thumbnailTask := thumbnailSystemTask(progress.Thumb, queue.ThumbQueued, queue.ActiveThumb)
	previewTask := aggregateMediaTask("preview_creation", "高清预览创建", "为浏览器无法直接显示的图片创建高清预览", "媒体入库后自动运行", progress.Preview, queue.PreviewQueued, queue.ActivePreview)
	posterTask := aggregateMediaTask("video_poster_creation", "视频封面创建", "创建视频播放前显示的首帧封面", "媒体入库后自动运行", progress.VideoPoster, queue.VideoPosterQueued, queue.ActiveVideoPoster)
	storyboardProgress, _ := s.db.WorkProgress(r.Context(), "storyboard", nil)
	storyboardTask := aggregateMediaTask("storyboard_creation", "进度预览图创建", "为视频进度条创建鼠标悬停预览图", "手动扫描后自动运行", storyboardProgress, queue.StoryboardQueued, queue.ActiveStoryboard)
	mediaScanTask := mediaScanSystemTask(mediaScanRun, scanStatus)
	attachSummaryFailure(&mediaScanTask)
	thumbnailTask.AverageSecondsPerItem = float64MapValue(mediaAverages, "thumb")
	previewTask.AverageSecondsPerItem = float64MapValue(mediaAverages, "preview")
	posterTask.AverageSecondsPerItem = float64MapValue(mediaAverages, "video_poster")
	storyboardTask.AverageSecondsPerItem = float64MapValue(mediaAverages, "storyboard")
	applyMediaJobActivity(&thumbnailTask, thumbActivity)
	applyMediaJobActivity(&previewTask, previewActivity)
	applyMediaJobActivity(&posterTask, posterActivity)
	applyMediaJobActivity(&storyboardTask, storyboardActivity)
	_ = thumbnailControl
	_ = previewControl
	_ = posterControl
	_ = storyboardControl
	applyAutomaticQueueState(&thumbnailTask, queue.ThumbQueued, queue.ActiveThumb, executorHealth.BlockedReason)
	applyAutomaticQueueState(&previewTask, queue.PreviewQueued, queue.ActivePreview, executorHealth.BlockedReason)
	applyAutomaticQueueState(&posterTask, queue.VideoPosterQueued, queue.ActiveVideoPoster, executorHealth.BlockedReason)
	applyAutomaticQueueState(&storyboardTask, queue.StoryboardQueued, queue.ActiveStoryboard, executorHealth.BlockedReason)
	aiTask := aiAnalysisSystemTask(aiStatus, aiActivity, queue.AIQueued, queue.ActiveAI, aiSettings.AutoAnalyze || aiSettings.ManualRun)
	aiTask.AverageSecondsPerItem = aiAverage
	_ = aiControl
	aiStatus.Queued = queue.AIQueued
	aiStatus.Active = queue.ActiveAI
	aiStatus.Staged, aiStatus.StagedBytes, _ = s.db.AIStageStats(r.Context())
	if running, batchErr := s.db.HasRunningSourceIOBatch(r.Context(), "ai_stage_batch"); batchErr == nil {
		aiStatus.SourceReading = running
	}
	aiTask.BlockedReason = s.aiPauseReason(r.Context(), aiStatus, aiSettings)
	applyAIExecutionState(&aiTask, aiStatus, aiSettings, aiTask.BlockedReason)
	applyRunningTaskDuration(&aiTask, aiControl, time.Now().Unix())
	attachMediaFailures(r, s, &mediaScanTask, "metadata")
	attachMediaFailures(r, s, &thumbnailTask, "thumb")
	attachMediaFailures(r, s, &previewTask, "preview")
	attachMediaFailures(r, s, &posterTask, "video_poster")
	attachMediaFailures(r, s, &storyboardTask, "storyboard")
	attachAIFailures(r, s, &aiTask)
	scanTask := scanSystemTask("library_scan", "媒体库检查", "仅检查数据库现有媒体对应的文件路径是否仍然存在", reconcileRun, scanStatus)
	attachSummaryFailure(&scanTask)
	duplicateTask := duplicateScanSystemTask(
		duplicateScanState,
		duplicateTotal,
		duplicateCompleted,
		s.duplicateScanFailureCount(),
		s.duplicateScanIsRunning(),
	)
	duplicateTask.Failures = s.duplicateScanFailures()
	attachSummaryFailure(&duplicateTask)
	storageTask := storedSystemTask(taskStorageHealth, "存储连接检查", "在集中读取窗口检查每个图库根目录及抽样媒体是否可访问", "每天 03:00 及手动任务前", storageHealth, false)
	attachSummaryFailure(&storageTask)
	aiHealthTask := aiHealthSystemTask(aiHealth)
	attachSummaryFailure(&aiHealthTask)
	cacheTask := storedSystemTask(taskCacheCleanup, "缓存清理", "空间不足时按最近访问时间回收播放缓存，并清除 AI 暂存和中断残留；保留缩略图、视频封面与进度预览图", "持续监控；每天 03:00 深度检查", cacheCleanup, false)
	attachSummaryFailure(&cacheTask)
	executorTask := taskExecutorSystemTask(executorHealth)
	sourceIOTask := sourceIOSystemTask(sourceIO)
	nasWatcherTask := s.nasWatcherSystemTask()
	items := []SystemTaskDTO{
		sourceIOTask,
		nasWatcherTask,
		scanTask,
		mediaScanTask,
		duplicateTask,
		thumbnailTask,
		previewTask,
		posterTask,
		storyboardTask,
		aiTask,
		storageTask,
		aiHealthTask,
		cacheTask,
		executorTask,
	}
	for index := range items {
		items[index].Actions = automaticFailureRetryActions(items[index])
		items[index].SupportsScope = false
		if items[index].Failures == nil {
			items[index].Failures = []SystemTaskFailureDTO{}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func automaticFailureRetryActions(item SystemTaskDTO) []SystemTaskActionDTO {
	failed := int(item.FailedCount)
	if item.Progress != nil && item.Progress.Failed > failed {
		failed = item.Progress.Failed
	}
	if len(item.Failures) > failed {
		failed = len(item.Failures)
	}
	if failed <= 0 {
		return []SystemTaskActionDTO{}
	}
	switch item.ID {
	case "media_scan", "library_scan", "thumbnail_creation", "preview_creation", "video_poster_creation", "storyboard_creation", "ai_analysis", taskDuplicateScan, taskStorageHealth, "ai_health_check", taskCacheCleanup:
		return []SystemTaskActionDTO{{
			ID: "retry_failed", Label: fmt.Sprintf("重试失败（%d）", failed), Kind: "secondary", Enabled: true,
		}}
	default:
		return []SystemTaskActionDTO{}
	}
}

func mediaScanSystemTask(run *model.ScanRun, status scanner.Status) SystemTaskDTO {
	liveTask := status.Progress.Task == "media_scan" || status.Progress.Task == "metadata"
	item := SystemTaskDTO{
		ID: "media_scan", Name: "媒体扫描", Description: "扫描图库路径，将新增和变更媒体写入数据库；完成后按需触发后续处理任务",
		Schedule: "实时监听；每天 03:00 完整检查", Status: "never", SupportsScope: false,
	}
	if run != nil {
		applyScanRun(&item, run)
		applyFinishedScanBlockedReason(&item, run)
		item.Progress = &SystemTaskProgressDTO{Total: run.TotalSeen, Completed: run.TotalSeen, Failed: run.Errors}
		item.Message = fmt.Sprintf("扫描 %d 项，新增 %d，变更 %d，缺失 %d", run.TotalSeen, run.AssetsAdded, run.AssetsUpdated, run.AssetsDeleted)
		if run.Status == "running" && (!status.Running || !liveTask) {
			item.Status = "stopped"
			item.Succeeded = boolValue(false)
			item.Message = "媒体扫描当前未运行"
		}
	}
	if status.Running && liveTask {
		item.Status = "running"
		item.Succeeded = nil
		item.LastFinishedAt = nil
		lastStartedAt := status.LastStart
		item.LastStartedAt = &lastStartedAt
		total := maxInt(status.Progress.TotalFiles, status.Progress.DiscoveredFiles)
		completed := maxInt(status.Progress.ScannedFiles, status.Progress.TotalSeen)
		processing := 0
		pending := maxInt(0, total-completed)
		if status.Progress.Phase == "finalizing" {
			processing = 1
		} else if pending > 0 {
			processing = 1
			pending--
		}
		item.Progress = &SystemTaskProgressDTO{Total: total, Completed: completed, Pending: pending, Processing: processing, Failed: status.Progress.Errors}
		item.Message = fmt.Sprintf("已扫描并入库 %d / %d，新增 %d，变更 %d，缺失 %d", completed, total, status.Progress.AssetsAdded, status.Progress.AssetsUpdated, status.Progress.AssetsDeleted)
		item.Processed = completed
	}
	waitingTask := "media_scan"
	if liveTask {
		waitingTask = status.Progress.Task
	}
	waitingToResume := applyLiveScanBlockedReason(&item, status, waitingTask)
	failed := int(item.FailedCount)
	item.Actions = []SystemTaskActionDTO{}
	if item.Status == "running" || waitingToResume {
		item.Actions = append(item.Actions, SystemTaskActionDTO{ID: "stop", Label: "停止", Kind: "danger", Enabled: true})
	} else {
		item.Actions = append(item.Actions, SystemTaskActionDTO{ID: "continue", Label: "继续", Kind: "primary", Enabled: true})
	}
	if failed > 0 {
		item.Actions = append(item.Actions, SystemTaskActionDTO{ID: "retry_failed", Label: fmt.Sprintf("重试失败（%d）", failed), Kind: "secondary", Enabled: true})
	}
	item.Actions = append(item.Actions,
		SystemTaskActionDTO{ID: "rebuild", Label: "全部重建", Kind: "danger", Enabled: item.Status != "running" && !waitingToResume, RequiresConfirmation: true},
	)
	return item
}

func applyLiveScanBlockedReason(item *SystemTaskDTO, status scanner.Status, task string) bool {
	if item == nil || status.Progress.Task != task || status.Progress.PauseReason == "" {
		return false
	}
	item.Status, item.Succeeded = "pending", nil
	switch status.Progress.PauseReason {
	case "playback":
		item.BlockedReason = "正在播放或加载媒体，任务暂时暂停；播放结束后将自动继续"
	default:
		item.BlockedReason = "任务暂时暂停，原因：" + status.Progress.PauseReason
	}
	return true
}

func applyFinishedScanBlockedReason(item *SystemTaskDTO, run *model.ScanRun) {
	if item == nil || run == nil || run.LastError == nil {
		return
	}
	switch run.Status {
	case "paused", "interrupted", "skipped":
		item.BlockedReason = *run.LastError
	}
}

func duplicateScanSystemTask(state *db.SystemTaskState, total int, completed int, failed int, running bool) SystemTaskDTO {
	item := storedSystemTask(
		taskDuplicateScan,
		"重复文件扫描",
		"仅对文件大小相同的媒体计算内容哈希，并让智能页面使用同一结果识别重复文件",
		"手动运行；结果持久保存，新媒体入库后可再次增量扫描",
		state,
		false,
	)
	if total < 0 {
		total = 0
	}
	if completed < 0 {
		completed = 0
	}
	if completed > total {
		completed = total
	}
	remaining := total - completed
	if failed < 0 {
		failed = 0
	}
	if failed > remaining {
		failed = remaining
	}
	processing := 0
	if running && remaining > failed {
		processing = 1
	}
	pending := maxInt(0, remaining-failed-processing)
	item.Progress = &SystemTaskProgressDTO{
		Total: total, Completed: completed, Pending: pending, Processing: processing, Failed: failed,
	}
	item.Processed = completed
	item.FailedCount = int64(failed)
	item.CanRetry = remaining > 0
	if running {
		item.Status = "running"
		item.Succeeded = nil
		item.LastFinishedAt = nil
	} else if item.Status == "running" {
		item.Status = "stopped"
		item.Succeeded = boolValue(false)
		item.Message = "服务重启后扫描已停止，可重新开始"
	}
	if running {
		item.Actions = []SystemTaskActionDTO{{ID: "stop", Label: "停止", Kind: "danger", Enabled: true}}
	} else if remaining > 0 {
		item.Actions = []SystemTaskActionDTO{{ID: "scan", Label: "开始扫描", Kind: "primary", Enabled: true}}
	} else {
		item.Actions = []SystemTaskActionDTO{}
	}
	applyRunningTaskDuration(&item, state, time.Now().Unix())
	return item
}

func sourceIOSystemTask(batch *db.SourceIOBatch) SystemTaskDTO {
	item := SystemTaskDTO{
		ID: "source_io_scheduler", Name: "存储读取调度",
		Description: "集中安排所有 NAS 源文件读取；播放会抢占后台任务",
		Schedule:    "每天 03:00、手动任务及媒体播放", Status: "never",
		Actions: []SystemTaskActionDTO{}, Failures: []SystemTaskFailureDTO{},
	}
	if batch == nil {
		item.Message = "尚未产生 NAS 读取"
		return item
	}
	item.LastStartedAt = &batch.StartedAt
	item.LastFinishedAt = batch.FinishedAt
	item.Processed = batch.ItemCount
	item.Message = fmt.Sprintf("%s · %d 项 · 读取 %d 字节", sourceIOReasonLabel(batch.Reason), batch.ItemCount, batch.BytesRead)
	switch batch.State {
	case "running":
		item.Status = "running"
	case "success":
		item.Status = "success"
		ok := true
		item.Succeeded = &ok
	case "preempted":
		item.Status = "stopped"
		item.Message = "已被当前媒体播放抢占；后台项目已恢复等待"
	default:
		item.Status = "failed"
		ok := false
		item.Succeeded = &ok
		item.LastError = batch.Error
	}
	if batch.FinishedAt != nil {
		duration := *batch.FinishedAt - batch.StartedAt
		if duration < 0 {
			duration = 0
		}
		item.DurationSeconds = &duration
	}
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	nextUnix := next.Unix()
	item.NextRunAt = &nextUnix
	return item
}

func sourceIOReasonLabel(reason string) string {
	switch reason {
	case "ai_stage_batch":
		return "AI 批量准备"
	case "video_playback":
		return "当前视频播放"
	case "viewer_image":
		return "当前图片加载"
	case "daily_source_window":
		return "每日集中读取"
	default:
		return reason
	}
}

func attachMediaFailures(r *http.Request, s *Server, item *SystemTaskDTO, jobType string) {
	if item.FailedCount <= 0 {
		return
	}
	failures, err := s.db.MediaJobFailures(r.Context(), jobType, 100)
	if err != nil {
		return
	}
	item.Failures = make([]SystemTaskFailureDTO, 0, len(failures))
	for _, failure := range failures {
		item.Failures = append(item.Failures, SystemTaskFailureDTO{
			AssetID: failure.AssetID,
			Path:    failure.RelPath,
			Reason:  readableTaskError(failure.Reason),
		})
	}
	if len(item.Failures) == 0 {
		attachSummaryFailure(item)
	}
}

func attachAIFailures(r *http.Request, s *Server, item *SystemTaskDTO) {
	if item.FailedCount <= 0 {
		return
	}
	failures, err := s.db.AIAnalysisFailures(r.Context(), 100)
	if err != nil {
		return
	}
	item.Failures = make([]SystemTaskFailureDTO, 0, len(failures))
	for _, failure := range failures {
		item.Failures = append(item.Failures, SystemTaskFailureDTO{
			AssetID: failure.AssetID,
			Path:    failure.RelPath,
			Reason:  readableTaskError(failure.Reason),
		})
	}
	if len(item.Failures) == 0 {
		attachSummaryFailure(item)
	}
}

func attachSummaryFailure(item *SystemTaskDTO) {
	if item.LastError == "" {
		return
	}
	item.Failures = []SystemTaskFailureDTO{{Reason: item.LastError}}
}

func aggregateMediaTask(id, name, description, schedule string, counts db.WorkStatusCounts, queued, active int) SystemTaskDTO {
	required := maxInt(0, counts.Total-counts.NotRequired)
	processing := counts.Processing
	if processing > active {
		processing = active
	}
	pending := counts.Pending + maxInt(0, counts.Processing-processing)
	item := SystemTaskDTO{
		ID: id, Name: name, Description: description, Schedule: schedule, SupportsScope: true,
		Processed: counts.Ready, FailedCount: int64(counts.Error),
		Progress: &SystemTaskProgressDTO{Total: required, Completed: counts.Ready, Queued: queued, Pending: pending, Processing: processing, Failed: counts.Error},
		Message:  fmt.Sprintf("已处理 %d，待处理 %d，处理中 %d，失败 %d", counts.Ready, maxInt(pending, queued), processing, counts.Error),
	}
	switch {
	case active > 0:
		item.Status = "running"
	case queued > 0 || pending > 0:
		item.Status = "pending"
	case counts.Error > 0 && counts.Ready > 0:
		item.Status, item.Succeeded = "warning", boolValue(false)
	case counts.Error > 0:
		item.Status, item.Succeeded = "failed", boolValue(false)
	case required == 0:
		item.Status = "never"
	default:
		item.Status, item.Succeeded = "success", boolValue(true)
	}
	return item
}

func applyAutomaticQueueState(item *SystemTaskDTO, queued, active int, blocker string) {
	if item == nil || queued == 0 || active > 0 {
		return
	}
	item.Status, item.Succeeded = "pending", nil
	switch blocker {
	case "media_scan":
		item.BlockedReason = "媒体扫描优先，任务会自动继续"
	case "playback", "foreground":
		item.BlockedReason = "当前播放优先，任务会自动继续"
	case "storyboard":
		item.BlockedReason = "进度预览图优先，任务会自动继续"
	case "load":
		item.BlockedReason = "系统负载较高，任务会自动继续"
	case "memory":
		item.BlockedReason = "可用内存不足，任务会自动继续"
	default:
		item.BlockedReason = "等待执行器调度；系统正在自动检查"
	}
}

func taskExecutorSystemTask(health jobs.ExecutorHealth) SystemTaskDTO {
	item := SystemTaskDTO{
		ID: "task_executor_health", Name: "任务执行器自检", Description: "持续检查队列、执行器和阻塞原因，并识别队列无人处理的异常状态",
		Schedule: "每 10 秒", Status: "success", Succeeded: boolValue(true),
		Progress: &SystemTaskProgressDTO{Total: health.Queued + health.Active, Queued: health.Queued, Processing: health.Active},
	}
	switch {
	case !health.Healthy:
		item.Status, item.Succeeded = "failed", boolValue(false)
		item.Message = fmt.Sprintf("队列中有 %d 项，但 %d 秒内没有执行器取出任务", health.Queued, health.StalledSeconds)
		item.LastError = item.Message
	case health.Active > 0:
		item.Status, item.Succeeded = "running", nil
		item.Message = fmt.Sprintf("%d 个任务正在处理，%d 项等待", health.Active, health.Queued)
	case health.BlockedReason != "":
		item.Status, item.Succeeded = "pending", nil
		item.Message = automaticTaskBlockerMessage(health.BlockedReason, health.Queued)
	case health.Queued > 0:
		item.Status, item.Succeeded = "pending", nil
		item.Message = fmt.Sprintf("%d 项正在等待执行器调度", health.Queued)
	default:
		item.Message = "队列和执行器正常"
	}
	return item
}

func automaticTaskBlockerMessage(reason string, queued int) string {
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
	default:
		return fmt.Sprintf("%d 项自动等待", queued)
	}
}

func standardMediaTaskActions(running bool, continuable, failed, total int) []SystemTaskActionDTO {
	actions := make([]SystemTaskActionDTO, 0, 3)
	if running {
		actions = append(actions, SystemTaskActionDTO{ID: "stop", Label: "停止", Kind: "danger", Enabled: true})
	} else if continuable > 0 {
		actions = append(actions, SystemTaskActionDTO{ID: "continue", Label: "继续", Kind: "primary", Enabled: true})
	}
	if failed > 0 {
		actions = append(actions, SystemTaskActionDTO{ID: "retry_failed", Label: fmt.Sprintf("重试失败（%d）", failed), Kind: "secondary", Enabled: true})
	}
	if total > 0 {
		actions = append(actions, SystemTaskActionDTO{ID: "rebuild", Label: "全部重建", Kind: "danger", Enabled: !running, RequiresConfirmation: true})
	}
	return actions
}

func manualStartTaskActions(running bool, actionable, failed, total int) []SystemTaskActionDTO {
	actions := make([]SystemTaskActionDTO, 0, 3)
	if running {
		actions = append(actions, SystemTaskActionDTO{ID: "stop", Label: "停止", Kind: "danger", Enabled: true})
	} else if actionable > 0 {
		actions = append(actions, SystemTaskActionDTO{ID: "start", Label: "手动开始", Kind: "primary", Enabled: true})
	}
	if failed > 0 {
		actions = append(actions, SystemTaskActionDTO{ID: "retry_failed", Label: fmt.Sprintf("重试失败（%d）", failed), Kind: "secondary", Enabled: true})
	}
	if total > 0 {
		actions = append(actions, SystemTaskActionDTO{ID: "rebuild", Label: "全部重建", Kind: "danger", Enabled: !running, RequiresConfirmation: true})
	}
	return actions
}

func thumbnailSystemTask(counts db.WorkStatusCounts, queued, active int) SystemTaskDTO {
	return aggregateMediaTask("thumbnail_creation", "缩略图创建", "为瀑布流和媒体列表创建缩略图", "媒体入库后自动运行", counts, queued, active)
}

func applyMediaJobActivity(item *SystemTaskDTO, state *db.MediaJobTaskState) {
	if state == nil {
		return
	}
	item.LastStartedAt = state.StartedAt
	item.LastFinishedAt = state.FinishedAt
	if item.Status == "failed" && state.Message != nil {
		item.LastError = readableMediaJobError(state)
	}
	setTaskDuration(item)
}

func applyStoppedState(item *SystemTaskDTO, state *db.SystemTaskState) {
	if state == nil || state.Status != "stopped" || item.Status == "running" {
		return
	}
	item.Status, item.Succeeded = "stopped", boolValue(false)
	if state.Message != nil {
		item.Message = *state.Message
	}
}

func scanSystemTask(id, name, description string, run *model.ScanRun, status scanner.Status) SystemTaskDTO {
	item := SystemTaskDTO{ID: id, Name: name, Description: description, Schedule: "每天 03:00 自动运行", Status: "never", SupportsScope: false}
	if run != nil {
		applyScanRun(&item, run)
		applyFinishedScanBlockedReason(&item, run)
		item.Progress = &SystemTaskProgressDTO{Total: run.TotalSeen, Completed: run.TotalSeen, Failed: run.Errors}
		item.Message = fmt.Sprintf("已核对 %d 项，缺失 %d", run.TotalSeen, run.AssetsDeleted)
	}
	if status.Running && status.Progress.Task == "reconcile" {
		item.Status = "running"
		total := maxInt(status.Progress.TotalFiles, status.Progress.DiscoveredFiles)
		completed := maxInt(status.Progress.ScannedFiles, status.Progress.TotalSeen)
		item.Progress = &SystemTaskProgressDTO{Total: total, Completed: completed, Pending: maxInt(0, total-completed), Failed: status.Progress.Errors}
		item.Message = fmt.Sprintf("已核对 %d / %d，缺失 %d", completed, total, status.Progress.AssetsDeleted)
	}
	waitingToResume := applyLiveScanBlockedReason(&item, status, "reconcile")
	if item.Status == "running" || waitingToResume {
		item.Actions = []SystemTaskActionDTO{{ID: "stop", Label: "停止", Kind: "danger", Enabled: true}}
	} else {
		item.Actions = []SystemTaskActionDTO{{ID: "reconcile", Label: "开始对账", Kind: "primary", Enabled: true}}
	}
	return item
}

func applyScanRun(item *SystemTaskDTO, run *model.ScanRun) {
	item.LastStartedAt = &run.StartedAt
	item.LastFinishedAt = run.FinishedAt
	item.Status, item.Succeeded = scanTaskResult(run.Status)
	item.Processed = run.TotalSeen
	item.FailedCount = int64(run.Errors)
	item.Message = fmt.Sprintf("处理 %d 项，新增 %d，变更 %d，缺失 %d", run.TotalSeen, run.AssetsAdded, run.AssetsUpdated, run.AssetsDeleted)
	if run.LastError != nil {
		item.LastError = *run.LastError
	}
	setTaskDuration(item)
}

func aiAnalysisSystemTask(status db.AIStatus, activity db.AIActivity, queued, active int, runRequested bool) SystemTaskDTO {
	item := SystemTaskDTO{
		ID: "ai_analysis", Name: "AI 媒体分析", Description: "为图片和视频生成描述与标签", Schedule: "媒体入库后自动运行",
		LastStartedAt: activity.LastStartedAt, LastFinishedAt: activity.LastFinishedAt, FailedCount: status.Failed, CanRetry: status.Failed > 0,
		Processed: int(status.Ready), Progress: &SystemTaskProgressDTO{Total: int(status.Total), Completed: int(status.Ready), Queued: queued, Pending: int(status.Pending + status.Stale), Processing: int(status.Processing), Failed: int(status.Failed)},
		Message: fmt.Sprintf("已完成 %d / %d，失败 %d，待分析 %d", status.Ready, status.Total, status.Failed, status.Pending+status.Stale),
	}
	switch {
	case active > 0 || status.Processing > 0:
		item.Status = "running"
	case queued > 0 || runRequested && (status.Pending > 0 || status.Stale > 0):
		item.Status = "pending"
	case status.Failed > 0:
		item.Status, item.Succeeded = "failed", boolValue(false)
	case status.Pending > 0 || status.Stale > 0:
		item.Status = "pending"
	default:
		item.Status, item.Succeeded = "success", boolValue(true)
	}
	setTaskDuration(&item)
	return item
}

func applyAIExecutionState(item *SystemTaskDTO, status db.AIStatus, settings db.AISettings, reason string) {
	if item == nil || reason == "" {
		return
	}
	runRequested := settings.AutoAnalyze || settings.ManualRun
	if !runRequested {
		item.Status, item.Succeeded = "stopped", boolValue(false)
		return
	}
	if status.Active == 0 {
		item.Status, item.Succeeded = "pending", nil
	}
}

func storedSystemTask(id, name, description, schedule string, state *db.SystemTaskState, supportsScope bool) SystemTaskDTO {
	item := SystemTaskDTO{ID: id, Name: name, Description: description, Schedule: schedule, Status: "never", SupportsScope: supportsScope}
	if state != nil {
		item.Status, item.LastStartedAt, item.LastFinishedAt = state.Status, state.LastStartedAt, state.LastFinishedAt
		if state.Message != nil {
			item.Message = *state.Message
			if state.Status == "failed" {
				item.LastError = *state.Message
			}
		}
		if state.Status == "success" {
			item.Succeeded = boolValue(true)
		} else if state.Status == "failed" {
			item.Succeeded = boolValue(false)
		}
	}
	setTaskDuration(&item)
	switch id {
	case taskStorageHealth:
		item.Actions = []SystemTaskActionDTO{{ID: "check", Label: "立即检查", Kind: "primary", Enabled: item.Status != "running"}}
	case taskCacheCleanup:
		running := item.Status == "running"
		if running {
			item.Actions = []SystemTaskActionDTO{{ID: "stop", Label: "停止", Kind: "danger", Enabled: true}}
		} else {
			item.Actions = []SystemTaskActionDTO{{ID: "cleanup", Label: "立即清理", Kind: "danger", Enabled: true, RequiresConfirmation: true}}
		}
	}
	return item
}

func aiHealthSystemTask(state *db.SystemTaskState) SystemTaskDTO {
	item := storedSystemTask("ai_health_check", "AI 服务检查", "检查 AI 服务健康，并在需要时自动重启", "每 30 分钟", state, false)
	item.Actions = []SystemTaskActionDTO{{ID: "check", Label: "立即检查", Kind: "primary", Enabled: item.Status != "running"}}
	if item.Status == "failed" {
		item.Actions = append(item.Actions, SystemTaskActionDTO{ID: "restart", Label: "重启服务", Kind: "danger", Enabled: true, RequiresConfirmation: true})
	}
	if item.LastStartedAt != nil {
		next := *item.LastStartedAt + 30*60
		item.NextRunAt = &next
	}
	return item
}

func scanTaskResult(status string) (string, *bool) {
	switch status {
	case "running":
		return "running", nil
	case "finished":
		return "success", boolValue(true)
	case "finished_with_errors":
		return "warning", boolValue(false)
	case "paused":
		return "stopped", boolValue(false)
	case "interrupted":
		return "interrupted", boolValue(false)
	default:
		return "failed", boolValue(false)
	}
}

func readableMediaJobError(state *db.MediaJobTaskState) string {
	reason := ""
	if state.Message != nil {
		reason = *state.Message
	}
	message := readableTaskError(reason)
	if state.RelPath != nil && *state.RelPath != "" {
		return message + "：" + *state.RelPath
	}
	return message
}

func readableTaskError(reason string) string {
	if detail, ok := modelOutputErrorDetail(reason); ok {
		return detail
	}
	raw := strings.ToLower(reason)
	message := "处理媒体时发生错误"
	switch {
	case strings.Contains(raw, "model_output_invalid"):
		message = "AI 模型输出格式错误，已自动重新生成 1 次"
	case strings.Contains(raw, "ai_transient:") && (strings.Contains(raw, "model response does not contain") || strings.Contains(raw, "description length")):
		message = "AI 模型输出格式不正确，自动重试一次后仍失败"
	case strings.Contains(raw, "ai_transient:") && (strings.Contains(raw, "server disconnected") || strings.Contains(raw, " eof")):
		message = "AI 描述模型运行中断，自动重试一次后仍失败"
	case strings.Contains(raw, "ai_transient:") && strings.Contains(raw, "connection refused"):
		message = "AI 描述模型临时退出，自动重试一次后仍失败"
	case strings.Contains(raw, "ai_transient:") && (strings.Contains(raw, "timeout") || strings.Contains(raw, "timed out") || strings.Contains(raw, "deadline exceeded")):
		message = "AI 分析超时，自动重试一次后仍失败"
	case strings.Contains(raw, "ai_transient:"):
		message = "AI 分析暂时失败，自动重试一次后仍失败"
	case strings.Contains(raw, "invalid nal unit"), strings.Contains(raw, "error splitting the input"), strings.Contains(raw, "missing picture"), strings.Contains(raw, "moov atom not found"), strings.Contains(raw, "invalid data found"), strings.Contains(raw, "corrupt"):
		if strings.Contains(raw, "ai_media:") || strings.Contains(raw, "ai service") {
			message = "媒体文件损坏或未完整写入，无法提取 AI 分析画面"
		} else {
			message = "无法读取视频数据，文件可能损坏或未完整写入"
		}
	case strings.Contains(raw, "/cache/ai-staging/") && strings.Contains(raw, "unable to open for write"):
		message = "AI 输入缓存写入失败"
	case strings.Contains(raw, "permission denied"):
		message = "没有权限读取媒体文件"
	case strings.Contains(raw, "no such file"), strings.Contains(raw, "not found"):
		message = "找不到源媒体文件"
	case strings.Contains(raw, "unsupported codec"), strings.Contains(raw, "decoder not found"):
		message = "当前系统不支持这个媒体的编码格式"
	case strings.Contains(raw, "does not contain any stream"), strings.Contains(raw, "no streams"):
		if strings.Contains(raw, "ai_media:") || strings.Contains(raw, "ai service") {
			message = "媒体中没有可供 AI 分析的视频画面"
		} else {
			message = "媒体中没有可处理的音视频轨道"
		}
	case strings.Contains(raw, "ai service 500") && strings.Contains(raw, "connection refused"):
		message = "AI 描述模型临时退出，自动重试一次后仍失败"
	case strings.Contains(raw, "ai service unavailable"), strings.Contains(raw, "post \"http://ai:") && strings.Contains(raw, "connection refused"):
		message = "AI 服务无法连接，自动重试一次后仍失败"
	case strings.Contains(raw, "connection refused"):
		message = "AI 描述模型临时退出，自动重试一次后仍失败"
	case strings.Contains(raw, "server disconnected"), strings.HasSuffix(strings.TrimSpace(raw), "eof"):
		message = "AI 描述模型运行中断，自动重试一次后仍失败"
	case strings.Contains(raw, "model response does not contain"), strings.Contains(raw, "description length"):
		message = "AI 模型输出格式不正确，自动重试一次后仍失败"
	case strings.Contains(raw, "ffmpeg"), strings.Contains(raw, "non-zero exit status"):
		if strings.Contains(raw, "ai_media:") || strings.Contains(raw, "ai service") {
			message = "无法从该媒体提取 AI 分析画面"
		} else {
			message = "媒体处理失败，无法生成所需内容"
		}
	case strings.Contains(raw, "timeout"), strings.Contains(raw, "timed out"), strings.Contains(raw, "deadline exceeded"):
		if strings.Contains(raw, "ai") {
			message = "AI 分析超时，自动重试一次后仍失败"
		} else {
			message = "媒体处理超时"
		}
	}
	if strings.TrimSpace(reason) != "" && message == "处理媒体时发生错误" {
		return reason
	}
	return message
}

func modelOutputErrorDetail(reason string) (string, bool) {
	start := strings.Index(reason, "{")
	if start < 0 {
		return "", false
	}
	var envelope struct {
		Detail struct {
			Code         string `json:"code"`
			Message      string `json:"message"`
			ParseError   string `json:"parseError"`
			FinishReason string `json:"finishReason"`
			Output       string `json:"output"`
			Attempts     []struct {
				Attempt      int    `json:"attempt"`
				ParseError   string `json:"parseError"`
				FinishReason string `json:"finishReason"`
				Output       string `json:"output"`
			} `json:"attempts"`
		} `json:"detail"`
	}
	if err := json.Unmarshal([]byte(reason[start:]), &envelope); err != nil || envelope.Detail.Code != "model_output_invalid" {
		return "", false
	}
	lines := []string{"AI 模型输出格式错误，已自动重新生成 1 次"}
	for index, attempt := range envelope.Detail.Attempts {
		number := attempt.Attempt
		if number <= 0 {
			number = index + 1
		}
		if value := strings.TrimSpace(attempt.ParseError); value != "" {
			lines = append(lines, fmt.Sprintf("第 %d 次解析原因：%s", number, value))
		}
		if value := strings.TrimSpace(attempt.FinishReason); value != "" {
			lines = append(lines, fmt.Sprintf("第 %d 次结束原因：%s", number, value))
		}
		if value := strings.TrimSpace(attempt.Output); value != "" {
			lines = append(lines, fmt.Sprintf("第 %d 次模型原始输出：\n%s", number, value))
		}
	}
	if len(envelope.Detail.Attempts) > 0 {
		return strings.Join(lines, "\n"), true
	}
	if value := strings.TrimSpace(envelope.Detail.ParseError); value != "" {
		lines = append(lines, "解析原因："+value)
	}
	if value := strings.TrimSpace(envelope.Detail.FinishReason); value != "" {
		lines = append(lines, "结束原因："+value)
	}
	if value := strings.TrimSpace(envelope.Detail.Output); value != "" {
		lines = append(lines, "模型原始输出：\n"+value)
	}
	return strings.Join(lines, "\n"), true
}

func float64MapValue(values map[string]float64, key string) *float64 {
	value, ok := values[key]
	if !ok {
		return nil
	}
	return &value
}

func setTaskDuration(item *SystemTaskDTO) {
	if item.LastStartedAt == nil || item.LastFinishedAt == nil || *item.LastFinishedAt < *item.LastStartedAt {
		return
	}
	duration := *item.LastFinishedAt - *item.LastStartedAt
	item.DurationSeconds = &duration
}

func applyRunningTaskDuration(item *SystemTaskDTO, state *db.SystemTaskState, now int64) {
	if item.Status != "running" {
		return
	}
	if state != nil && state.Status == "running" && state.LastStartedAt != nil {
		item.LastStartedAt = state.LastStartedAt
	}
	item.LastFinishedAt = nil
	if item.LastStartedAt == nil {
		item.DurationSeconds = nil
		return
	}
	duration := maxInt64(0, now-*item.LastStartedAt)
	item.DurationSeconds = &duration
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boolValue(value bool) *bool { return &value }
