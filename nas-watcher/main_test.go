package main

import "testing"

func TestIsNestedFileExcludesRootFiles(t *testing.T) {
	tests := map[string]bool{
		"root.mp4":                   false,
		"folder/video.mp4":           true,
		"folder/subfolder/photo.jpg": true,
		"":                           false,
	}
	for relPath, want := range tests {
		if got := isNestedFile(relPath); got != want {
			t.Fatalf("isNestedFile(%q) = %v, want %v", relPath, got, want)
		}
	}
}
