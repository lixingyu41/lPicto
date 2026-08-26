package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetContentDispositionDownload(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/assets/1/original?download=1", nil)
	value := assetContentDisposition(req, "照片 01.jpg")
	if !strings.HasPrefix(value, "attachment;") || !strings.Contains(value, "filename*=UTF-8''") {
		t.Fatalf("download content disposition = %q", value)
	}
}

func TestAssetContentDispositionInlineByDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/assets/1/original", nil)
	if value := assetContentDisposition(req, "photo.jpg"); !strings.HasPrefix(value, "inline;") {
		t.Fatalf("default content disposition = %q", value)
	}
}
