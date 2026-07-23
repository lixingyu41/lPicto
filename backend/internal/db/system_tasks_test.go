package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSystemTaskStateAndTypedScanRuns(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, testDatabaseURL(t, ctx), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	runID, err := database.StartScanRun(ctx, "count")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.FinishScanRun(ctx, runID, ScanFinish{Status: "finished", TotalSeen: 12}); err != nil {
		t.Fatal(err)
	}
	run, err := database.LastScanRunForTask(ctx, "count")
	if err != nil || run == nil || run.TaskType != "count" || run.TotalSeen != 12 {
		t.Fatalf("count scan run = %#v, %v", run, err)
	}

	if err := database.BeginSystemTask(ctx, SystemTaskAIHealth); err != nil {
		t.Fatal(err)
	}
	if err := database.FinishSystemTask(ctx, SystemTaskAIHealth, "success", "AI 服务运行正常"); err != nil {
		t.Fatal(err)
	}
	state, err := database.SystemTaskState(ctx, SystemTaskAIHealth)
	if err != nil || state == nil || state.Status != "success" || state.LastSuccessAt == nil {
		t.Fatalf("AI health task = %#v, %v", state, err)
	}

	if _, err := database.Conn().ExecContext(ctx, `
INSERT INTO media_job(asset_id,job_type,status,started_at,finished_at)
VALUES(NULL,'thumb','ready',now(),now())`); err != nil {
		t.Fatal(err)
	}
	mediaTask, err := database.LatestMediaJobTask(ctx, "thumb")
	if err != nil || mediaTask == nil || mediaTask.Status != "ready" || mediaTask.FinishedAt == nil {
		t.Fatalf("thumbnail task = %#v, %v", mediaTask, err)
	}
}
