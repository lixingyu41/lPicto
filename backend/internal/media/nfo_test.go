package media

import (
	"strings"
	"testing"
	"time"
)

func TestParseNFOSearchTextIncludesSupportedFields(t *testing.T) {
	info := ParseNFO("movie.nfo", `<movie>
  <title>Example Title</title>
  <year>2024</year>
  <premiered>2024-05-01</premiered>
  <tag>Favorite</tag>
  <genre>Drama</genre>
  <uniqueid type="imdb">tt1234567</uniqueid>
  <actor><name>Alice Actor</name><role>Lead</role></actor>
</movie>`)
	search := NFOSearchText(info)
	for _, want := range []string{"example title", "2024", "2024-05-01", "favorite", "drama", "tt1234567", "alice actor", "actor", "title", "premiered", "首映时间"} {
		if !strings.Contains(search, want) {
			t.Fatalf("search text %q missing %q", search, want)
		}
	}
	if info.Fields["标题"] != "Example Title" {
		t.Fatalf("title field = %q", info.Fields["标题"])
	}
}

func TestNFOTimelineAtUsesPremieredBeforeYear(t *testing.T) {
	info := ParseNFO("movie.nfo", `<movie>
  <year>2020</year>
  <premiered>2024-05-01</premiered>
</movie>`)
	got := NFOTimelineAt(info)
	want := time.Date(2024, 5, 1, 0, 0, 0, 0, time.Local).Unix()
	if got == nil || *got != want {
		t.Fatalf("nfo timeline = %v, want %d", got, want)
	}
}

func TestNFOTimelineAtJSONUsesStoredNFO(t *testing.T) {
	got := NFOTimelineAtJSON(`{"groups":[{"title":"基本","items":[{"key":"releasedate","label":"发布日期","value":"2023-02-03","copyable":false}]}]}`)
	want := time.Date(2023, 2, 3, 0, 0, 0, 0, time.Local).Unix()
	if got == nil || *got != want {
		t.Fatalf("stored nfo timeline = %v, want %d", got, want)
	}
}

func TestMergeEmbeddedAuthorsKeepsNFOAuthorsAndReplacesEmbeddedValues(t *testing.T) {
	info := ParseNFO("movie.nfo", `<movie><writer>NFO Author</writer></movie>`)
	merged, changed := MergeEmbeddedAuthors(&info, []string{"Video Creator", "nfo author"})
	if !changed {
		t.Fatal("expected embedded author merge to change metadata")
	}
	if got := merged.Fields["作者"]; got != "NFO Author / Video Creator" {
		t.Fatalf("merged author field = %q", got)
	}
	var artists int
	for _, group := range merged.Groups {
		for _, item := range group.Items {
			if item.Key == "artist" && item.Value == "Video Creator" {
				artists++
			}
		}
	}
	if artists != 1 {
		t.Fatalf("embedded artist entries = %d, want 1", artists)
	}

	withoutEmbedded, changed := MergeEmbeddedAuthors(merged, nil)
	if !changed || withoutEmbedded.Fields["作者"] != "NFO Author" {
		t.Fatalf("authors after embedded removal = %#v", withoutEmbedded.Fields)
	}
}
