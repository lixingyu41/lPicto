package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const maxEventBatch = 1000

var mediaExtensions = map[string]struct{}{
	".jpg": {}, ".jpeg": {}, ".png": {}, ".webp": {}, ".gif": {}, ".bmp": {}, ".tif": {}, ".tiff": {}, ".heic": {}, ".heif": {},
	".mp4": {}, ".webm": {}, ".mov": {}, ".mkv": {}, ".avi": {}, ".m4v": {},
	".mp3": {}, ".aac": {}, ".m4a": {}, ".flac": {}, ".wav": {}, ".ogg": {}, ".oga": {}, ".opus": {}, ".wma": {}, ".ape": {}, ".alac": {}, ".aif": {}, ".aiff": {}, ".amr": {}, ".ac3": {}, ".mka": {}, ".dsf": {}, ".dff": {},
}

type watchRoot struct {
	name string
	path string
}

type fileCandidate struct {
	rootName string
	relPath  string
	absPath  string
	size     int64
	mtimeNS  int64
	dueAt    time.Time
}

type event struct {
	Root      string `json:"root"`
	Path      string `json:"path"`
	Operation string `json:"operation"`
}

type eventRequest struct {
	InstanceID string  `json:"instanceId"`
	Events     []event `json:"events"`
}

type app struct {
	watcher        *fsnotify.Watcher
	roots          []watchRoot
	watchedDirs    map[string]string
	candidates     map[string]fileCandidate
	outbound       map[string]event
	client         *http.Client
	endpoint       string
	token          string
	instanceID     string
	stableDelay    time.Duration
	heartbeat      time.Duration
	lastConnected  bool
	connectionSeen bool
}

func main() {
	application, err := newApp()
	if err != nil {
		log.Fatal(err)
	}
	defer application.watcher.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := application.installInitialWatches(); err != nil {
		log.Fatal(err)
	}
	log.Printf("NAS 监听已启动：目录=%d，lPicto=%s", len(application.watchedDirs), application.endpoint)
	application.run(ctx)
}

func newApp() (*app, error) {
	endpoint := strings.TrimRight(envOr("LPICTO_URL", "http://192.168.2.97:18080"), "/") + "/api/integrations/nas-watcher/events"
	token := strings.TrimSpace(os.Getenv("LPICTO_WATCHER_TOKEN"))
	if token == "" {
		return nil, errors.New("LPICTO_WATCHER_TOKEN 未配置")
	}
	roots, err := parseRoots(envOr("WATCH_ROOTS", "PIC=/watch/PIC;VID=/watch/VID"))
	if err != nil {
		return nil, err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("创建文件监听器失败: %w", err)
	}
	hostname, _ := os.Hostname()
	return &app{
		watcher: watcher, roots: roots, watchedDirs: map[string]string{}, candidates: map[string]fileCandidate{}, outbound: map[string]event{},
		client: &http.Client{Timeout: 10 * time.Second}, endpoint: endpoint, token: token, instanceID: hostname,
		stableDelay: secondsEnv("STABLE_SECONDS", 3), heartbeat: secondsEnv("HEARTBEAT_SECONDS", 30),
	}, nil
}

func (a *app) installInitialWatches() error {
	for _, root := range a.roots {
		info, err := os.Stat(root.path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("监听目录不可访问 %s=%s: %w", root.name, root.path, err)
		}
		if err := a.addTree(root, root.path, false); err != nil {
			return err
		}
		a.queue(event{Root: root.name, Operation: "recover_root"})
	}
	return nil
}

func (a *app) run(ctx context.Context) {
	workTicker := time.NewTicker(time.Second)
	defer workTicker.Stop()
	heartbeatTicker := time.NewTicker(a.heartbeat)
	defer heartbeatTicker.Stop()
	_ = a.send(ctx, nil)
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-a.watcher.Errors:
			if ok && err != nil {
				log.Printf("文件监听错误：%v", err)
				if errors.Is(err, fsnotify.ErrEventOverflow) {
					for _, root := range a.roots {
						a.queue(event{Root: root.name, Operation: "recover_root"})
					}
					log.Printf("监听事件队列溢出，已请求 lPicto 对全部监听目录补账")
				}
			}
		case fsEvent, ok := <-a.watcher.Events:
			if ok {
				a.handleEvent(fsEvent)
			}
		case now := <-workTicker.C:
			a.promoteStableFiles(now)
			a.flush(ctx)
		case <-heartbeatTicker.C:
			if len(a.outbound) == 0 {
				_ = a.send(ctx, nil)
			}
		}
	}
}

func (a *app) handleEvent(fsEvent fsnotify.Event) {
	root, relPath, ok := a.rootAndRel(fsEvent.Name)
	if !ok || relPath == "" {
		return
	}
	if fsEvent.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		if _, isDir := a.watchedDirs[filepath.Clean(fsEvent.Name)]; isDir {
			a.dropWatchedTree(fsEvent.Name)
			a.queue(event{Root: root.name, Path: relPath, Operation: "remove_tree"})
		} else if isNestedFile(relPath) && isMediaPath(fsEvent.Name) {
			delete(a.candidates, filepath.Clean(fsEvent.Name))
			a.queue(event{Root: root.name, Path: relPath, Operation: "remove"})
		}
		return
	}
	if fsEvent.Op&(fsnotify.Create|fsnotify.Write) == 0 {
		return
	}
	info, err := os.Stat(fsEvent.Name)
	if err != nil {
		return
	}
	if info.IsDir() {
		if fsEvent.Op&fsnotify.Create != 0 {
			if err := a.addTree(root, fsEvent.Name, true); err != nil {
				log.Printf("新增目录监听失败：%s: %v", fsEvent.Name, err)
			}
		}
		return
	}
	if info.Mode().IsRegular() && isNestedFile(relPath) && isMediaPath(fsEvent.Name) {
		a.candidates[filepath.Clean(fsEvent.Name)] = fileCandidate{
			rootName: root.name, relPath: relPath, absPath: filepath.Clean(fsEvent.Name),
			size: info.Size(), mtimeNS: info.ModTime().UnixNano(), dueAt: time.Now().Add(a.stableDelay),
		}
	}
}

func (a *app) promoteStableFiles(now time.Time) {
	for key, candidate := range a.candidates {
		if now.Before(candidate.dueAt) {
			continue
		}
		info, err := os.Stat(candidate.absPath)
		if errors.Is(err, os.ErrNotExist) {
			delete(a.candidates, key)
			a.queue(event{Root: candidate.rootName, Path: candidate.relPath, Operation: "remove"})
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			candidate.dueAt = now.Add(a.stableDelay)
			a.candidates[key] = candidate
			continue
		}
		if info.Size() != candidate.size || info.ModTime().UnixNano() != candidate.mtimeNS {
			candidate.size = info.Size()
			candidate.mtimeNS = info.ModTime().UnixNano()
			candidate.dueAt = now.Add(a.stableDelay)
			a.candidates[key] = candidate
			continue
		}
		delete(a.candidates, key)
		a.queue(event{Root: candidate.rootName, Path: candidate.relPath, Operation: "upsert"})
	}
}

func (a *app) addTree(root watchRoot, start string, enqueueExisting bool) error {
	return filepath.WalkDir(start, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("跳过不可访问路径：%s: %v", current, walkErr)
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			current = filepath.Clean(current)
			if _, exists := a.watchedDirs[current]; !exists {
				if err := a.watcher.Add(current); err != nil {
					return err
				}
				a.watchedDirs[current] = root.name
			}
			return nil
		}
		if enqueueExisting && isMediaPath(current) {
			info, err := entry.Info()
			if err == nil {
				_, relPath, ok := a.rootAndRel(current)
				if ok && isNestedFile(relPath) {
					a.candidates[filepath.Clean(current)] = fileCandidate{rootName: root.name, relPath: relPath, absPath: filepath.Clean(current), size: info.Size(), mtimeNS: info.ModTime().UnixNano(), dueAt: time.Now().Add(a.stableDelay)}
				}
			}
		}
		return nil
	})
}

func (a *app) dropWatchedTree(prefix string) {
	prefix = filepath.Clean(prefix)
	for directory := range a.watchedDirs {
		if directory == prefix || strings.HasPrefix(directory, prefix+string(filepath.Separator)) {
			_ = a.watcher.Remove(directory)
			delete(a.watchedDirs, directory)
		}
	}
}

func (a *app) rootAndRel(filename string) (watchRoot, string, bool) {
	filename = filepath.Clean(filename)
	for _, root := range a.roots {
		relPath, err := filepath.Rel(root.path, filename)
		if err != nil || relPath == "." || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
			continue
		}
		return root, filepath.ToSlash(relPath), true
	}
	return watchRoot{}, "", false
}

func (a *app) queue(item event) {
	key := item.Root + "\x00" + item.Path
	a.outbound[key] = item
}

func (a *app) flush(ctx context.Context) {
	if len(a.outbound) == 0 {
		return
	}
	keys := make([]string, 0, len(a.outbound))
	for key := range a.outbound {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > maxEventBatch {
		keys = keys[:maxEventBatch]
	}
	items := make([]event, 0, len(keys))
	for _, key := range keys {
		items = append(items, a.outbound[key])
	}
	if err := a.send(ctx, items); err != nil {
		return
	}
	for index, key := range keys {
		if current, exists := a.outbound[key]; exists && current == items[index] {
			delete(a.outbound, key)
		}
	}
}

func (a *app) send(ctx context.Context, items []event) error {
	body, err := json.Marshal(eventRequest{InstanceID: a.instanceID, Events: items})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+a.token)
	response, err := a.client.Do(request)
	if err != nil {
		a.reportConnection(false, err.Error())
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		err = fmt.Errorf("lPicto 返回 HTTP %d", response.StatusCode)
		a.reportConnection(false, err.Error())
		return err
	}
	a.reportConnection(true, "")
	if len(items) > 0 {
		log.Printf("已提交媒体事件：%d 项", len(items))
	}
	return nil
}

func (a *app) reportConnection(connected bool, message string) {
	if a.connectionSeen && connected == a.lastConnected {
		return
	}
	a.connectionSeen = true
	a.lastConnected = connected
	if connected {
		log.Printf("已连接 lPicto")
	} else {
		log.Printf("lPicto 暂时无法连接，事件将在本地队列重试：%s", message)
	}
}

func parseRoots(value string) ([]watchRoot, error) {
	var roots []watchRoot
	for _, entry := range strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == '\n' }) {
		name, rootPath, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok {
			return nil, fmt.Errorf("WATCH_ROOTS 项格式错误：%s", entry)
		}
		name = strings.ToUpper(strings.TrimSpace(name))
		rootPath = filepath.Clean(strings.TrimSpace(rootPath))
		if name == "" || rootPath == "." || !filepath.IsAbs(rootPath) {
			return nil, fmt.Errorf("WATCH_ROOTS 项无效：%s", entry)
		}
		roots = append(roots, watchRoot{name: name, path: rootPath})
	}
	if len(roots) == 0 {
		return nil, errors.New("WATCH_ROOTS 为空")
	}
	sort.Slice(roots, func(i, j int) bool { return len(roots[i].path) > len(roots[j].path) })
	return roots, nil
}

func isMediaPath(filename string) bool {
	_, ok := mediaExtensions[strings.ToLower(filepath.Ext(filename))]
	return ok
}

func isNestedFile(relPath string) bool {
	relPath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(relPath)))
	return relPath != "." && relPath != "" && strings.Contains(relPath, "/")
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func secondsEnv(key string, fallback int) time.Duration {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value < 1 {
		value = fallback
	}
	return time.Duration(value) * time.Second
}
