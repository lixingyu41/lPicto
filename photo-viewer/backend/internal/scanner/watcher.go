package scanner

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
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
	err := filepath.WalkDir(s.Store.PhotoRoot, func(path string, entry fs.DirEntry, err error) error {
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
		s.Logger.Warn("watch setup failed", "error", err)
	}
}

func (s *Scanner) handleWatchEvent(watcher *fsnotify.Watcher, event fsnotify.Event) {
	if event.Op&fsnotify.Create == 0 {
		return
	}
	info, err := os.Stat(event.Name)
	if err != nil || !info.IsDir() {
		return
	}
	filepath.WalkDir(event.Name, func(path string, entry fs.DirEntry, err error) error {
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
		}
		return nil
	})
}

func watchLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
