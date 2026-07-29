package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/scanner"
)

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
	if got := taskActionIDs(item.Actions); got != "reconcile,stop" {
		t.Fatalf("reconcile actions = %q", got)
	}
	if item.Actions[0].Label != "开始对账" || !item.Actions[0].Enabled {
		t.Fatalf("reconcile action = %#v", item.Actions[0])
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
}

func TestStandardTaskActions(t *testing.T) {
	thumbnail := thumbnailSystemTask(db.WorkStatusCounts{Total: 10, Ready: 3, Pending: 5, Error: 2}, 0, 0)
	if got := taskActionIDs(thumbnail.Actions); got != "start,retry_failed,rebuild,stop" {
		t.Fatalf("thumbnail actions = %#v", thumbnail.Actions)
	}
	if thumbnail.Actions[0].Label != "手动开始" {
		t.Fatalf("thumbnail start label = %#v", thumbnail.Actions[0])
	}
	if thumbnail.Actions[1].Label != "重试失败（2）" || thumbnail.Actions[2].Label != "全部重建" || !thumbnail.Actions[2].RequiresConfirmation {
		t.Fatalf("thumbnail labels = %#v", thumbnail.Actions)
	}
	aiTask := aiAnalysisSystemTask(db.AIStatus{Total: 10, Pending: 7, Failed: 2, Ready: 1}, db.AIActivity{}, 0, 0, false)
	if got := taskActionIDs(aiTask.Actions); got != "continue,retry_failed,rebuild,stop" {
		t.Fatalf("AI actions = %#v", aiTask.Actions)
	}
	failedOnly := thumbnailSystemTask(db.WorkStatusCounts{Total: 2, Error: 2}, 0, 0)
	if !failedOnly.Actions[0].Enabled {
		t.Fatalf("manual start should be enabled for failed work: %#v", failedOnly.Actions)
	}
	failedAIOnly := aiAnalysisSystemTask(db.AIStatus{Total: 2, Failed: 2}, db.AIActivity{}, 0, 0, false)
	if !failedAIOnly.Actions[0].Enabled {
		t.Fatalf("AI continue should be enabled for failed work: %#v", failedAIOnly.Actions)
	}
	running := thumbnailSystemTask(db.WorkStatusCounts{Total: 10, Pending: 10}, 1, 0)
	for _, action := range running.Actions {
		if action.Enabled != (action.ID == "stop") {
			t.Fatalf("running action %q enabled = %v", action.ID, action.Enabled)
		}
	}
	runningWithFailures := thumbnailSystemTask(db.WorkStatusCounts{Total: 10, Pending: 7, Error: 3}, 1, 0)
	for _, action := range runningWithFailures.Actions {
		wantEnabled := action.ID == "retry_failed" || action.ID == "stop"
		if action.Enabled != wantEnabled {
			t.Fatalf("running task with failures action %q enabled = %v, want %v", action.ID, action.Enabled, wantEnabled)
		}
	}
}

func TestAIAnalysisTaskShowsRequestedRunWhilePreparing(t *testing.T) {
	item := aiAnalysisSystemTask(db.AIStatus{Total: 10, Pending: 10}, db.AIActivity{}, 0, 0, true)
	if item.Status != "running" {
		t.Fatalf("requested AI analysis status = %q, want running", item.Status)
	}
	for _, action := range item.Actions {
		if action.Enabled != (action.ID == "stop") {
			t.Fatalf("preparing action %q enabled = %v", action.ID, action.Enabled)
		}
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
