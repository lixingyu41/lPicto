package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/events"
	"lpicto/backend/internal/media"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

const (
	assetDeleteModeFiles  = "files"
	assetDeleteModeFolder = "folder"
)

type assetDeletePlanInternal struct {
	asset          model.Asset
	mode           string
	token          string
	files          []assetDeleteEntry
	folder         *assetDeleteEntry
	folderContents []assetDeleteEntry
	warnings       []string
	blockers       []string
}

type assetDeleteEntry struct {
	relPath string
	absPath string
	name    string
	kind    string
	size    int64
	mtime   int64
	reason  string
	isMedia bool
}

func (s *Server) assetDeletePlan(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	plan, err := s.buildAssetDeletePlan(asset)
	if err != nil {
		s.writeAssetDeletePlanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, assetDeletePlanDTO(plan))
}

func (s *Server) deleteAsset(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	var payload AssetDeleteConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_delete_confirm", "删除确认无效")
		return
	}
	plan, err := s.buildAssetDeletePlan(asset)
	if err != nil {
		s.writeAssetDeletePlanError(w, err)
		return
	}
	if strings.TrimSpace(payload.Token) == "" || payload.Token != plan.token || len(plan.blockers) > 0 {
		writeJSON(w, http.StatusConflict, AssetDeleteConflictDTO{Stale: true, Plan: assetDeletePlanDTO(plan)})
		return
	}
	result := s.executeAssetDeletePlan(r.Context(), plan)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeAssetDeletePlanError(w http.ResponseWriter, err error) {
	var planErr assetDeletePlanError
	if errors.As(err, &planErr) {
		writeError(w, planErr.status, planErr.code, planErr.message)
		return
	}
	writeError(w, http.StatusInternalServerError, "asset_delete_plan_failed", "计算删除范围失败")
}

type assetDeletePlanError struct {
	status  int
	code    string
	message string
}

func (e assetDeletePlanError) Error() string {
	return e.message
}

func (s *Server) buildAssetDeletePlan(asset model.Asset) (assetDeletePlanInternal, error) {
	root, _, err := s.store.RootForRel(asset.RelPath)
	if err != nil {
		return assetDeletePlanInternal{}, assetDeletePlanError{status: http.StatusBadRequest, code: "asset_root_invalid", message: "资源路径无效"}
	}
	if !sourceRootAvailable(root.Path) {
		return assetDeletePlanInternal{}, assetDeletePlanError{status: http.StatusServiceUnavailable, code: "asset_root_unavailable", message: "来源不可用，不能执行物理删除"}
	}
	assetPath, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		return assetDeletePlanInternal{}, assetDeletePlanError{status: http.StatusNotFound, code: "asset_not_found", message: "资源不存在"}
	}
	info, err := os.Lstat(assetPath)
	if errors.Is(err, os.ErrNotExist) {
		return assetDeletePlanInternal{}, assetDeletePlanError{status: http.StatusNotFound, code: "asset_not_found", message: "原文件不存在"}
	}
	if err != nil {
		return assetDeletePlanInternal{}, err
	}
	if info.IsDir() {
		return assetDeletePlanInternal{}, assetDeletePlanError{status: http.StatusBadRequest, code: "asset_is_folder", message: "资源路径不是媒体文件"}
	}

	parentAbs := filepath.Dir(assetPath)
	if !storage.IsWithinRoot(root.Path, parentAbs) {
		return assetDeletePlanInternal{}, assetDeletePlanError{status: http.StatusBadRequest, code: "asset_parent_invalid", message: "资源父文件夹无效"}
	}
	parentRel := storage.ParentRelPath(asset.RelPath)
	directMediaCount, err := s.directMediaCount(parentAbs)
	if err != nil {
		return assetDeletePlanInternal{}, err
	}

	plan := assetDeletePlanInternal{asset: asset, mode: assetDeleteModeFiles}
	if directMediaCount == 1 && !s.protectedDeleteFolder(parentRel) {
		contents, hasOtherMedia, err := s.folderDeleteContents(root.Path, parentAbs, asset.RelPath)
		if err != nil {
			return assetDeletePlanInternal{}, err
		}
		if !hasOtherMedia {
			folder, err := s.deleteEntryForPath(root.Path, parentAbs, "媒体所在文件夹")
			if err != nil {
				return assetDeletePlanInternal{}, err
			}
			plan.mode = assetDeleteModeFolder
			plan.folder = &folder
			plan.folderContents = contents
			plan.token = assetDeleteToken(plan)
			return plan, nil
		}
		plan.warnings = append(plan.warnings, "子文件夹内还有其他媒体，已改为只删除同名文件")
	} else if directMediaCount == 1 {
		plan.warnings = append(plan.warnings, "媒体位于图库根目录或挂载根目录，不删除根文件夹")
	}

	files, err := s.sameStemDeleteFiles(root.Path, parentAbs, asset)
	if err != nil {
		return assetDeletePlanInternal{}, err
	}
	if len(files) == 0 {
		plan.blockers = append(plan.blockers, "没有找到可删除文件")
	}
	plan.files = files
	plan.token = assetDeleteToken(plan)
	return plan, nil
}

func (s *Server) directMediaCount(parentAbs string) (int, error) {
	entries, err := os.ReadDir(parentAbs)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if media.DetectByPath(entry.Name()).OK {
			count++
		}
	}
	return count, nil
}

func (s *Server) protectedDeleteFolder(rel string) bool {
	if rel == "" {
		return true
	}
	_, childRel, err := s.store.RootForRel(rel)
	return err != nil || childRel == ""
}

func (s *Server) sameStemDeleteFiles(rootPath string, parentAbs string, asset model.Asset) ([]assetDeleteEntry, error) {
	entries, err := os.ReadDir(parentAbs)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(asset.Filename, filepath.Ext(asset.Filename))
	result := make([]assetDeleteEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matched, reason := sameStemDeleteMatch(base, asset.MediaType, entry.Name())
		if !matched {
			continue
		}
		item, err := s.deleteEntryForPath(rootPath, filepath.Join(parentAbs, entry.Name()), reason)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].relPath) < strings.ToLower(result[j].relPath)
	})
	return result, nil
}

func sameStemDeleteMatch(base string, mediaType string, filename string) (bool, string) {
	ext := strings.ToLower(filepath.Ext(filename))
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	if strings.EqualFold(stem, base) {
		return true, "同名文件"
	}
	if mediaType == model.MediaTypeVideo && subtitleExtension(ext) && subtitleStemMatches(base, stem) {
		return true, "字幕文件"
	}
	return false, ""
}

func subtitleExtension(ext string) bool {
	switch ext {
	case ".ass", ".srt", ".ssa", ".vtt":
		return true
	default:
		return false
	}
}

func (s *Server) folderDeleteContents(rootPath string, folderAbs string, assetRel string) ([]assetDeleteEntry, bool, error) {
	var result []assetDeleteEntry
	hasOtherMedia := false
	err := filepath.WalkDir(folderAbs, func(absPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if sameFilesystemPath(absPath, folderAbs) {
			return nil
		}
		reason := "文件夹内容"
		item, err := s.deleteEntryForPath(rootPath, absPath, reason)
		if err != nil {
			return err
		}
		if item.isMedia && item.relPath != assetRel {
			hasOtherMedia = true
		}
		result = append(result, item)
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].kind == result[j].kind {
			return strings.ToLower(result[i].relPath) < strings.ToLower(result[j].relPath)
		}
		if result[i].kind == "folder" {
			return false
		}
		return true
	})
	return result, hasOtherMedia, nil
}

func (s *Server) deleteEntryForPath(rootPath string, absPath string, reason string) (assetDeleteEntry, error) {
	if !storage.IsWithinRoot(rootPath, absPath) {
		return assetDeleteEntry{}, fmt.Errorf("delete path escapes root: %s", absPath)
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return assetDeleteEntry{}, err
	}
	rel, err := s.store.RelPath(absPath)
	if err != nil {
		return assetDeleteEntry{}, err
	}
	kind := "file"
	if info.Mode()&os.ModeSymlink != 0 {
		kind = "symlink"
	} else if info.IsDir() {
		kind = "folder"
	}
	isMedia := kind != "folder" && media.DetectByPath(absPath).OK
	return assetDeleteEntry{
		relPath: rel,
		absPath: absPath,
		name:    filepath.Base(absPath),
		kind:    kind,
		size:    deleteEntrySize(info, kind),
		mtime:   info.ModTime().Unix(),
		reason:  reason,
		isMedia: isMedia,
	}, nil
}

func deleteEntrySize(info os.FileInfo, kind string) int64 {
	if kind == "folder" {
		return 0
	}
	return info.Size()
}

func assetDeleteToken(plan assetDeletePlanInternal) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%s\n%d\n", plan.mode, plan.asset.ID)
	if plan.folder != nil {
		writeDeleteTokenEntry(hash, *plan.folder)
	}
	for _, item := range plan.files {
		writeDeleteTokenEntry(hash, item)
	}
	for _, item := range plan.folderContents {
		writeDeleteTokenEntry(hash, item)
	}
	for _, warning := range plan.warnings {
		_, _ = fmt.Fprintf(hash, "warning:%s\n", warning)
	}
	for _, blocker := range plan.blockers {
		_, _ = fmt.Fprintf(hash, "blocker:%s\n", blocker)
	}
	return hex.EncodeToString(hash.Sum(nil))[:32]
}

func writeDeleteTokenEntry(hash interface{ Write([]byte) (int, error) }, item assetDeleteEntry) {
	_, _ = fmt.Fprintf(hash, "%s|%s|%d|%d|%s|%t\n", item.kind, item.relPath, item.size, item.mtime, item.reason, item.isMedia)
}

func assetDeletePlanDTO(plan assetDeletePlanInternal) AssetDeletePlanDTO {
	var folder *AssetDeleteEntryDTO
	if plan.folder != nil {
		dto := assetDeleteEntryDTO(*plan.folder)
		folder = &dto
	}
	return AssetDeletePlanDTO{
		Asset:          assetDTO(plan.asset),
		Mode:           plan.mode,
		Token:          plan.token,
		CanDelete:      len(plan.blockers) == 0,
		Files:          assetDeleteEntryDTOs(plan.files),
		Folder:         folder,
		FolderContents: assetDeleteEntryDTOs(plan.folderContents),
		Warnings:       stringListDTO(plan.warnings),
		Blockers:       stringListDTO(plan.blockers),
	}
}

func stringListDTO(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}

func assetDeleteEntryDTOs(items []assetDeleteEntry) []AssetDeleteEntryDTO {
	result := make([]AssetDeleteEntryDTO, 0, len(items))
	for _, item := range items {
		result = append(result, assetDeleteEntryDTO(item))
	}
	return result
}

func assetDeleteEntryDTO(item assetDeleteEntry) AssetDeleteEntryDTO {
	return AssetDeleteEntryDTO{
		RelPath: item.relPath,
		Name:    item.name,
		Kind:    item.kind,
		Size:    item.size,
		Reason:  item.reason,
		IsMedia: item.isMedia,
	}
}

func (s *Server) executeAssetDeletePlan(ctx context.Context, plan assetDeletePlanInternal) AssetDeleteResultDTO {
	result := AssetDeleteResultDTO{Deleted: true, DeletedAssetIDs: []int64{}, Failures: []AssetDeleteFailureDTO{}}
	deletedAt := util.UnixNow()
	var deletedAssets []db.DeletedAsset

	markDeleted := func(rel string) {
		deleted, err := s.db.MarkDeletedWithCache(ctx, rel, deletedAt)
		if err != nil {
			result.Failures = append(result.Failures, AssetDeleteFailureDTO{RelPath: rel, Message: "数据库标记删除失败"})
			return
		}
		if deleted != nil {
			deletedAssets = append(deletedAssets, *deleted)
			result.DeletedAssetIDs = append(result.DeletedAssetIDs, deleted.ID)
		}
	}

	removeFile := func(item assetDeleteEntry) {
		if err := os.Remove(item.absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			result.Failures = append(result.Failures, AssetDeleteFailureDTO{RelPath: item.relPath, Message: err.Error()})
			return
		}
		if item.isMedia {
			markDeleted(item.relPath)
		}
	}

	if plan.mode == assetDeleteModeFolder {
		for _, item := range plan.folderContents {
			if item.kind == "folder" {
				continue
			}
			removeFile(item)
		}
		if len(result.Failures) == 0 {
			dirs := deleteDirectories(plan)
			for _, item := range dirs {
				if err := os.Remove(item.absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					result.Failures = append(result.Failures, AssetDeleteFailureDTO{RelPath: item.relPath, Message: err.Error()})
				}
			}
		}
		if len(result.Failures) == 0 && plan.folder != nil {
			items, err := s.db.MarkDeletedUnder(ctx, plan.folder.relPath, deletedAt)
			if err != nil {
				result.Failures = append(result.Failures, AssetDeleteFailureDTO{RelPath: plan.folder.relPath, Message: "数据库标记删除失败"})
			} else {
				for _, item := range items {
					deletedAssets = append(deletedAssets, item)
					result.DeletedAssetIDs = append(result.DeletedAssetIDs, item.ID)
				}
			}
		}
	} else {
		for _, item := range plan.files {
			removeFile(item)
		}
	}

	result.DeletedAssetIDs = uniqueInt64s(result.DeletedAssetIDs)
	deletedAssets = uniqueDeletedAssets(deletedAssets)
	if len(result.Failures) > 0 {
		result.Deleted = false
	}
	if len(deletedAssets) > 0 {
		s.removeDeletedAssetCaches(deletedAssets)
		s.invalidateProcessingProgress()
		s.publishAssetDeletedEvents(deletedAssets)
	}
	if len(deletedAssets) > 0 || plan.mode == assetDeleteModeFolder && len(result.Failures) == 0 {
		s.refreshFoldersAfterExplicitDelete(plan.asset)
	}
	return result
}

func deleteDirectories(plan assetDeletePlanInternal) []assetDeleteEntry {
	dirs := make([]assetDeleteEntry, 0)
	for _, item := range plan.folderContents {
		if item.kind == "folder" {
			dirs = append(dirs, item)
		}
	}
	if plan.folder != nil {
		dirs = append(dirs, *plan.folder)
	}
	sort.Slice(dirs, func(i, j int) bool {
		leftDepth := strings.Count(filepath.Clean(dirs[i].absPath), string(filepath.Separator))
		rightDepth := strings.Count(filepath.Clean(dirs[j].absPath), string(filepath.Separator))
		if leftDepth == rightDepth {
			return strings.ToLower(dirs[i].relPath) > strings.ToLower(dirs[j].relPath)
		}
		return leftDepth > rightDepth
	})
	return dirs
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueDeletedAssets(items []db.DeletedAsset) []db.DeletedAsset {
	seen := make(map[int64]struct{}, len(items))
	result := make([]db.DeletedAsset, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		result = append(result, item)
	}
	return result
}

func (s *Server) publishAssetDeletedEvents(items []db.DeletedAsset) {
	if s.events == nil {
		return
	}
	for _, item := range items {
		s.events.Publish(events.Event{Type: "asset_deleted", Payload: model.Asset{ID: item.ID, RelPath: item.RelPath, CacheKey: item.CacheKey}})
	}
}

func (s *Server) refreshFoldersAfterExplicitDelete(asset model.Asset) {
	go func() {
		if err := s.db.RefreshFolders(context.Background()); err != nil {
			s.logger.Warn("refresh folders after explicit asset deletion failed", "assetID", asset.ID, "relPath", asset.RelPath, "error", err)
		}
	}()
}

func sameFilesystemPath(a string, b string) bool {
	rel, err := filepath.Rel(a, b)
	if err == nil && rel == "." {
		return true
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
