package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

type scanFolderRequest struct {
	RelPath string `json:"relPath"`
}

type scanLibraryRequest struct {
	Name     string   `json:"name"`
	RelPaths []string `json:"relPaths"`
}

func (s *Server) settingsProgress(w http.ResponseWriter, r *http.Request) {
	progress, err := s.db.ProcessingProgress(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "progress_failed", "读取处理进度失败")
		return
	}
	writeJSON(w, http.StatusOK, processingProgressDTO(progress, s.jobs.Stats()))
}

func (s *Server) scanLibraries(w http.ResponseWriter, r *http.Request) {
	libraries, configured, err := s.db.GetScanLibraries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_libraries_failed", "读取 LIB 失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": configured,
		"items":      s.scanLibraryDTOs(libraries),
	})
}

func (s *Server) addScanLibrary(w http.ResponseWriter, r *http.Request) {
	var payload scanLibraryRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_name", "LIB 名称不能为空")
		return
	}
	roots, err := s.validSourceRoots(payload.RelPaths)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}
	libraries, library, err := s.db.AddScanLibrary(r.Context(), name, roots)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_library_add_failed", "添加 LIB 失败")
		return
	}
	started := s.scanner.TriggerRoots("library:"+library.Name, library.Roots)
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": true,
		"items":      s.scanLibraryDTOs(libraries),
		"library":    s.scanLibraryDTO(library),
		"started":    started,
	})
}

func (s *Server) removeScanLibrary(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "LIB ID 无效")
		return
	}
	libraries, err := s.db.RemoveScanLibrary(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_library_remove_failed", "删除 LIB 失败")
		return
	}
	started := s.scanner.Trigger("library_removed")
	if len(libraries) == 0 {
		if _, err := s.db.MarkAllDeleted(r.Context(), util.UnixNow()); err != nil {
			s.logger.Warn("clear assets after library removal failed", "error", err)
			writeError(w, http.StatusInternalServerError, "scan_library_remove_failed", "清空相册缓存失败")
			return
		}
		if err := s.db.RefreshFolders(r.Context()); err != nil {
			s.logger.Warn("refresh folders after library removal failed", "error", err)
			writeError(w, http.StatusInternalServerError, "scan_library_remove_failed", "刷新文件夹失败")
			return
		}
		started = s.scanner.TriggerRoots("library_removed_empty", nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "items": s.scanLibraryDTOs(libraries), "started": started})
}

func (s *Server) scanLibrary(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "LIB ID 无效")
		return
	}
	library, err := s.db.FindScanLibrary(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "scan_library_not_found", "LIB 不存在")
		return
	}
	started := s.scanner.TriggerRoots("library:"+library.Name, library.Roots)
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": started})
}

func (s *Server) scanFolders(w http.ResponseWriter, r *http.Request) {
	folders, configured, err := s.db.GetScanFolders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_folders_failed", "读取扫描文件夹失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": configured,
		"items":      s.scanFolderDTOs(folders),
	})
}

func (s *Server) addScanFolder(w http.ResponseWriter, r *http.Request) {
	var payload scanFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	rel, err := storage.NormalizeRelPath(payload.RelPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "文件夹路径无效")
		return
	}
	if !s.sourceDirExists(rel) {
		writeError(w, http.StatusBadRequest, "folder_not_found", "文件夹不存在或不可访问")
		return
	}
	folders, err := s.db.AddScanFolder(r.Context(), rel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_folder_add_failed", "添加扫描文件夹失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.scanFolderDTOs(folders)})
}

func (s *Server) validSourceRoots(relPaths []string) ([]string, error) {
	if len(relPaths) == 0 {
		return nil, errors.New("至少选择一个文件夹")
	}
	roots := make([]string, 0, len(relPaths))
	for _, raw := range relPaths {
		rel, err := storage.NormalizeRelPath(raw)
		if err != nil {
			return nil, errors.New("文件夹路径无效")
		}
		if !s.sourceDirExists(rel) {
			return nil, errors.New("文件夹不存在或不可访问")
		}
		roots = append(roots, rel)
	}
	normalized, err := db.NormalizeScanFolders(roots)
	if err != nil {
		return nil, errors.New("文件夹路径无效")
	}
	if len(normalized) == 0 {
		return nil, errors.New("至少选择一个文件夹")
	}
	return normalized, nil
}

func (s *Server) removeScanFolder(w http.ResponseWriter, r *http.Request) {
	rel, err := storage.NormalizeRelPath(r.URL.Query().Get("relPath"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "文件夹路径无效")
		return
	}
	folders, err := s.db.RemoveScanFolder(r.Context(), rel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_folder_remove_failed", "移除扫描文件夹失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.scanFolderDTOs(folders)})
}

func (s *Server) sourceFolders(w http.ResponseWriter, r *http.Request) {
	parentRel, err := storage.NormalizeRelPath(r.URL.Query().Get("parentRelPath"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "文件夹路径无效")
		return
	}
	folders, _, err := s.db.GetScanFolders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_folders_failed", "读取扫描文件夹失败")
		return
	}
	parentPath, err := s.store.PhotoPath(parentRel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "文件夹路径无效")
		return
	}
	entries, err := util.ReadDirPartial(parentPath)
	warning := ""
	if err != nil && len(entries) == 0 {
		s.sourceFoldersFromDB(w, r, parentRel, folders)
		return
	}
	if err != nil {
		warning = "源目录部分读取失败"
	}
	items := make([]SourceFolderDTO, 0, len(entries))
	for _, entry := range entries {
		childRel := joinRel(parentRel, entry.Name())
		childPath, err := s.store.PhotoPath(childRel)
		if err != nil {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			inside, _, err := storage.SymlinkTargetWithinRoot(s.store.PhotoRoot, childPath)
			if err != nil || !inside {
				continue
			}
		}
		info, err := os.Stat(childPath)
		if err != nil || !info.IsDir() {
			continue
		}
		items = append(items, s.sourceFolderDTO(childRel, folders))
	}
	items = s.mergeManifestSourceFolders(items, parentRel, folders)
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{
		"current": s.sourceFolderDTO(parentRel, folders),
		"items":   items,
		"warning": warning,
	})
}

func (s *Server) sourceFoldersFromDB(w http.ResponseWriter, r *http.Request, parentRel string, scanFolders []string) {
	folders, err := s.db.FolderTree(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "source_folders_failed", "读取源目录失败")
		return
	}
	items := make([]SourceFolderDTO, 0)
	for _, folder := range folders {
		parent := ""
		if folder.ParentRelPath != nil {
			parent = *folder.ParentRelPath
		}
		if parent == parentRel && folder.RelPath != parentRel {
			items = append(items, s.sourceFolderDTO(folder.RelPath, scanFolders))
		}
	}
	items = s.mergeManifestSourceFolders(items, parentRel, scanFolders)
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{
		"current": s.sourceFolderDTO(parentRel, scanFolders),
		"items":   items,
		"warning": "源目录读取失败，显示已扫描目录",
	})
}

func (s *Server) mergeManifestSourceFolders(items []SourceFolderDTO, parentRel string, scanFolders []string) []SourceFolderDTO {
	manifestFolders, err := storage.LoadSourceFolderManifest(s.cfg.DataRoot)
	if err != nil {
		s.logger.Warn("load source folder manifest failed", "error", err)
		return items
	}
	seen := map[string]struct{}{}
	for _, item := range items {
		seen[item.RelPath] = struct{}{}
	}
	for _, rel := range storage.ManifestChildren(manifestFolders, parentRel) {
		if _, ok := seen[rel]; ok {
			continue
		}
		items = append(items, s.sourceFolderDTO(rel, scanFolders))
		seen[rel] = struct{}{}
	}
	return items
}

func (s *Server) scanFolderDTOs(folders []string) []ScanFolderDTO {
	items := make([]ScanFolderDTO, 0, len(folders))
	for _, rel := range folders {
		parent := parentPtr(rel)
		items = append(items, ScanFolderDTO{
			RelPath: rel, Name: storage.FolderName(rel), ParentRelPath: parent,
			Depth: storage.FolderDepth(rel), Exists: s.sourceDirExists(rel),
		})
	}
	return items
}

func (s *Server) scanLibraryDTOs(libraries []db.ScanLibrary) []ScanLibraryDTO {
	items := make([]ScanLibraryDTO, 0, len(libraries))
	for _, library := range libraries {
		items = append(items, s.scanLibraryDTO(library))
	}
	return items
}

func (s *Server) scanLibraryDTO(library db.ScanLibrary) ScanLibraryDTO {
	folders := s.scanFolderDTOs(library.Roots)
	exists := len(folders) > 0
	for _, folder := range folders {
		if !folder.Exists {
			exists = false
			break
		}
	}
	return ScanLibraryDTO{ID: library.ID, Name: library.Name, Folders: folders, Exists: exists}
}

func (s *Server) sourceFolderDTO(rel string, folders []string) SourceFolderDTO {
	selected := false
	for _, folder := range folders {
		if folder == rel {
			selected = true
			break
		}
	}
	return SourceFolderDTO{
		RelPath: rel, Name: storage.FolderName(rel), ParentRelPath: parentPtr(rel),
		Depth: storage.FolderDepth(rel), Selected: selected, Included: db.AssetInScanFolders(rel, folders),
	}
}

func (s *Server) sourceDirExists(rel string) bool {
	fullPath, err := s.store.PhotoPath(rel)
	if err != nil {
		return false
	}
	linkInfo, err := os.Lstat(fullPath)
	if err != nil {
		return false
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		inside, _, err := storage.SymlinkTargetWithinRoot(s.store.PhotoRoot, fullPath)
		if err != nil || !inside {
			return false
		}
	}
	info, err := os.Stat(fullPath)
	return err == nil && info.IsDir()
}

func joinRel(parent string, name string) string {
	if parent == "" {
		return path.Clean(name)
	}
	return path.Clean(parent + "/" + name)
}

func parentPtr(rel string) *string {
	if rel == "" {
		return nil
	}
	parent := storage.ParentRelPath(rel)
	return &parent
}
