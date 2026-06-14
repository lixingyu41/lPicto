package api

import "testing"

func TestClampPage(t *testing.T) {
	page, pageSize := ClampPage(0, 999, 100, 500)
	if page != 1 || pageSize != 500 {
		t.Fatalf("clamped = %d/%d", page, pageSize)
	}
	page, pageSize = ClampPage(2, 0, 100, 500)
	if page != 2 || pageSize != 100 {
		t.Fatalf("defaulted = %d/%d", page, pageSize)
	}
}
