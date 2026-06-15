package scanner

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"lpicto/backend/internal/media"
	"lpicto/backend/internal/util"
)

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
		pending := false
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
					s.handleWatchEvent(watcher, event)
					pending = true
					timer.Reset(debounce)
				}
			case <-timer.C:
				if pending {
					pending = false
					s.Trigger("fsnotify")
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

func (s *Scanner) handleWatchEvent(watcher *fsnotify.Watcher, event fsnotify.Event) {
	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		s.handleRemovedPath(event.Name)
	}
	if event.Op&fsnotify.Create != 0 {
		s.handleCreatedPath(watcher, event.Name)
	}
}

func (s *Scanner) handleCreatedPath(watcher *fsnotify.Watcher, name string) {
	info, err := os.Stat(name)
	if err != nil {
		return
	}
	if !info.IsDir() {
		if info.Mode().IsRegular() && media.DetectByPath(name).OK {
			s.adjustProgressTotal(1)
		}
		return
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

func watchLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
