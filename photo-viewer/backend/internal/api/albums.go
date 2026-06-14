package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/storage"
)

type albumRequest struct {
	Name              string               `json:"name"`
	FolderRelPaths    []string             `json:"folderRelPaths"`
	Sources           []albumSourceRequest `json:"sources"`
	MediaTypeFilter   string               `json:"mediaTypeFilter"`
	OrientationFilter string               `json:"orientationFilter"`
}

type albumSourceRequest struct {
	RelPath           string `json:"relPath"`
	Recursive         *bool  `json:"recursive"`
	MediaTypeFilter   string `json:"mediaTypeFilter"`
	OrientationFilter string `json:"orientationFilter"`
}

func (s *Server) albums(w http.ResponseWriter, r *http.Request) {
	albums, err := s.db.ListAlbums(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "albums_failed", "读取相册失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": albumDTOs(albums)})
}

func (s *Server) album(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	album, err := s.db.GetAlbum(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "album_not_found", "相册不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "album_failed", "读取相册失败")
		return
	}
	writeJSON(w, http.StatusOK, albumDTO(album))
}

func (s *Server) createAlbum(w http.ResponseWriter, r *http.Request) {
	var payload albumRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	create := db.AlbumCreate{
		Name:              payload.Name,
		MediaTypeFilter:   payload.MediaTypeFilter,
		OrientationFilter: payload.OrientationFilter,
	}
	if len(payload.Sources) > 0 {
		sources, err := s.validAlbumSources(r.Context(), payload.Sources)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_album_source", err.Error())
			return
		}
		create.Sources = sources
	} else {
		folders, err := s.validAlbumFolders(r.Context(), payload.FolderRelPaths)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_album_source", err.Error())
			return
		}
		create.FolderRelPaths = folders
	}
	album, err := s.db.CreateAlbum(r.Context(), create)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "album_create_failed", "创建相册失败")
		return
	}
	writeJSON(w, http.StatusOK, albumDTO(album))
}

func (s *Server) deleteAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteAlbum(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "album_delete_failed", "删除相册失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) refreshAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	roots, err := s.db.AlbumScanRoots(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "album_not_found", "相册不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "album_refresh_failed", "刷新相册失败")
		return
	}
	_ = s.db.TouchAlbum(r.Context(), id)
	started := s.scanner.TriggerRoots("album_refresh", roots)
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": started})
}

func (s *Server) albumAssets(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	page, pageSize := s.page(r, s.cfg.PageSizeDefault)
	opts := db.AssetListOptions{
		Page: page, PageSize: pageSize, Sort: safeSort(r.URL.Query().Get("sort")),
		Query: strings.TrimSpace(r.URL.Query().Get("q")),
	}
	assets, err := s.db.ListAlbumAssets(r.Context(), id, opts)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "album_not_found", "相册不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "album_assets_failed", "读取相册资源失败")
		return
	}
	writeJSON(w, http.StatusOK, PageDTO[AssetDTO]{
		Items: assetDTOs(assets.Items), Page: assets.Page, PageSize: assets.PageSize, HasMore: assets.HasMore,
	})
}

func (s *Server) albumSourceFolders(w http.ResponseWriter, r *http.Request) {
	parentRel, err := storage.NormalizeRelPath(r.URL.Query().Get("parentRelPath"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "文件夹路径无效")
		return
	}
	scanRoots, _, err := s.db.GetScanFolders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan_folders_failed", "读取 LIB 失败")
		return
	}
	folders, err := s.db.FolderTree(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "album_source_folders_failed", "读取相册文件夹失败")
		return
	}
	current := albumSourceFolderDTO(parentRel, scanRoots)
	items := make([]SourceFolderDTO, 0)
	for _, folder := range folders {
		parent := ""
		if folder.ParentRelPath != nil {
			parent = *folder.ParentRelPath
		}
		if parent != parentRel || folder.RelPath == parentRel || !folderVisibleForAlbum(folder.RelPath, scanRoots) {
			continue
		}
		items = append(items, albumSourceFolderDTO(folder.RelPath, scanRoots))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Included == items[j].Included {
			return items[i].Name < items[j].Name
		}
		return items[i].Included
	})
	writeJSON(w, http.StatusOK, map[string]any{"current": current, "items": items})
}

func (s *Server) validAlbumFolders(ctx context.Context, relPaths []string) ([]string, error) {
	if len(relPaths) == 0 {
		return nil, errors.New("至少选择一个 LIB 内文件夹")
	}
	scanRoots, _, err := s.db.GetScanFolders(ctx)
	if err != nil {
		return nil, errors.New("读取 LIB 失败")
	}
	normalized, err := db.NormalizeScanFolders(relPaths)
	if err != nil {
		return nil, errors.New("文件夹路径无效")
	}
	for _, rel := range normalized {
		if !db.AssetInScanFolders(rel, scanRoots) {
			return nil, errors.New("只能选择已在 LIB 中的文件夹")
		}
		if _, err := s.db.GetFolderByRel(ctx, rel); err != nil {
			return nil, errors.New("文件夹不存在或尚未扫描")
		}
	}
	return normalized, nil
}

func (s *Server) validAlbumSources(ctx context.Context, payload []albumSourceRequest) ([]db.AlbumSourceCreate, error) {
	if len(payload) == 0 {
		return nil, errors.New("至少添加一个相册筛选")
	}
	scanRoots, _, err := s.db.GetScanFolders(ctx)
	if err != nil {
		return nil, errors.New("读取 LIB 失败")
	}
	sources := make([]db.AlbumSourceCreate, 0, len(payload))
	for _, item := range payload {
		rel, err := storage.NormalizeRelPath(item.RelPath)
		if err != nil {
			return nil, errors.New("文件夹路径无效")
		}
		if !db.AssetInScanFolders(rel, scanRoots) {
			return nil, errors.New("只能选择已在 LIB 中的文件夹")
		}
		if _, err := s.db.GetFolderByRel(ctx, rel); err != nil {
			return nil, errors.New("文件夹不存在或尚未扫描")
		}
		recursive := true
		if item.Recursive != nil {
			recursive = *item.Recursive
		}
		sources = append(sources, db.AlbumSourceCreate{
			RelPath:           rel,
			Recursive:         recursive,
			MediaTypeFilter:   item.MediaTypeFilter,
			OrientationFilter: item.OrientationFilter,
		})
	}
	return sources, nil
}

func albumSourceFolderDTO(rel string, scanRoots []string) SourceFolderDTO {
	return SourceFolderDTO{
		RelPath: rel, Name: storage.FolderName(rel), ParentRelPath: parentPtr(rel),
		Depth: storage.FolderDepth(rel), Selected: scanRootExact(rel, scanRoots), Included: db.AssetInScanFolders(rel, scanRoots),
	}
}

func folderVisibleForAlbum(rel string, scanRoots []string) bool {
	if db.AssetInScanFolders(rel, scanRoots) {
		return true
	}
	for _, root := range scanRoots {
		if root != "" && (rel == storage.ParentRelPath(root) || strings.HasPrefix(root, rel+"/")) {
			return true
		}
	}
	return false
}

func scanRootExact(rel string, scanRoots []string) bool {
	for _, root := range scanRoots {
		if root == rel {
			return true
		}
	}
	return false
}
