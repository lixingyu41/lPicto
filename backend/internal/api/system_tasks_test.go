package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/scanner"
)

func TestStorageSampleErrorOnlyFailsLibraryForSourceErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "readable", err: nil, want: false},
		{name: "missing media", err: errors.New("no such file"), want: false},
		{name: "media permission", err: errors.New("permission denied"), want: false},
		{name: "stale mount", err: errors.New("stale file handle"), want: true},
		{name: "storage IO", err: errors.New("input/output error"), want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := storageSampleErrorMakesLibraryUnavailable(test.err); got != test.want {
				t.Fatalf("storage sample error classification = %v, want %v", got, test.want)
			}
		})
	}
}

func TestVisibleOnlyRequiresExplicitReadyFilter(t *testing.T) {
	for path, want := range map[string]bool{
		"/api/library/assets":               false,
		"/api/library/assets?visible=all":   false,
		"/api/library/assets?visible=ready": true,
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if got := visibleOnly(request); got != want {
			t.Fatalf("visibleOnly(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestSystemTaskResultLabels(t *testing.T) {
	status, succeeded := scanTaskResult("finished")
	if status != "success" || succeeded == nil || !*succeeded {
		t.Fatalf("finished result = %q, %#v", status, succeeded)
	}
	status, succeeded = scanTaskResult("finished_with_errors")
	if status != "warning" || succeeded == nil || *succeeded {
		t.Fatalf("warning result = %q, %#v", status, succeeded)
	}
}

func TestLibraryReconcileTaskUsesManualReconcileAction(t *testing.T) {
	item := scanSystemTask("library_scan", "图库文件对账", "对账", nil, scanner.Status{})
	if got := taskActionIDs(item.Actions); got != "reconcile" {
		t.Fatalf("reconcile actions = %q", got)
	}
	if item.Actions[0].Label != "开始对账" || !item.Actions[0].Enabled {
		t.Fatalf("reconcile action = %#v", item.Actions[0])
	}
}

func TestMediaScanTaskUsesIndependentDiscoveryProgress(t *testing.T) {
	item := mediaScanSystemTask(&model.ScanRun{Status: "finished", TotalSeen: 25}, scanner.Status{})
	if item.ID != "media_scan" || item.Name != "媒体扫描" || item.Progress == nil || item.Progress.Total != 25 {
		t.Fatalf("media scan task = %#v", item)
	}
	if got := taskActionIDs(item.Actions); got != "continue,rebuild" {
		t.Fatalf("media scan actions = %q", got)
	}
	running := mediaScanSystemTask(nil, scanner.Status{Running: true, Progress: scanner.Progress{Task: "media_scan", TotalFiles: 30, ScannedFiles: 12}})
	if running.Status != "running" || running.Progress == nil || running.Progress.Completed != 12 || running.Progress.Pending != 17 || running.Progress.Processing != 1 {
		t.Fatalf("running media scan task = %#v", running)
	}
}

func TestMediaScanTaskDoesNotKeepStaleRunningState(t *testing.T) {
	item := mediaScanSystemTask(&model.ScanRun{Status: "running"}, scanner.Status{})
	if item.Status != "stopped" || item.Succeeded == nil || *item.Succeeded {
		t.Fatalf("stale media scan task = %#v", item)
	}
}

func TestMediaScanTaskIncludesAutomaticMetadataIngestion(t *testing.T) {
	item := mediaScanSystemTask(nil, scanner.Status{Running: true, Progress: scanner.Progress{Task: "metadata", TotalFiles: 30, ScannedFiles: 12, AssetsAdded: 12}})
	if item.Status != "running" || item.Progress == nil || item.Progress.Completed != 12 || item.Progress.Processing != 1 {
		t.Fatalf("metadata ingestion task = %#v", item)
	}
}

func TestMediaScanTaskShowsPlaybackPauseAndKeepsAutomaticResumePending(t *testing.T) {
	item := mediaScanSystemTask(
		&model.ScanRun{Status: "paused", TotalSeen: 12},
		scanner.Status{Progress: scanner.Progress{State: "paused", Task: "media_scan", PauseReason: "playback", TotalFiles: 30, ScannedFiles: 12}},
	)
	if item.Status != "pending" || !strings.Contains(item.BlockedReason, "播放结束后将自动继续") {
		t.Fatalf("paused media scan task = %#v", item)
	}
	if item.Actions[0].ID != "stop" || !item.Actions[0].Enabled {
		t.Fatalf("paused media scan actions = %#v", item.Actions)
	}
}

func TestDuplicateScanTaskReportsAggregateProgress(t *testing.T) {
	startedAt := int64(100)
	item := duplicateScanSystemTask(
		&db.SystemTaskState{Status: "running", LastStartedAt: &startedAt},
		10,
		4,
		2,
		true,
	)
	if item.Status != "running" || item.Progress == nil {
		t.Fatalf("running duplicate task = %#v", item)
	}
	if item.Progress.Total != 10 || item.Progress.Completed != 4 || item.Progress.Processing != 1 || item.Progress.Pending != 3 || item.Progress.Failed != 2 {
		t.Fatalf("duplicate progress = %#v", item.Progress)
	}
	if got := taskActionIDs(item.Actions); got != "stop" {
		t.Fatalf("duplicate actions = %q", got)
	}
	if !item.Actions[0].Enabled {
		t.Fatalf("running duplicate actions = %#v", item.Actions)
	}

	item = duplicateScanSystemTask(&db.SystemTaskState{Status: "running", LastStartedAt: &startedAt}, 10, 4, 0, false)
	if item.Status != "stopped" || taskActionIDs(item.Actions) != "scan" || !item.Actions[0].Enabled {
		t.Fatalf("stale duplicate task = %#v", item)
	}
}

func TestInterruptedScanAndMediaErrorsUseReadableMessages(t *testing.T) {
	errorText := "[h264] Invalid NAL unit size; Error splitting the input into NAL units"
	relPath := "相册/损坏视频.mp4"
	message := readableMediaJobError(&db.MediaJobTaskState{Status: "failed", Message: &errorText, RelPath: &relPath})
	if message != "无法读取视频数据，文件可能损坏或未完整写入：相册/损坏视频.mp4" {
		t.Fatalf("readable media error = %q", message)
	}
}

func TestTaskFailureKeepsUsefulUnknownReason(t *testing.T) {
	const reason = "AI 服务返回空描述"
	if got := readableTaskError(reason); got != reason {
		t.Fatalf("readable task error = %q, want %q", got, reason)
	}
}

func TestTaskFailureTranslatesKnownServiceErrors(t *testing.T) {
	cases := map[string]string{
		"Output file #0 does not contain any stream":                                            "媒体中没有可处理的音视频轨道",
		"AI service 500 Internal Server Error: {\"detail\":\"[Errno 111] Connection refused\"}": "AI 描述模型临时退出，自动重试一次后仍失败",
		"AI service: [Errno 111] Connection refused":                                            "AI 描述模型临时退出，自动重试一次后仍失败",
		"ai_transient: Server disconnected without sending a response.":                         "AI 描述模型运行中断，自动重试一次后仍失败",
		"ai_transient: model response does not contain a JSON object":                           "AI 模型输出格式不正确，自动重试一次后仍失败",
		"ai_media: AI service 500: model_output_invalid: bad json":                              "AI 模型输出格式错误，已自动重新生成 1 次",
		"ai_media: ffmpeg returned non-zero exit status 69":                                     "无法从该媒体提取 AI 分析画面",
		"ai_media: Output file #0 does not contain any stream":                                  "媒体中没有可供 AI 分析的视频画面",
	}
	for input, want := range cases {
		if got := readableTaskError(input); got != want {
			t.Fatalf("readableTaskError(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTaskFailureIncludesStructuredModelOutputDetail(t *testing.T) {
	reason := `AI service 500 Internal Server Error: {"detail":{"code":"model_output_invalid","message":"模型输出格式错误","parseError":"缺少 JSON 逗号","finishReason":"stop","output":"{\"description\":\"测试\" \"tags\":[]}"}}`
	got := readableTaskError(reason)
	for _, part := range []string{
		"AI 模型输出格式错误，已自动重新生成 1 次",
		"解析原因：缺少 JSON 逗号",
		"结束原因：stop",
		`模型原始输出：`,
		`{"description":"测试" "tags":[]}`,
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("readable model output error %q does not contain %q", got, part)
		}
	}
}

func TestTaskFailureIncludesBothModelOutputAttempts(t *testing.T) {
	reason := `AI service 500 Internal Server Error: {"detail":{"code":"model_output_invalid","message":"模型输出格式错误","attempts":[{"attempt":1,"parseError":"缺少逗号","finishReason":"stop","output":"第一次输出"},{"attempt":2,"parseError":"没有 JSON 对象","finishReason":"length","output":"第二次输出"}]}}`
	got := readableTaskError(reason)
	for _, part := range []string{
		"第 1 次解析原因：缺少逗号",
		"第 1 次模型原始输出：\n第一次输出",
		"第 2 次解析原因：没有 JSON 对象",
		"第 2 次模型原始输出：\n第二次输出",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("two-attempt model error %q does not contain %q", got, part)
		}
	}
}

func TestAIAnalysisTaskOffersRetryForFailures(t *testing.T) {
	item := aiAnalysisSystemTask(db.AIStatus{Total: 10, Ready: 7, Failed: 3}, db.AIActivity{}, 0, 0, false)
	if item.Status != "failed" || !item.CanRetry || item.FailedCount != 3 {
		t.Fatalf("AI analysis task = %#v", item)
	}
	item = aiAnalysisSystemTask(db.AIStatus{Total: 10, Ready: 7, Failed: 3}, db.AIActivity{}, 0, 1, false)
	if item.Status != "running" || !item.CanRetry {
		t.Fatalf("active AI analysis task = %#v", item)
	}
}

func TestRunningAITaskUsesWholeRunDuration(t *testing.T) {
	activityStarted, activityFinished := int64(155), int64(155)
	runStarted := int64(100)
	item := aiAnalysisSystemTask(
		db.AIStatus{Total: 10, Processing: 1},
		db.AIActivity{LastStartedAt: &activityStarted, LastFinishedAt: &activityFinished},
		0,
		1,
		false,
	)
	applyRunningTaskDuration(&item, &db.SystemTaskState{Status: "running", LastStartedAt: &runStarted}, 160)
	if item.LastStartedAt == nil || *item.LastStartedAt != 100 || item.LastFinishedAt != nil {
		t.Fatalf("running AI timestamps = %#v", item)
	}
	if item.DurationSeconds == nil || *item.DurationSeconds != 60 {
		t.Fatalf("running AI duration = %#v, want 60", item.DurationSeconds)
	}
}

func TestThumbnailTaskUsesAggregateQueueState(t *testing.T) {
	counts := db.WorkStatusCounts{Total: 100, Ready: 50, Pending: 45, Processing: 5}
	item := thumbnailSystemTask(counts, 0, 0)
	if item.Status != "pending" {
		t.Fatalf("idle partial thumbnail task status = %q, want pending", item.Status)
	}
	item = thumbnailSystemTask(counts, 40, 2)
	if item.Status != "running" {
		t.Fatalf("queued thumbnail task status = %q, want running", item.Status)
	}
	if item.Progress == nil || item.Progress.Queued != 40 {
		t.Fatalf("queued thumbnail progress = %#v, want 40 queued", item.Progress)
	}
	item = thumbnailSystemTask(db.WorkStatusCounts{Total: 100, Ready: 100}, 0, 0)
	if item.Status != "success" || item.Succeeded == nil || !*item.Succeeded {
		t.Fatalf("completed thumbnail task = %#v", item)
	}
	if len(item.Actions) != 0 {
		t.Fatalf("completed thumbnail actions = %#v, want none", item.Actions)
	}
}

func TestAutomaticMediaTasksExposeNoManualActions(t *testing.T) {
	thumbnail := thumbnailSystemTask(db.WorkStatusCounts{Total: 10, Ready: 3, Pending: 5, Error: 2}, 0, 0)
	if len(thumbnail.Actions) != 0 {
		t.Fatalf("thumbnail actions = %#v, want none", thumbnail.Actions)
	}
	aiTask := aiAnalysisSystemTask(db.AIStatus{Total: 10, Pending: 7, Failed: 2, Ready: 1}, db.AIActivity{}, 0, 0, false)
	if len(aiTask.Actions) != 0 {
		t.Fatalf("AI actions = %#v, want none", aiTask.Actions)
	}
}

func TestAutomaticTasksExposeOnlyFailureRetryActions(t *testing.T) {
	thumbnail := thumbnailSystemTask(db.WorkStatusCounts{Total: 10, Ready: 3, Pending: 5, Error: 2}, 0, 0)
	thumbnail.Failures = []SystemTaskFailureDTO{{AssetID: 1, Path: "PIC/a.jpg", Reason: "没有权限读取媒体文件"}}
	if got := taskActionIDs(automaticFailureRetryActions(thumbnail)); got != "retry_failed" {
		t.Fatalf("thumbnail automatic actions = %q, want retry_failed", got)
	}
	aiTask := aiAnalysisSystemTask(db.AIStatus{Total: 10, Pending: 7, Failed: 2, Ready: 1}, db.AIActivity{}, 0, 0, false)
	if got := taskActionIDs(automaticFailureRetryActions(aiTask)); got != "retry_failed" {
		t.Fatalf("AI automatic actions = %q, want retry_failed", got)
	}
	pending := thumbnailSystemTask(db.WorkStatusCounts{Total: 10, Ready: 3, Pending: 7}, 0, 0)
	if got := taskActionIDs(automaticFailureRetryActions(pending)); got != "" {
		t.Fatalf("pending automatic actions = %q, want none", got)
	}
}

func TestAIAnalysisTaskShowsRequestedRunWhilePreparing(t *testing.T) {
	item := aiAnalysisSystemTask(db.AIStatus{Total: 10, Pending: 10}, db.AIActivity{}, 0, 0, true)
	if item.Status != "pending" {
		t.Fatalf("requested AI analysis status = %q, want pending", item.Status)
	}
	if len(item.Actions) != 0 {
		t.Fatalf("preparing AI actions = %#v, want none", item.Actions)
	}
}

func taskActionIDs(actions []SystemTaskActionDTO) string {
	ids := make([]string, 0, len(actions))
	for _, action := range actions {
		ids = append(ids, action.ID)
	}
	return strings.Join(ids, ",")
}

func TestCacheCleanupPreservesReferencedVariants(t *testing.T) {
	keys := map[string]struct{}{"0123456789abcdef0123": {}}
	for _, name := range []string{"0123456789abcdef0123", "0123456789abcdef0123-hls-aabb", "0123456789abcdef0123-s100"} {
		if !cacheFileReferenced(name, keys) {
			t.Fatalf("referenced cache %q was not preserved", name)
		}
	}
	if cacheFileReferenced("fedcba98765432100123", keys) {
		t.Fatal("orphan cache was preserved")
	}
}
