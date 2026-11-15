package clawhub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClawHub serves a feed, the well-known file and downloads from a local TLS server.
type fakeClawHub struct {
	srv       *httptest.Server
	feed      atomic.Value // []byte
	apiBase   atomic.Value // string
	feedHits  atomic.Int32
	failFeed  atomic.Bool
	archive   []byte
	lastQuery atomic.Value // string
	lastUA    atomic.Value // string
}

func newFakeClawHub(t *testing.T) *fakeClawHub {
	t.Helper()
	f := &fakeClawHub{archive: readTestdata(t, "blogwatcher-1.0.0.zip")}
	f.feed.Store(readTestdata(t, "feed-sample.json"))
	f.apiBase.Store("")
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastUA.Store(r.Header.Get("User-Agent"))
		switch r.URL.Path {
		case feedPath:
			f.feedHits.Add(1)
			if f.failFeed.Load() {
				http.Error(w, "down", http.StatusInternalServerError)
				return
			}
			w.Write(f.feed.Load().([]byte))
		case wellKnownPath:
			base := f.apiBase.Load().(string)
			if base == "" {
				base = "https://" + r.Host
			}
			w.Write([]byte(`{"apiBase":"` + base + `","registry":"https://clawhub.ai"}`))
		case downloadPath:
			f.lastQuery.Store(r.URL.RawQuery)
			switch r.URL.Query().Get("slug") {
			case "gone":
				http.Error(w, "gone", http.StatusGone)
			case "handoff":
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"sourceRef":"public-github"}`))
			default:
				w.Header().Set("Content-Type", "application/zip")
				w.Write(f.archive)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeClawHub) client(t *testing.T, now *time.Time) *Client {
	t.Helper()
	c := New(t.TempDir(), t.TempDir())
	c.HTTP = f.srv.Client()
	c.Registry = f.srv.URL
	c.Now = func() time.Time { return *now }
	return c
}

func withExpiry(t *testing.T, expires string) []byte {
	t.Helper()
	data := string(readTestdata(t, "feed-sample.json"))
	return []byte(strings.Replace(data, `"expiresAt": "2026-11-22T00:42:18.813Z"`, `"expiresAt": "`+expires+`"`, 1))
}

func TestFeed_CachedOnDiskUntilItExpires(t *testing.T) {
	f := newFakeClawHub(t)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	c := f.client(t, &now)
	ctx := context.Background()

	snap, err := c.Feed(ctx, false)
	if err != nil || len(snap.Feed.Entries) != 5 || snap.Stale {
		t.Fatalf("first fetch: %+v, %v", snap, err)
	}
	if snap.APIBase != f.srv.URL {
		t.Errorf("APIBase = %q, want the well-known value %q", snap.APIBase, f.srv.URL)
	}
	if _, err := os.Stat(filepath.Join(c.CacheDir, cacheFile)); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	// A new client reads the same cache: nothing is fetched until the feed's expiresAt.
	now = time.Date(2026, 11, 21, 23, 0, 0, 0, time.UTC)
	c2 := New(c.CacheDir, c.StoreDir)
	c2.HTTP, c2.Registry, c2.Now = c.HTTP, c.Registry, c.Now
	if snap, err = c2.Feed(ctx, false); err != nil || f.feedHits.Load() != 1 || snap.APIBase != f.srv.URL {
		t.Fatalf("cached read: hits=%d apiBase=%q err=%v", f.feedHits.Load(), snap.APIBase, err)
	}

	now = time.Date(2026, 11, 22, 0, 42, 19, 0, time.UTC)
	if _, err := c2.Feed(ctx, false); err != nil || f.feedHits.Load() != 2 {
		t.Fatalf("after expiry: hits=%d err=%v", f.feedHits.Load(), err)
	}

	if _, err := c2.Feed(ctx, true); err != nil || f.feedHits.Load() != 3 {
		t.Fatalf("refresh: hits=%d err=%v", f.feedHits.Load(), err)
	}
}

func TestFeed_ExpiryOutsideReasonUsesBoundedLifetime(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		expires string
		want    time.Time
	}{
		"already past": {"2026-11-01T00:00:00Z", now.Add(fallbackTTL)},
		"missing":      {"", now.Add(fallbackTTL)},
		"far future":   {"2030-01-01T00:00:00Z", now.Add(maxTTL)},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeClawHub(t)
			f.feed.Store(withExpiry(t, tc.expires))
			snap, err := f.client(t, &now).Feed(context.Background(), false)
			if err != nil {
				t.Fatal(err)
			}
			if got := snap.ValidUntil(); !got.Equal(tc.want) {
				t.Errorf("ValidUntil = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFeed_FailedFetchFallsBackToStaleCopy(t *testing.T) {
	f := newFakeClawHub(t)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	c := f.client(t, &now)
	if _, err := c.Feed(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	f.failFeed.Store(true)
	now = now.Add(30 * 24 * time.Hour)
	snap, err := c.Feed(context.Background(), false)
	if err != nil || !snap.Stale || snap.FetchErr == nil || len(snap.Feed.Entries) != 5 {
		t.Fatalf("stale fallback: %+v, %v", snap, err)
	}

	empty := f.client(t, &now)
	if _, err := empty.Feed(context.Background(), false); err == nil {
		t.Fatal("a failed fetch with no cache must be an error")
	}
}

func TestFeed_RejectsBadFeedAndKeepsGenericUserAgent(t *testing.T) {
	f := newFakeClawHub(t)
	f.feed.Store([]byte(`{"schemaVersion":7,"entries":[]}`))
	now := time.Now()
	if _, err := f.client(t, &now).Feed(context.Background(), false); err == nil {
		t.Fatal("unsupported schema accepted")
	}
	if ua := f.lastUA.Load(); ua != "skillshare" {
		t.Errorf("User-Agent = %q, want the generic \"skillshare\"", ua)
	}
}

func TestCleanBase(t *testing.T) {
	for in, want := range map[string]string{
		"https://clawhub.ai":           "https://clawhub.ai",
		"https://clawhub.ai/":          "https://clawhub.ai",
		"https://api.example.test/reg": "https://api.example.test/reg",
		"http://clawhub.ai":            "",
		"https://user@clawhub.ai":      "",
		"https://clawhub.ai/?x=1":      "",
		"javascript:alert(1)":          "",
		"":                             "",
	} {
		if got := cleanBase(in); got != want {
			t.Errorf("cleanBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDownload_PassesOwnerAndPinnedVersion(t *testing.T) {
	f := newFakeClawHub(t)
	now := time.Now()
	c := f.client(t, &now)
	data, err := c.Download(context.Background(), f.srv.URL, "steipete", "blogwatcher", "1.0.0")
	if err != nil || len(data) != len(f.archive) {
		t.Fatalf("Download: %d bytes, %v", len(data), err)
	}
	if q := f.lastQuery.Load(); q != "ownerHandle=steipete&slug=blogwatcher&version=1.0.0" {
		t.Errorf("query = %q", q)
	}

	var de *DownloadError
	if _, err := c.Download(context.Background(), f.srv.URL, "a", "gone", "1"); !errors.As(err, &de) || de.Status != http.StatusGone {
		t.Errorf("gone: %v", err)
	}
	if _, err := c.Download(context.Background(), f.srv.URL, "a", "handoff", "1"); err == nil || !strings.Contains(err.Error(), "zip") {
		t.Errorf("JSON handoff: %v", err)
	}
}

func TestFetch_UnpacksVerifiedReleaseIntoCache(t *testing.T) {
	f := newFakeClawHub(t)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	c := f.client(t, &now)
	snap, err := c.Feed(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := snap.Feed.Lookup("@steipete/blogwatcher")
	rel, err := c.Fetch(context.Background(), snap, entry)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	want := filepath.Join(c.StoreDir, "releases", "@steipete", "blogwatcher@1.0.0")
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("release folder exists before Place (err=%v)", err)
	}
	if err := rel.Place(); err != nil {
		t.Fatal(err)
	}
	if rel.Dir != want || !rel.Fresh || rel.Version != "1.0.0" || rel.Origin != "clawhub://@steipete/blogwatcher@1.0.0" {
		t.Errorf("release = %+v, want Dir %s", rel, want)
	}
	if _, err := os.Stat(filepath.Join(rel.Dir, "SKILL.md")); err != nil {
		t.Error(err)
	}
	// The same release again replaces the copy, and says it was there before.
	again, err := c.Fetch(context.Background(), snap, entry)
	if err != nil || again.Place() != nil || again.Dir != want || again.Fresh {
		t.Errorf("second fetch = %+v, %v", again, err)
	}
	// One that is not placed leaves nothing behind (checked with the listing below).
	dropped, err := c.Fetch(context.Background(), snap, entry)
	if err != nil {
		t.Fatal(err)
	}
	dropped.Discard()

	// The feed lists another hash for the same download: refused, and nothing is left behind.
	other, _ := snap.Feed.Lookup("@steipete/gog")
	other.ID = entry.ID
	other.Install.Candidates[0].Package = entry.ID
	other.Install.Candidates[0].Version = "9.9.9"
	if _, err := c.Fetch(context.Background(), snap, other); !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("mismatch: %v", err)
	}
	left, _ := os.ReadDir(filepath.Dir(want))
	if len(left) != 1 || left[0].Name() != "blogwatcher@1.0.0" {
		t.Errorf("release folder holds %v, want only blogwatcher@1.0.0", left)
	}

	gh, _ := snap.Feed.Lookup("@aws/agents-build")
	var ni *NotInstallableError
	if _, err := c.Fetch(context.Background(), snap, gh); !errors.As(err, &ni) || ni.Reason != BlockedGitHub {
		t.Errorf("GitHub-backed: %v", err)
	}
}
