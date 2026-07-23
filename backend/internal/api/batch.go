package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"lpicto/backend/internal/db"
)

type batchAssetIDsRequest struct {
	AssetIDs []int64 `json:"assetIds"`
}

type batchDeleteRequest struct {
	AssetIDs                []int64 `json:"assetIds"`
	PurgeUnavailable        bool    `json:"purgeUnavailable"`
	RefreshCollectionCounts bool    `json:"refreshCollectionCounts"`
}

type batchTagRequest struct {
	AssetIDs []int64  `json:"assetIds"`
	Tag      string   `json:"tag"`
	Tags     []string `json:"tags"`
}

type batchRatingRequest struct {
	AssetIDs []int64 `json:"assetIds"`
	Rating   int     `json:"rating"`
}

type batchRotationRequest struct {
	AssetIDs []int64 `json:"assetIds"`
	Rotation int     `json:"rotation"`
}

type batchAlbumRequest struct {
	AssetIDs []int64 `json:"assetIds"`
	AlbumID  int64   `json:"albumId"`
}

func (s *Server) batchAddTag(w http.ResponseWriter, r *http.Request) {
	var payload batchTagRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	tags := payload.Tags
	if payload.Tag != "" {
		tags = append(tags, payload.Tag)
	}
	updated := map[int64]struct{}{}
	for _, tag := range tags {
		ids, err := s.db.AddTagToAssets(r.Context(), payload.AssetIDs, tag)
		if errors.Is(err, db.ErrEmptyAssetTag) {
			continue
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "batch_tag_failed", "批量添加标签失败")
			return
		}
		for _, id := range ids {
			updated[id] = struct{}{}
		}
	}
	if len(updated) == 0 && len(tags) == 0 {
		writeError(w, http.StatusBadRequest, "asset_tag_empty", "标签不能为空")
		return
	}
	ids := make([]int64, 0, len(updated))
	for id := range updated {
		ids = append(ids, id)
	}
	writeJSON(w, http.StatusOK, BatchOperationResultDTO{UpdatedAssetIDs: ids})
}

func (s *Server) batchSetRating(w http.ResponseWriter, r *http.Request) {
	var payload batchRatingRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	if !db.ValidRating(payload.Rating) {
		writeError(w, http.StatusBadRequest, "rating_invalid", "星级必须是 0 到 5")
		return
	}
	ids, err := s.db.SetAssetsRating(r.Context(), payload.AssetIDs, payload.Rating)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "batch_rating_failed", "批量评分失败")
		return
	}
	writeJSON(w, http.StatusOK, BatchOperationResultDTO{UpdatedAssetIDs: ids})
}

func (s *Server) batchSetRotation(w http.ResponseWriter, r *http.Request) {
	var payload batchRotationRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	ids, err := s.db.SetAssetsRotation(r.Context(), payload.AssetIDs, payload.Rotation)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "batch_rotation_failed", "批量旋转失败")
		return
	}
	writeJSON(w, http.StatusOK, BatchOperationResultDTO{UpdatedAssetIDs: ids})
}

func (s *Server) batchHide(w http.ResponseWriter, r *http.Request) {
	var payload batchAssetIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	ids, err := s.db.SetAssetsHidden(r.Context(), payload.AssetIDs, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "batch_hide_failed", "批量隐藏失败")
		return
	}
	writeJSON(w, http.StatusOK, BatchOperationResultDTO{UpdatedAssetIDs: ids})
}

func (s *Server) batchUnhide(w http.ResponseWriter, r *http.Request) {
	var payload batchAssetIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	ids, err := s.db.SetAssetsHidden(r.Context(), payload.AssetIDs, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "batch_unhide_failed", "恢复隐藏项失败")
		return
	}
	writeJSON(w, http.StatusOK, BatchOperationResultDTO{UpdatedAssetIDs: ids})
}

func (s *Server) batchAddToAlbum(w http.ResponseWriter, r *http.Request) {
	var payload batchAlbumRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	ids, err := s.db.AddAlbumAssets(r.Context(), payload.AlbumID, payload.AssetIDs)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "album_not_found", "相册不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "batch_album_failed", "批量加入相册失败")
		return
	}
	writeJSON(w, http.StatusOK, BatchOperationResultDTO{UpdatedAssetIDs: ids})
}

func (s *Server) batchDelete(w http.ResponseWriter, r *http.Request) {
	var payload batchDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	result := BatchOperationResultDTO{UpdatedAssetIDs: []int64{}, DeletedAssetIDs: []int64{}, Failures: []AssetDeleteFailureDTO{}}
	directPurgeIDs := make([]int64, 0)
	for _, assetID := range uniqueInt64s(payload.AssetIDs) {
		asset, err := s.db.GetAssetRecordIncludingDeleted(r.Context(), assetID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "batch_delete_failed", "批量删除失败")
			return
		}
		plan, err := s.buildAssetDeletePlan(asset)
		if err != nil {
			if payload.PurgeUnavailable && assetDeleteUnavailable(err) {
				directPurgeIDs = append(directPurgeIDs, asset.ID)
				continue
			}
			result.Failures = append(result.Failures, AssetDeleteFailureDTO{RelPath: asset.RelPath, Message: err.Error()})
			continue
		}
		deleted := s.executeAssetDeletePlan(r.Context(), plan)
		result.DeletedAssetIDs = append(result.DeletedAssetIDs, deleted.DeletedAssetIDs...)
		result.UpdatedAssetIDs = append(result.UpdatedAssetIDs, deleted.DeletedAssetIDs...)
		result.Failures = append(result.Failures, deleted.Failures...)
	}
	if len(directPurgeIDs) > 0 {
		items, err := s.db.PurgeAssetIDs(r.Context(), directPurgeIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "batch_purge_failed", "永久删除数据库记录失败")
			return
		}
		if len(items) > 0 {
			s.removeDeletedAssetCaches(items)
			s.publishAssetDeletedEvents(items)
		}
		for _, item := range items {
			result.DeletedAssetIDs = append(result.DeletedAssetIDs, item.ID)
			result.UpdatedAssetIDs = append(result.UpdatedAssetIDs, item.ID)
		}
	}
	result.DeletedAssetIDs = uniqueInt64s(result.DeletedAssetIDs)
	result.UpdatedAssetIDs = uniqueInt64s(result.UpdatedAssetIDs)
	if payload.RefreshCollectionCounts {
		if _, err := s.db.RefreshSystemCollectionCounts(r.Context()); err != nil && s.logger != nil {
			s.logger.Warn("refresh system collection counts after batch delete failed", "error", err)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) addAlbumAssets(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	var payload batchAssetIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	ids, err := s.db.AddAlbumAssets(r.Context(), id, payload.AssetIDs)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "album_not_found", "相册不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "album_assets_add_failed", "加入相册失败")
		return
	}
	writeJSON(w, http.StatusOK, BatchOperationResultDTO{UpdatedAssetIDs: ids})
}

func (s *Server) removeAlbumAssets(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	var payload batchAssetIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	ids, err := s.db.RemoveAlbumAssets(r.Context(), id, payload.AssetIDs)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "album_not_found", "相册不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "album_assets_remove_failed", "移出相册失败")
		return
	}
	writeJSON(w, http.StatusOK, BatchOperationResultDTO{UpdatedAssetIDs: ids})
}
