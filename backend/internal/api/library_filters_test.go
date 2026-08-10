package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/model"
)

func TestLibraryAPIHandlesAdvancedFiltersAndNFOOptions(t *testing.T) {
	server, database, _ := testMissingSourceServer(t)
	id := testInsertAsset(t, database, "advanced.jpg", "advancedfiltercache01", model.MediaTypeImage)
	nfoJSON := `{"groups":[{"title":"演员","items":[{"key":"actor","value":"Alice"}]}]}`
	if _, err := database.Conn().ExecContext(context.Background(), `
UPDATE media_asset
SET width = $1, height = $2, nfo_json = $3::jsonb, nfo_search_text = $4
WHERE id = $5`, 1920, 1080, nfoJSON, "alice", id); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/library/assets?page=1&pageSize=20&type=image&nfoActor=alice&widthMin=1000&from=5&to=15", nil)
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}
	var page struct {
		Items []AssetDTO `json:"items"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != id {
		t.Fatalf("items = %#v, want asset %d", page.Items, id)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/library/nfo-options?field=actor&q=ali", nil)
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}
	var options struct {
		Items []string `json:"items"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&options); err != nil {
		t.Fatal(err)
	}
	if len(options.Items) != 1 || options.Items[0] != "Alice" {
		t.Fatalf("items = %#v, want Alice", options.Items)
	}
}

func TestLegacySearchAPIRouteIsRemoved(t *testing.T) {
	server, _, _ := testMissingSourceServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/search/assets", nil)
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestAllCollectionAndSmartRuleSupportFilenameOnlySearch(t *testing.T) {
	server, database, _ := testMissingSourceServer(t)
	matchingID := testInsertAsset(t, database, "角色a 001.jpg", "filenamecollection01", model.MediaTypeImage)
	testInsertAsset(t, database, "其他内容.jpg", "filenamecollection02", model.MediaTypeImage)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/collections/all/assets?page=1&pageSize=20&q=角色a", nil)
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("all collection status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}
	var page struct {
		Items []AssetDTO `json:"items"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != matchingID {
		t.Fatalf("all collection items = %#v, want asset %d", page.Items, matchingID)
	}

	collection, err := database.CreateSmartCollection(context.Background(), db.CollectionCreate{Name: "角色a", RuleJSON: `{"q":"角色a"}`})
	if err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/collections/"+collection.ID+"/assets?page=1&pageSize=20", nil)
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("smart collection status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}
	page.Items = nil
	if err := json.NewDecoder(recorder.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != matchingID {
		t.Fatalf("smart collection items = %#v, want asset %d", page.Items, matchingID)
	}
}

func TestCollectionRuleKeepsFilenameAndCombinedSearchSeparate(t *testing.T) {
	rule := `{"q":"文件名条件","combinedQuery":"描述条件"}`
	opts := optionsFromCollectionRule(&rule, 1, 100)
	if opts.Query != "文件名条件" || opts.CombinedQuery != "描述条件" {
		t.Fatalf("collection search options = query %q combined %q", opts.Query, opts.CombinedQuery)
	}
}

func TestCollectionNeighborAPIUsesSavedSmartRule(t *testing.T) {
	server, database, _ := testMissingSourceServer(t)
	firstVideo := testInsertAsset(t, database, "a-video.mp4", "neighborfiltervid001", model.MediaTypeVideo)
	image := testInsertAsset(t, database, "b-image.jpg", "neighborfilterimg001", model.MediaTypeImage)
	secondVideo := testInsertAsset(t, database, "c-video.mp4", "neighborfiltervid002", model.MediaTypeVideo)
	collection, err := database.CreateSmartCollection(context.Background(), db.CollectionCreate{Name: "仅视频", RuleJSON: `{"type":"video","sort":"filename"}`})
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/assets/"+strconv.FormatInt(firstVideo, 10)+"/neighbors?context=collection&collectionId="+collection.ID+"&sort=filename", nil)
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}
	var result NeighborsDTO
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Current.ID != firstVideo || len(result.Next) != 1 || result.Next[0].ID != secondVideo {
		t.Fatalf("neighbors = %#v, want current %d then video %d", result, firstVideo, secondVideo)
	}
	if result.Next[0].ID == image {
		t.Fatal("smart collection neighbor leaked an image excluded by the saved rule")
	}
}
