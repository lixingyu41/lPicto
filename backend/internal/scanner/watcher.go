package scanner

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/media"
	"lpicto/backend/internal/util"
)

const watchPathBatchLimit = 100

func (s *Scanner) StartWatcher(ctx context.Context, debounce time.Duration) {
	go func() {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			s.Logger.Warn("fsnotify disabled", "error", err)
			return
		}
		defer watcher.Close()
		s.addExistingWatches(watcher)
		timer := time.NewTimer(time.Hour)
		if !timer.Stop() {
			<-timer.C
		}
		pendingPaths := map[string]struct{}{}
		pendingRoots := map[string]struct{}{}
		fullMetadata := false
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-watcher.Errors:
				if err != nil {
					s.Logger.Warn("fsnotify error", "error", err)
				}
			case event := <-watcher.Events:
				if event.Name != "" {
					rel, root, full := s.handleWatchEvent(watcher, event)
					if rel != "" {
						pendingPaths[rel] = struct{}{}
					}
					if root != "" {
						pendingRoots[root] = struct{}{}
					}
					if full {
						fullMetadata = true
					}
					timer.Reset(debounce)
				}
			case <-timer.C:
				if len(pendingPaths) > 0 || len(pendingRoots) > 0 || fullMetadata {
					s.flushWatchMetadata(pendingPaths, pendingRoots, fullMetadata)
					pendingPaths = map[string]struct{}{}
					pendingRoots = map[string]struct{}{}
					fullMetadata = false
				}
			}
		}
	}()
}

func (s *Scanner) addExistingWatches(watcher *fsnotify.Watcher) {
	for _, root := range s.Store.Roots {
		err := filepath.WalkDir(root.Path, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				s.Logger.Warn("watch walk failed", "path", path, "error", err)
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return filepath.SkipDir
			}
			if entry.IsDir() {
				if err := watcher.Add(path); err != nil {
					s.Logger.Warn("watch add failed", "path", path, "error", err)
				}
			}
			return nil
		})
		if err != nil {
			s.Logger.Warn("watch setup failed", "root", root.Path, "error", err)
		}
	}
}

func (s *Scanner) handleWatchEvent(watcher *fsnotify.Watcher, event fsnotify.Event) (string, string, bool) {
	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		s.handleRemovedPath(event.Name)
		if root := s.watchRootForPath(event.Name); root != "" && s.Jobs != nil {
			s.Jobs.Enqueue(jobs.Task{Type: "scan_count", Reason: "fsnotify_remove", Roots: []string{root}})
		}
		return "", "", false
	}
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) != 0 {
		return s.handleCreatedPath(watcher, event.Name)
	}
	return "", "", false
}

func (s *Scanner) handleCreatedPath(watcher *fsnotify.Watcher, name string) (string, string, bool) {
	info, err := os.Stat(name)
	if err != nil {
		return "", "", false
	}
	rel, relErr := s.Store.RelPath(name)
	rootRel := s.watchRootForPath(name)
	if !info.IsDir() {
		if info.Mode().IsRegular() && media.DetectByPath(name).OK {
			s.adjustProgressTotal(1)
			if relErr == nil {
				return rel, rootRel, false
			}
		}
		return "", rootRel, false
	}
	added := 0
	filepath.WalkDir(name, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			s.Logger.Warn("watch new dir walk failed", "path", path, "error", err)
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			if err := watcher.Add(path); err != nil {
				s.Logger.Warn("watch add new dir failed", "path", path, "error", err)
			}
			return nil
		}
		if media.DetectByPath(entry.Name()).OK {
			added++
		}
		return nil
	})
	s.adjustProgressTotal(added)
	return "", rootRel, true
}

func (s *Scanner) handleRemovedPath(name string) {
	rel, err := s.Store.RelPath(name)
	if err != nil {
		return
	}
	deleted, err := s.DB.MarkDeletedUnder(context.Background(), rel, util.UnixNow())
	if err != nil {
		s.Logger.Warn("watch mark deleted failed", "relPath", rel, "error", err)
		return
	}
	if len(deleted) == 0 {
		return
	}
	s.removeDeletedCaches(deleted, s.Logger)
	s.adjustProgressTotal(-len(deleted))
}

func (s *Scanner) flushWatchMetadata(paths map[string]struct{}, roots map[string]struct{}, fullMetadata bool) {
	if s.Jobs == nil {
		return
	}
	rootList := mapKeys(roots)
	if len(rootList) == 0 {
		rootList = s.Store.RootRelPaths()
	}
	if fullMetadata || len(paths) > watchPathBatchLimit {
		s.Jobs.Enqueue(jobs.Task{Type: "scan_metadata", Reason: "fsnotify", Roots: rootList})
		return
	}
	s.Jobs.Enqueue(jobs.Task{Type: "scan_metadata_paths", Reason: "fsnotify", Roots: rootList, Paths: mapKeys(paths)})
}

func (s *Scanner) watchRootForPath(name string) string {
	rel, err := s.Store.RelPath(name)
	if err != nil {
		return ""
	}
	root, _, err := s.Store.RootForRel(rel)
	if err != nil {
		return ""
	}
	return strings.Trim(root.ID, "/")
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func watchLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
