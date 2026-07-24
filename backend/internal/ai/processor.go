package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/storage"
)

type Processor struct {
	DB                  *db.DB
	BaseURL             string
	Logger              *slog.Logger
	Client              *http.Client
	Sources             *storage.SourceHealth
	HealthWaitTimeout   time.Duration
	HealthRetryInterval time.Duration
}

type analyzeRequest struct {
	AssetID   int64    `json:"assetId"`
	RelPath   string   `json:"relPath"`
	MediaType string   `json:"mediaType"`
	CacheKey  string   `json:"cacheKey"`
	Duration  *float64 `json:"duration,omitempty"`
	Focus     string   `json:"focus,omitempty"`
}

type analyzeResponse struct {
	Description             string          `json:"description"`
	Tags                    []db.AITag      `json:"tags"`
	TagModel                string          `json:"tagModel"`
	TagModelVersion         string          `json:"tagModelVersion"`
	DescriptionModel        string          `json:"descriptionModel"`
	DescriptionModelVersion string          `json:"descriptionModelVersion"`
	TaxonomyVersion         string          `json:"taxonomyVersion"`
	SampledFrames           json.RawMessage `json:"sampledFrames"`
	Palette                 []db.AIColor    `json:"palette"`
}

func (p *Processor) Handle(ctx context.Context, task jobs.Task) error {
	if task.Type != "ai_analyze" {
		return nil
	}
	if task.Priority != 1 {
		enabled, err := p.DB.AIExecutionEnabled(ctx)
		if err != nil {
			return err
		}
		if !enabled {
			return nil
		}
	}
	asset, err := p.DB.GetAsset(ctx, task.AssetID)
	if err != nil {
		return err
	}
	if p.Sources != nil {
		if available, _ := p.Sources.AvailableForRel(asset.RelPath); !available {
			if p.Logger != nil {
				p.Logger.Info("skip AI analysis because storage is unavailable", "assetID", asset.ID, "relPath", asset.RelPath)
			}
			return nil
		}
	}
	if err := p.DB.EnsureAIQueued(ctx, asset.ID, asset.CacheKey, false); err != nil {
		return err
	}
	if _, err := p.DB.MarkAIProcessing(ctx, asset.ID, asset.CacheKey); err != nil {
		return err
	}
	p.resumeService()
	if err := p.waitForHealth(ctx); err != nil {
		if ctx.Err() != nil {
			return p.interrupt(asset.ID, asset.CacheKey, context.Cause(ctx))
		}
		return p.fail(ctx, asset.ID, asset.CacheKey, err, true)
	}
	if task.Priority != 1 {
		enabled, err := p.DB.AIExecutionEnabled(ctx)
		if err != nil {
			return err
		}
		if !enabled {
			return p.interrupt(asset.ID, asset.CacheKey, context.Canceled)
		}
	}

	focus, err := p.DB.AIFocusForPath(ctx, asset.RelPath)
	if err != nil {
		return p.fail(ctx, asset.ID, asset.CacheKey, err, true)
	}
	payload, _ := json.Marshal(analyzeRequest{AssetID: asset.ID, RelPath: asset.RelPath, MediaType: asset.MediaType, CacheKey: asset.CacheKey, Duration: asset.Duration, Focus: focus})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/analyze", bytes.NewReader(payload))
	if err != nil {
		return p.fail(ctx, asset.ID, asset.CacheKey, err, true)
	}
	req.Header.Set("Content-Type", "application/json")
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return p.interrupt(asset.ID, asset.CacheKey, context.Cause(ctx))
		}
		return p.fail(ctx, asset.ID, asset.CacheKey, err, true)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if isPlaybackInterruptionResponse(resp.StatusCode, body) {
			return p.interrupt(asset.ID, asset.CacheKey, jobs.ErrPlaybackPriority)
		}
		cause := fmt.Errorf("AI service %s: %s", resp.Status, strings.TrimSpace(string(body)))
		p.Sources.RecordSourceError(asset.RelPath, cause)
		if storage.IsSourceUnavailable(cause) {
			if p.Logger != nil {
				p.Logger.Warn("skip AI analysis because storage became unavailable", "assetID", asset.ID, "relPath", asset.RelPath)
			}
			return p.interrupt(asset.ID, asset.CacheKey, cause)
		}
		return p.fail(ctx, asset.ID, asset.CacheKey, cause, !isMediaAnalysisError(cause))
	}
	var result analyzeResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); err != nil {
		return p.fail(ctx, asset.ID, asset.CacheKey, err, true)
	}
	description := strings.TrimSpace(result.Description)
	if n := len([]rune(description)); n < 30 || n > 140 {
		return p.fail(ctx, asset.ID, asset.CacheKey, fmt.Errorf("description length %d outside 30..140", n), true)
	}
	if len(result.Tags) > 10 {
		result.Tags = result.Tags[:10]
	}
	if len(result.SampledFrames) == 0 {
		result.SampledFrames = json.RawMessage("[]")
	}
	err = p.DB.SaveAIResult(ctx, asset.ID, asset.CacheKey, description, result.TagModel, result.TagModelVersion, result.DescriptionModel, result.DescriptionModelVersion, result.TaxonomyVersion, result.SampledFrames, result.Tags, result.Palette)
	if err != nil && ctx.Err() != nil {
		return p.interrupt(asset.ID, asset.CacheKey, context.Cause(ctx))
	}
	return err
}

func isPlaybackInterruptionResponse(statusCode int, body []byte) bool {
	return statusCode == http.StatusConflict && strings.Contains(strings.ToLower(string(body)), "ai analysis paused for media playback")
}

func (p *Processor) resumeService() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/resume", nil)
	if err != nil {
		return
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func (p *Processor) interrupt(assetID int64, cacheKey string, cause error) error {
	if cause == nil {
		cause = context.Canceled
	}
	if errors.Is(cause, jobs.ErrPlaybackPriority) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/pause", nil)
		if err == nil {
			client := p.Client
			if client == nil {
				client = &http.Client{Timeout: 3 * time.Second}
			}
			resp, requestErr := client.Do(req)
			if requestErr == nil {
				resp.Body.Close()
			}
		}
		cancel()
	}
	dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.DB.RequeueAIInterrupted(dbCtx, assetID, cacheKey); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (p *Processor) health(ctx context.Context) error {
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, strings.TrimRight(p.BaseURL, "/")+"/health", nil)
	if err != nil {
		return err
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("AI service unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("AI service unavailable: %s", resp.Status)
	}
	return nil
}

func (p *Processor) waitForHealth(ctx context.Context) error {
	timeout := p.HealthWaitTimeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	interval := p.HealthRetryInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var lastErr error
	for {
		if err := p.health(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-deadline.C:
			timer.Stop()
			return lastErr
		case <-timer.C:
		}
	}
}

func (p *Processor) fail(ctx context.Context, assetID int64, cacheKey string, cause error, retryable bool) error {
	prefix := "ai_media: "
	if retryable {
		prefix = "ai_transient: "
	}
	message := prefix + cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	retry, err := p.DB.MarkAIFailed(ctx, assetID, cacheKey, message)
	if err != nil && !errors.Is(err, context.Canceled) {
		return errors.Join(cause, err)
	}
	if retryable && retry {
		return errors.Join(jobs.ErrRetryable, cause)
	}
	return cause
}

func isMediaAnalysisError(err error) bool {
	if err == nil {
		return false
	}
	raw := strings.ToLower(err.Error())
	mediaSignals := []string{
		"ffmpeg", "non-zero exit status", "calledprocesserror", "no such file",
		"permission denied", "does not contain any stream", "no streams", "invalid nal unit",
		"invalid data found", "moov atom not found", "cannot identify image file", "unidentifiedimageerror",
		"unsupported image", "decoder not found", "corrupt",
	}
	for _, signal := range mediaSignals {
		if strings.Contains(raw, signal) {
			return true
		}
	}
	return false
}
