// Fork-only (endgame302/skillshare): the official registry's index: first crawl, resume,
// incremental passes, backoff and details.

package mcpreg

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRegistry serves the official fixtures and records each request's query.
type fakeRegistry struct {
	t        *testing.T
	mu       sync.Mutex
	requests []*http.Request
	fail     func(r *http.Request) int // a status to answer instead, or 0
}

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	fail := f.fail
	f.mu.Unlock()
	if ua := r.Header.Get("User-Agent"); ua != "skillshare" {
		f.t.Errorf("User-Agent = %q", ua)
	}
	if fail != nil {
		if code := fail(r); code != 0 {
			if code == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "120")
			}
			w.WriteHeader(code)
			return
		}
	}
	q := r.URL.Query()
	file := ""
	switch {
	case strings.HasPrefix(r.URL.EscapedPath(), "/v0.1/servers/"):
		if r.URL.EscapedPath() != "/v0.1/servers/io.github.brave%2Fbrave-search-mcp-server/versions/2.1.3" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		data := loadPage(f.t, "official-page1.json")
		writeJSONBody(f.t, w, data.Servers[0])
		return
	case q.Get("updated_since") != "":
		file = "official-incremental.json"
	case q.Get("cursor") == "":
		file = "official-page1.json"
	case q.Get("cursor") == "io.github.Oncorporation/filesystem-server:0.1.4":
		file = "official-page2.json"
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile("testdata/" + file)
	if err != nil {
		f.t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (f *fakeRegistry) take() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.requests
	f.requests = nil
	return out
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time      { return c.now }
func (c *clock) add(d time.Duration) { c.now = c.now.Add(d) }

func newTestOfficial(t *testing.T, dir string, base string, c *clock) *Official {
	o := NewOfficial(dir)
	o.Base, o.Now, o.PageDelay, o.PersistEvery = base, c.Now, 0, 1
	return o
}

func TestOfficialCrawl(t *testing.T) {
	fake := &fakeRegistry{t: t}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := &clock{now: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	o := newTestOfficial(t, dir, srv.URL, c)

	if !o.Due(false) {
		t.Fatal("an empty index is not due")
	}
	if err := o.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	reqs := fake.take()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	for _, r := range reqs {
		if q := r.URL.Query(); q.Get("version") != "latest" || q.Get("limit") != "100" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
	}
	entries, st := o.Snapshot()
	if len(entries) != 13 || st.State != "ready" || !st.Complete || st.Entries != 13 || st.Syncing || st.FetchedAt.IsZero() {
		t.Fatalf("status = %+v, %d entries", st, len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Name >= entries[i].Name {
			t.Fatalf("not sorted: %s, %s", entries[i-1].Name, entries[i].Name)
		}
	}
	if o.Due(false) || o.Start(false) {
		t.Fatal("a fresh index is due")
	}

	again := newTestOfficial(t, dir, srv.URL, c)
	if entries, st := again.Snapshot(); len(entries) != 13 || !st.Complete {
		t.Fatalf("reloaded = %d entries, %+v", len(entries), st)
	}
	if e, ok := again.Lookup("io.github.daedalus/old-sqlite"); !ok || e.Status != "deprecated" {
		t.Fatalf("lookup = %+v %v", e, ok)
	}
}

func TestOfficialCrawlResumes(t *testing.T) {
	fake := &fakeRegistry{t: t}
	fake.fail = func(r *http.Request) int {
		if r.URL.Query().Get("cursor") != "" {
			return http.StatusInternalServerError
		}
		return 0
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := &clock{now: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	o := newTestOfficial(t, dir, srv.URL, c)

	err := o.Sync(context.Background())
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 500 || !strings.Contains(err.Error(), "the MCP Registry answered HTTP 500") {
		t.Fatalf("err = %v", err)
	}
	entries, st := o.Snapshot()
	if len(entries) != 6 || st.State != "loading" || st.Complete || st.Error == "" || !st.RetryAt.Equal(c.now.Add(30*time.Second)) {
		t.Fatalf("status = %+v, %d entries", st, len(entries))
	}
	if o.Due(false) {
		t.Fatal("due before the retry time")
	}
	c.add(31 * time.Second)
	if !o.Due(false) {
		t.Fatal("not due after the retry time")
	}

	fake.take()
	fake.mu.Lock()
	fake.fail = nil
	fake.mu.Unlock()
	resumed := newTestOfficial(t, dir, srv.URL, c)
	if err := resumed.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	reqs := fake.take()
	if len(reqs) != 1 || reqs[0].URL.Query().Get("cursor") == "" {
		t.Fatalf("did not resume: %d requests", len(reqs))
	}
	if entries, st := resumed.Snapshot(); len(entries) != 13 || !st.Complete || st.Error != "" {
		t.Fatalf("after resume: %d, %+v", len(entries), st)
	}
}

func TestOfficialIncremental(t *testing.T) {
	fake := &fakeRegistry{t: t}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	start := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	c := &clock{now: start}
	o := newTestOfficial(t, t.TempDir(), srv.URL, c)
	if err := o.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake.take()

	c.add(61 * time.Minute)
	if !o.Due(false) {
		t.Fatal("not due after an hour")
	}
	if err := o.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	reqs := fake.take()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	q := reqs[0].URL.Query()
	if q.Get("version") != "" || q.Get("updated_since") != start.Format(time.RFC3339Nano) {
		t.Fatalf("incremental query = %s", reqs[0].URL.RawQuery)
	}
	if _, ok := o.Lookup("io.github.brave/brave-search-mcp-server"); ok {
		t.Fatal("deleted server still listed")
	}
	if e, _ := o.Lookup("com.pulsemcp/remote-filesystem"); e.Version != "0.2.0" {
		t.Fatalf("pulsemcp = %s", e.Version)
	}
	if e, _ := o.Lookup("io.github.daedalus/mcp-sqlite3"); e.Status != "deprecated" {
		t.Fatalf("mcp-sqlite3 = %s", e.Status)
	}
	if _, ok := o.Lookup("io.github.mrfentmen/sqlite-mcp"); !ok {
		t.Fatal("new server missing")
	}

	second := c.now
	c.add(61 * time.Minute)
	if err := o.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	reqs = fake.take()
	if got := reqs[0].URL.Query().Get("updated_since"); got != second.Add(-time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("second updated_since = %s", got)
	}

	// A 429 sets the retry time from Retry-After.
	fake.mu.Lock()
	fake.fail = func(*http.Request) int { return http.StatusTooManyRequests }
	fake.mu.Unlock()
	c.add(61 * time.Minute)
	if err := o.Sync(context.Background()); err == nil {
		t.Fatal("no error on 429")
	}
	if _, st := o.Snapshot(); !st.RetryAt.Equal(c.now.Add(120*time.Second)) || st.State != "ready" || st.Error == "" {
		t.Fatalf("after 429: %+v", st)
	}
}

func TestOfficialDetail(t *testing.T) {
	fake := &fakeRegistry{t: t}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := &clock{now: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)}
	o := newTestOfficial(t, t.TempDir(), srv.URL, c)
	if err := o.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake.take()

	r, err := o.Detail(context.Background(), "io.github.brave/brave-search-mcp-server")
	if err != nil || r.Server.Name != "io.github.brave/brave-search-mcp-server" {
		t.Fatalf("detail = %+v, %v", r, err)
	}
	if _, err := o.Detail(context.Background(), "io.github.brave/brave-search-mcp-server"); err != nil {
		t.Fatal(err)
	}
	if reqs := fake.take(); len(reqs) != 1 {
		t.Fatalf("detail requests = %d (not cached)", len(reqs))
	}
	if _, err := o.Detail(context.Background(), "io.github.nobody/nothing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}
