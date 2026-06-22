package db

import (
	"testing"
	"time"
)

func TestUniformAnchorsUseSampledAssetValues(t *testing.T) {
	items := []libraryAnchorRow{
		{TimelineAt: 1000},
		{TimelineAt: 900},
		{TimelineAt: 100},
	}
	anchors := uniformAnchors("timeline_desc", items, 100)
	if len(anchors) != 3 {
		t.Fatalf("anchors len = %d, want 3", len(anchors))
	}
	want := time.Unix(900, 0).Local().Format("2006-01-02")
	if anchors[1].Value != 900 || anchors[1].Label != want {
		t.Fatalf("middle anchor = %+v, want value 900 label %s", anchors[1], want)
	}
}
