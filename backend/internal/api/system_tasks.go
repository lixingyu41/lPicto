package api

import (
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
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	Schedule        string                 `json:"schedule"`
	Status          string                 `json:"status"`
	Succeeded       *bool                  `json:"succeeded"`
	LastStartedAt   *int64                 `json:"lastStartedAt"`
	LastFinishedAt  *int64                 `json:"lastFinishedAt"`
	NextRunAt       *int64                 `json:"nextRunAt"`
	DurationSeconds *int64                 `json:"durationSeconds"`
	Message         string                 `json:"message"`
	LastError       string                 `json:"lastError"`
	Processed       int                    `json:"processed"`
	FailedCount     int64                  `json:"failedCount"`
	CanRetry        bool                   `json:"canRetry"`
	SupportsScope   bool                   `json:"supportsScope"`
	Progress        *SystemTaskProgressDTO `json:"progress"`
	Actions         []SystemTaskActionDTO  `json:"actions"`
	Failures        []SystemTaskFailureDTO `json:"failures"`
}

func (s *Server) systemTasks(w http.ResponseWriter, r *http.Request) {
	countRun, err := s.db.LastScanRunForTask(r.Context(), "count")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tasks_failed", "读取图库扫描任务失败")
		return
	}
	metadataRun, err := s.db.LastScanRunForTask(r.Context(), "metadata")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tasks_failed", "读取媒体信息任务失败")
		return
	}
	metadata, err := s.db.MetadataProgress(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tasks_failed", "读取媒体信息进度失败")
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
	aiHealth, _ := s.db.SystemTaskState(r.Context(), db.SystemTaskAIHealth)
	storageHealth, _ := s.db.SystemTaskState(r.Context(), taskStorageHealth)
	cacheCleanup, _ := s.db.SystemTaskState(r.Context(), taskCacheCleanup)
	thumbnailControl, _ := s.db.SystemTaskState(r.Context(), "thumbnail_creation")
	previewControl, _ := s.db.SystemTaskState(r.Context(), "preview_creation")
	posterControl, _ := s.db.SystemTaskState(r.Context(), "video_poster_creation")
	aiControl, _ := s.db.SystemTaskState(r.Context(), "ai_analysis")
	aiStatus, err := s.db.AIStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tasks_failed", "读取 AI 分析任务失败")
		return
	}
	aiActivity, _ := s.db.AIActivity(r.Context())
	queue := jobs.QueueStats{}
	if s.jobs != nil {
		queue = s.jobs.Stats()
	}
	scanStatus, _ := s.scanner.Status(r.Context())

	thumbnailTask := aggregateMediaTask("thumbnail_creation", "缩略图创建", "为瀑布流和媒体列表创建缩略图", "媒体入库后自动运行", progress.Thumb, queue.ThumbQueued, queue.ActiveThumb)
	previewTask := aggregateMediaTask("preview_creation", "高清预览创建", "为浏览器无法直接显示的图片创建高清预览", "媒体入库后自动运行", progress.Preview, queue.PreviewQueued, queue.ActivePreview)
	posterTask := aggregateMediaTask("video_poster_creation", "视频封面创建", "创建视频播放前显示的首帧封面", "媒体入库后自动运行", progress.VideoPoster, queue.VideoPosterQueued, queue.ActiveVideoPoster)
	metadataTask := metadataSystemTask(metadata, metadataRun, scanStatus)
	applyMediaJobActivity(&thumbnailTask, thumbActivity)
	applyMediaJobActivity(&previewTask, previewActivity)
	applyMediaJobActivity(&posterTask, posterActivity)
	applyStoppedState(&thumbnailTask, thumbnailControl)
	applyStoppedState(&previewTask, previewControl)
	applyStoppedState(&posterTask, posterControl)
	aiTask := aiAnalysisSystemTask(aiStatus, aiActivity, queue.AIQueued, queue.ActiveAI)
	applyStoppedState(&aiTask, aiControl)
	applyRunningTaskDuration(&aiTask, aiControl, time.Now().Unix())
	attachMediaFailures(r, s, &metadataTask, "metadata")
	attachMediaFailures(r, s, &thumbnailTask, "thumb")
	attachMediaFailures(r, s, &previewTask, "preview")
	attachMediaFailures(r, s, &posterTask, "video_poster")
	attachAIFailures(r, s, &aiTask)
	scanTask := scanSystemTask("library_scan", "图库文件扫描", "检查图库中的新增、变更和缺失文件", countRun, scanStatus)
	attachSummaryFailure(&scanTask)
	storageTask := storedSystemTask(taskStorageHealth, "存储连接检查", "检查每个图库根目录及抽样媒体是否可访问", "每 30 分钟及扫描前", storageHealth, false)
	attachSummaryFailure(&storageTask)
	aiHealthTask := aiHealthSystemTask(aiHealth)
	attachSummaryFailure(&aiHealthTask)
	cacheTask := storedSystemTask(taskCacheCleanup, "无效缓存清理", "删除失去数据库引用的缓存和过期临时文件", "每天 03:00", cacheCleanup, false)
	attachSummaryFailure(&cacheTask)
	items := []SystemTaskDTO{
		scanTask,
		metadataTask,
		thumbnailTask,
		previewTask,
		posterTask,
		aiTask,
		storageTask,
		aiHealthTask,
		cacheTask,
	}
	for index := range items {
		if items[index].Failures == nil {
			items[index].Failures = []SystemTaskFailureDTO{}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
	item := SystemTaskDTO{
		ID: id, Name: name, Description: description, Schedule: schedule, SupportsScope: true,
		Processed: counts.Ready, FailedCount: int64(counts.Error),
		Progress: &SystemTaskProgressDTO{Total: required, Completed: counts.Ready, Queued: queued, Pending: counts.Pending, Processing: counts.Processing, Failed: counts.Error},
		Message:  fmt.Sprintf("已完成 %d / %d，等待 %d，处理中 %d，失败 %d，队列 %d", counts.Ready, required, counts.Pending, counts.Processing, counts.Error, queued),
	}
	switch {
	case active > 0 || queued > 0:
		item.Status = "running"
	case counts.Processing > 0 || counts.Pending > 0:
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
	item.Actions = standardMediaTaskActions(item.Status == "running", counts.Pending+counts.Processing+counts.Error, counts.Error, required)
	return item
}

func standardMediaTaskActions(running bool, continuable, failed, total int) []SystemTaskActionDTO {
	actions := []SystemTaskActionDTO{
		{ID: "continue", Label: "继续", Kind: "primary", Enabled: !running && continuable > 0},
	}
	if failed > 0 {
		actions = append(actions, SystemTaskActionDTO{ID: "retry_failed", Label: fmt.Sprintf("重试失败（%d）", failed), Kind: "secondary", Enabled: !running})
	}
	actions = append(actions,
		SystemTaskActionDTO{ID: "rebuild", Label: "全部重建", Kind: "danger", Enabled: !running && total > 0, RequiresConfirmation: true},
		SystemTaskActionDTO{ID: "stop", Label: "停止", Kind: "danger", Enabled: running},
	)
	return actions
}

func thumbnailSystemTask(counts db.WorkStatusCounts, queued, active int) SystemTaskDTO {
	return aggregateMediaTask("thumbnail_creation", "缩略图创建", "为瀑布流和媒体列表创建缩略图", "媒体入库后自动运行", counts, queued, active)
}

func metadataSystemTask(counts db.WorkStatusCounts, run *model.ScanRun, scanStatus scanner.Status) SystemTaskDTO {
	item := aggregateMediaTask("metadata_extraction", "媒体信息提取", "读取图片尺寸、视频时长、编码和拍摄时间", "媒体入库后自动运行", counts, 0, 0)
	aggregateStatus, aggregateSucceeded := item.Status, item.Succeeded
	aggregateProcessed, aggregateFailed := item.Processed, item.FailedCount
	aggregateMessage, aggregateProgress := item.Message, item.Progress
	if run != nil {
		applyScanRun(&item, run)
		item.Status, item.Succeeded = aggregateStatus, aggregateSucceeded
		item.Processed, item.FailedCount = aggregateProcessed, aggregateFailed
		item.Message, item.Progress = aggregateMessage, aggregateProgress
	}
	if scanStatus.Running && scanStatus.Progress.Task == "metadata" {
		item.Status = "running"
	}
	item.Actions = standardMediaTaskActions(item.Status == "running", counts.Pending+counts.Processing+counts.Error, counts.Error, counts.Total)
	return item
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
	item := SystemTaskDTO{ID: id, Name: name, Description: description, Schedule: "按需运行", Status: "never", SupportsScope: true}
	if run != nil {
		applyScanRun(&item, run)
		item.Progress = &SystemTaskProgressDTO{Total: run.TotalSeen, Completed: run.TotalSeen, Failed: run.Errors}
	}
	if status.Running && status.Progress.Task == "count" {
		item.Status = "running"
		total := maxInt(status.Progress.TotalFiles, status.Progress.DiscoveredFiles)
		completed := maxInt(status.Progress.ScannedFiles, status.Progress.TotalSeen)
		item.Progress = &SystemTaskProgressDTO{Total: total, Completed: completed, Pending: maxInt(0, total-completed), Failed: status.Progress.Errors}
		item.Message = fmt.Sprintf("已检查 %d / %d，新增 %d，变更 %d，缺失 %d", completed, total, status.Progress.AssetsAdded, status.Progress.AssetsUpdated, status.Progress.AssetsDeleted)
	}
	if item.Status == "running" {
		item.Actions = []SystemTaskActionDTO{
			{ID: "scan", Label: "扫描图库", Kind: "primary", Enabled: false},
			{ID: "stop", Label: "停止", Kind: "danger", Enabled: true},
		}
	} else {
		item.Actions = []SystemTaskActionDTO{
			{ID: "scan", Label: "扫描图库", Kind: "primary", Enabled: true},
			{ID: "stop", Label: "停止", Kind: "danger", Enabled: false},
		}
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

func aiAnalysisSystemTask(status db.AIStatus, activity db.AIActivity, queued, active int) SystemTaskDTO {
	item := SystemTaskDTO{
		ID: "ai_analysis", Name: "AI 媒体分析", Description: "为图片和视频生成描述与标签", Schedule: "自动或手动运行",
		LastStartedAt: activity.LastStartedAt, LastFinishedAt: activity.LastFinishedAt, FailedCount: status.Failed, CanRetry: status.Failed > 0,
		Processed: int(status.Ready), Progress: &SystemTaskProgressDTO{Total: int(status.Total), Completed: int(status.Ready), Queued: queued, Pending: int(status.Pending + status.Stale), Processing: int(status.Processing), Failed: int(status.Failed)},
		Message: fmt.Sprintf("已完成 %d / %d，失败 %d，待分析 %d", status.Ready, status.Total, status.Failed, status.Pending+status.Stale),
	}
	switch {
	case active > 0 || queued > 0 || status.Processing > 0:
		item.Status = "running"
	case status.Failed > 0:
		item.Status, item.Succeeded = "failed", boolValue(false)
	case status.Pending > 0 || status.Stale > 0:
		item.Status = "pending"
	default:
		item.Status, item.Succeeded = "success", boolValue(true)
	}
	item.Actions = standardMediaTaskActions(item.Status == "running", int(status.Pending+status.Stale+status.Processing+status.Failed), int(status.Failed), int(status.Total))
	setTaskDuration(&item)
	return item
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
		item.Actions = []SystemTaskActionDTO{
			{ID: "cleanup", Label: "立即清理", Kind: "danger", Enabled: !running, RequiresConfirmation: true},
			{ID: "stop", Label: "停止", Kind: "danger", Enabled: running},
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
	raw := strings.ToLower(reason)
	message := "处理媒体时发生错误"
	switch {
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
