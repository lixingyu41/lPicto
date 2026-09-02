package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"lpicto/backend/internal/db"
)

func (s *Server) libraryAssetSelection(w http.ResponseWriter, r *http.Request) {
	ids, err := s.db.SearchAssetIDs(r.Context(), s.libraryAssetOptions(r, 1, 1))
	s.writeAssetSelection(w, ids, err, "library_selection_failed", "读取当前筛选结果失败")
}

func (s *Server) folderAssetSelection(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	opts := db.AssetListOptions{
		Page: 1, PageSize: 1, Type: safeType(r.URL.Query().Get("type")), Sort: safeSort(r.URL.Query().Get("sort")), Group: safeGroup(r.URL.Query().Get("group")),
		Query: strings.TrimSpace(r.URL.Query().Get("q")), Recursive: boolQuery(r, "recursive", false), VisibleOnly: visibleOnly(r), Rating: ratingQueryPtr(r, "rating"),
		Orientation: searchOrientation(r), CombinedTags: combinedTagsQuery(r), TagNodes: tagNodesQuery(r),
	}
	ids, err := s.db.FolderAssetIDs(r.Context(), id, opts)
	s.writeAssetSelection(w, ids, err, "folder_selection_failed", "读取当前文件夹筛选结果失败")
}

func (s *Server) albumAssetSelection(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	ids, err := s.db.AlbumAssetIDs(r.Context(), id, s.libraryAssetOptions(r, 1, 1))
	s.writeAssetSelection(w, ids, err, "album_selection_failed", "读取当前相册筛选结果失败")
}

func (s *Server) collectionAssetSelection(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chiParam(r, "id"))
	opts := s.collectionAssetOptions(r, 1, 1)
	var ids []int64
	var err error
	if db.IsSystemCollectionKind(id) {
		ids, err = s.db.SystemCollectionAssetIDs(r.Context(), id, opts)
	} else {
		var resolved db.AssetListOptions
		resolved, err = s.resolveDynamicCollectionOptions(r.Context(), id, opts)
		if err == nil {
			ids, err = s.db.SearchAssetIDs(r.Context(), resolved)
		}
	}
	s.writeAssetSelection(w, ids, err, "collection_selection_failed", "读取当前集合筛选结果失败")
}

func (s *Server) writeAssetSelection(w http.ResponseWriter, ids []int64, err error, code string, message string) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "selection_scope_not_found", "当前筛选范围不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, code, message)
		return
	}
	if ids == nil {
		ids = []int64{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"assetIds": ids})
}
