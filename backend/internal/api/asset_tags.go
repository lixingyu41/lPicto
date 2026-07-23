package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"lpicto/backend/internal/db"
)

type assetTagRequest struct {
	Tag string `json:"tag"`
}

type tagRequest struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type tagMergeRequest struct {
	TargetTagID  int64   `json:"targetTagId"`
	SourceTagIDs []int64 `json:"sourceTagIds"`
	TargetName   string  `json:"targetName"`
}

func (s *Server) tags(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.ListTags(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tags_failed", "读取标签失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tagSummaryDTOs(items)})
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request) {
	var payload tagRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	tag, err := s.db.CreateTag(r.Context(), payload.Name)
	if errors.Is(err, db.ErrEmptyAssetTag) {
		writeError(w, http.StatusBadRequest, "asset_tag_empty", "标签不能为空")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tag_create_failed", "创建标签失败")
		return
	}
	writeJSON(w, http.StatusOK, tagSummaryDTO(tag))
}

func (s *Server) updateTag(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	var payload tagRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	tag, err := s.db.RenameTag(r.Context(), id, payload.Name)
	s.writeTagMutationResult(w, tag, err, "tag_update_failed", "保存标签失败")
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteTag(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tag_not_found", "标签不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "tag_delete_failed", "删除标签失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) mergeTags(w http.ResponseWriter, r *http.Request) {
	var payload tagMergeRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	tag, err := s.db.MergeTags(r.Context(), payload.TargetTagID, payload.SourceTagIDs, payload.TargetName)
	s.writeTagMutationResult(w, tag, err, "tag_merge_failed", "合并标签失败")
}

func (s *Server) writeTagMutationResult(w http.ResponseWriter, tag db.TagSummary, err error, code string, message string) {
	if errors.Is(err, db.ErrEmptyAssetTag) {
		writeError(w, http.StatusBadRequest, "asset_tag_empty", "标签不能为空")
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tag_not_found", "标签不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, code, message)
		return
	}
	writeJSON(w, http.StatusOK, tagSummaryDTO(tag))
}

func (s *Server) assetTags(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	tags, err := s.db.ListAssetTags(r.Context(), asset.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_tags_failed", "读取标签失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": assetTagDTOs(asset.ID, tags)})
}

func (s *Server) addAssetTag(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	var payload assetTagRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	if _, err := s.db.AddAssetTag(r.Context(), asset.ID, payload.Tag); errors.Is(err, db.ErrEmptyAssetTag) {
		writeError(w, http.StatusBadRequest, "asset_tag_empty", "标签不能为空")
		return
	} else if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "asset_not_found", "资源不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_tag_add_failed", "添加标签失败")
		return
	}
	s.assetTags(w, r)
}

func (s *Server) removeAssetTag(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteAssetTag(r.Context(), asset.ID, r.URL.Query().Get("tag")); errors.Is(err, db.ErrEmptyAssetTag) {
		writeError(w, http.StatusBadRequest, "asset_tag_empty", "标签不能为空")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_tag_delete_failed", "删除标签失败")
		return
	}
	s.assetTags(w, r)
}

func assetTagDTOs(assetID int64, tags []db.AssetTag) []AssetTagDTO {
	result := make([]AssetTagDTO, 0, len(tags))
	for _, tag := range tags {
		result = append(result, AssetTagDTO{AssetID: assetID, Tag: tag.Name, CreatedAt: tag.CreatedAt})
	}
	return result
}

func tagSummaryDTO(tag db.TagSummary) TagSummaryDTO {
	return TagSummaryDTO{ID: tag.ID, Name: tag.Name, AssetCount: tag.AssetCount, CreatedAt: tag.CreatedAt}
}

func tagSummaryDTOs(tags []db.TagSummary) []TagSummaryDTO {
	result := make([]TagSummaryDTO, 0, len(tags))
	for _, tag := range tags {
		result = append(result, tagSummaryDTO(tag))
	}
	return result
}
