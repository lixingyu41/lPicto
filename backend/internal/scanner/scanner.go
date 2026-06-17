package scanner

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
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
	VideoProxyEnabled bool
	ScanWorkers       int
	Logger            *slog.Logger

	startOnce sync.Once
	commands  chan scanCommand

	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
	lastStart int64
	progress  Progress
}

type Controller = Scanner

type Status struct {
	Running   bool           `json:"running"`
	LastStart int64          `json:"lastStart"`
	LastRun   *model.ScanRun `json:"lastRun"`
	Progress  Progress       `json:"progress"`
}

type Progress struct {
	State           string   `json:"state"`
	RequestedAction string   `json:"requestedAction"`
	Reason          string   `json:"reason"`
	Phase           string   `json:"phase"`
	Roots           []string `json:"roots"`
	CurrentRoot     string   `json:"currentRoot"`
	CurrentRelPath  string   `json:"currentRelPath"`
	DiscoveredFiles int      `json:"discoveredFiles"`
	TotalFiles      int      `json:"totalFiles"`
	ScannedFiles    int      `json:"scannedFiles"`
	TotalSeen       int      `json:"totalSeen"`
	AssetsAdded     int      `json:"assetsAdded"`
	AssetsUpdated   int      `json:"assetsUpdated"`
	AssetsDeleted   int      `json:"assetsDeleted"`
	Errors          int      `json:"errors"`
}

type counters struct {
	totalSeen     int
	assetsAdded   int
	assetsUpdated int
	assetsDeleted int
	errors        int
	lastError     *string
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
	errorText       *string
}

type scanState struct {
	scanner  *Scanner
	ctx      context.Context
	files    chan scanFile
	writes   chan scanWrite
	seen     map[string]struct{}
	counts   *counters
	rebuild  bool
	mu       sync.Mutex
	wg       sync.WaitGroup
	writerWG sync.WaitGroup
}

type scanRequest struct {
	reason      string
	roots       []string
	hasOverride bool
	rebuild     bool
}

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
	return s.RequestScan(reason).Started
}

func (s *Scanner) TriggerRoots(reason string, roots []string) bool {
	return s.RequestScanRoots(reason, roots).Started
}

func (s *Scanner) TriggerRebuild(reason string) bool {
	return s.RequestRebuild(reason).Started
}

func (s *Scanner) RequestScan(reason string) CommandResult {
	return s.requestStart(scanRequest{reason: reason})
}

func (s *Scanner) RequestScanRoots(reason string, roots []string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, roots: append([]string(nil), roots...), hasOverride: true})
}

func (s *Scanner) RequestRebuild(reason string) CommandResult {
	return s.requestStart(scanRequest{reason: reason, rebuild: true})
}

func (s *Scanner) requestStart(req scanRequest) CommandResult {
	if strings.TrimSpace(req.reason) == "" {
		req.reason = "manual"
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
	var cancel context.CancelFunc
	var done <-chan struct{}
	var pendingStart *scanRequest
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
					cancel, done = s.startRun(ctx, req)
					cmd.reply <- CommandResult{Accepted: true, Started: true, State: "running"}
					continue
				}
				pendingStart = &req
				if cancel != nil {
					cancel()
				}
				s.setStopping("start")
				cmd.reply <- CommandResult{Accepted: true, Started: false, State: "stopping"}
			case scanCommandStop:
				pendingStart = nil
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
			if pendingStart != nil && ctx.Err() == nil {
				next := *pendingStart
				pendingStart = nil
				cancel, done = s.startRun(ctx, next)
				continue
			}
			s.setIdleAfterRun()
		}
	}
}

func (s *Scanner) startRun(parent context.Context, req scanRequest) (context.CancelFunc, <-chan struct{}) {
	s.mu.Lock()
	s.running = true
	s.lastStart = util.UnixNow()
	s.progress = Progress{
		State:           "running",
		RequestedAction: "start",
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
	s.progress.Phase = "idle"
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) setStopping(requestedAction string) {
	s.mu.Lock()
	if s.running {
		s.progress.State = "stopping"
		s.progress.RequestedAction = requestedAction
		s.progress.Phase = "stopping"
	}
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Scanner) statusSnapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Running: s.running, LastStart: s.lastStart, Progress: s.progress}
}

func (s *Scanner) publishStatus() {
	if s.Events == nil {
		return
	}
	s.Events.Publish(events.Event{Type: "scan_status", Payload: s.statusSnapshot()})
}

func (s *Scanner) run(ctx context.Context, req scanRequest) {
	logger := s.Logger.With("reason", req.reason)
	runID, err := s.DB.StartScanRun(ctx)
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
	activeBefore, err := s.DB.ActiveRelPathsForRoots(ctx, scanRoots)
	if err != nil {
		logger.Error("load active assets failed", "error", err)
		message := "读取现有资源失败"
		_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
		return
	}
	counts := counters{}
	s.updateProgressPhase("discovering")
	seen := make(map[string]struct{}, len(activeBefore))
	state := s.newScanState(ctx, seen, &counts, req.rebuild)
	state.start(s.scanWorkerCount())
	failedRoots := map[string]struct{}{}
	for _, root := range scanRoots {
		if ctx.Err() != nil {
			break
		}
		walkErr := s.walkRoot(ctx, root, state)
		if ctx.Err() != nil {
			break
		}
		if walkErr != nil {
			failedRoots[root] = struct{}{}
			state.recordError("扫描目录失败", walkErr)
			logger.Warn("walk failed", "root", root, "error", walkErr)
		}
		if root == "" && walkErr != nil {
			s.walkManifestTopLevel(ctx, state)
		}
	}
	s.updateProgressPhase("scanning")
	state.finish()
	if ctx.Err() != nil {
		s.finishPaused(runID, counts)
		return
	}
	deletedAt := util.UnixNow()
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
		if assetUnderFailedRoot(rel, failedRoots) {
			continue
		}
		deleted, err := s.DB.MarkDeletedWithCache(ctx, rel, deletedAt)
		if err != nil {
			counts.recordError("标记删除失败", err)
			logger.Warn("mark deleted failed", "relPath", rel, "error", err)
			continue
		}
		if deleted == nil {
			continue
		}
		if err := s.removeCacheKey(deleted.CacheKey); err != nil {
			counts.recordError("删除缓存失败", err)
			logger.Warn("remove cache failed", "relPath", rel, "cacheKey", deleted.CacheKey, "error", err)
		}
		counts.assetsDeleted++
		s.updateProgressCounts(counts, rel)
	}
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
	logger.Info("scan finished", "seen", counts.totalSeen, "added", counts.assetsAdded, "updated", counts.assetsUpdated, "deleted", counts.assetsDeleted, "errors", counts.errors)
}

func (s *Scanner) finishPaused(runID int64, counts counters) {
	s.updateProgressPhase("paused")
	message := "扫描已暂停"
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

func (s *Scanner) newScanState(ctx context.Context, seen map[string]struct{}, counts *counters, rebuild bool) *scanState {
	return &scanState{
		scanner: s,
		ctx:     ctx,
		files:   make(chan scanFile, maxInt(64, s.scanWorkerCount()*4)),
		writes:  make(chan scanWrite, maxInt(64, s.scanWorkerCount()*4)),
		seen:    seen,
		counts:  counts,
		rebuild: rebuild,
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
	st.scanner.addDiscoveredFile()
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
	st.mu.Lock()
	st.seen[rel] = struct{}{}
	st.counts.totalSeen++
	counts := *st.counts
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
	counts := *st.counts
	st.mu.Unlock()
	st.scanner.updateProgressCounts(counts, "")
}

func (st *scanState) updateProgress(currentRelPath string) {
	st.mu.Lock()
	counts := *st.counts
	st.mu.Unlock()
	st.scanner.updateProgressCounts(counts, currentRelPath)
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
	importedAt := util.UnixNow()
	mtime := info.ModTime().Unix()
	meta := s.Extractor.Extract(ctx, absPath, detection, mtime, importedAt)
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
		errorText:       errorText,
	}:
	case <-ctx.Done():
	}
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
	nfoJSON, nfoSearchText, nfoScanned := st.nfoMetadata(write.absPath, rel)
	importedAt := util.UnixNow()
	mtime := write.info.ModTime().Unix()
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
		TakenAt:           write.meta.TakenAt,
		ImportedAt:        importedAt,
		TimelineAt:        media.TimelineAt(write.meta.TakenAt, write.meta.VideoCreatedAt, mtime, importedAt),
		CacheKey:          storage.CacheKey(rel, write.info.Size(), mtime),
		BrowserPlayable:   write.browserPlayable,
		ThumbStatus:       write.thumbStatus,
		PreviewStatus:     write.previewStatus,
		VideoPosterStatus: write.posterStatus,
		VideoProxyStatus:  write.proxyStatus,
		MetadataJSON:      write.metadataJSON,
		NFOJSON:           nfoJSON,
		NFOSearchText:     nfoSearchText,
		NFOScanned:        nfoScanned,
		Error:             write.errorText,
	}
	result, err := s.DB.UpsertAssetDetailed(ctx, params)
	if err != nil {
		st.recordError("写入资源失败", err)
		st.updateProgress(rel)
		s.Logger.Warn("upsert asset failed", "relPath", rel, "error", err)
		return
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
	if st.rebuild {
		if err := s.DB.ResetAssetThumbnail(ctx, result.ID); err != nil {
			st.recordError("重建缩略图失败", err)
			s.Logger.Warn("reset thumbnail failed", "relPath", rel, "assetID", result.ID, "error", err)
		}
		if err := s.Store.RemoveCacheVariant(params.CacheKey, "thumbs", "webp"); err != nil {
			st.recordError("删除缩略图缓存失败", err)
			s.Logger.Warn("remove thumbnail cache failed", "relPath", rel, "cacheKey", params.CacheKey, "error", err)
		}
	}
	st.updateProgress(rel)
	if result.Added || result.Updated || st.rebuild {
		s.enqueueWork(result.ID, write.detection.MediaType, write.previewStatus, write.proxyStatus, st.rebuild)
		return
	}
	if asset, err := s.DB.GetAsset(ctx, result.ID); err == nil {
		s.enqueuePendingWork(asset)
	}
}

func (st *scanState) nfoMetadata(absPath string, rel string) (*string, *string, bool) {
	scanNFO := st.rebuild
	if !scanNFO {
		hasNFO, err := st.scanner.DB.AssetHasNFO(st.ctx, rel)
		if err != nil {
			st.recordError("读取NFO状态失败", err)
			st.scanner.Logger.Warn("read nfo state failed", "relPath", rel, "error", err)
			return nil, nil, false
		}
		scanNFO = !hasNFO
	}
	if !scanNFO {
		return nil, nil, false
	}
	root, err := st.scanner.Store.RootForPath(absPath)
	if err != nil {
		st.recordError("NFO路径不安全", err)
		st.scanner.Logger.Warn("nfo root lookup failed", "relPath", rel, "error", err)
		return nil, nil, false
	}
	info, err := media.ReadNFOForAsset(absPath, root.Path, media.MaxNFOBytes)
	if err != nil {
		st.recordError("读取NFO失败", err)
		st.scanner.Logger.Warn("read nfo failed", "relPath", rel, "error", err)
		return nil, nil, false
	}
	if info == nil {
		return nil, nil, st.rebuild
	}
	nfoJSON, err := media.NFOJSON(*info)
	if err != nil {
		st.recordError("解析NFO失败", err)
		st.scanner.Logger.Warn("marshal nfo failed", "relPath", rel, "error", err)
		return nil, nil, false
	}
	nfoSearchText := media.NFOSearchText(*info)
	return &nfoJSON, &nfoSearchText, true
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

func (s *Scanner) addDiscoveredFile() {
	s.mu.Lock()
	if s.running {
		s.progress.DiscoveredFiles++
		s.progress.TotalFiles = s.progress.DiscoveredFiles
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

func (s *Scanner) enqueueWork(assetID int64, mediaType string, previewStatus string, proxyStatus string, rebuild bool) {
	if s.Jobs == nil {
		return
	}
	if mediaType == model.MediaTypeImage || mediaType == model.MediaTypeVideo {
		s.Jobs.Enqueue(jobs.Task{Type: "thumb", AssetID: assetID})
	}
	if !rebuild && mediaType == model.MediaTypeImage && previewStatus == model.StatusPending {
		s.Jobs.Enqueue(jobs.Task{Type: "preview", AssetID: assetID})
	}
	if !rebuild && mediaType == model.MediaTypeVideo && proxyStatus == model.StatusPending {
		s.Jobs.Enqueue(jobs.Task{Type: "video_proxy", AssetID: assetID})
	}
}

func (s *Scanner) enqueuePendingWork(asset model.Asset) {
	if s.Jobs == nil {
		return
	}
	if recoverableWorkStatus(asset.ThumbStatus) {
		s.Jobs.Enqueue(jobs.Task{Type: "thumb", AssetID: asset.ID})
	}
	if asset.MediaType == model.MediaTypeImage && recoverableWorkStatus(asset.PreviewStatus) {
		s.Jobs.Enqueue(jobs.Task{Type: "preview", AssetID: asset.ID})
	}
	if asset.MediaType == model.MediaTypeVideo && s.VideoProxyEnabled && recoverableWorkStatus(asset.VideoProxyStatus) {
		s.Jobs.Enqueue(jobs.Task{Type: "video_proxy", AssetID: asset.ID})
	}
}

func recoverableWorkStatus(status string) bool {
	return status == model.StatusPending || status == model.StatusProcessing || status == model.StatusError
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
