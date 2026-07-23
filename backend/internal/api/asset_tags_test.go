package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lpicto/backend/internal/model"
)

func TestAssetTagsAPIAllowsImageAssets(t *testing.T) {
	server, database, _ := testMissingSourceServer(t)
	id := testInsertAsset(t, database, "tagged.jpg", "1234567890abcdef1234", model.MediaTypeImage)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/assets/"+int64String(id)+"/tags", strings.NewReader(`{"tag":"  favorite  "}`))
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Items []AssetTagDTO `json:"items"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || payload.Items[0].AssetID != id || payload.Items[0].Tag != "favorite" {
		t.Fatalf("items = %#v, want one image tag", payload.Items)
	}
}
