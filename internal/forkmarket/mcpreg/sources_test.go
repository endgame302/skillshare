// Fork-only (endgame302/skillshare): GitHub's registry, Docker's catalog and the market front.

package mcpreg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func writeJSONBody(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Error(err)
	}
}

// githubPage is the GitHub fixture with a README added, as the live registry sends it.
func githubPage(t *testing.T) []byte {
	t.Helper()
	page := loadPage(t, "github-page.json")
	for i := range page.Servers {
		var pp map[string]map[string]any
		if err := json.Unmarshal(page.Servers[i].Server.Meta[MetaPublisher], &pp); err != nil {
			t.Fatal(err)
		}
		pp["github"]["readme"] = "# A long README"
		raw, _ := json.Marshal(pp)
		page.Servers[i].Server.Meta[MetaPublisher] = raw
	}
	data, _ := json.Marshal(page)
	return data
}

func TestCurated(t *testing.T) {
	var hits atomic.Int32
	var fail atomic.Bool
	body := githubPage(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.URL.Path != "/v0.1/servers" || r.URL.Query().Get("limit") != "100" {
			t.Errorf("request %s", r.URL)
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	c := &clock{now: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	cur := NewCurated(dir)
	cur.Base, cur.Now = srv.URL, c.Now

	entries, st, err := cur.Load(context.Background(), false)
	if err != nil || len(entries) != 2 || st.State != "ready" || !st.Complete {
		t.Fatalf("load = %d, %+v, %v", len(entries), st, err)
	}
	if entries[0].Name != "microsoft/markitdown" || entries[0].Stars != 195628 || entries[0].License != "MIT License" {
		t.Fatalf("markitdown = %+v", entries[0])
	}
	var saved curatedSnapshot
	if err := readGzipJSON(filepath.Join(dir, curatedFile), &saved); err != nil || len(saved.Servers) != 2 {
		t.Fatalf("cache = %d, %v", len(saved.Servers), err)
	}
	for _, r := range saved.Servers {
		if strings.Contains(string(r.Server.Meta[MetaPublisher]), `"readme":`) {
			t.Fatal("the cache keeps the README")
		}
		if r.Server.GitHub() == nil || r.Server.GitHub().Stars == 0 {
			t.Fatal("dropping the README lost the GitHub data")
		}
	}

	c.add(23 * time.Hour)
	if _, _, err := cur.Load(context.Background(), false); err != nil || hits.Load() != 1 {
		t.Fatalf("refetched within the TTL: %d, %v", hits.Load(), err)
	}
	// A new instance reads the cache instead of fetching.
	other := NewCurated(dir)
	other.Base, other.Now = srv.URL, c.Now
	if e, _, err := other.Load(context.Background(), false); err != nil || len(e) != 2 || hits.Load() != 1 {
		t.Fatalf("second instance = %d, %d, %v", len(e), hits.Load(), err)
	}

	c.add(2 * time.Hour)
	fail.Store(true)
	entries, st, err = cur.Load(context.Background(), false)
	if err != nil || len(entries) != 2 || !st.Stale || st.Error == "" || hits.Load() != 2 {
		t.Fatalf("failed refresh = %d, %+v, %v", len(entries), st, err)
	}
	// A failure is not retried on every request.
	if _, _, _ = cur.Load(context.Background(), false); hits.Load() != 2 {
		t.Fatalf("retried at once: %d", hits.Load())
	}
	fail.Store(false)
	if _, st, _ := cur.Load(context.Background(), true); st.Stale || st.Error != "" || hits.Load() != 3 {
		t.Fatalf("refresh = %+v", st)
	}

	if r, err := cur.Server(context.Background(), "microsoft/markitdown"); err != nil || r.Server.Name != "microsoft/markitdown" {
		t.Fatalf("server = %v", err)
	}
	if _, err := cur.Server(context.Background(), "nobody/nothing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}

	empty := NewCurated(t.TempDir())
	fail.Store(true)
	empty.Base, empty.Now = srv.URL, c.Now
	if _, st, err := empty.Load(context.Background(), false); err == nil || st.State != "error" {
		t.Fatalf("no cache and failing = %+v, %v", st, err)
	}
}

func TestDocker(t *testing.T) {
	data, err := os.ReadFile("testdata/docker-catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var mode atomic.Value // "", "304", "bad"
	mode.Store("")
	var lastIfNone atomic.Value
	lastIfNone.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastIfNone.Store(r.Header.Get("If-None-Match"))
		switch mode.Load() {
		case "304":
			w.WriteHeader(http.StatusNotModified)
		case "bad":
			_, _ = w.Write([]byte("version: [not yaml"))
		default:
			w.Header().Set("ETag", `"v1"`)
			_, _ = w.Write(data)
		}
	}))
	defer srv.Close()
	c := &clock{now: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)}
	d := NewDocker(t.TempDir())
	d.URL, d.Now = srv.URL, c.Now

	entries, st, err := d.Load(context.Background(), false)
	if err != nil || len(entries) != 13 || st.State != "ready" {
		t.Fatalf("load = %d, %+v, %v", len(entries), st, err)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Name >= entries[i].Name {
			t.Fatal("not sorted by name")
		}
	}
	if lastIfNone.Load() != "" {
		t.Fatal("conditional request without a copy")
	}

	mode.Store("304")
	c.add(time.Hour)
	entries, st, err = d.Load(context.Background(), true)
	if err != nil || len(entries) != 13 || !st.FetchedAt.Equal(c.now) || lastIfNone.Load() != `"v1"` {
		t.Fatalf("304 = %d, %+v, %v, %v", len(entries), st, err, lastIfNone.Load())
	}

	mode.Store("bad")
	c.add(7 * time.Hour)
	entries, st, err = d.Load(context.Background(), false)
	if err != nil || len(entries) != 13 || !st.Stale || st.Error == "" {
		t.Fatalf("bad answer = %d, %+v, %v", len(entries), st, err)
	}
	// The copy on disk was not replaced by the bad answer.
	again := NewDocker(d.Dir)
	again.URL, again.Now = srv.URL, func() time.Time { return c.now.Add(-7 * time.Hour) }
	if e, _, err := again.Load(context.Background(), false); err != nil || len(e) != 13 {
		t.Fatalf("cached copy = %d, %v", len(e), err)
	}

	if e, err := d.Entry(context.Background(), "brave"); err != nil || e.Image == "" {
		t.Fatalf("brave = %+v, %v", e, err)
	}
	if _, err := d.Entry(context.Background(), "nothing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func newTestMarket(t *testing.T, dockerOnPath bool) *Market {
	t.Helper()
	official := httptest.NewServer(&fakeRegistry{t: t})
	t.Cleanup(official.Close)
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile("testdata/github-page.json")
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(github.Close)
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "testdata/docker-catalog.yaml")
	}))
	t.Cleanup(docker.Close)

	c := &clock{now: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)}
	m := NewMarket(t.TempDir())
	m.Official.Base, m.Official.Now, m.Official.PageDelay = official.URL, c.Now, 0
	m.Curated.Base, m.Curated.Now = github.URL, c.Now
	m.Docker.URL, m.Docker.Now = docker.URL, c.Now
	m.GOOS = "windows"
	m.LookPath = func(string) (string, error) {
		if dockerOnPath {
			return "docker", nil
		}
		return "", errors.New("not found")
	}
	// Fill the index here, so List finds it fresh and starts no background pass.
	if err := m.Official.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMarketList(t *testing.T) {
	m := newTestMarket(t, false)
	ctx := context.Background()

	res, err := m.List(ctx, SourceOfficial, Query{}, false)
	if err != nil || res.Total != 12 || res.HiddenDeprecated != 1 || res.Status.State != "ready" || res.DockerFound != nil {
		t.Fatalf("official = %+v, %v", res, err)
	}
	if res.Items[0].Name > res.Items[1].Name {
		t.Fatal("official is not in name order")
	}
	if res, _ := m.List(ctx, SourceOfficial, Query{ShowDeprecated: true}, false); res.Total != 13 {
		t.Fatalf("with deprecated = %d", res.Total)
	}

	res, err = m.List(ctx, SourceCurated, Query{}, false)
	if err != nil || res.Total != 2 {
		t.Fatalf("curated = %+v, %v", res, err)
	}
	official := map[string]bool{}
	for _, e := range res.Items {
		official[e.Name] = e.Official
	}
	if !official["io.github.netdata/mcp-server"] || official["microsoft/markitdown"] {
		t.Fatalf("official marks = %v", official)
	}
	if res.Items[0].Name != "microsoft/markitdown" {
		t.Fatal("curated is not sorted by stars")
	}

	res, err = m.List(ctx, SourceDocker, Query{}, false)
	if err != nil || res.Total != 13 || res.DockerFound == nil || *res.DockerFound {
		t.Fatalf("docker = %+v, %v", res, err)
	}
	for i := 1; i < len(res.Items); i++ {
		if res.Items[i-1].Pulls < res.Items[i].Pulls {
			t.Fatal("docker is not sorted by pulls")
		}
	}

	if _, err := m.List(ctx, "glama", Query{}, false); !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("unknown = %v", err)
	}
}

func TestMarketPlan(t *testing.T) {
	m := newTestMarket(t, false)
	ctx := context.Background()

	p, err := m.Plan(ctx, SourceOfficial, "io.github.brave/brave-search-mcp-server", nil)
	if err != nil || len(p.Options) != 1 || p.Options[0].Kind != "npm" || p.Options[0].Server == nil || p.OS != "windows" || p.Server == nil {
		t.Fatalf("official plan = %+v, %v", p, err)
	}
	if hasWarning(p.Options[0], WarnDockerMissing) {
		t.Fatal("npm option warned about Docker")
	}

	p, err = m.Plan(ctx, SourceCurated, "microsoft/markitdown", nil)
	if err != nil || !hasWarning(p.Options[0], WarnNotInOfficial) {
		t.Fatalf("curated plan = %+v, %v", p, err)
	}
	if p, _ := m.Plan(ctx, SourceCurated, "io.github.netdata/mcp-server", nil); hasWarning(p.Options[0], WarnNotInOfficial) || !p.Entry.Official {
		t.Fatal("an official entry was warned about")
	}

	p, err = m.Plan(ctx, SourceDocker, "brave", nil)
	if err != nil || p.DockerFound || !hasWarning(p.Options[0], WarnDockerMissing) || p.Options[0].Server == nil || p.Docker == nil {
		t.Fatalf("docker plan = %+v, %v", p, err)
	}

	if _, err := m.Plan(ctx, "nope", "x", nil); !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("unknown source = %v", err)
	}
	if _, err := m.Plan(ctx, SourceDocker, "nothing", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown docker entry = %v", err)
	}
}

func TestMarketPlanWarnings(t *testing.T) {
	m := newTestMarket(t, true)
	ctx := context.Background()
	// brave becomes deprecated and its namespace a bulk publisher.
	m.Official.mu.Lock()
	brave := m.Official.state.Entries["io.github.brave/brave-search-mcp-server"]
	brave.Status = "deprecated"
	m.Official.state.Entries[brave.Name] = brave
	for i := range BulkThreshold {
		name := fmt.Sprintf("io.github.brave/gen%02d", i)
		m.Official.state.Entries[name] = Entry{Name: name, Namespace: "io.github.brave", Status: "active"}
	}
	m.Official.dirty = true
	m.Official.mu.Unlock()

	p, err := m.Plan(ctx, SourceOfficial, brave.Name, nil)
	if err != nil {
		t.Fatal(err)
	}
	o := p.Options[0]
	if !hasWarning(o, WarnDeprecated) || warning(o, WarnBulk).Params["count"] != "51" || !hasWarning(o, WarnFirstRun) {
		t.Fatalf("warnings = %+v", o.Warnings)
	}
	if o.Warnings[0].Code != WarnFirstRun {
		t.Fatal("the entry's own warnings come first")
	}
	res, _ := m.List(ctx, SourceOfficial, Query{}, false)
	if res.HiddenBulk != BulkThreshold || res.HiddenDeprecated != 2 {
		t.Fatalf("hidden bulk = %d, deprecated = %d", res.HiddenBulk, res.HiddenDeprecated)
	}

	p, err = m.Plan(ctx, SourceDocker, "brave", nil)
	if err != nil || hasWarning(p.Options[0], WarnDockerMissing) || !p.DockerFound {
		t.Fatalf("docker found = %+v, %v", p, err)
	}
}
