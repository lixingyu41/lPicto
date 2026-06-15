package media

import (
	"strings"
	"testing"
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
