package db

import (
	"reflect"
	"testing"

	"lpicto/backend/internal/model"
)

func TestAssetOrientationAllMatchesUnsetFilterSQL(t *testing.T) {
	base := AssetListOptions{
		Page:        1,
		PageSize:    50,
		Type:        model.MediaTypeImage,
		Sort:        "timeline_desc",
		VisibleOnly: true,
	}
	unsetWhere, unsetArgs := assetFilterSQL(base, false)

	all := base
	all.Orientation = "all"
	allWhere, allArgs := assetFilterSQL(all, false)
	if allWhere != unsetWhere || !reflect.DeepEqual(allArgs, unsetArgs) {
		t.Fatalf("orientation=all filter = %q %#v, want unset %q %#v", allWhere, allArgs, unsetWhere, unsetArgs)
	}

	all.Orientation = " ALL "
	allWhere, allArgs = assetFilterSQL(all, false)
	if allWhere != unsetWhere || !reflect.DeepEqual(allArgs, unsetArgs) {
		t.Fatalf("orientation= ALL filter = %q %#v, want unset %q %#v", allWhere, allArgs, unsetWhere, unsetArgs)
	}
}

func TestAssetOrientationAllDoesNotDisableFastSizeNeighbors(t *testing.T) {
	base := AssetListOptions{
		Type:        model.MediaTypeImage,
		Sort:        "size_desc",
		VisibleOnly: true,
	}
	if !fastMediaNeighborEligible(base, "library") {
		t.Fatal("unset orientation should be eligible for fast size neighbors")
	}

	all := base
	all.Orientation = "all"
	if !fastMediaNeighborEligible(all, "library") {
		t.Fatal("orientation=all should match unset fast size neighbor eligibility")
	}

	landscape := base
	landscape.Orientation = "landscape"
	if fastMediaNeighborEligible(landscape, "library") {
		t.Fatal("landscape orientation should not use the all-orientation fast path")
	}
}
