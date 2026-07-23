package db

import (
	"context"
	"path/filepath"
	"testing"

	"lpicto/backend/internal/model"
)

func TestPersistentFilterFieldsAndNFOIndexStaySynchronized(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "persistent-filter.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	nfo := `{"groups":[{"title":"演员","items":[{"key":"actor","value":"Alice"}]},{"title":"信息","items":[{"key":"year","value":"2025"}]}]}`
	asset := AssetUpsert{
		RelPath: "indexed.jpg", ParentRelPath: "", Filename: "indexed.jpg", Ext: "jpg", MediaType: model.MediaTypeImage,
		Size: 10, Mtime: 10, ImportedAt: 10, TimelineAt: 10, CacheKey: "indexed-cache", Width: intValue(1920), Height: intValue(1080),
		NFOJSON: &nfo, NFOScanned: true, ThumbStatus: model.StatusReady, PreviewStatus: model.StatusReady,
		VideoPosterStatus: model.StatusNotRequired, VideoProxyStatus: model.StatusNotRequired,
	}
	id, _, _, err := database.UpsertAsset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}

	var rating, rotation, orientation int
	if err := database.Conn().QueryRowContext(ctx, `SELECT rating, rotation, orientation FROM media_asset WHERE id=$1`, id).Scan(&rating, &rotation, &orientation); err != nil {
		t.Fatal(err)
	}
	if rating != 0 || rotation != 0 || orientation != 1 {
		t.Fatalf("initial filter fields = rating %d rotation %d orientation %d", rating, rotation, orientation)
	}

	if _, err := database.SetAssetPreferences(ctx, id, intValue(90), intValue(4)); err != nil {
		t.Fatal(err)
	}
	if err := database.Conn().QueryRowContext(ctx, `SELECT rating, rotation, orientation FROM media_asset WHERE id=$1`, id).Scan(&rating, &rotation, &orientation); err != nil {
		t.Fatal(err)
	}
	if rating != 4 || rotation != 90 || orientation != 2 {
		t.Fatalf("updated filter fields = rating %d rotation %d orientation %d", rating, rotation, orientation)
	}

	wantedRating := 4
	page, err := database.SearchAssets(ctx, AssetListOptions{Page: 1, PageSize: 20, Rating: &wantedRating, Orientation: "portrait", NFOActor: "alic"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != id {
		t.Fatalf("indexed search = %#v, want asset %d", page.Items, id)
	}

	if _, err := database.Conn().ExecContext(ctx, `UPDATE media_asset SET width=800,height=1200 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := database.Conn().QueryRowContext(ctx, `SELECT orientation FROM media_asset WHERE id=$1`, id).Scan(&orientation); err != nil {
		t.Fatal(err)
	}
	if orientation != 1 {
		t.Fatalf("orientation after rotated dimension update = %d, want landscape", orientation)
	}
}
