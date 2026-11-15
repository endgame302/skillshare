package skillssh

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const onePage = `<script>self.__next_f.push([1,"{\"initialSkills\":[{\"source\":\"a/b\",\"skillId\":\"c\",\"name\":\"c\",\"installs\":3}]}"])</script>`

type fakeSite struct {
	*httptest.Server
	hits   atomic.Int32
	status atomic.Int32
	body   atomic.Value
	req    atomic.Value
}

func newFakeSite(t *testing.T, body string) *fakeSite {
	t.Helper()
	f := &fakeSite{}
	f.body.Store(body)
	f.status.Store(http.StatusOK)
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.req.Store(r.Clone(context.Background()))
		w.WriteHeader(int(f.status.Load()))
		_, _ = w.Write([]byte(f.body.Load().(string)))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeSite) lastRequest() *http.Request { return f.req.Load().(*http.Request) }

func newTestClient(site *fakeSite, now *time.Time) *Client {
	c := New()
	c.BaseURL = site.URL
	c.now = func() time.Time { return *now }
	return c
}

func TestBoardFetchesThePageAndCachesIt(t *testing.T) {
	site := newFakeSite(t, onePage)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	c := newTestClient(site, &now)

	for _, b := range Boards {
		res, err := c.Board(context.Background(), b)
		if err != nil || len(res.Skills) != 1 || res.Stale {
			t.Fatalf("Board(%s) = %+v, %v", b, res, err)
		}
		wantPath, _ := b.path()
		if got := site.lastRequest().URL.Path; got != wantPath {
			t.Errorf("Board(%s) fetched %q, want %q", b, got, wantPath)
		}
	}
	if ua := site.lastRequest().Header.Get("User-Agent"); ua != "skillshare" {
		t.Errorf("User-Agent = %q, want the generic skillshare", ua)
	}

	now = now.Add(59 * time.Minute)
	if _, err := c.Board(context.Background(), Hot); err != nil {
		t.Fatal(err)
	}
	if n := site.hits.Load(); n != 3 {
		t.Fatalf("hits = %d, want 3: a list younger than an hour comes from memory", n)
	}
	now = now.Add(2 * time.Minute)
	if _, err := c.Board(context.Background(), Hot); err != nil {
		t.Fatal(err)
	}
	if n := site.hits.Load(); n != 4 {
		t.Fatalf("hits = %d, want 4: an hour-old list is fetched again", n)
	}
}

func TestBoardServesAnOldCopyWhenSkillsShIsDown(t *testing.T) {
	site := newFakeSite(t, onePage)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	c := newTestClient(site, &now)
	first, err := c.Board(context.Background(), Trending)
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Hour)
	site.status.Store(http.StatusServiceUnavailable)
	res, err := c.Board(context.Background(), Trending)
	if err != nil || !res.Stale || len(res.Skills) != 1 || !res.FetchedAt.Equal(first.FetchedAt) {
		t.Fatalf("Board = %+v, %v; want the old copy marked stale", res, err)
	}

	var httpErr *HTTPError
	if _, err := c.Board(context.Background(), Hot); !errors.As(err, &httpErr) || httpErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("err = %v, want an HTTPError 503: a list never read has no copy to fall back on", err)
	}
}

func TestBoardReportsAChangedPage(t *testing.T) {
	site := newFakeSite(t, onePage)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	c := newTestClient(site, &now)
	if _, err := c.Board(context.Background(), AllTime); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	site.body.Store(`<html><body>redesigned</body></html>`)
	if _, err := c.Board(context.Background(), AllTime); !errors.Is(err, ErrFormatChanged) {
		t.Fatalf("err = %v, want ErrFormatChanged even with an old copy", err)
	}
}

func TestBoardRejectsUnknownNames(t *testing.T) {
	c := New()
	c.BaseURL = "http://127.0.0.1:1" // never contacted
	if _, err := c.Board(context.Background(), Board("weekly")); !errors.Is(err, ErrUnknownBoard) {
		t.Fatalf("err = %v, want ErrUnknownBoard", err)
	}
}

func TestBoardCapsThePageSize(t *testing.T) {
	site := newFakeSite(t, onePage+strings.Repeat(" ", maxPageBytes))
	now := time.Now()
	c := newTestClient(site, &now)
	if _, err := c.Board(context.Background(), AllTime); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v, want a size error", err)
	}
}

func TestSearch(t *testing.T) {
	site := newFakeSite(t, `{"query":"re","skills":[{"id":"a/b/c","source":"a/b","skillId":"c","name":"c","installs":9}]}`)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	c := newTestClient(site, &now)

	res, err := c.Search(context.Background(), "  React Hooks ", 500)
	if err != nil || len(res.Skills) != 1 || res.Skills[0].Installs != 9 {
		t.Fatalf("Search = %+v, %v", res, err)
	}
	req := site.lastRequest()
	if req.URL.Path != "/api/search" || req.URL.Query().Get("q") != "React Hooks" || req.URL.Query().Get("limit") != "100" {
		t.Fatalf("request = %s, want q trimmed and limit capped at 100", req.URL)
	}

	now = now.Add(9 * time.Minute)
	if _, err := c.Search(context.Background(), "react hooks", 100); err != nil {
		t.Fatal(err)
	}
	if n := site.hits.Load(); n != 1 {
		t.Fatalf("hits = %d, want 1: the same search within ten minutes comes from memory", n)
	}
	now = now.Add(2 * time.Minute)
	if _, err := c.Search(context.Background(), "react hooks", 100); err != nil {
		t.Fatal(err)
	}
	if n := site.hits.Load(); n != 2 {
		t.Fatalf("hits = %d, want 2", n)
	}
}

func TestSearchNeedsTwoCharacters(t *testing.T) {
	site := newFakeSite(t, `{"skills":[]}`)
	now := time.Now()
	c := newTestClient(site, &now)
	for _, q := range []string{"", " r ", "é"} {
		if _, err := c.Search(context.Background(), q, 10); !errors.Is(err, ErrQueryTooShort) {
			t.Errorf("Search(%q) err = %v, want ErrQueryTooShort", q, err)
		}
	}
	if _, err := c.Search(context.Background(), "日本", 10); err != nil {
		t.Errorf("two characters of any script should do: %v", err)
	}
	if n := site.hits.Load(); n != 1 {
		t.Fatalf("hits = %d, want 1", n)
	}
}

func TestSearchPassesOnTheSiteError(t *testing.T) {
	site := newFakeSite(t, `{"error":"Query must be at least 2 characters"}`)
	site.status.Store(http.StatusBadRequest)
	now := time.Now()
	c := newTestClient(site, &now)
	_, err := c.Search(context.Background(), "ab", 10)
	if err == nil || !strings.Contains(err.Error(), "Query must be at least 2 characters") {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchKeepsTheCacheBounded(t *testing.T) {
	site := newFakeSite(t, `{"skills":[]}`)
	now := time.Now()
	c := newTestClient(site, &now)
	for i := range maxSearches + 5 {
		if _, err := c.Search(context.Background(), "query"+strings.Repeat("x", i), 10); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(c.searches); n > maxSearches {
		t.Fatalf("%d searches kept, want at most %d", n, maxSearches)
	}
}
