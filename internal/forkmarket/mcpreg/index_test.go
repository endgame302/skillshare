// Fork-only (endgame302/skillshare): trimmed entries, and how a page of them is picked.

package mcpreg

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func fixtureRecord(t *testing.T, name string) ServerResponse {
	t.Helper()
	for _, file := range []string{"official-page1.json", "official-page2.json", "github-page.json"} {
		for _, r := range loadPage(t, file).Servers {
			if r.Server.Name == name {
				return r
			}
		}
	}
	t.Fatalf("no fixture record %s", name)
	return ServerResponse{}
}

func TestEntryFromServer(t *testing.T) {
	brave := EntryFromServer(fixtureRecord(t, "io.github.brave/brave-search-mcp-server"))
	if !slices.Equal(brave.Kinds, []string{"npm"}) || !brave.Local || brave.Remote || !brave.Installable || brave.Blocked != "" {
		t.Fatalf("brave = %+v", brave)
	}
	if brave.Namespace != "io.github.brave" || brave.Status != "active" || brave.Version != "2.1.3" || brave.UpdatedAt == "" {
		t.Fatalf("brave = %+v", brave)
	}

	sse := EntryFromServer(fixtureRecord(t, "io.github.Evozim/chroot-filesystem-jail-mcp"))
	if !sse.Remote || sse.Installable || sse.Blocked != RefuseSSE {
		t.Fatalf("sse entry = %+v", sse)
	}

	md := EntryFromServer(fixtureRecord(t, "microsoft/markitdown"))
	if md.License != "MIT License" || md.Stars != 195628 || md.Title == "" {
		t.Fatalf("markitdown = %+v", md)
	}

	old := EntryFromServer(fixtureRecord(t, "io.github.daedalus/old-sqlite"))
	if old.Status != "deprecated" {
		t.Fatalf("old-sqlite status = %q", old.Status)
	}

	long := ServerResponse{Server: ServerJSON{Name: "io.github.x/long", Description: strings.Repeat("é", 500)}}
	if d := EntryFromServer(long).Description; len([]rune(d)) != maxDescription+1 || !strings.HasSuffix(d, "…") {
		t.Fatalf("description not cut: %d runes", len([]rune(d)))
	}
	if e := EntryFromServer(long); e.Title != "long" || e.Installable || e.Blocked != RefuseNoInstallData {
		t.Fatalf("no-install entry = %+v", e)
	}
}

func TestEntryFromDocker(t *testing.T) {
	c := loadCatalog(t)
	brave := EntryFromDocker(c.Registry["brave"])
	if !brave.DockerBuilt || brave.Pulls <= 0 || !brave.Installable || !brave.Local || brave.Image == "" {
		t.Fatalf("brave = %+v", brave)
	}
	curl := EntryFromDocker(c.Registry["curl"])
	if curl.Installable || curl.Blocked != RefusePOCI {
		t.Fatalf("curl = %+v", curl)
	}
}

func names(p Page) []string {
	var out []string
	for _, e := range p.Items {
		out = append(out, e.Name)
	}
	return out
}

func sample() []Entry {
	var entries []Entry
	for i := range 50 {
		entries = append(entries, Entry{Name: fmt.Sprintf("io.github.bulk/x%02d", i), Namespace: "io.github.bulk", Status: "active", Local: true, Installable: true})
	}
	entries = append(entries,
		Entry{Name: "io.github.a/weather", Title: "Weather", Description: "Forecasts by city", Namespace: "io.github.a", Status: "active", Local: true, Installable: true, UpdatedAt: "2026-01-01", Stars: 5, Pulls: 10},
		Entry{Name: "io.github.b/sky", Title: "Sky weather", Description: "Clouds", Namespace: "io.github.b", Status: "active", Remote: true, Installable: true, UpdatedAt: "2026-03-01", Stars: 50, Pulls: 1},
		Entry{Name: "io.github.c/old", Title: "Old weather", Namespace: "io.github.c", Status: "deprecated", Local: true, UpdatedAt: "2026-02-01", Stars: 1, Pulls: 100},
		Entry{Name: "io.github.d/sse", Title: "SSE thing", Category: "weather tools", Namespace: "io.github.d", Status: "active", Remote: true, Blocked: RefuseSSE},
	)
	return entries
}

func TestFilter(t *testing.T) {
	entries := sample()
	counts := NamespaceCounts(entries)
	if counts["io.github.bulk"] != 50 || counts["io.github.a"] != 1 {
		t.Fatalf("counts = %v", counts)
	}

	p := Filter(entries, Query{}, counts)
	if p.Total != 3 || p.HiddenBulk != 50 || p.HiddenDeprecated != 1 || p.BulkThreshold != BulkThreshold {
		t.Fatalf("default page = %+v", p)
	}
	if got := names(p); !slices.Equal(got, []string{"io.github.a/weather", "io.github.b/sky", "io.github.d/sse"}) {
		t.Fatalf("default names = %v", got)
	}
	if p := Filter(entries, Query{ShowBulk: true, Limit: 100}, counts); p.Total != 53 || p.HiddenBulk != 0 {
		t.Fatalf("with bulk = %d hidden %d", p.Total, p.HiddenBulk)
	}
	if p := Filter(entries, Query{ShowDeprecated: true}, counts); p.Total != 4 {
		t.Fatalf("with deprecated = %d", p.Total)
	}
	if p := Filter(entries, Query{}, nil); p.Total != 53 {
		t.Fatalf("no counts = %d", p.Total)
	}

	// Terms must all match, in any of the searched fields.
	if got := names(Filter(entries, Query{Text: "weather"}, counts)); !slices.Equal(got, []string{"io.github.a/weather", "io.github.b/sky", "io.github.d/sse"}) {
		t.Fatalf("weather = %v", got)
	}
	if got := names(Filter(entries, Query{Text: "city WEATHER"}, counts)); !slices.Equal(got, []string{"io.github.a/weather"}) {
		t.Fatalf("city weather = %v", got)
	}
	if got := names(Filter(entries, Query{Text: "weather", Kind: "remote"}, counts)); !slices.Equal(got, []string{"io.github.b/sky", "io.github.d/sse"}) {
		t.Fatalf("remote = %v", got)
	}
	if got := names(Filter(entries, Query{Text: "weather", Kind: "remote", InstallableOnly: true}, counts)); !slices.Equal(got, []string{"io.github.b/sky"}) {
		t.Fatalf("installable remote = %v", got)
	}
	if got := names(Filter(entries, Query{Text: "weather", Kind: "local"}, counts)); !slices.Equal(got, []string{"io.github.a/weather"}) {
		t.Fatalf("local = %v", got)
	}

	sorted := func(sort string) []string {
		return names(Filter(entries, Query{Sort: sort, ShowDeprecated: true}, counts))
	}
	if got := sorted("updated"); !slices.Equal(got, []string{"io.github.b/sky", "io.github.c/old", "io.github.a/weather", "io.github.d/sse"}) {
		t.Fatalf("updated = %v", got)
	}
	if got := sorted("stars"); got[0] != "io.github.b/sky" {
		t.Fatalf("stars = %v", got)
	}
	if got := sorted("pulls"); got[0] != "io.github.c/old" {
		t.Fatalf("pulls = %v", got)
	}
	// Relevance: an exact name or title match first.
	if got := names(Filter(entries, Query{Text: "sky weather"}, counts)); got[0] != "io.github.b/sky" {
		t.Fatalf("relevance = %v", got)
	}
	if got := names(Filter(entries, Query{Text: "weather"}, counts)); got[0] != "io.github.a/weather" {
		t.Fatalf("relevance by short name = %v", got)
	}

	// Paging.
	all := Filter(entries, Query{ShowBulk: true, Limit: 20, Offset: 40}, counts)
	if all.Total != 53 || len(all.Items) != 13 {
		t.Fatalf("page 3 = %d of %d", len(all.Items), all.Total)
	}
	if p := Filter(entries, Query{ShowBulk: true}, counts); len(p.Items) != 50 {
		t.Fatalf("default limit = %d", len(p.Items))
	}
	if p := Filter(entries, Query{ShowBulk: true, Limit: 500}, counts); len(p.Items) != 53 {
		t.Fatalf("limit cap = %d", len(p.Items))
	}
	if p := Filter(entries, Query{Offset: 999}, counts); p.Items == nil || len(p.Items) != 0 {
		t.Fatalf("past the end = %#v", p.Items)
	}
	if p := Filter(nil, Query{}, nil); p.Items == nil {
		t.Fatal("Items is nil")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := t.TempDir() + "/a/b/x.json.gz"
	in := map[string]int{"x": 1}
	if err := writeGzipJSON(path, in); err != nil {
		t.Fatal(err)
	}
	var out map[string]int
	if err := readGzipJSON(path, &out); err != nil || out["x"] != 1 {
		t.Fatalf("read back %v, %v", out, err)
	}
}
