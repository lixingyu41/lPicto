package api

import (
	"testing"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/scanner"
)

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

func TestScanLibraryProgressUsesLiveCountsWhenActive(t *testing.T) {
	library := db.ScanLibrary{ID: "lib-1", Name: "PIC", Roots: []string{"nas/PIC"}}
	stats := scanLibraryProgressStats{
		DiscoveredFiles: 80444,
		Progress:        db.ProcessingProgress{AssetTotal: 80444},
	}
	status := scanner.Status{
		Running: true,
		Progress: scanner.Progress{
			Roots:           []string{"nas/PIC"},
			DiscoveredFiles: 1279,
			TotalFiles:      1279,
			ScannedFiles:    1147,
			TotalSeen:       1147,
		},
	}

	progress := scanLibraryProgressDTO(library, stats, status)
	if !progress.Active {
		t.Fatal("progress should be active")
	}
	if progress.ScannedFiles != 80444 || progress.DiscoveredFiles != 80444 || progress.UnscannedFiles != 0 {
		t.Fatalf("progress = %#v, want existing database scan count preserved", progress)
	}
}

func TestScanLibraryProgressUsesLiveCountWhenDatabaseHasFewerAssets(t *testing.T) {
	library := db.ScanLibrary{ID: "lib-1", Name: "PIC", Roots: []string{"nas/PIC"}}
	stats := scanLibraryProgressStats{
		DiscoveredFiles: 80444,
		Progress:        db.ProcessingProgress{AssetTotal: 1000},
	}
	status := scanner.Status{
		Running: true,
		Progress: scanner.Progress{
			Roots:           []string{"nas/PIC"},
			DiscoveredFiles: 1279,
			TotalFiles:      1279,
			ScannedFiles:    1147,
			TotalSeen:       1147,
		},
	}

	progress := scanLibraryProgressDTO(library, stats, status)
	if progress.ScannedFiles != 1147 || progress.DiscoveredFiles != 80444 || progress.UnscannedFiles != 79297 {
		t.Fatalf("progress = %#v, want live count above stale database count", progress)
	}
}

func TestScanLibraryProgressUsesRootStatsDuringMultiRootScan(t *testing.T) {
	status := scanner.Status{
		Running: true,
		Progress: scanner.Progress{
			Roots: []string{"nas/PIC", "nas/VID"},
			RootStats: map[string]scanner.RootProgress{
				"nas/PIC": {DiscoveredFiles: 5129, TotalFiles: 5129, ScannedFiles: 4997, TotalSeen: 4997},
				"nas/VID": {DiscoveredFiles: 0, TotalFiles: 0, ScannedFiles: 0, TotalSeen: 0},
			},
		},
	}

	pic := scanLibraryProgressDTO(db.ScanLibrary{ID: "pic", Name: "PIC", Roots: []string{"nas/PIC"}}, scanLibraryProgressStats{
		DiscoveredFiles: 80444,
		Progress:        db.ProcessingProgress{AssetTotal: 80444},
	}, status)
	if !pic.Active || pic.ScannedFiles != 80444 || pic.DiscoveredFiles != 80444 || pic.UnscannedFiles != 0 {
		t.Fatalf("pic progress = %#v, want existing database count preserved during root scan", pic)
	}

	vid := scanLibraryProgressDTO(db.ScanLibrary{ID: "vid", Name: "VID", Roots: []string{"nas/VID"}}, scanLibraryProgressStats{
		DiscoveredFiles: 14421,
		Progress:        db.ProcessingProgress{AssetTotal: 14421},
	}, status)
	if !vid.Active || vid.ScannedFiles != 14421 || vid.DiscoveredFiles != 14421 || vid.UnscannedFiles != 0 {
		t.Fatalf("vid progress = %#v, want existing database count preserved before root is reached", vid)
	}
}
