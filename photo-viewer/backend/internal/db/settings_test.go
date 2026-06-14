package db

import "testing"

func TestNormalizeScanFolders(t *testing.T) {
	got, err := NormalizeScanFolders([]string{"2024/01", "2024", "2024/01", "2025"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2024", "2025"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func TestNormalizeScanFoldersRootWins(t *testing.T) {
	got, err := NormalizeScanFolders([]string{"2024", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("got %#v", got)
	}
}

func TestAssetInScanFolders(t *testing.T) {
	if !AssetInScanFolders("2024/01/a.jpg", []string{"2024"}) {
		t.Fatal("expected asset under root")
	}
	if AssetInScanFolders("2023/a.jpg", []string{"2024"}) {
		t.Fatal("unexpected asset outside root")
	}
}

func TestNormalizeScanLibraries(t *testing.T) {
	got, err := NormalizeScanLibraries([]ScanLibrary{
		{ID: "lib-a", Name: " 家庭 ", Roots: []string{"Photo/2024", "Photo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "家庭" {
		t.Fatalf("name = %q", got[0].Name)
	}
	if len(got[0].Roots) != 1 || got[0].Roots[0] != "Photo" {
		t.Fatalf("roots = %#v, want Photo", got[0].Roots)
	}
}

func TestScanRootsFromLibraries(t *testing.T) {
	roots := ScanRoots([]ScanLibrary{
		{ID: "a", Name: "A", Roots: []string{"Photo"}},
		{ID: "b", Name: "B", Roots: []string{"Photo/Child", "Video"}},
	})
	want := []string{"Photo", "Video"}
	if len(roots) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(roots), len(want), roots)
	}
	for i := range want {
		if roots[i] != want[i] {
			t.Fatalf("roots = %#v, want %#v", roots, want)
		}
	}
}
