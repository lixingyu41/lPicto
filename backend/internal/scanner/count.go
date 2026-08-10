package scanner

import (
	"context"
	"os"
	"path/filepath"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/media"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

func CountMediaFilesForRoots(ctx context.Context, store storage.Store, roots []string) (int, error) {
	return CountMediaFilesForRootsProgress(ctx, store, roots, nil)
}

func CountMediaFilesForRootsProgress(ctx context.Context, store storage.Store, roots []string, onProgress func(int)) (int, error) {
	normalized, err := db.NormalizeScanFolders(roots)
	if err != nil {
		return 0, err
	}
	total := 0
	var firstErr error
	reportFound := func() {
		total++
		if onProgress != nil {
			onProgress(total)
		}
	}
	for _, root := range normalized {
		_, err := countMediaRoot(ctx, store, root, reportFound)
		if err == nil {
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
		if root == "" {
			_, manifestErr := countManifestMediaTopLevel(ctx, store, reportFound)
			if firstErr == nil {
				firstErr = manifestErr
			}
		}
	}
	return total, firstErr
}

func countMediaRoot(ctx context.Context, store storage.Store, rootRel string, onFound func()) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if rootRel == "" && store.HasVirtualRoot() {
		total := 0
		var firstErr error
		for _, rel := range store.RootRelPaths() {
			count, err := countMediaRoot(ctx, store, rel, onFound)
			total += count
			if firstErr == nil {
				firstErr = err
			}
		}
		return total, firstErr
	}
	rootPath, err := store.PhotoPath(rootRel)
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
	return countMediaDir(ctx, store, rootPath, onFound)
}

func countMediaDir(ctx context.Context, store storage.Store, dirPath string, onFound func()) (int, error) {
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
			count, err := countMediaSymlink(store, absPath)
			total += count
			if count > 0 && onFound != nil {
				onFound()
			}
			if readErr == nil {
				readErr = err
			}
			continue
		}
		if entry.IsDir() {
			count, err := countMediaDir(ctx, store, absPath, onFound)
			total += count
			if readErr == nil {
				readErr = err
			}
			continue
		}
		if media.DetectByPath(entry.Name()).OK {
			total++
			if onFound != nil {
				onFound()
			}
		}
	}
	return total, readErr
}

func countMediaSymlink(store storage.Store, absPath string) (int, error) {
	inside, _, err := store.SymlinkTargetWithinRoot(absPath)
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

func countManifestMediaTopLevel(ctx context.Context, store storage.Store, onFound func()) (int, error) {
	folders, err := storage.LoadSourceFolderManifest(store.DataRoot)
	if err != nil {
		return 0, err
	}
	total := 0
	var firstErr error
	for _, rel := range storage.ManifestTopLevelFolders(folders) {
		count, err := countMediaRoot(ctx, store, rel, onFound)
		total += count
		if firstErr == nil {
			firstErr = err
		}
	}
	return total, firstErr
}
