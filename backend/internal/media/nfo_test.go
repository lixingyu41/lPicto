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
	want := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC).Unix()
	if got == nil || *got != want {
		t.Fatalf("nfo timeline = %v, want %d", got, want)
	}
}

func TestNFOTimelineAtJSONUsesStoredNFO(t *testing.T) {
	got := NFOTimelineAtJSON(`{"groups":[{"title":"基本","items":[{"key":"releasedate","label":"发布日期","value":"2023-02-03","copyable":false}]}]}`)
	want := time.Date(2023, 2, 3, 0, 0, 0, 0, time.UTC).Unix()
	if got == nil || *got != want {
		t.Fatalf("stored nfo timeline = %v, want %d", got, want)
	}
}
