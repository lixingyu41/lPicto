package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/model"
)

type collectionRequest struct {
	Name string          `json:"name"`
	Rule json.RawMessage `json:"rule"`
}

func (s *Server) collections(w http.ResponseWriter, r *http.Request) {
	system := db.SystemCollections()
	counts, _, err := s.db.GetSystemCollectionCounts(r.Context())
	if err != nil && s.logger != nil {
		s.logger.Warn("read system collection count cache failed", "error", err)
	}
	for i := range system {
		if system[i].SystemKind == db.SystemCollectionAIPending || system[i].SystemKind == db.SystemCollectionAIReady || system[i].SystemKind == db.SystemCollectionAIFailed {
			if count, countErr := s.db.CountSystemCollectionAssets(r.Context(), system[i].SystemKind, db.AssetListOptions{Page: 1, PageSize: 1}); countErr == nil {
				system[i].AssetCount = count
			}
		} else {
			system[i].AssetCount = counts[system[i].SystemKind]
		}
	}
	smart, err := s.db.ListSmartCollections(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "collections_failed", "读取集合失败")
		return
	}
	for i := range smart {
		opts := optionsFromCollectionRule(smart[i].RuleJSON, 1, s.cfg.PageSizeDefault)
		count, err := s.db.CountAssets(r.Context(), opts, true)
		if err == nil {
			smart[i].AssetCount = count
		}
	}
	items := append(system, smart...)
	writeJSON(w, http.StatusOK, map[string]any{"items": collectionDTOs(items)})
}

func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	var payload collectionRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	rule := strings.TrimSpace(string(payload.Rule))
	if rule == "" || rule == "null" {
		rule = "{}"
	}
	collection, err := s.db.CreateSmartCollection(r.Context(), db.CollectionCreate{Name: payload.Name, RuleJSON: rule})
	if err != nil {
		writeError(w, http.StatusBadRequest, "collection_create_failed", "创建集合失败")
		return
	}
	writeJSON(w, http.StatusOK, collectionDTO(collection))
}

func (s *Server) updateCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := smartCollectionIDParam(w, r)
	if !ok {
		return
	}
	var payload collectionRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	rule := strings.TrimSpace(string(payload.Rule))
	if rule == "" || rule == "null" {
		rule = "{}"
	}
	collection, err := s.db.UpdateSmartCollection(r.Context(), id, db.CollectionCreate{Name: payload.Name, RuleJSON: rule})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "collection_not_found", "集合不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "collection_update_failed", "保存集合失败")
		return
	}
	writeJSON(w, http.StatusOK, collectionDTO(collection))
}

func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := smartCollectionIDParam(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteSmartCollection(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "collection_not_found", "集合不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "collection_delete_failed", "删除集合失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) collectionAssets(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	page, pageSize := s.page(r, s.cfg.PageSizeDefault)
	id := strings.TrimSpace(chiParam(r, "id"))
	opts := s.collectionAssetOptions(r, page, pageSize)
	var pageResult model.Page[model.Asset]
	var err error
	if db.IsSystemCollectionKind(id) {
		if id == db.SystemCollectionDuplicates {
			_ = s.ensureDuplicateHashes(r.Context())
		}
		pageResult, err = s.db.ListSystemCollectionAssets(r.Context(), id, opts)
	} else if aiTag, ok := parseAITagCollectionID(id); ok {
		opts.AITag = aiTag
		pageResult, err = s.db.SearchAssets(r.Context(), opts)
	} else if tag, ok := parseCombinedTagCollectionID(id); ok {
		opts.CombinedTag = tag
		pageResult, err = s.db.SearchAssets(r.Context(), opts)
	} else if id == "tags" && (len(opts.CombinedTags) > 0 || len(opts.TagNodes) > 0) {
		pageResult, err = s.db.SearchAssets(r.Context(), opts)
	} else if smartID, ok := parseSmartCollectionID(id); ok {
		collection, getErr := s.db.GetSmartCollection(r.Context(), smartID)
		if getErr != nil {
			err = getErr
		} else {
			opts = mergeCollectionOptions(optionsFromCollectionRule(collection.RuleJSON, page, pageSize), opts)
			pageResult, err = s.db.SearchAssets(r.Context(), opts)
		}
	} else {
		err = sql.ErrNoRows
	}
	s.recordFilterTiming(w, r, started)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "collection_not_found", "集合不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "collection_assets_failed", "读取集合资源失败")
		return
	}
	items, err := s.listAssetDTOs(r, pageResult.Items)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "collection_assets_failed", "读取 AI 摘要失败")
		return
	}
	writeJSON(w, http.StatusOK, PageDTO[AssetDTO]{Items: items, Page: pageResult.Page, PageSize: pageResult.PageSize, HasMore: pageResult.HasMore})
}

func (s *Server) collectionAnchors(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	_, pageSize := s.page(r, s.cfg.PageSizeDefault)
	id := strings.TrimSpace(chiParam(r, "id"))
	opts := s.collectionAssetOptions(r, 1, pageSize)
	var result db.LibraryAnchorResult
	var err error
	if db.IsSystemCollectionKind(id) {
		result, err = s.db.SystemCollectionAnchors(r.Context(), id, opts)
	} else if aiTag, ok := parseAITagCollectionID(id); ok {
		opts.AITag = aiTag
		result, err = s.db.SearchAnchors(r.Context(), opts)
	} else if tag, ok := parseCombinedTagCollectionID(id); ok {
		opts.CombinedTag = tag
		result, err = s.db.SearchAnchors(r.Context(), opts)
	} else if id == "tags" && (len(opts.CombinedTags) > 0 || len(opts.TagNodes) > 0) {
		result, err = s.db.SearchAnchors(r.Context(), opts)
	} else if smartID, ok := parseSmartCollectionID(id); ok {
		collection, getErr := s.db.GetSmartCollection(r.Context(), smartID)
		if getErr != nil {
			err = getErr
		} else {
			opts = mergeCollectionOptions(optionsFromCollectionRule(collection.RuleJSON, 1, pageSize), opts)
			result, err = s.db.SearchAnchors(r.Context(), opts)
		}
	} else {
		err = sql.ErrNoRows
	}
	s.recordFilterTiming(w, r, started)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "collection_not_found", "集合不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "collection_anchors_failed", "读取集合索引失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": libraryAnchorDTOs(result.Items), "total": result.Total})
}

func (s *Server) collectionNeighborsForAsset(ctx context.Context, collectionID string, assetID int64, opts db.AssetListOptions, limit int) (db.Neighbors, error) {
	if db.IsSystemCollectionKind(collectionID) {
		if collectionID == db.SystemCollectionDuplicates {
			_ = s.ensureDuplicateHashes(ctx)
		}
		return s.db.SystemCollectionNeighbors(ctx, collectionID, assetID, opts, limit)
	}
	resolved, err := s.resolveDynamicCollectionOptions(ctx, collectionID, opts)
	if err != nil {
		return db.Neighbors{}, err
	}
	return s.db.AssetFilterNeighbors(ctx, assetID, resolved, true, limit)
}

func (s *Server) collectionPositionForAsset(ctx context.Context, collectionID string, assetID int64, opts db.AssetListOptions) (db.AssetPosition, error) {
	if db.IsSystemCollectionKind(collectionID) {
		if collectionID == db.SystemCollectionDuplicates {
			_ = s.ensureDuplicateHashes(ctx)
		}
		return s.db.SystemCollectionAssetPosition(ctx, collectionID, assetID, opts)
	}
	resolved, err := s.resolveDynamicCollectionOptions(ctx, collectionID, opts)
	if err != nil {
		return db.AssetPosition{}, err
	}
	return s.db.AssetPosition(ctx, assetID, resolved, true)
}

func (s *Server) resolveDynamicCollectionOptions(ctx context.Context, collectionID string, opts db.AssetListOptions) (db.AssetListOptions, error) {
	if aiTag, ok := parseAITagCollectionID(collectionID); ok {
		opts.AITag = aiTag
		return opts, nil
	}
	if tag, ok := parseCombinedTagCollectionID(collectionID); ok {
		opts.CombinedTag = tag
		return opts, nil
	}
	if collectionID == "tags" && (len(opts.CombinedTags) > 0 || len(opts.TagNodes) > 0) {
		return opts, nil
	}
	if smartID, ok := parseSmartCollectionID(collectionID); ok {
		collection, err := s.db.GetSmartCollection(ctx, smartID)
		if err != nil {
			return db.AssetListOptions{}, err
		}
		return mergeCollectionOptions(optionsFromCollectionRule(collection.RuleJSON, opts.Page, opts.PageSize), opts), nil
	}
	return db.AssetListOptions{}, sql.ErrNoRows
}

func (s *Server) duplicateGroups(w http.ResponseWriter, r *http.Request) {
	_ = s.ensureDuplicateHashes(r.Context())
	groups, err := s.db.DuplicateGroups(r.Context(), intQuery(r, "limit", 50))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "duplicate_groups_failed", "读取重复文件失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": duplicateGroupDTOs(groups)})
}

func (s *Server) duplicateSelection(w http.ResponseWriter, r *http.Request) {
	_ = s.ensureDuplicateHashes(r.Context())
	ids, err := s.db.DuplicateDeleteCandidateIDs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "duplicate_selection_failed", "生成重复文件选择失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"assetIds":   ids,
		"keepPolicy": "oldest_imported",
	})
}

func (s *Server) ensureDuplicateHashes(ctx context.Context) error {
	candidates, err := s.db.DuplicateHashCandidates(ctx, 200)
	if err != nil {
		return err
	}
	for _, asset := range candidates {
		if asset.SHA256 != nil && *asset.SHA256 != "" {
			continue
		}
		absPath, err := s.store.PhotoPath(asset.RelPath)
		if err != nil {
			continue
		}
		hash, err := fileSHA256Hex(absPath)
		if err != nil {
			continue
		}
		_ = s.db.SetAssetSHA256Hex(ctx, asset.ID, hash)
	}
	return nil
}

func fileSHA256Hex(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Server) collectionAssetOptions(r *http.Request, page int, pageSize int) db.AssetListOptions {
	typeFilter := safeType(r.URL.Query().Get("type"))
	if typeFilter == "all" {
		typeFilter = ""
	}
	return db.AssetListOptions{
		Page: page, PageSize: pageSize, Type: typeFilter, Sort: safeSort(r.URL.Query().Get("sort")), Group: safeGroup(r.URL.Query().Get("group")),
		CombinedQuery: strings.TrimSpace(r.URL.Query().Get("combinedQuery")), VisibleOnly: visibleOnly(r), Rating: ratingQueryPtr(r, "rating"),
		ManualTag: strings.TrimSpace(r.URL.Query().Get("manualTag")), CombinedTag: strings.TrimSpace(r.URL.Query().Get("combinedTag")), CombinedTags: combinedTagsQuery(r), TagNodes: tagNodesQuery(r), AIDescription: strings.TrimSpace(r.URL.Query().Get("aiDescription")), AITag: strings.TrimSpace(r.URL.Query().Get("aiTag")), Orientation: searchOrientation(r),
	}
}

func optionsFromCollectionRule(rule *string, page int, pageSize int) db.AssetListOptions {
	opts := db.AssetListOptions{Page: page, PageSize: pageSize, Sort: "timeline_desc"}
	if rule == nil || strings.TrimSpace(*rule) == "" {
		return opts
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(*rule), &raw); err != nil {
		return opts
	}
	opts.CombinedQuery = stringRule(raw, "combinedQuery", "q", "query")
	opts.NFOQuery = stringRule(raw, "nfo")
	opts.NFOActor = stringRule(raw, "nfoActor")
	opts.NFOID = stringRule(raw, "nfoId")
	opts.NFOTag = stringRule(raw, "nfoTag")
	opts.ManualTag = stringRule(raw, "manualTag")
	opts.CombinedTag = stringRule(raw, "combinedTag")
	opts.CombinedTags = stringListRule(raw, "combinedTags")
	opts.TagNodes = tagNodeListRule(raw, "tagNodes")
	opts.AIDescription = stringRule(raw, "aiDescription")
	opts.AITag = stringRule(raw, "aiTag")
	opts.NFOTitle = stringRule(raw, "nfoTitle")
	opts.NFOYear = stringRule(raw, "nfoYear")
	opts.Type = safeType(stringRule(raw, "type"))
	if opts.Type == "all" {
		opts.Type = ""
	}
	opts.Sort = safeSort(stringRule(raw, "sort"))
	opts.Group = safeGroup(stringRule(raw, "group"))
	opts.Orientation = safeOrientation(stringRule(raw, "orientation"))
	opts.Rating = intRulePtr(raw, "rating")
	opts.From = int64RulePtr(raw, "from")
	opts.To = int64RulePtr(raw, "to")
	opts.MinWidth = intRulePtr(raw, "widthMin")
	opts.MaxWidth = intRulePtr(raw, "widthMax")
	opts.MinHeight = intRulePtr(raw, "heightMin")
	opts.MaxHeight = intRulePtr(raw, "heightMax")
	opts.MinDuration = floatRulePtr(raw, "durationMin")
	opts.MaxDuration = floatRulePtr(raw, "durationMax")
	opts.MinSize = int64RulePtr(raw, "sizeMin")
	opts.MaxSize = int64RulePtr(raw, "sizeMax")
	opts.MatchAnyAxis = stringRule(raw, "dimensionMode") == "both"
	return opts
}

func mergeCollectionOptions(base db.AssetListOptions, override db.AssetListOptions) db.AssetListOptions {
	base.Page = override.Page
	base.PageSize = override.PageSize
	if override.Sort != "" {
		base.Sort = override.Sort
	}
	if override.Group != "" {
		base.Group = override.Group
	}
	if override.CombinedQuery != "" {
		base.CombinedQuery = override.CombinedQuery
	}
	if override.Type != "" {
		base.Type = override.Type
	}
	if override.Rating != nil {
		base.Rating = override.Rating
	}
	if override.Orientation != "" && override.Orientation != "all" {
		base.Orientation = override.Orientation
	}
	if override.ManualTag != "" {
		base.ManualTag = override.ManualTag
	}
	if override.CombinedTag != "" {
		base.CombinedTag = override.CombinedTag
	}
	if len(override.CombinedTags) > 0 {
		base.CombinedTags = override.CombinedTags
	}
	if len(override.TagNodes) > 0 {
		base.TagNodes = override.TagNodes
	}
	if override.AIDescription != "" {
		base.AIDescription = override.AIDescription
	}
	if override.AITag != "" {
		base.AITag = override.AITag
	}
	return base
}

func stringRule(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if text, ok := value.(string); ok {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func stringListRule(raw map[string]any, key string) []string {
	values, ok := raw[key].([]any)
	if !ok {
		if encoded, stringOK := raw[key].(string); stringOK {
			if err := json.Unmarshal([]byte(encoded), &values); err != nil {
				return nil
			}
		} else {
			return nil
		}
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			continue
		}
		tag := strings.TrimSpace(text)
		if tag == "" || len([]rune(tag)) > 80 {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
		if len(out) == 32 {
			break
		}
	}
	return out
}

func tagNodeListRule(raw map[string]any, key string) []string {
	values, ok := raw[key].([]any)
	if !ok {
		if encoded, stringOK := raw[key].(string); stringOK {
			if err := json.Unmarshal([]byte(encoded), &values); err != nil {
				return nil
			}
		} else {
			return nil
		}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		node, ok := value.(string)
		node = strings.TrimSpace(node)
		if !ok || len([]rune(node)) > 160 || (!strings.HasPrefix(node, "ai:") && !strings.HasPrefix(node, "manual:")) {
			continue
		}
		if _, exists := seen[node]; exists {
			continue
		}
		seen[node] = struct{}{}
		out = append(out, node)
		if len(out) == 32 {
			break
		}
	}
	return out
}

func intRulePtr(raw map[string]any, key string) *int {
	if value, ok := raw[key]; ok {
		switch typed := value.(type) {
		case float64:
			v := int(typed)
			return &v
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func int64RulePtr(raw map[string]any, key string) *int64 {
	if value, ok := raw[key]; ok {
		switch typed := value.(type) {
		case float64:
			v := int64(typed)
			return &v
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func floatRulePtr(raw map[string]any, key string) *float64 {
	if value, ok := raw[key]; ok {
		switch typed := value.(type) {
		case float64:
			return &typed
		case string:
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func smartCollectionIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, ok := parseSmartCollectionID(chiParam(r, "id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "collection_id_invalid", "集合 ID 无效")
		return 0, false
	}
	return id, true
}

func parseSmartCollectionID(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "smart-")
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0
}

func parseAITagCollectionID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "ai-tag:") {
		return "", false
	}
	tag := strings.TrimSpace(strings.TrimPrefix(value, "ai-tag:"))
	return tag, tag != "" && len([]rune(tag)) <= 80
}

func parseCombinedTagCollectionID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "tag:") {
		return "", false
	}
	tag := strings.TrimSpace(strings.TrimPrefix(value, "tag:"))
	return tag, tag != "" && len([]rune(tag)) <= 80
}

func combinedTagsQuery(r *http.Request) []string {
	raw := strings.TrimSpace(r.URL.Query().Get("combinedTags"))
	if raw == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		tag := strings.TrimSpace(value)
		if tag == "" || len([]rune(tag)) > 80 {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
		if len(out) == 32 {
			break
		}
	}
	return out
}

func tagNodesQuery(r *http.Request) []string {
	raw := strings.TrimSpace(r.URL.Query().Get("tagNodes"))
	if raw == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	valid := make([]string, 0, len(values))
	for _, value := range values {
		node := strings.TrimSpace(value)
		if len([]rune(node)) > 160 || (!strings.HasPrefix(node, "ai:") && !strings.HasPrefix(node, "manual:")) {
			continue
		}
		if _, exists := seen[node]; exists {
			continue
		}
		seen[node] = struct{}{}
		valid = append(valid, node)
		if len(valid) == 32 {
			break
		}
	}
	return valid
}

func chiParam(r *http.Request, key string) string {
	return strings.TrimSpace(chi.URLParam(r, key))
}
