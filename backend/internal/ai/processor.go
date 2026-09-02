package ai

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/storage"
)

type Processor struct {
	DB                  *db.DB
	BaseURL             string
	ExternalToken       string
	Logger              *slog.Logger
	Client              *http.Client
	Sources             *storage.SourceHealth
	HealthWaitTimeout   time.Duration
	HealthRetryInterval time.Duration
	Stager              *Stager
	nodeMu              sync.Mutex
	nodeSlots           map[string]chan struct{}
	nodeUnhealthyUntil  map[string]time.Time
	nextNode            uint64
}

var errRetainAIStage = errors.New("retain AI staging input")

type analyzeRequest struct {
	AssetID      int64     `json:"assetId"`
	RelPath      string    `json:"relPath"`
	MediaType    string    `json:"mediaType"`
	CacheKey     string    `json:"cacheKey"`
	Duration     *float64  `json:"duration,omitempty"`
	Focus        string    `json:"focus,omitempty"`
	StagedPath   string    `json:"stagedPath,omitempty"`
	SampleRatios []float64 `json:"sampleRatios,omitempty"`
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

func (p *Processor) Handle(ctx context.Context, task jobs.Task) (resultErr error) {
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
		if available, _ := p.Sources.CachedAvailableForRel(asset.RelPath); !available {
			if p.Logger != nil {
				p.Logger.Info("skip AI analysis because storage is unavailable", "assetID", asset.ID, "relPath", asset.RelPath)
			}
			return nil
		}
	}
	settings, err := p.DB.GetAISettings(ctx)
	if err != nil {
		return err
	}
	nodes, err := ComputeNodes(settings, p.BaseURL, p.ExternalToken)
	if err != nil {
		return errors.Join(jobs.ErrRetryable, err)
	}
	node, releaseNode, err := p.acquireHealthyNode(ctx, nodes)
	if err != nil {
		return errors.Join(jobs.ErrRetryable, err)
	}
	defer releaseNode()
	if err := p.DB.EnsureAIQueued(ctx, asset.ID, asset.CacheKey, false); err != nil {
		return err
	}
	if _, err := p.DB.MarkAIProcessing(ctx, asset.ID, asset.CacheKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if task.Priority != 1 {
		enabled, err := p.DB.AIExecutionEnabled(ctx)
		if err != nil {
			return err
		}
		if !enabled {
			return p.interrupt(asset.ID, asset.CacheKey, context.Canceled, node)
		}
	}

	focus, err := p.DB.AIFocusForPath(ctx, asset.RelPath)
	if err != nil {
		return p.fail(ctx, asset.ID, asset.CacheKey, err, true)
	}
	var stage *db.AIStage
	if p.Stager != nil {
		stage, err = p.Stager.Prepare(ctx, asset)
		if err != nil {
			if ctx.Err() != nil {
				return p.interrupt(asset.ID, asset.CacheKey, context.Cause(ctx), node)
			}
			libraryRoot, _ := p.DB.ScanLibraryRootForPath(ctx, asset.RelPath)
			if p.Sources != nil && p.Sources.AssetReadErrorIsSourceUnavailable(asset.RelPath, err, libraryRoot) {
				return p.interrupt(asset.ID, asset.CacheKey, err, node)
			}
			return p.fail(ctx, asset.ID, asset.CacheKey, err, true)
		}
		if stage != nil {
			if err := p.DB.MarkAIStageProcessing(ctx, asset.ID, asset.CacheKey); err != nil {
				p.Stager.Remove(context.Background(), stage)
				return p.fail(ctx, asset.ID, asset.CacheKey, err, true)
			}
			releaseStage := p.Stager.Pin(stage)
			defer releaseStage()
			defer func() {
				if shouldRetainAIStage(resultErr) {
					if err := p.DB.RestoreAIStageReady(context.Background(), asset.ID, asset.CacheKey); err != nil {
						resultErr = errors.Join(resultErr, err)
					}
					return
				}
				p.Stager.Remove(context.Background(), stage)
			}()
		}
	}
	stagedPath := ""
	if stage != nil {
		stagedPath = stage.StagePath
	}
	payload := analyzeRequest{AssetID: asset.ID, RelPath: asset.RelPath, MediaType: asset.MediaType, CacheKey: asset.CacheKey, Duration: asset.Duration, Focus: focus, StagedPath: stagedPath}
	if !node.External {
		go p.DiscardPrefetchedBundle(nodes, asset.CacheKey)
	}
	resp, err := p.sendAnalyzeRequest(ctx, node, payload, stage)
	if err != nil {
		if ctx.Err() != nil {
			return p.interrupt(asset.ID, asset.CacheKey, context.Cause(ctx), node)
		}
		return p.requeueNodeFailure(asset.ID, asset.CacheKey, node.BaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		if isPlaybackInterruptionResponse(resp.StatusCode, body) {
			return p.interrupt(asset.ID, asset.CacheKey, jobs.ErrPlaybackPriority, node)
		}
		cause := fmt.Errorf("AI service %s: %s", resp.Status, strings.TrimSpace(string(body)))
		if isComputeNodeFailure(resp.StatusCode, body) {
			return p.requeueNodeFailure(asset.ID, asset.CacheKey, node.BaseURL, cause)
		}
		sourceUnavailable := storage.IsSourceUnavailable(cause)
		if p.Sources != nil {
			libraryRoot, _ := p.DB.ScanLibraryRootForPath(ctx, asset.RelPath)
			sourceUnavailable = p.Sources.AssetReadErrorIsSourceUnavailable(asset.RelPath, cause, libraryRoot)
		}
		if sourceUnavailable {
			if p.Logger != nil {
				p.Logger.Warn("skip AI analysis because storage became unavailable", "assetID", asset.ID, "relPath", asset.RelPath)
			}
			return p.interrupt(asset.ID, asset.CacheKey, cause, node)
		}
		retryable := !isMediaAnalysisError(cause) && !strings.Contains(strings.ToLower(cause.Error()), "model_output_invalid")
		return p.fail(ctx, asset.ID, asset.CacheKey, cause, retryable)
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
		return p.interrupt(asset.ID, asset.CacheKey, context.Cause(ctx), node)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return p.interrupt(asset.ID, asset.CacheKey, errors.Join(jobs.ErrRetryable, err), node)
	}
	return nil
}

func shouldRetainAIStage(err error) bool {
	return err != nil && (errors.Is(err, jobs.ErrRetryable) || errors.Is(err, errRetainAIStage) ||
		errors.Is(err, jobs.ErrMediaScanPriority) || errors.Is(err, jobs.ErrMediaCachePriority) ||
		errors.Is(err, jobs.ErrPlaybackPriority) || errors.Is(err, jobs.ErrTaskStopped) ||
		errors.Is(err, context.Canceled))
}

func (p *Processor) acquireHealthyNode(ctx context.Context, nodes []ComputeNode) (ComputeNode, func(), error) {
	remaining := append([]ComputeNode(nil), nodes...)
	var lastErr error
	for len(remaining) > 0 {
		node, release, err := p.acquireNode(ctx, remaining)
		if err != nil {
			return ComputeNode{}, nil, err
		}
		p.resumeServiceAt(node)
		timeout := p.HealthWaitTimeout
		if timeout <= 0 {
			timeout = 45 * time.Second
		}
		if err := p.waitForHealthAt(ctx, node, timeout); err == nil {
			p.markNodeHealthy(node.BaseURL)
			return node, release, nil
		} else {
			lastErr = err
			p.markNodeUnhealthy(node.BaseURL)
			release()
		}
		next := make([]ComputeNode, 0, len(remaining)-1)
		for _, candidate := range remaining {
			if candidate.BaseURL != node.BaseURL {
				next = append(next, candidate)
			}
		}
		remaining = next
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的 AI 计算节点")
	}
	return ComputeNode{}, nil, lastErr
}

func (p *Processor) acquireNode(ctx context.Context, nodes []ComputeNode) (ComputeNode, func(), error) {
	if len(nodes) == 0 {
		return ComputeNode{}, nil, errors.New("没有配置 AI 计算节点")
	}
	for {
		p.nodeMu.Lock()
		if p.nodeSlots == nil {
			p.nodeSlots = make(map[string]chan struct{})
		}
		if p.nodeUnhealthyUntil == nil {
			p.nodeUnhealthyUntil = make(map[string]time.Time)
		}
		start := int(p.nextNode % uint64(len(nodes)))
		p.nextNode++
		now := time.Now()
		ordered := make([]ComputeNode, 0, len(nodes))
		slots := make([]chan struct{}, 0, len(nodes))
		for offset := range nodes {
			node := nodes[(start+offset)%len(nodes)]
			if len(nodes) > 1 && now.Before(p.nodeUnhealthyUntil[node.BaseURL]) {
				continue
			}
			slot := p.nodeSlots[node.BaseURL]
			if slot == nil {
				slot = make(chan struct{}, 1)
				p.nodeSlots[node.BaseURL] = slot
			}
			ordered = append(ordered, node)
			slots = append(slots, slot)
		}
		p.nodeMu.Unlock()
		for index, slot := range slots {
			select {
			case slot <- struct{}{}:
				node := ordered[index]
				return node, func() { <-slot }, nil
			default:
			}
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ComputeNode{}, nil, context.Cause(ctx)
		case <-timer.C:
		}
	}
}

func (p *Processor) markNodeUnhealthy(baseURL string) {
	p.nodeMu.Lock()
	if p.nodeUnhealthyUntil == nil {
		p.nodeUnhealthyUntil = make(map[string]time.Time)
	}
	p.nodeUnhealthyUntil[baseURL] = time.Now().Add(time.Minute)
	p.nodeMu.Unlock()
}

func (p *Processor) markNodeHealthy(baseURL string) {
	p.nodeMu.Lock()
	delete(p.nodeUnhealthyUntil, baseURL)
	p.nodeMu.Unlock()
}

func (p *Processor) newAnalyzeHTTPRequest(ctx context.Context, node ComputeNode, payload analyzeRequest, stage *db.AIStage) (*http.Request, error) {
	if !node.External {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(node.BaseURL, "/")+"/analyze", bytes.NewReader(encoded))
		if err == nil {
			request.Header.Set("Content-Type", "application/json")
		}
		return request, err
	}
	if stage == nil || p.Stager == nil {
		return nil, errors.New("外部 AI 需要已暂存的媒体输入")
	}
	return p.newExternalBundleHTTPRequest(ctx, node, payload, stage, "/analyze-bundle")
}

func (p *Processor) newExternalBundleHTTPRequest(ctx context.Context, node ComputeNode, payload analyzeRequest, stage *db.AIStage, endpoint string) (*http.Request, error) {
	ratios, framePaths, err := p.stageBundle(stage)
	if err != nil {
		return nil, err
	}
	payload.StagedPath = ""
	payload.SampleRatios = ratios
	metadata, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	field, err := writer.CreateFormField("metadata")
	if err != nil {
		return nil, err
	}
	if _, err := field.Write(metadata); err != nil {
		return nil, err
	}
	for _, framePath := range framePaths {
		part, err := writer.CreateFormFile("frames", filepath.Base(framePath))
		if err != nil {
			return nil, err
		}
		frame, err := os.Open(framePath)
		if err != nil {
			return nil, err
		}
		_, copyErr := io.Copy(part, frame)
		closeErr := frame.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(node.BaseURL, "/")+endpoint, bytes.NewReader(body.Bytes()))
	if err == nil {
		request.Header.Set("Content-Type", writer.FormDataContentType())
		setComputeNodeAuth(request, node)
	}
	return request, err
}

func (p *Processor) sendAnalyzeRequest(ctx context.Context, node ComputeNode, payload analyzeRequest, stage *db.AIStage) (*http.Response, error) {
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Minute}
	}
	if node.External {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(node.BaseURL, "/")+"/analyze-prefetched", bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		setComputeNodeAuth(request, node)
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusNotFound {
			return response, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
	}
	request, err := p.newAnalyzeHTTPRequest(ctx, node, payload, stage)
	if err != nil {
		return nil, err
	}
	return client.Do(request)
}

type PrefetchStatus struct {
	Capacity  int      `json:"capacity"`
	CacheKeys []string `json:"cacheKeys"`
}

func (p *Processor) ExternalNode(nodes []ComputeNode) (ComputeNode, bool) {
	for _, node := range nodes {
		if node.External {
			return node, true
		}
	}
	return ComputeNode{}, false
}

func (p *Processor) PrefetchStatus(ctx context.Context, node ComputeNode) (PrefetchStatus, error) {
	var status PrefetchStatus
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(node.BaseURL, "/")+"/prefetch-status", nil)
	if err != nil {
		return status, err
	}
	setComputeNodeAuth(request, node)
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return status, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return status, fmt.Errorf("AI prefetch status: %s", response.Status)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&status); err != nil {
		return status, err
	}
	if status.Capacity < 1 || status.Capacity > 5 {
		return status, fmt.Errorf("AI prefetch capacity %d outside 1..5", status.Capacity)
	}
	return status, nil
}

func (p *Processor) PrefetchStage(ctx context.Context, node ComputeNode, assetID int64, cacheKey string, stage *db.AIStage) error {
	asset, err := p.DB.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	payload := analyzeRequest{AssetID: asset.ID, RelPath: asset.RelPath, MediaType: asset.MediaType, CacheKey: cacheKey, Duration: asset.Duration}
	request, err := p.newExternalBundleHTTPRequest(ctx, node, payload, stage, "/prefetch-bundle")
	if err != nil {
		return err
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		return fmt.Errorf("AI prefetch upload %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (p *Processor) DiscardPrefetchedBundle(nodes []ComputeNode, cacheKey string) {
	node, ok := p.ExternalNode(nodes)
	if !ok {
		return
	}
	discardCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = p.DiscardPrefetchedBundleAt(discardCtx, node, cacheKey)
}

func (p *Processor) DiscardPrefetchedBundleAt(ctx context.Context, node ComputeNode, cacheKey string) error {
	endpoint := strings.TrimRight(node.BaseURL, "/") + "/prefetch-bundle?cacheKey=" + url.QueryEscape(cacheKey)
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	setComputeNodeAuth(request, node)
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("AI prefetch discard: %s", response.Status)
	}
	return nil
}

func (p *Processor) stageBundle(stage *db.AIStage) ([]float64, []string, error) {
	if stage.SizeBytes <= 0 || stage.SizeBytes > StageMaxBundleBytes {
		return nil, nil, fmt.Errorf("AI 暂存输入大小 %d 超出 16 MiB 限制", stage.SizeBytes)
	}
	root := filepath.Clean(filepath.Join(p.Stager.Store.CacheRoot, filepath.FromSlash(stage.StagePath)))
	if !storage.IsWithinRoot(p.Stager.Store.CacheRoot, root) {
		return nil, nil, errors.New("AI 暂存路径越界")
	}
	metaBytes, err := os.ReadFile(filepath.Join(root, "meta.json"))
	if err != nil {
		return nil, nil, err
	}
	var meta struct {
		Ratios []float64 `json:"ratios"`
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, nil, err
	}
	paths, err := filepath.Glob(filepath.Join(root, "*.jpg"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 || len(paths) > 10 || len(paths) != len(meta.Ratios) {
		return nil, nil, fmt.Errorf("AI 暂存帧数量无效：%d", len(paths))
	}
	return meta.Ratios, paths, nil
}

func isPlaybackInterruptionResponse(statusCode int, body []byte) bool {
	return statusCode == http.StatusConflict && strings.Contains(strings.ToLower(string(body)), "ai analysis paused for media playback")
}

func (p *Processor) resumeServiceAt(node ComputeNode) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(node.BaseURL, "/")+"/resume", nil)
	if err != nil {
		return
	}
	setComputeNodeAuth(req, node)
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func (p *Processor) interrupt(assetID int64, cacheKey string, cause error, node ComputeNode) error {
	if cause == nil {
		cause = context.Canceled
	}
	if errors.Is(cause, jobs.ErrPlaybackPriority) || errors.Is(cause, jobs.ErrMediaScanPriority) || errors.Is(cause, jobs.ErrMediaCachePriority) || errors.Is(cause, jobs.ErrTaskStopped) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(node.BaseURL, "/")+"/pause", nil)
		if err == nil {
			setComputeNodeAuth(req, node)
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
	return errors.Join(errRetainAIStage, cause)
}

func (p *Processor) requeueNodeFailure(assetID int64, cacheKey, baseURL string, cause error) error {
	p.markNodeUnhealthy(baseURL)
	dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.DB.RequeueAIInterrupted(dbCtx, assetID, cacheKey); err != nil {
		return errors.Join(jobs.ErrRetryable, cause, err)
	}
	return errors.Join(jobs.ErrRetryable, cause)
}

func (p *Processor) health(ctx context.Context) error {
	return p.healthAt(ctx, ComputeNode{BaseURL: p.BaseURL})
}

func (p *Processor) healthAt(ctx context.Context, node ComputeNode) error {
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, strings.TrimRight(node.BaseURL, "/")+"/ready", nil)
	if err != nil {
		return err
	}
	setComputeNodeAuth(req, node)
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
	var body struct {
		Status          string `json:"status"`
		Service         string `json:"service"`
		ProtocolVersion int    `json:"protocolVersion"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return fmt.Errorf("AI service readiness response invalid: %w", err)
	}
	if body.Service != "lpicto-ai" || body.ProtocolVersion != computeProtocolVersion || (body.Status != "ok" && body.Status != "paused") {
		return errors.New("AI service readiness protocol mismatch")
	}
	return nil
}

func (p *Processor) waitForHealth(ctx context.Context) error {
	return p.waitForHealthAt(ctx, ComputeNode{BaseURL: p.BaseURL}, p.HealthWaitTimeout)
}

func (p *Processor) waitForHealthAt(ctx context.Context, node ComputeNode, timeout time.Duration) error {
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
		if err := p.healthAt(ctx, node); err == nil {
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

func isComputeNodeFailure(statusCode int, body []byte) bool {
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || statusCode == http.StatusBadGateway || statusCode == http.StatusServiceUnavailable || statusCode == http.StatusGatewayTimeout {
		return true
	}
	raw := strings.ToLower(string(body))
	for _, signal := range []string{"connection refused", "connection reset", "server disconnected", "connecterror", "description model unavailable"} {
		if strings.Contains(raw, signal) {
			return true
		}
	}
	return false
}
