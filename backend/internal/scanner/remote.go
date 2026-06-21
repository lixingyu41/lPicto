package scanner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/model"
)

const scanStatusKey = "lpicto:scan:status"

var scanStatusSetScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
local incoming = tonumber(ARGV[2]) or 0
if current then
  local ok, decoded = pcall(cjson.decode, current)
  if ok and decoded and decoded["revision"] then
    local current_revision = tonumber(decoded["revision"]) or 0
    if current_revision > incoming then
      return 0
    end
  end
end
redis.call("SET", KEYS[1], ARGV[1], "EX", ARGV[3])
return 1
`)

type RedisStatusStore struct {
	client *redis.Client
}

func NewRedisStatusStore(ctx context.Context, redisURL string) (*RedisStatusStore, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &RedisStatusStore{client: client}, nil
}

func (s *RedisStatusStore) SetScanStatus(ctx context.Context, status Status) error {
	if status.Revision <= 0 {
		status.Revision = nextStatusRevision()
	}
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	ttlSeconds := int64((24 * time.Hour) / time.Second)
	return scanStatusSetScript.Run(ctx, s.client, []string{scanStatusKey}, string(data), status.Revision, ttlSeconds).Err()
}

func (s *RedisStatusStore) GetScanStatus(ctx context.Context) (Status, error) {
	data, err := s.client.Get(ctx, scanStatusKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return Status{Progress: Progress{State: "idle", Phase: "idle"}}, nil
	}
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, err
	}
	return status, nil
}

type RemoteController struct {
	DB          *db.DB
	Jobs        *jobs.Manager
	StatusStore *RedisStatusStore
}

func (c RemoteController) RequestScan(reason string) CommandResult {
	return c.RequestMetadataScan(reason)
}

func (c RemoteController) RequestScanRoots(reason string, roots []string) CommandResult {
	return c.RequestMetadataScanRoots(reason, roots)
}

func (c RemoteController) RequestRebuild(reason string) CommandResult {
	return c.RequestThumbnailRebuild(reason)
}

func (c RemoteController) RequestCountScan(reason string) CommandResult {
	c.setQueuedScanStatus(reason, scanTaskCount, nil)
	c.Jobs.Enqueue(jobs.Task{Type: "scan_count", Reason: reason, Priority: 10})
	return CommandResult{Accepted: true, Started: false, State: "queued"}
}

func (c RemoteController) RequestCountScanRoots(reason string, roots []string) CommandResult {
	c.setQueuedScanStatus(reason, scanTaskCount, roots)
	c.Jobs.Enqueue(jobs.Task{Type: "scan_count", Reason: reason, Roots: append([]string(nil), roots...), Priority: 10})
	return CommandResult{Accepted: true, Started: false, State: "queued"}
}

func (c RemoteController) RequestMetadataScan(reason string) CommandResult {
	c.setQueuedScanStatus(reason, scanTaskMetadata, nil)
	c.Jobs.Enqueue(jobs.Task{Type: "scan_metadata", Reason: reason, Priority: 10})
	return CommandResult{Accepted: true, Started: false, State: "queued"}
}

func (c RemoteController) RequestMetadataScanRoots(reason string, roots []string) CommandResult {
	c.setQueuedScanStatus(reason, scanTaskMetadata, roots)
	c.Jobs.Enqueue(jobs.Task{Type: "scan_metadata", Reason: reason, Roots: append([]string(nil), roots...), Priority: 10})
	return CommandResult{Accepted: true, Started: false, State: "queued"}
}

func (c RemoteController) RequestThumbnailRebuild(reason string) CommandResult {
	c.setQueuedScanStatus(reason, scanTaskThumbRebuild, nil)
	c.Jobs.Enqueue(jobs.Task{Type: "thumb_rebuild", Reason: reason, Priority: 10})
	return CommandResult{Accepted: true, Started: false, State: "queued"}
}

func (c RemoteController) RequestThumbnailRebuildRoots(reason string, roots []string) CommandResult {
	c.setQueuedScanStatus(reason, scanTaskThumbRebuild, roots)
	c.Jobs.Enqueue(jobs.Task{Type: "thumb_rebuild", Reason: reason, Roots: append([]string(nil), roots...), Priority: 10})
	return CommandResult{Accepted: true, Started: false, State: "queued"}
}

func (c RemoteController) RequestStop() CommandResult {
	c.setStoppingStatus()
	c.Jobs.Enqueue(jobs.Task{Type: "scan_stop", Priority: 1})
	return CommandResult{Accepted: true, Paused: true, State: "queued"}
}

func (c RemoteController) setQueuedScanStatus(reason string, task scanTask, roots []string) {
	if c.StatusStore == nil {
		return
	}
	now := time.Now().Unix()
	_ = c.StatusStore.SetScanStatus(context.Background(), Status{
		Running:   true,
		LastStart: now,
		Revision:  nextStatusRevision(),
		Progress: Progress{
			State:           "running",
			RequestedAction: "start",
			Task:            string(task),
			Reason:          reason,
			Phase:           "queued",
			Roots:           append([]string(nil), roots...),
		},
	})
}

func (c RemoteController) setStoppingStatus() {
	if c.StatusStore == nil {
		return
	}
	ctx := context.Background()
	status, err := c.StatusStore.GetScanStatus(ctx)
	if err != nil || !status.Running {
		return
	}
	status.Progress.State = "stopping"
	status.Progress.RequestedAction = "stop"
	status.Progress.Phase = "stopping"
	status.Revision = nextStatusRevision()
	_ = c.StatusStore.SetScanStatus(ctx, status)
}

func (c RemoteController) Status(ctx context.Context) (Status, error) {
	status, err := c.StatusStore.GetScanStatus(ctx)
	if err != nil {
		return Status{}, err
	}
	lastRun, err := c.DB.LastScanRun(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Status{}, err
	}
	status.LastRun = lastRun
	if status.Progress.State == "" {
		status.Progress = Progress{State: "idle", Phase: "idle"}
	}
	return status, nil
}

func StatusFromRun(run *model.ScanRun) Status {
	status := Status{LastRun: run, Progress: Progress{State: "idle", Phase: "idle"}}
	if run != nil && run.Status == "running" {
		status.Running = true
		status.LastStart = run.StartedAt
		status.Progress.State = "running"
	}
	return status
}
