package scanner

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/events"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/media"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

type Scanner struct {
	DB                *db.DB
	Store             storage.Store
	Extractor         media.Extractor
	Jobs              *jobs.Manager
	Events            *events.Bus
	StatusReporter    StatusReporter
	VideoProxyEnabled bool
	ScanWorkers       int
	Logger            *slog.Logger
	Sources           *storage.SourceHealth

	startOnce sync.Once
	commands  chan scanCommand

	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
	lastStart int64
	progress  Progress
}

const globalScanReason = "global_scan"

type StatusReporter interface {
	SetScanStatus(context.Context, Status) error
}

type Controller = Scanner

type Status struct {
	Running   bool           `json:"running"`
	LastStart int64          `json:"lastStart"`
	LastRun   *model.ScanRun `json:"lastRun"`
	Progress  Progress       `json:"progress"`
	Revision  int64          `json:"revision,omitempty"`
}

type Progress struct {
	State           string                  `json:"state"`
	RequestedAction string                  `json:"requestedAction"`
	PauseReason     string                  `json:"pauseReason,omitempty"`
	Task            string                  `json:"task"`
	Reason          string                  `json:"reason"`
	Phase           string                  `json:"phase"`
	Roots           []string                `json:"roots"`
	CurrentRoot     string                  `json:"currentRoot"`
	CurrentRelPath  string                  `json:"currentRelPath"`
	DiscoveredFiles int                     `json:"discoveredFiles"`
	TotalFiles      int                     `json:"totalFiles"`
	ScannedFiles    int                     `json:"scannedFiles"`
	TotalSeen       int                     `json:"totalSeen"`
	AssetsAdded     int                     `json:"assetsAdded"`
	AssetsUpdated   int                     `json:"assetsUpdated"`
	AssetsDeleted   int                     `json:"assetsDeleted"`
	Errors          int                     `json:"errors"`
	RootStats       map[string]RootProgress `json:"rootStats,omitempty"`
}

type RootProgress struct {
	DiscoveredFiles int  `json:"discoveredFiles"`
	TotalFiles      int  `json:"totalFiles"`
	ScannedFiles    int  `json:"scannedFiles"`
	TotalSeen       int  `json:"totalSeen"`
	Finished        bool `json:"finished"`
}

type counters struct {
	totalSeen     int
	assetsAdded   int
	assetsUpdated int
	assetsDeleted int
	errors        int
	lastError     *string
	rootSeen      map[string]int
}

type scanFile struct {
	absPath string
	info    os.FileInfo
}

type scanWrite struct {
	kind            string
	folderRel       string
	absPath         string
	rel             string
	info            os.FileInfo
	detection       media.Detection
	meta            media.Metadata
	mimeType        string
	browserPlayable bool
	thumbStatus     string
	previewStatus   string
	posterStatus    string
	proxyStatus     string
	metadataJSON    *string
	nfoChanged      bool
	nfoSize         *int64
	nfoMtime        *int64
	hasSubtitle     bool
	hasDanmaku      bool
	errorText       *string
}

type scanState struct {
	scanner       *Scanner
	ctx           context.Context
	files         chan scanFile
	writes        chan scanWrite
	seen          map[string]struct{}
	counts        *counters
	roots         []string
	task          scanTask
	forceMetadata bool
	deferWork     bool
	deferredWork  map[int64]scanDeferredWork
	mu            sync.Mutex
	wg            sync.WaitGroup
	writerWG      sync.WaitGroup
}

type scanDeferredWork struct {
	assetID       int64
	mediaType     string
	previewStatus string
}

type scanRequest struct {
	reason           string
	roots            []string
	paths            []string
	hasOverride      bool
	excludeRootFiles bool
	task             scanTask
}

type scanTask string

const (
	scanTaskMetadata      scanTask = "metadata"
	scanTaskMediaScan     scanTask = "media_scan"
	scanTaskReconcile     scanTask = "reconcile"
	scanTaskCount         scanTask = "count"
	scanTaskThumbContinue scanTask = "thumb_continue"
	scanTaskThumbRebuild  scanTask = "thumb_rebuild"
)

type scanCommandKind string

const (
	scanCommandStart scanCommandKind = "start"
	scanCommandStop  scanCommandKind = "stop"
)

type scanCommand struct {
	kind  scanCommandKind
	req   scanRequest
	reply chan CommandResult
}

type CommandResult struct {
	Accepted bool   `json:"accepted"`
	Started  bool   `json:"started"`
	Paused   bool   `json:"paused"`
	State    string `json:"state"`
}

func (s *Scanner) Trigger(reason string) bool {
	return s.RequestMetadataScan(reason).Started
}

func (s *Scanner) TriggerRoots(reason string, roots []string) bool {
	return s.RequestMetadataScanRoots(reason, roots).Started
}

func (s *Scanner) TriggerRebuild(reason string) bool {
	return s.RequestThumbnailRebuild(reason).Started
}

func (s *Scanner) RequestScan(reason string) CommandResult {
	return s.RequestMetadataScan(reason)
}

func (s *Scanner) RequestScanRoots(reason string, roots []string) CommandResult {
	return s.RequestMetadataScanRoots(reason, roots)
}

func (s *Scanner) RequestReconcileScan(reason string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, task: scanTaskReconcile})
}

func (s *Scanner) RequestReconcileScanRoots(reason string, roots []string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, roots: append([]string(nil), roots...), hasOverride: true, task: scanTaskReconcile})
}

func (s *Scanner) RequestRebuild(reason string) CommandResult {
	return s.RequestThumbnailRebuild(reason)
}

func (s *Scanner) RequestCountScan(reason string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, task: scanTaskCount})
}

func (s *Scanner) RequestCountScanRoots(reason string, roots []string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, roots: append([]string(nil), roots...), hasOverride: true, task: scanTaskCount})
}

func (s *Scanner) RequestMetadataScan(reason string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, task: scanTaskMetadata})
}

func (s *Scanner) RequestMetadataScanRoots(reason string, roots []string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, roots: append([]string(nil), roots...), hasOverride: true, task: scanTaskMetadata})
}

func (s *Scanner) RequestMetadataScanPaths(reason string, roots []string, paths []string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, roots: append([]string(nil), roots...), paths: append([]string(nil), paths...), hasOverride: true, task: scanTaskMetadata})
}

func (s *Scanner) RequestMediaScan(reason string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, task: scanTaskMediaScan})
}

func (s *Scanner) RequestMediaScanRoots(reason string, roots []string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, roots: append([]string(nil), roots...), hasOverride: true, task: scanTaskMediaScan})
}

func (s *Scanner) RequestMediaScanNestedRoots(reason string, roots []string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, roots: append([]string(nil), roots...), hasOverride: true, excludeRootFiles: true, task: scanTaskMediaScan})
}

func (s *Scanner) RequestThumbnailRebuild(reason string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, task: scanTaskThumbRebuild})
}

func (s *Scanner) RequestThumbnailRebuildRoots(reason string, roots []string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, roots: append([]string(nil), roots...), hasOverride: true, task: scanTaskThumbRebuild})
}

func (s *Scanner) RequestThumbnailContinue(reason string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, task: scanTaskThumbContinue})
}

func (s *Scanner) RequestThumbnailContinueRoots(reason string, roots []string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, roots: append([]string(nil), roots...), hasOverride: true, task: scanTaskThumbContinue})
}

func (s *Scanner) requestStart(req scanRequest) CommandResult {
	if strings.TrimSpace(req.reason) == "" {
		req.reason = "manual"
	}
	if req.task == "" {
		req.task = scanTaskMetadata
	}
	return s.submitCommand(scanCommand{kind: scanCommandStart, req: req})
}

func (s *Scanner) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		s.commands = make(chan scanCommand, 128)
		s.setIdleProgress()
		go s.commandLoop(ctx)
	})
}

func (s *Scanner) ensureStarted() {
	s.Start(context.Background())
}

func (s *Scanner) submitCommand(cmd scanCommand) CommandResult {
	s.ensureStarted()
	cmd.reply = make(chan CommandResult, 1)
	select {
	case s.commands <- cmd:
	case <-time.After(2 * time.Second):
		return CommandResult{Accepted: false, State: s.currentState()}
	}
	select {
	case result := <-cmd.reply:
		return result
	case <-time.After(2 * time.Second):
		return CommandResult{Accepted: false, State: s.currentState()}
	}
}

func (s *Scanner) commandLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			if s.Logger != nil {
				s.Logger.Error("scanner command loop panicked", "panic", r)
			}
		}
	}()
	var cancel context.CancelFunc
	var done <-chan struct{}
	var activeStart *scanRequest
	var pendingStarts []scanRequest
	startPendingIfReady := func() {
		if len(pendingStarts) == 0 || done != nil || ctx.Err() != nil {
			return
		}
		next := pendingStarts[0]
		pendingStarts = pendingStarts[1:]
		cancel, done = s.startRun(ctx, next)
		activeStart = &next
	}
	for {
		select {
		case <-ctx.Done():
			if cancel != nil {
				cancel()
			}
			return
		case cmd := <-s.commands:
			switch cmd.kind {
			case scanCommandStart:
				req := cmd.req
				if done == nil {
					if len(pendingStarts) > 0 {
						pendingStarts = enqueueScanRequest(pendingStarts, req, isAutomaticScanRequest(req))
						startPendingIfReady()
						cmd.reply <- CommandResult{Accepted: true, Started: done != nil, State: s.currentState()}
						continue
					}
					cancel, done = s.startRun(ctx, req)
					activeStart = &req
					cmd.reply <- CommandResult{Accepted: true, Started: true, State: "running"}
					continue
				}
				if activeStart != nil && sameScanRequest(*activeStart, req) {
					cmd.reply <- CommandResult{Accepted: true, Started: false, State: s.currentState()}
					continue
				}
				if containsScanRequest(pendingStarts, req) {
					cmd.reply <- CommandResult{Accepted: true, Started: false, State: s.currentState()}
					continue
				}
				if isAutomaticScanRequest(req) {
					pendingStarts = enqueueScanRequest(pendingStarts, req, true)
					cmd.reply <- CommandResult{Accepted: true, Started: false, State: s.currentState()}
					continue
				}
				pendingStarts = []scanRequest{req}
				if cancel != nil {
					cancel()
				}
				s.setStopping("start")
				cmd.reply <- CommandResult{Accepted: true, Started: false, State: "stopping"}
			case scanCommandStop:
				pendingStarts = nil
				if done == nil {
					s.setIdleProgress()
					cmd.reply <- CommandResult{Accepted: false, Paused: false, State: "idle"}
					continue
				}
				if cancel != nil {
					cancel()
				}
				s.setStopping("stop")
				cmd.reply <- CommandResult{Accepted: true, Paused: true, State: "stopping"}
			default:
				cmd.reply <- CommandResult{Accepted: false, State: s.currentState()}
			}
		case <-done:
			done = nil
			cancel = nil
			activeStart = nil
			if len(pendingStarts) > 0 && ctx.Err() == nil {
				startPendingIfReady()
				continue
			}
			s.setIdleAfterRun()
		}
	}
}

func containsScanRequest(queue []scanRequest, req scanRequest) bool {
	for _, queued := range queue {
		if sameScanRequest(queued, req) {
			return true
		}
	}
	return false
}

func enqueueScanRequest(queue []scanRequest, req scanRequest, coalesce bool) []scanRequest {
	if !coalesce {
		return append(queue, req)
	}
	for index := range queue {
		if queue[index].task != req.task {
			continue
		}
		queue[index] = mergeScanRequests(queue[index], req)
		return queue
	}
	return append(queue, req)
}

func mergeScanRequests(existing, incoming scanRequest) scanRequest {
	merged := existing
	merged.excludeRootFiles = existing.excludeRootFiles && incoming.excludeRootFiles
	if !existing.hasOverride || !incoming.hasOverride {
		merged.hasOverride = false
		merged.roots = nil
	} else {
		merged.roots = mergeStringSets(existing.roots, incoming.roots)
	}
	if len(existing.paths) == 0 || len(incoming.paths) == 0 {
		merged.paths = nil
	} else {
		merged.paths = mergeStringSets(existing.paths, incoming.paths)
	}
	return merged
}

func mergeStringSets(first, second []string) []string {
	values := make(map[string]struct{}, len(first)+len(second))
	for _, value := range first {
		values[value] = struct{}{}
	}
	for _, value := range second {
		values[value] = struct{}{}
	}
	merged := make([]string, 0, len(values))
	for value := range values {
		merged = append(merged, value)
	}
	sort.Strings(merged)
	return merged
}

func sameScanRequest(a scanRequest, b scanRequest) bool {
	return a.task == b.task && a.hasOverride == b.hasOverride && a.excludeRootFiles == b.excludeRootFiles && equalStringSet(a.roots, b.roots) && equalStringSet(a.paths, b.paths)
}

func isAutomaticScanRequest(req scanRequest) bool {
	reason := strings.TrimSpace(req.reason)
	return reason == "storage_recovered" || strings.HasPrefix(reason, "auto_") || strings.HasPrefix(reason, "fsnotify") || strings.HasPrefix(reason, "count_changed:")
}

func equalStringSet(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, value := range a {
		counts[value]++
	}
	for _, value := range b {
		if counts[value] == 0 {
			return false
		}
		counts[value]--
	}
	return true
}

func (s *Scanner) startRun(parent context.Context, req scanRequest) (context.CancelFunc, <-chan struct{}) {
	s.mu.Lock()
	s.running = true
	s.lastStart = util.UnixNow()
	s.progress = Progress{
		State:           "running",
		RequestedAction: "start",
		Task:            string(req.task),
		Reason:          req.reason,
		Phase:           "queued",
		Roots:           append([]string(nil), req.roots...),
	}
	runCtx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.mu.Unlock()
	s.publishStatus()
	done := make(chan struct{}, 1)
	go func() {
		s.run(runCtx, req)
		if _, err := s.DB.RefreshSystemCollectionCounts(context.Background()); err != nil && s.Logger != nil {
			s.Logger.Warn("refresh system collection count cache failed", "error", err)
		}
		done <- struct{}{}
	}()
	return cancel, done
}

func (s *Scanner) Pause() bool {
	return s.RequestStop().Paused
}

func (s *Scanner) RequestStop() CommandResult {
	return s.submitCommand(scanCommand{kind: scanCommandStop})
}

func (s *Scanner) StartPeriodic(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Trigger("periodic")
			}
		}
	}()
}

func (s *Scanner) StartPeriodicCount(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.Jobs != nil {
					s.Jobs.Enqueue(jobs.Task{Type: "scan_count", Reason: "auto_count"})
				}
			}
		}
	}()
}

func (s *Scanner) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	status := Status{Running: s.running, LastStart: s.lastStart, Progress: s.progress}
	s.mu.Unlock()
	lastRun, err := s.DB.LastScanRun(ctx)
	if err != nil {
		return Status{}, err
	}
	status.LastRun = lastRun
	return status, nil
}

func (s *Scanner) currentState() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress.State != "" {
		return s.progress.State
	}
	if s.running {
		return "running"
	}
	return "idle"
}

func (s *Scanner) setIdleProgress() {
	s.mu.Lock()
	s.running = false
	s.cancel = nil
	s.progress = Progress{State: "idle", Phase: "idle"}
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) setIdleAfterRun() {
	s.mu.Lock()
	s.running = false
	s.cancel = nil
	s.progress.State = "idle"
	s.progress.RequestedAction = ""
	s.progress.PauseReason = ""
	s.progress.Phase = "idle"
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) setStopping(requestedAction string) {
	s.mu.Lock()
	if s.running {
		s.progress.State = "stopping"
		s.progress.RequestedAction = requestedAction
		s.progress.PauseReason = ""
		s.progress.Phase = "stopping"
	}
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) setPausedProgress(reason string, running bool) {
	s.mu.Lock()
	s.running = running
	if !running {
		s.cancel = nil
	}
	s.progress.State = "paused"
	s.progress.RequestedAction = "resume"
	s.progress.PauseReason = reason
	s.progress.Phase = "waiting"
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) statusSnapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Running: s.running, LastStart: s.lastStart, Progress: cloneProgress(s.progress)}
}

func cloneProgress(progress Progress) Progress {
	progress.Roots = append([]string(nil), progress.Roots...)
	if len(progress.RootStats) > 0 {
		rootStats := make(map[string]RootProgress, len(progress.RootStats))
		for root, stat := range progress.RootStats {
			rootStats[root] = stat
		}
		progress.RootStats = rootStats
	}
	return progress
}

func (s *Scanner) publishStatus() {
	status := s.statusSnapshot()
	status.Revision = nextStatusRevision()
	if s.StatusReporter != nil {
		go func() {
			_ = s.StatusReporter.SetScanStatus(context.Background(), status)
		}()
	}
	if s.Events == nil {
		return
	}
	s.Events.Publish(events.Event{Type: "scan_status", Payload: status})
}

func nextStatusRevision() int64 {
	return time.Now().UnixNano()
}

func (s *Scanner) run(ctx context.Context, req scanRequest) {
	logger := s.Logger.With("reason", req.reason)
	priority := 100
	if strings.HasPrefix(req.reason, "task:") || req.reason == globalScanReason {
		priority = 50
	}
	sourceBatchID, _ := s.DB.BeginSourceIOBatch(context.Background(), req.reason, priority)
	defer func() {
		state, message := "success", ""
		if ctx.Err() != nil {
			state, message = "preempted", s.currentPauseMessage()
		}
		_ = s.DB.FinishSourceIOBatch(context.Background(), sourceBatchID, state, 0, 0, message)
	}()
	releasePriority := jobs.EnterMediaScanPriority()
	defer releasePriority()
	runID, err := s.DB.StartScanRun(ctx, string(req.task))
	if err != nil {
		logger.Error("start scan run failed", "error", err)
		return
	}
	var scanRoots []string
	if req.hasOverride {
		scanRoots, err = db.NormalizeScanFolders(req.roots)
		if err != nil {
			logger.Error("normalize scan roots failed", "error", err)
			message := "扫描文件夹无效"
			_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
			return
		}
	} else {
		scanRoots, _, err = s.DB.GetScanFolders(ctx)
		if err != nil {
			logger.Error("load scan folders failed", "error", err)
			message := "读取扫描文件夹失败"
			_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
			return
		}
	}
	s.updateProgressRoots(scanRoots)
	if len(scanRoots) == 0 {
		s.updateProgressPhase("finished")
		if err := s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "finished"}); err != nil {
			logger.Error("finish empty scan run failed", "error", err)
		}
		return
	}
	if unavailable := s.unavailableScanRoots(scanRoots); len(unavailable) > 0 {
		message := "存储不可达，扫描已暂停"
		s.updateProgressPhase("storage_unavailable")
		if err := s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "skipped", LastError: &message}); err != nil {
			logger.Warn("finish unavailable storage scan failed", "error", err)
		}
		logger.Warn("scan skipped because storage is unavailable", "roots", unavailable)
		return
	}
	switch req.task {
	case scanTaskCount:
		s.runCount(ctx, runID, scanRoots, req.reason, logger)
		return
	case scanTaskReconcile:
		s.runReconcile(ctx, runID, scanRoots, logger)
		return
	case scanTaskThumbRebuild:
		s.runThumbnailRebuild(ctx, runID, scanRoots, logger)
		return
	case scanTaskThumbContinue:
		s.runThumbnailContinue(ctx, runID, scanRoots, logger)
		return
	}
	activeBefore, err := s.DB.ActiveRelPathsForRoots(ctx, scanRoots)
	if err != nil {
		logger.Error("load active assets failed", "error", err)
		message := "读取现有资源失败"
		_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
		return
	}
	counts := counters{rootSeen: make(map[string]int, len(scanRoots))}
	s.updateProgressPhase("discovering")
	seen := make(map[string]struct{}, len(activeBefore))
	state := s.newScanState(ctx, seen, &counts, scanRoots, req.task)
	state.forceMetadata = req.task == scanTaskMetadata && len(req.paths) > 0
	state.start(s.scanWorkerCount())
	failedRoots := map[string]struct{}{}
	if len(req.paths) > 0 {
		for _, rel := range req.paths {
			if ctx.Err() != nil {
				break
			}
			if err := s.submitRelPath(ctx, rel, state); err != nil {
				state.recordError("扫描文件失败", err)
				logger.Warn("path scan failed", "relPath", rel, "error", err)
			}
		}
	} else {
		for _, root := range scanRoots {
			if ctx.Err() != nil {
				break
			}
			var walkErr error
			if req.excludeRootFiles {
				walkErr = s.walkNestedRoot(ctx, root, state)
			} else {
				walkErr = s.walkRoot(ctx, root, state)
			}
			if ctx.Err() != nil {
				break
			}
			if walkErr != nil {
				s.Sources.MarkUnavailableForRel(root, walkErr)
				failedRoots[root] = struct{}{}
				state.recordError("扫描目录失败", walkErr)
				logger.Warn("walk failed", "root", root, "error", walkErr)
			}
			if root == "" && walkErr != nil {
				s.walkManifestTopLevel(ctx, state)
			}
		}
	}
	s.updateProgressPhase("scanning")
	state.finish()
	if ctx.Err() != nil {
		s.finishPaused(runID, counts)
		return
	}
	if len(req.paths) == 0 {
		missingPaths := make([]string, 0)
		for rel := range activeBefore {
			if ctx.Err() != nil {
				s.finishPaused(runID, counts)
				return
			}
			if _, ok := seen[rel]; ok {
				continue
			}
			inScanScope := db.AssetInScanFolders(rel, scanRoots)
			if !inScanScope {
				continue
			}
			if req.excludeRootFiles && assetDirectlyInScanRoots(rel, scanRoots) {
				continue
			}
			if assetUnderFailedRoot(rel, failedRoots) {
				continue
			}
			missingPaths = append(missingPaths, rel)
		}
		if len(missingPaths) > 0 {
			missing, err := s.DB.MarkMissingRelPaths(ctx, missingPaths)
			if err != nil {
				counts.recordError("标记缺失媒体失败", err)
				logger.Warn("mark missing media failed", "count", len(missingPaths), "error", err)
			} else {
				counts.assetsDeleted += missing
				s.updateProgressCounts(counts, "")
			}
		}
	}
	s.updateProgressPhase("finalizing")
	if err := s.DB.RefreshFolders(ctx); err != nil {
		counts.recordError("更新文件夹统计失败", err)
		logger.Warn("refresh folders failed", "error", err)
	}
	s.updateProgressPhase("finished")
	status := "finished"
	if counts.errors > 0 {
		status = "finished_with_errors"
	}
	if err := s.DB.FinishScanRun(ctx, runID, db.ScanFinish{
		Status:        status,
		TotalSeen:     counts.totalSeen,
		AssetsAdded:   counts.assetsAdded,
		AssetsUpdated: counts.assetsUpdated,
		AssetsDeleted: counts.assetsDeleted,
		Errors:        counts.errors,
		LastError:     counts.lastError,
	}); err != nil {
		logger.Error("finish scan run failed", "error", err)
	}
	if req.task == scanTaskMediaScan {
		if counts.assetsAdded > 0 || counts.assetsUpdated > 0 {
			state.enqueueDeferredWork()
		}
		if req.reason == globalScanReason {
			s.enqueueGlobalRepair(ctx, scanRoots, logger)
		}
	}
	logger.Info("scan finished", "seen", counts.totalSeen, "added", counts.assetsAdded, "updated", counts.assetsUpdated, "deleted", counts.assetsDeleted, "errors", counts.errors)
}

// enqueueGlobalRepair restores only unfinished work after a full discovery pass.
// Ready media and completed cache products remain untouched.
func (s *Scanner) enqueueGlobalRepair(ctx context.Context, roots []string, logger *slog.Logger) {
	metadataPaths, err := s.DB.MetadataWorkPathsForRoots(ctx, roots)
	if err != nil {
		logger.Warn("load global scan metadata repair work failed", "error", err)
	} else if len(metadataPaths) > 0 {
		result := s.RequestMetadataScanPaths("auto_global_scan_repair", roots, metadataPaths)
		if !result.Accepted {
			logger.Warn("queue global scan metadata repair failed", "count", len(metadataPaths), "state", result.State)
		}
	}

	if s.Jobs == nil {
		return
	}
	for _, taskType := range []string{"thumb", "video_poster", "preview", "storyboard"} {
		items, workErr := s.DB.ContinueWorkForRoots(ctx, taskType, roots)
		if workErr != nil {
			logger.Warn("load global scan repair work failed", "type", taskType, "error", workErr)
			continue
		}
		for _, item := range items {
			s.Jobs.Enqueue(jobs.Task{Type: item.Type, AssetID: item.AssetID, Priority: 50})
		}
	}
}

func (s *Scanner) runReconcile(ctx context.Context, runID int64, scanRoots []string, logger *slog.Logger) {
	activePaths, err := s.DB.ActiveRelPathsForRoots(ctx, scanRoots)
	if err != nil {
		message := "读取现有媒体记录失败"
		_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
		logger.Error("load media records for reconciliation failed", "error", err)
		return
	}

	s.updateProgressPhase("reconciling")
	s.updateProgressTotalFiles(len(activePaths))
	counts := counters{rootSeen: make(map[string]int, len(scanRoots))}
	missingPaths := make([]string, 0)
	for relPath := range activePaths {
		if ctx.Err() != nil {
			s.finishPaused(runID, counts)
			return
		}
		sourcePath, pathErr := s.Store.PhotoPath(relPath)
		if pathErr != nil {
			counts.recordError("解析媒体路径失败", pathErr)
			s.updateProgressCounts(counts, relPath)
			continue
		}
		exists, statErr := sourceFileExists(sourcePath)
		if statErr != nil {
			if s.Sources != nil {
				s.Sources.MarkUnavailableForRel(relPath, statErr)
			}
			message := "存储读取失败，对账已停止"
			counts.recordError(message, statErr)
			s.updateProgressCounts(counts, relPath)
			_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{
				Status: "error", TotalSeen: counts.totalSeen, Errors: counts.errors, LastError: counts.lastError,
			})
			logger.Warn("reconciliation stopped because source lookup failed", "relPath", relPath, "error", statErr)
			return
		}
		counts.totalSeen++
		if !exists {
			missingPaths = append(missingPaths, relPath)
		}
		s.updateProgressCounts(counts, relPath)
	}

	if len(missingPaths) > 0 {
		missing, markErr := s.DB.MarkMissingRelPaths(ctx, missingPaths)
		if markErr != nil {
			counts.recordError("标记缺失媒体失败", markErr)
			logger.Warn("mark reconciled media missing failed", "count", len(missingPaths), "error", markErr)
		} else {
			counts.assetsDeleted = missing
			s.updateProgressCounts(counts, "")
		}
	}
	if err := s.DB.RefreshFolders(ctx); err != nil {
		counts.recordError("更新文件夹统计失败", err)
		logger.Warn("refresh folders after reconciliation failed", "error", err)
	}
	status := "finished"
	if counts.errors > 0 {
		status = "finished_with_errors"
	}
	s.updateProgressPhase("finished")
	_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{
		Status: status, TotalSeen: counts.totalSeen, AssetsDeleted: counts.assetsDeleted,
		Errors: counts.errors, LastError: counts.lastError,
	})
	logger.Info("media reconciliation finished", "checked", counts.totalSeen, "missing", counts.assetsDeleted, "errors", counts.errors)
}

func sourceFileExists(path string) (bool, error) {
	info, err := os.Stat(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}

func (s *Scanner) unavailableScanRoots(scanRoots []string) []string {
	if s.Sources == nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, rel := range scanRoots {
		if rel == "" && s.Store.HasVirtualRoot() {
			for _, rootRel := range s.Store.RootRelPaths() {
				if available, _ := s.Sources.AvailableForRel(rootRel); !available {
					seen[rootRel] = struct{}{}
				}
			}
			continue
		}
		if available, _ := s.Sources.AvailableForRel(rel); !available {
			seen[rel] = struct{}{}
		}
	}
	roots := make([]string, 0, len(seen))
	for rel := range seen {
		roots = append(roots, rel)
	}
	return roots
}

func (s *Scanner) runCount(ctx context.Context, runID int64, scanRoots []string, reason string, logger *slog.Logger) {
	s.updateProgressPhase("counting")
	libraries, _, err := s.DB.GetScanLibraries(ctx)
	if err != nil {
		message := "读取图库失败"
		_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
		logger.Error("load scan libraries for count failed", "error", err)
		return
	}
	total := 0
	errors := 0
	var lastError *string
	countedAnyLibrary := false
	estimatedTotal := 0
	for _, library := range libraries {
		if scanRootsOverlap(library.Roots, scanRoots) {
			estimatedTotal += library.DiscoveredFiles
		}
	}
	s.updateCountProgress(0, estimatedTotal)
	lastReported := 0
	reportProgress := func(counted int) {
		if counted-lastReported < 100 {
			return
		}
		lastReported = counted
		s.updateCountProgress(counted, estimatedTotal)
	}
	for _, library := range libraries {
		if !scanRootsOverlap(library.Roots, scanRoots) {
			continue
		}
		base := total
		count, err := CountMediaFilesForRootsProgress(ctx, s.Store, library.Roots, func(current int) {
			reportProgress(base + current)
		})
		if ctx.Err() != nil {
			total += count
			s.updateCountProgress(total, estimatedTotal)
			s.finishPaused(runID, counters{totalSeen: total})
			return
		}
		if err != nil {
			errors++
			message := "文件数量扫描失败"
			lastError = &message
			logger.Warn("count library media files failed", "library", library.Name, "roots", library.Roots, "error", err)
		}
		now := util.UnixNow()
		if err := s.DB.UpdateScanLibraryDiscovered(ctx, library.ID, count, now); err != nil {
			errors++
			message := "保存文件数量失败"
			lastError = &message
			logger.Warn("save discovered count failed", "library", library.Name, "error", err)
		}
		total += count
		countedAnyLibrary = true
		s.updateCountProgress(total, estimatedTotal)
		if strings.HasPrefix(reason, "auto_count") && library.DiscoveredAt != nil && library.DiscoveredFiles != count && s.Jobs != nil {
			s.Jobs.Enqueue(jobs.Task{Type: "scan_media", Reason: "task:media_scan:auto_count:" + library.Name, Roots: append([]string(nil), library.Roots...), Priority: 10})
		}
	}
	if !countedAnyLibrary {
		count, err := CountMediaFilesForRootsProgress(ctx, s.Store, scanRoots, reportProgress)
		if ctx.Err() != nil {
			s.updateCountProgress(count, estimatedTotal)
			s.finishPaused(runID, counters{totalSeen: count})
			return
		}
		if err != nil {
			errors++
			message := "文件数量扫描失败"
			lastError = &message
			logger.Warn("count media files failed", "roots", scanRoots, "error", err)
		}
		total = count
		s.updateCountProgress(total, estimatedTotal)
	}
	s.updateCountProgress(total, total)
	s.updateProgressPhase("finished")
	status := "finished"
	if errors > 0 {
		status = "finished_with_errors"
	}
	_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: status, TotalSeen: total, Errors: errors, LastError: lastError})
	logger.Info("file count scan finished", "total", total, "errors", errors)
}

func (s *Scanner) runThumbnailRebuild(ctx context.Context, runID int64, scanRoots []string, logger *slog.Logger) {
	s.updateProgressPhase("thumb_rebuild")
	reset, err := s.DB.ResetAssetThumbnailsForRoots(ctx, scanRoots)
	if err != nil {
		message := "重置缩略图状态失败"
		_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
		logger.Error("reset thumbnails for rebuild failed", "error", err)
		return
	}
	items, err := s.DB.ThumbnailWorkForRoots(ctx, scanRoots)
	if err != nil {
		message := "读取缩略图任务失败"
		_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
		logger.Error("load thumbnail rebuild work failed", "error", err)
		return
	}
	if s.Jobs != nil {
		for _, item := range items {
			s.Jobs.Enqueue(jobs.Task{Type: item.Type, AssetID: item.AssetID})
		}
	}
	s.updateProgressTotalFiles(reset)
	s.updateProgressCounts(counters{totalSeen: reset}, "")
	s.updateProgressPhase("finished")
	_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "finished", TotalSeen: reset})
	logger.Info("thumbnail rebuild queued", "reset", reset, "queued", len(items))
}

func (s *Scanner) runThumbnailContinue(ctx context.Context, runID int64, scanRoots []string, logger *slog.Logger) {
	s.updateProgressPhase("thumb_continue")
	items, err := s.DB.ThumbnailWorkForRoots(ctx, scanRoots)
	if err != nil {
		message := "读取未完成缩略图任务失败"
		_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
		logger.Error("load unfinished thumbnail work failed", "error", err)
		return
	}
	if s.Jobs != nil {
		for _, item := range items {
			s.Jobs.Enqueue(jobs.Task{Type: item.Type, AssetID: item.AssetID})
		}
	}
	s.updateProgressTotalFiles(len(items))
	s.updateProgressCounts(counters{totalSeen: len(items)}, "")
	s.updateProgressPhase("finished")
	_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "finished", TotalSeen: len(items)})
	logger.Info("unfinished thumbnails queued", "queued", len(items))
}

func (s *Scanner) finishPaused(runID int64, counts counters) {
	s.updateProgressPhase("paused")
	message := s.currentPauseMessage()
	_ = s.DB.FinishScanRun(context.Background(), runID, db.ScanFinish{
		Status:        "paused",
		TotalSeen:     counts.totalSeen,
		AssetsAdded:   counts.assetsAdded,
		AssetsUpdated: counts.assetsUpdated,
		AssetsDeleted: counts.assetsDeleted,
		Errors:        counts.errors,
		LastError:     &message,
	})
}

func (s *Scanner) currentPauseMessage() string {
	s.mu.Lock()
	reason := s.progress.PauseReason
	requestedAction := s.progress.RequestedAction
	s.mu.Unlock()
	switch reason {
	case "playback":
		return "正在播放或加载媒体，媒体扫描暂时暂停；播放结束后将自动继续"
	}
	if requestedAction == "stop" {
		return "用户已停止媒体扫描"
	}
	if requestedAction == "start" {
		return "媒体扫描正在切换到新的执行请求"
	}
	return "媒体扫描已暂停"
}

func (s *Scanner) countScanFiles(ctx context.Context, roots []string, logger *slog.Logger) int {
	s.updateProgressPhase("counting")
	total := 0
	for _, root := range roots {
		count, err := s.countRoot(ctx, root)
		total += count
		s.updateProgressTotalFiles(total)
		if err != nil {
			logger.Warn("count scan files failed", "root", root, "error", err)
			if root == "" {
				total += s.countManifestTopLevel(ctx, logger)
				s.updateProgressTotalFiles(total)
			}
		}
	}
	return total
}

func (s *Scanner) removeDeletedCaches(items []db.DeletedAsset, logger *slog.Logger) int {
	seen := make(map[string]struct{}, len(items))
	errors := 0
	for _, item := range items {
		if item.CacheKey == "" {
			continue
		}
		if _, ok := seen[item.CacheKey]; ok {
			continue
		}
		seen[item.CacheKey] = struct{}{}
		if err := s.removeCacheKey(item.CacheKey); err != nil {
			errors++
			logger.Warn("remove cache failed", "relPath", item.RelPath, "cacheKey", item.CacheKey, "error", err)
		}
	}
	return errors
}

func (s *Scanner) removeCacheKey(cacheKey string) error {
	if cacheKey == "" {
		return nil
	}
	return s.Store.RemoveCache(cacheKey)
}

func (s *Scanner) countRoot(ctx context.Context, rootRel string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	s.updateProgressRoot(rootRel)
	if rootRel == "" && s.Store.HasVirtualRoot() {
		total := 0
		var walkErr error
		for _, rel := range s.Store.RootRelPaths() {
			count, err := s.countRoot(ctx, rel)
			total += count
			if err != nil {
				walkErr = err
			}
		}
		return total, walkErr
	}
	rootPath, err := s.Store.PhotoPath(rootRel)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(rootPath)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, nil
	}
	return s.countDir(ctx, rootPath)
}

func (s *Scanner) countDir(ctx context.Context, dirPath string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	entries, readErr := util.ReadDirPartial(dirPath)
	total := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
		absPath := filepath.Join(dirPath, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			count, err := s.countSymlink(absPath)
			total += count
			if err != nil {
				readErr = err
			}
			continue
		}
		if entry.IsDir() {
			count, err := s.countDir(ctx, absPath)
			total += count
			if err != nil {
				readErr = err
			}
			continue
		}
		if !media.DetectByPath(entry.Name()).OK {
			continue
		}
		total++
	}
	return total, readErr
}

func (s *Scanner) countSymlink(absPath string) (int, error) {
	inside, _, err := s.Store.SymlinkTargetWithinRoot(absPath)
	if err != nil || !inside {
		return 0, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, nil
	}
	if info.Mode().IsRegular() && media.DetectByPath(absPath).OK {
		return 1, nil
	}
	return 0, nil
}

func (s *Scanner) countManifestTopLevel(ctx context.Context, logger *slog.Logger) int {
	folders, err := storage.LoadSourceFolderManifest(s.Store.DataRoot)
	if err != nil {
		logger.Warn("load source folder manifest failed", "error", err)
		return 0
	}
	total := 0
	for _, rel := range storage.ManifestTopLevelFolders(folders) {
		count, err := s.countRoot(ctx, rel)
		total += count
		if err != nil {
			logger.Warn("count manifest folder failed", "root", rel, "error", err)
		}
	}
	return total
}

func (s *Scanner) walkRoot(ctx context.Context, rootRel string, state *scanState) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.updateProgressRoot(rootRel)
	if rootRel == "" && s.Store.HasVirtualRoot() {
		state.submitFolder(rootRel)
		var walkErr error
		for _, rel := range s.Store.RootRelPaths() {
			if err := s.walkRoot(ctx, rel, state); err != nil {
				walkErr = err
				s.Logger.Warn("walk storage root failed", "root", rel, "error", err)
			}
		}
		return walkErr
	}
	rootPath, err := s.Store.PhotoPath(rootRel)
	if err != nil {
		return err
	}
	info, err := os.Stat(rootPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	state.submitFolder(rootRel)
	return s.walkDir(ctx, rootPath, state)
}

func (s *Scanner) walkNestedRoot(ctx context.Context, rootRel string, state *scanState) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.updateProgressRoot(rootRel)
	rootPath, err := s.Store.PhotoPath(rootRel)
	if err != nil {
		return err
	}
	info, err := os.Stat(rootPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	state.submitFolder(rootRel)
	entries, readErr := util.ReadDirPartial(rootPath)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		absPath := filepath.Join(rootPath, entry.Name())
		if err := s.ensureFolderForPath(ctx, absPath, state); err != nil {
			continue
		}
		if err := s.walkDir(ctx, absPath, state); err != nil {
			readErr = err
		}
	}
	return readErr
}

func assetDirectlyInScanRoots(relPath string, roots []string) bool {
	relPath = strings.Trim(strings.TrimSpace(filepath.ToSlash(relPath)), "/")
	for _, root := range roots {
		root = strings.Trim(strings.TrimSpace(filepath.ToSlash(root)), "/")
		remaining := relPath
		if root != "" {
			prefix := root + "/"
			if !strings.HasPrefix(relPath, prefix) {
				continue
			}
			remaining = strings.TrimPrefix(relPath, prefix)
		}
		if remaining != "" && !strings.Contains(remaining, "/") {
			return true
		}
	}
	return false
}

func (s *Scanner) walkDir(ctx context.Context, dirPath string, state *scanState) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	entries, readErr := util.ReadDirPartial(dirPath)
	if readErr != nil {
		state.recordError("读取目录项失败", readErr)
		s.Logger.Warn("walk entry failed", "path", dirPath, "error", readErr)
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		absPath := filepath.Join(dirPath, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			_ = s.handleSymlink(ctx, absPath, entry, state)
			continue
		}
		if entry.IsDir() {
			if err := s.ensureFolderForPath(ctx, absPath, state); err != nil {
				continue
			}
			if err := s.walkDir(ctx, absPath, state); err != nil {
				readErr = err
			}
			continue
		}
		if !media.DetectByPath(entry.Name()).OK {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			state.recordError("读取文件信息失败", err)
			s.Logger.Warn("file info failed", "path", absPath, "error", err)
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		state.submit(absPath, info)
	}
	return readErr
}

func (s *Scanner) walkManifestTopLevel(ctx context.Context, state *scanState) {
	folders, err := storage.LoadSourceFolderManifest(s.Store.DataRoot)
	if err != nil {
		s.Logger.Warn("load source folder manifest failed", "error", err)
		return
	}
	for _, rel := range storage.ManifestTopLevelFolders(folders) {
		if err := s.walkRoot(ctx, rel, state); err != nil {
			state.recordError("扫描目录失败", err)
			s.Logger.Warn("manifest folder walk failed", "root", rel, "error", err)
		}
	}
}

func (s *Scanner) submitRelPath(ctx context.Context, rel string, state *scanState) error {
	normalized, err := storage.NormalizeRelPath(rel)
	if err != nil {
		return err
	}
	if _, ok := scanRootForRel(normalized, state.roots); !ok {
		return nil
	}
	absPath, err := s.Store.PhotoPath(normalized)
	if err != nil {
		return err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			deletedAt := util.UnixNow()
			deleted, markErr := s.DB.MarkDeletedWithCache(ctx, normalized, deletedAt)
			if markErr != nil {
				return markErr
			}
			if deleted != nil {
				s.removeDeletedCaches([]db.DeletedAsset{*deleted}, s.Logger)
			}
			return nil
		}
		return err
	}
	if info.IsDir() {
		return s.walkDir(ctx, absPath, state)
	}
	if !info.Mode().IsRegular() || !media.DetectByPath(info.Name()).OK {
		return nil
	}
	state.submit(absPath, info)
	return nil
}

func (s *Scanner) handleSymlink(ctx context.Context, absPath string, entry fs.DirEntry, state *scanState) error {
	inside, target, err := s.Store.SymlinkTargetWithinRoot(absPath)
	if err != nil {
		state.recordError("解析符号链接失败", err)
		s.Logger.Warn("symlink eval failed", "path", absPath, "error", err)
		return nil
	}
	if !inside {
		s.Logger.Warn("symlink skipped because it escapes photo root", "path", absPath, "target", target)
		return nil
	}
	info, err := os.Stat(absPath)
	if err != nil {
		state.recordError("读取符号链接目标失败", err)
		return nil
	}
	if info.IsDir() {
		s.Logger.Warn("directory symlink skipped to avoid cycles", "path", absPath)
		return filepath.SkipDir
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	state.submit(absPath, info)
	_ = entry
	return nil
}

func assetUnderFailedRoot(rel string, failedRoots map[string]struct{}) bool {
	for root := range failedRoots {
		if root == "" || rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

func scanRootForRel(rel string, roots []string) (string, bool) {
	for _, root := range roots {
		if root == "" || rel == root || strings.HasPrefix(rel, root+"/") {
			return root, true
		}
	}
	return "", false
}

func scanRootsOverlap(a []string, b []string) bool {
	left, err := db.NormalizeScanFolders(a)
	if err != nil {
		return false
	}
	right, err := db.NormalizeScanFolders(b)
	if err != nil {
		return false
	}
	for _, l := range left {
		for _, r := range right {
			if l == "" || r == "" || l == r || strings.HasPrefix(l, r+"/") || strings.HasPrefix(r, l+"/") {
				return true
			}
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Scanner) ensureFolderForPath(ctx context.Context, absPath string, state *scanState) error {
	rel, err := s.Store.RelPath(absPath)
	if err != nil {
		state.recordError("目录路径不安全", err)
		return nil
	}
	state.submitFolder(rel)
	return nil
}

func (s *Scanner) newScanState(ctx context.Context, seen map[string]struct{}, counts *counters, roots []string, task scanTask) *scanState {
	return &scanState{
		scanner:      s,
		ctx:          ctx,
		files:        make(chan scanFile, maxInt(64, s.scanWorkerCount()*4)),
		writes:       make(chan scanWrite, maxInt(64, s.scanWorkerCount()*4)),
		seen:         seen,
		counts:       counts,
		roots:        append([]string(nil), roots...),
		task:         task,
		deferWork:    task == scanTaskMediaScan,
		deferredWork: make(map[int64]scanDeferredWork),
	}
}

func (s *Scanner) scanWorkerCount() int {
	if s.ScanWorkers > 0 {
		return s.ScanWorkers
	}
	return 1
}

func (st *scanState) start(workers int) {
	if workers < 1 {
		workers = 1
	}
	st.writerWG.Add(1)
	go func() {
		defer st.writerWG.Done()
		for write := range st.writes {
			if write.kind == "folder" {
				st.writeFolder(write.folderRel)
				continue
			}
			st.writeAsset(write)
		}
	}()
	for i := 0; i < workers; i++ {
		st.wg.Add(1)
		go func() {
			defer st.wg.Done()
			for file := range st.files {
				st.processFile(file.absPath, file.info)
			}
		}()
	}
}

func (st *scanState) submit(absPath string, info os.FileInfo) {
	root, ok := st.rootForAbsPath(absPath)
	st.scanner.addDiscoveredFile(root, ok)
	select {
	case st.files <- scanFile{absPath: absPath, info: info}:
	case <-st.ctx.Done():
	}
}

func (st *scanState) submitFolder(rel string) {
	select {
	case st.writes <- scanWrite{kind: "folder", folderRel: rel}:
	case <-st.ctx.Done():
	}
}

func (st *scanState) finish() {
	close(st.files)
	st.wg.Wait()
	close(st.writes)
	st.writerWG.Wait()
}

func (st *scanState) markSeen(rel string) {
	root, hasRoot := scanRootForRel(rel, st.roots)
	st.mu.Lock()
	st.seen[rel] = struct{}{}
	st.counts.totalSeen++
	if hasRoot {
		if st.counts.rootSeen == nil {
			st.counts.rootSeen = map[string]int{}
		}
		st.counts.rootSeen[root]++
	}
	counts := st.counts.clone()
	st.mu.Unlock()
	st.scanner.updateProgressCounts(counts, rel)
}

func (st *scanState) addAdded() {
	st.mu.Lock()
	st.counts.assetsAdded++
	st.mu.Unlock()
}

func (st *scanState) addUpdated() {
	st.mu.Lock()
	st.counts.assetsUpdated++
	st.mu.Unlock()
}

func (st *scanState) recordError(publicMessage string, err error) {
	st.mu.Lock()
	st.counts.recordError(publicMessage, err)
	counts := st.counts.clone()
	st.mu.Unlock()
	st.scanner.updateProgressCounts(counts, "")
}

func (st *scanState) updateProgress(currentRelPath string) {
	st.mu.Lock()
	counts := st.counts.clone()
	st.mu.Unlock()
	st.scanner.updateProgressCounts(counts, currentRelPath)
}

func (st *scanState) rootForAbsPath(absPath string) (string, bool) {
	rel, err := st.scanner.Store.RelPath(absPath)
	if err != nil {
		return "", false
	}
	return scanRootForRel(rel, st.roots)
}

func (c counters) clone() counters {
	result := c
	if len(c.rootSeen) > 0 {
		result.rootSeen = make(map[string]int, len(c.rootSeen))
		for root, count := range c.rootSeen {
			result.rootSeen[root] = count
		}
	}
	return result
}

func (st *scanState) processFile(absPath string, info os.FileInfo) {
	s := st.scanner
	ctx := st.ctx
	if ctx.Err() != nil {
		return
	}
	rel, err := s.Store.RelPath(absPath)
	if err != nil {
		st.recordError("文件路径不安全", err)
		return
	}
	detection := media.DetectByPath(info.Name())
	if !detection.OK {
		return
	}
	currentInfo, err := os.Stat(absPath)
	if err != nil {
		if !os.IsNotExist(err) {
			st.recordError("读取文件信息失败", err)
		}
		return
	}
	if !currentInfo.Mode().IsRegular() {
		return
	}
	info = currentInfo
	nfoSignature, nfoErr := s.nfoFileSignature(absPath)
	if nfoErr != nil {
		st.recordError("读取NFO状态失败", nfoErr)
		s.Logger.Warn("nfo signature failed", "relPath", rel, "error", nfoErr)
	}
	nfoChanged := nfoErr == nil
	signature, err := s.DB.AssetSignature(ctx, rel)
	if err != nil {
		st.recordError("读取资源签名失败", err)
		s.Logger.Warn("asset signature failed", "relPath", rel, "error", err)
	}
	if signature != nil {
		nfoChanged = !sameNFOSignature(signature, nfoSignature)
		if signature.Size == info.Size() && signature.Mtime == info.ModTime().Unix() && !nfoChanged && !st.forceMetadata {
			hasSubtitle, hasDanmaku := sidecarFlagsForAsset(absPath, detection.MediaType)
			if signature.HasSubtitle != hasSubtitle || signature.HasDanmaku != hasDanmaku {
				if err := s.DB.SetAssetSidecarFlags(ctx, signature.ID, hasSubtitle, hasDanmaku); err != nil {
					st.recordError("写入附加文件状态失败", err)
					s.Logger.Warn("update sidecar flags failed", "relPath", rel, "error", err)
				} else {
					st.addUpdated()
				}
			}
			st.markSeen(rel)
			if !st.deferWork {
				if asset, err := s.DB.GetAsset(ctx, signature.ID); err == nil {
					s.enqueuePendingWork(asset)
				}
			}
			st.updateProgress(rel)
			return
		}
	}
	if signature == nil {
		nfoChanged = nfoSignature != nil
	}
	importedAt := util.UnixNow()
	mtime := info.ModTime().Unix()
	meta := extractMetadataWithAutomaticRetry(ctx, s.Extractor, absPath, detection, mtime, importedAt)
	if detection.MediaType == model.MediaTypeVideo && !meta.HasVideo && meta.HasAudio {
		detection.MediaType = model.MediaTypeAudio
		detection.MimeType = media.AudioMimeType(detection.Ext)
	}
	mimeType := detection.MimeType
	if meta.MimeType != "" {
		mimeType = meta.MimeType
	}
	browserPlayable := meta.BrowserPlayable
	if detection.MediaType == model.MediaTypeImage {
		browserPlayable = media.BrowserImageDisplayable(mimeType)
	}
	thumbStatus, previewStatus, posterStatus, proxyStatus := db.AssetStatuses(detection.MediaType, browserPlayable, s.VideoProxyEnabled)
	var metadataJSON *string
	if meta.RawJSON != "" {
		metadataJSON = &meta.RawJSON
	}
	hasSubtitle, hasDanmaku := sidecarFlagsForAsset(absPath, detection.MediaType)
	var errorText *string
	if meta.Err != nil {
		if ctx.Err() != nil {
			return
		}
		if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
			return
		}
		text := "元数据提取失败"
		errorText = &text
		st.recordError(text, meta.Err)
		st.updateProgress(rel)
		s.Logger.Warn("metadata extraction failed", "relPath", rel, "error", meta.Err)
	}
	select {
	case st.writes <- scanWrite{
		absPath:         absPath,
		rel:             rel,
		info:            info,
		detection:       detection,
		meta:            meta,
		mimeType:        mimeType,
		browserPlayable: browserPlayable,
		thumbStatus:     thumbStatus,
		previewStatus:   previewStatus,
		posterStatus:    posterStatus,
		proxyStatus:     proxyStatus,
		metadataJSON:    metadataJSON,
		nfoChanged:      nfoChanged,
		hasSubtitle:     hasSubtitle,
		hasDanmaku:      hasDanmaku,
		errorText:       errorText,
	}:
	case <-ctx.Done():
	}
}

func extractMetadataWithAutomaticRetry(ctx context.Context, extractor media.Extractor, path string, detection media.Detection, mtime, importedAt int64) media.Metadata {
	meta := extractor.Extract(ctx, path, detection, mtime, importedAt)
	for attempt := 1; meta.Err != nil && attempt <= jobs.MaxAutomaticRetries && ctx.Err() == nil; attempt++ {
		delay := time.Duration(attempt*attempt) * 250 * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return meta
		case <-timer.C:
		}
		meta = extractor.Extract(ctx, path, detection, mtime, importedAt)
	}
	return meta
}

func (st *scanState) writeFolder(rel string) {
	if st.ctx.Err() != nil {
		return
	}
	if err := st.scanner.DB.EnsureFolder(st.ctx, rel); err != nil {
		st.recordError("写入目录失败", err)
		st.scanner.Logger.Warn("ensure folder failed", "relPath", rel, "error", err)
	}
}

func (st *scanState) writeAsset(write scanWrite) {
	s := st.scanner
	ctx := st.ctx
	rel := write.rel
	if ctx.Err() != nil {
		return
	}
	if err := s.DB.EnsureAssetFolders(ctx, rel); err != nil {
		st.recordError("写入文件夹失败", err)
		st.updateProgress(rel)
		s.Logger.Warn("ensure folders failed", "relPath", rel, "error", err)
		return
	}
	nfoJSON, nfoSearchText, nfoTimelineAt, nfoSize, nfoMtime, nfoScanned := st.nfoMetadata(write.absPath, rel, write.nfoChanged)
	importedAt := util.UnixNow()
	mtime := write.info.ModTime().Unix()
	timelineAt := media.TimelineAt(write.meta.TakenAt, write.meta.VideoCreatedAt, mtime, importedAt)
	if nfoTimelineAt != nil && *nfoTimelineAt > 0 {
		timelineAt = *nfoTimelineAt
	}
	st.markSeen(rel)
	params := db.AssetUpsert{
		RelPath:           rel,
		ParentRelPath:     storage.ParentRelPath(rel),
		Filename:          filepath.Base(write.absPath),
		Ext:               write.detection.Ext,
		MediaType:         write.detection.MediaType,
		MimeType:          &write.mimeType,
		Size:              write.info.Size(),
		Mtime:             mtime,
		Width:             write.meta.Width,
		Height:            write.meta.Height,
		Duration:          write.meta.Duration,
		FPS:               write.meta.FPS,
		VideoCodec:        write.meta.VideoCodec,
		AudioCodec:        write.meta.AudioCodec,
		Container:         write.meta.Container,
		VideoBitrate:      write.meta.VideoBitrate,
		AudioBitrate:      write.meta.AudioBitrate,
		OverallBitrate:    write.meta.OverallBitrate,
		TakenAt:           write.meta.TakenAt,
		ImportedAt:        importedAt,
		TimelineAt:        timelineAt,
		CacheKey:          storage.CacheKey(rel, write.info.Size(), mtime),
		BrowserPlayable:   write.browserPlayable,
		ThumbStatus:       write.thumbStatus,
		PreviewStatus:     write.previewStatus,
		VideoPosterStatus: write.posterStatus,
		VideoProxyStatus:  write.proxyStatus,
		MetadataJSON:      write.metadataJSON,
		NFOJSON:           nfoJSON,
		NFOSearchText:     nfoSearchText,
		NFOSize:           nfoSize,
		NFOMtime:          nfoMtime,
		NFOScanned:        nfoScanned,
		HasSubtitle:       write.hasSubtitle,
		HasDanmaku:        write.hasDanmaku,
		Error:             write.errorText,
	}
	result, err := s.DB.UpsertAssetDetailed(ctx, params)
	if err != nil {
		st.recordError("写入资源失败", err)
		st.updateProgress(rel)
		s.Logger.Warn("upsert asset failed", "relPath", rel, "error", err)
		return
	}
	metadataStatus := model.StatusReady
	if write.errorText != nil {
		metadataStatus = model.StatusError
	}
	if err := s.DB.SetMetadataJobStatus(ctx, result.ID, metadataStatus, write.errorText); err != nil {
		st.recordError("写入媒体信息任务状态失败", err)
		s.Logger.Warn("update metadata job failed", "relPath", rel, "error", err)
	}
	if result.OldCacheKey != "" {
		if err := s.removeCacheKey(result.OldCacheKey); err != nil {
			st.recordError("删除旧缓存失败", err)
			s.Logger.Warn("remove old cache failed", "relPath", rel, "cacheKey", result.OldCacheKey, "error", err)
		}
	}
	if result.Added {
		st.addAdded()
	}
	if result.Updated {
		st.addUpdated()
	}
	st.updateProgress(rel)
	if result.Added || result.Updated {
		if st.deferWork {
			st.deferredWork[result.ID] = scanDeferredWork{assetID: result.ID, mediaType: write.detection.MediaType, previewStatus: write.previewStatus}
		} else {
			s.enqueueWork(result.ID, write.detection.MediaType, write.previewStatus, false)
		}
		return
	}
	if !st.deferWork {
		if asset, err := s.DB.GetAsset(ctx, result.ID); err == nil {
			s.enqueuePendingWork(asset)
		}
	}
}

func (st *scanState) enqueueDeferredWork() {
	for _, work := range st.deferredWork {
		st.scanner.enqueueWork(work.assetID, work.mediaType, work.previewStatus, false)
	}
}

func (st *scanState) nfoMetadata(absPath string, rel string, nfoChanged bool) (*string, *string, *int64, *int64, *int64, bool) {
	scanNFO := nfoChanged
	if !scanNFO {
		nfoJSON, err := st.scanner.DB.AssetNFOJSON(st.ctx, rel)
		if err != nil {
			st.recordError("读取NFO状态失败", err)
			st.scanner.Logger.Warn("read nfo state failed", "relPath", rel, "error", err)
			return nil, nil, nil, nil, nil, false
		}
		if nfoJSON != nil {
			return nil, nil, media.NFOTimelineAtJSON(*nfoJSON), nil, nil, false
		}
	}
	if !scanNFO {
		return nil, nil, nil, nil, nil, false
	}
	root, err := st.scanner.Store.RootForPath(absPath)
	if err != nil {
		st.recordError("NFO路径不安全", err)
		st.scanner.Logger.Warn("nfo root lookup failed", "relPath", rel, "error", err)
		return nil, nil, nil, nil, nil, false
	}
	signature, err := media.NFOFileSignatureForAsset(absPath, root.Path)
	if err != nil {
		st.recordError("读取NFO状态失败", err)
		st.scanner.Logger.Warn("nfo signature failed", "relPath", rel, "error", err)
		return nil, nil, nil, nil, nil, false
	}
	info, err := media.ReadNFOForAsset(absPath, root.Path, media.MaxNFOBytes)
	if err != nil {
		st.recordError("读取NFO失败", err)
		st.scanner.Logger.Warn("read nfo failed", "relPath", rel, "error", err)
		return nil, nil, nil, nil, nil, false
	}
	if info == nil {
		return nil, nil, nil, nil, nil, true
	}
	nfoJSON, err := media.NFOJSON(*info)
	if err != nil {
		st.recordError("解析NFO失败", err)
		st.scanner.Logger.Warn("marshal nfo failed", "relPath", rel, "error", err)
		return nil, nil, nil, nil, nil, false
	}
	nfoSearchText := media.NFOSearchText(*info)
	var nfoSize *int64
	var nfoMtime *int64
	if signature != nil {
		nfoSize = &signature.Size
		nfoMtime = &signature.Mtime
	}
	return &nfoJSON, &nfoSearchText, media.NFOTimelineAt(*info), nfoSize, nfoMtime, true
}

func (s *Scanner) nfoFileSignature(absPath string) (*media.NFOFileSignature, error) {
	root, err := s.Store.RootForPath(absPath)
	if err != nil {
		return nil, err
	}
	return media.NFOFileSignatureForAsset(absPath, root.Path)
}

func sameNFOSignature(signature *db.AssetSignature, current *media.NFOFileSignature) bool {
	if signature == nil {
		return current == nil
	}
	if current == nil {
		return !signature.HasNFO && signature.NFOSize == nil && signature.NFOMtime == nil
	}
	if signature.NFOSize == nil || signature.NFOMtime == nil {
		return false
	}
	return *signature.NFOSize == current.Size && *signature.NFOMtime == current.Mtime
}

func sidecarFlagsForAsset(absPath string, mediaType string) (bool, bool) {
	if mediaType != model.MediaTypeVideo {
		return false, false
	}
	entries, err := os.ReadDir(filepath.Dir(absPath))
	if err != nil {
		return false, false
	}
	base := strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
	hasSubtitle := false
	hasDanmaku := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		switch ext {
		case ".xml":
			if sidecarStemMatches(base, stem) {
				hasDanmaku = true
			}
		case ".ass", ".srt", ".ssa", ".vtt":
			if sidecarStemMatches(base, stem) {
				hasSubtitle = true
			}
		}
		if hasSubtitle && hasDanmaku {
			break
		}
	}
	return hasSubtitle, hasDanmaku
}

func sidecarStemMatches(assetBase string, sidecarStem string) bool {
	base := strings.ToLower(strings.TrimSpace(assetBase))
	stem := strings.ToLower(strings.TrimSpace(sidecarStem))
	return stem == base ||
		strings.HasPrefix(stem, base+".") ||
		strings.HasPrefix(stem, base+" ") ||
		strings.HasPrefix(stem, base+"-") ||
		strings.HasPrefix(stem, base+"_")
}

func (s *Scanner) updateProgressRoot(rootRel string) {
	s.mu.Lock()
	s.progress.CurrentRoot = rootRel
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) updateProgressRoots(roots []string) {
	s.mu.Lock()
	s.progress.Roots = append([]string(nil), roots...)
	rootStats := make(map[string]RootProgress, len(roots))
	for _, root := range roots {
		rootStats[root] = RootProgress{}
	}
	s.progress.RootStats = rootStats
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) updateProgressPhase(phase string) {
	s.mu.Lock()
	s.progress.Phase = phase
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) updateProgressTotalFiles(totalFiles int) {
	s.mu.Lock()
	s.progress.TotalFiles = totalFiles
	if s.progress.DiscoveredFiles < totalFiles {
		s.progress.DiscoveredFiles = totalFiles
	}
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) updateCountProgress(counted int, estimatedTotal int) {
	if counted < 0 {
		counted = 0
	}
	if estimatedTotal < counted {
		estimatedTotal = counted
	}
	s.mu.Lock()
	s.progress.DiscoveredFiles = counted
	s.progress.TotalFiles = estimatedTotal
	s.progress.ScannedFiles = counted
	s.progress.TotalSeen = counted
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) addDiscoveredFile(root string, hasRoot bool) {
	s.mu.Lock()
	if s.running {
		s.progress.DiscoveredFiles++
		s.progress.TotalFiles = s.progress.DiscoveredFiles
		if hasRoot {
			if s.progress.RootStats == nil {
				s.progress.RootStats = map[string]RootProgress{}
			}
			stat := s.progress.RootStats[root]
			stat.DiscoveredFiles++
			stat.TotalFiles = stat.DiscoveredFiles
			s.progress.RootStats[root] = stat
		}
	}
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) updateProgressCounts(counts counters, currentRelPath string) {
	s.mu.Lock()
	s.progress.CurrentRelPath = currentRelPath
	if counts.totalSeen > s.progress.DiscoveredFiles {
		s.progress.DiscoveredFiles = counts.totalSeen
	}
	if s.progress.DiscoveredFiles > s.progress.TotalFiles {
		s.progress.TotalFiles = s.progress.DiscoveredFiles
	}
	if counts.totalSeen > s.progress.TotalFiles {
		s.progress.TotalFiles = counts.totalSeen
	}
	s.progress.ScannedFiles = counts.totalSeen
	s.progress.TotalSeen = counts.totalSeen
	s.progress.AssetsAdded = counts.assetsAdded
	s.progress.AssetsUpdated = counts.assetsUpdated
	s.progress.AssetsDeleted = counts.assetsDeleted
	s.progress.Errors = counts.errors
	if len(counts.rootSeen) > 0 {
		if s.progress.RootStats == nil {
			s.progress.RootStats = map[string]RootProgress{}
		}
		for root, totalSeen := range counts.rootSeen {
			stat := s.progress.RootStats[root]
			if totalSeen > stat.DiscoveredFiles {
				stat.DiscoveredFiles = totalSeen
			}
			if stat.DiscoveredFiles > stat.TotalFiles {
				stat.TotalFiles = stat.DiscoveredFiles
			}
			if totalSeen > stat.TotalFiles {
				stat.TotalFiles = totalSeen
			}
			stat.ScannedFiles = totalSeen
			stat.TotalSeen = totalSeen
			s.progress.RootStats[root] = stat
		}
	}
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) adjustProgressTotal(delta int) {
	if delta == 0 {
		return
	}
	s.mu.Lock()
	if s.running {
		s.progress.TotalFiles += delta
		if s.progress.TotalFiles < s.progress.ScannedFiles {
			s.progress.TotalFiles = s.progress.ScannedFiles
		}
		if s.progress.TotalFiles < 0 {
			s.progress.TotalFiles = 0
		}
		s.progress.DiscoveredFiles = s.progress.TotalFiles
	}
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) enqueueWork(assetID int64, mediaType string, previewStatus string, rebuild bool) {
	if s.Jobs == nil {
		return
	}
	if mediaType == model.MediaTypeImage || mediaType == model.MediaTypeVideo {
		if enabled, err := s.DB.AIExecutionEnabled(context.Background()); err == nil && enabled {
			s.Jobs.Enqueue(jobs.Task{Type: "ai_analyze", AssetID: assetID, Priority: 10})
		}
	}
	if mediaType == model.MediaTypeImage {
		s.Jobs.Enqueue(jobs.Task{Type: "thumb", AssetID: assetID})
	} else if mediaType == model.MediaTypeVideo {
		s.Jobs.Enqueue(jobs.Task{Type: "video_poster", AssetID: assetID})
		if needed, err := s.DB.EnsureStoryboardPending(context.Background(), assetID, true); err == nil && needed {
			s.Jobs.Enqueue(jobs.Task{Type: "storyboard", AssetID: assetID})
		}
	}
	if !rebuild && mediaType == model.MediaTypeImage && previewStatus == model.StatusPending {
		s.Jobs.Enqueue(jobs.Task{Type: "preview", AssetID: assetID})
	}
}

func (s *Scanner) enqueuePendingWork(asset model.Asset) {
	if s.Jobs == nil {
		return
	}
	if recoverableWorkStatus(asset.ThumbStatus) {
		if asset.MediaType == model.MediaTypeVideo {
			s.Jobs.Enqueue(jobs.Task{Type: "video_poster", AssetID: asset.ID})
		} else {
			s.Jobs.Enqueue(jobs.Task{Type: "thumb", AssetID: asset.ID})
		}
	}
	if asset.MediaType == model.MediaTypeVideo && recoverableWorkStatus(asset.VideoPosterStatus) {
		s.Jobs.Enqueue(jobs.Task{Type: "video_poster", AssetID: asset.ID})
	}
	if asset.MediaType == model.MediaTypeVideo {
		if needed, err := s.DB.EnsureStoryboardPending(context.Background(), asset.ID, false); err == nil && needed {
			s.Jobs.Enqueue(jobs.Task{Type: "storyboard", AssetID: asset.ID})
		}
	}
	if asset.MediaType == model.MediaTypeImage && recoverableWorkStatus(asset.PreviewStatus) {
		s.Jobs.Enqueue(jobs.Task{Type: "preview", AssetID: asset.ID})
	}
}

func recoverableWorkStatus(status string) bool {
	return status == model.StatusPending || status == model.StatusProcessing
}

func (c *counters) recordError(publicMessage string, err error) {
	c.errors++
	if c.lastError == nil {
		if strings.TrimSpace(publicMessage) == "" {
			publicMessage = "扫描失败"
		}
		c.lastError = &publicMessage
	}
	_ = errors.Unwrap(err)
}
