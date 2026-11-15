package server

// Fork-only (endgame302/skillshare): the ClawHub marketplace routes.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"skillshare/internal/forkmarket/clawhub"
	"skillshare/internal/install"
)

const clawhubTestdata = "../forkmarket/clawhub/testdata"

// fakeClawHubSite serves the sample feed and the real blogwatcher archive over local TLS.
type fakeClawHubSite struct {
	feed      atomic.Value // []byte
	feedFails atomic.Bool
	feedHits  atomic.Int32
	downloads atomic.Int32
	query     atomic.Value // string
	cacheDir  string
	storeDir  string
}

func useFakeClawHub(t *testing.T) *fakeClawHubSite {
	t.Helper()
	feed, err := os.ReadFile(filepath.Join(clawhubTestdata, "feed-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(filepath.Join(clawhubTestdata, "blogwatcher-1.0.0.zip"))
	if err != nil {
		t.Fatal(err)
	}
	site := &fakeClawHubSite{cacheDir: t.TempDir(), storeDir: t.TempDir()}
	site.feed.Store(feed)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/feeds/skills":
			site.feedHits.Add(1)
			if site.feedFails.Load() {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			w.Write(site.feed.Load().([]byte))
		case "/.well-known/clawhub.json":
			w.Write([]byte(`{"apiBase":"https://` + r.Host + `"}`))
		case "/api/v1/download":
			site.downloads.Add(1)
			site.query.Store(r.URL.RawQuery)
			w.Header().Set("Content-Type", "application/zip")
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	prev := newClawHubClient
	newClawHubClient = func() *clawhub.Client {
		c := clawhub.New(site.cacheDir, site.storeDir)
		c.HTTP, c.Registry = srv.Client(), srv.URL
		c.Now = func() time.Time { return time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC) }
		return c
	}
	t.Cleanup(func() { newClawHubClient = prev })
	return site
}

func clawhubInstall(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	payload, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/fork/clawhub/install", bytes.NewReader(payload)))
	return rr
}

func clawhubErrorCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"error_code"`
	}
	json.Unmarshal(rr.Body.Bytes(), &body)
	return body.Code
}

func TestClawHubFeed_ListsTheFeed(t *testing.T) {
	useFakeClawHub(t)
	s, _ := newTestServer(t)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/fork/clawhub/feed", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Items []clawhub.Item `json:"items"`
		Stale bool           `json:"stale"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 5 || resp.Stale {
		t.Fatalf("items=%d stale=%v", len(resp.Items), resp.Stale)
	}
}

func TestClawHubFeed_UnavailableWithoutCache(t *testing.T) {
	site := useFakeClawHub(t)
	site.feedFails.Store(true)
	s, _ := newTestServer(t)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/fork/clawhub/feed", nil))
	if rr.Code != http.StatusBadGateway || clawhubErrorCode(t, rr) != "clawhub_feed_unavailable" {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
}

func TestClawHubFeed_RefusesCrossSiteRequest(t *testing.T) {
	site := useFakeClawHub(t)
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/fork/clawhub/feed", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden || site.feedHits.Load() != 0 {
		t.Fatalf("status %d, feed fetched %d times", rr.Code, site.feedHits.Load())
	}
}

func TestClawHubInstall_InstallsFromTheCachedFeedEntry(t *testing.T) {
	site := useFakeClawHub(t)
	s, sourceDir := newTestServer(t)

	// Cache the feed, then let ClawHub's copy change: the install must use the cached entry,
	// and ignore any hash or address the request carries.
	feed := site.feed.Load().([]byte)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/fork/clawhub/feed", nil))
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	site.feed.Store(bytes.ReplaceAll(feed, []byte("d93023e42fc21a78891b1bddfc6a6a7b3c05dc5a96b8cd3ea3c50ce8528d43d3"), []byte(strings.Repeat("0", 64))))

	rr = clawhubInstall(t, s, map[string]any{
		"id": "@steipete/blogwatcher", "version": "1.0.0",
		"integrity": "sha256:" + strings.Repeat("f", 64), "url": "https://evil.example/x.zip",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if site.feedHits.Load() != 1 {
		t.Errorf("feed fetched %d times, want once (then cached)", site.feedHits.Load())
	}
	if q, _ := site.query.Load().(string); q != "ownerHandle=steipete&slug=blogwatcher&version=1.0.0" {
		t.Errorf("download query = %q", q)
	}

	dest := filepath.Join(sourceDir, "blogwatcher")
	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "skill-card.md")); !os.IsNotExist(err) {
		t.Errorf("skill-card.md installed although the checksum does not cover it")
	}
	// The skill names the verified release folder as its source: a local install whose path ends
	// in the ClawHub origin, which update and a reinstall from the records can copy again.
	release := filepath.Join(site.storeDir, "releases", "@steipete", "blogwatcher@1.0.0")
	entry := install.LoadMetadataOrNew(sourceDir).GetByPath("blogwatcher")
	if entry == nil || entry.Source != release || entry.Version != "1.0.0" || entry.Type != "local" || entry.RepoURL != "" {
		t.Fatalf("metadata = %+v, want source %s", entry, release)
	}
	if src, err := install.RecordedSource(entry, install.ParseOptions{}); err != nil || src.Type != install.SourceTypeLocalPath || src.Path != release {
		t.Fatalf("RecordedSource = %+v, %v", src, err)
	}
	if _, err := os.Stat(filepath.Join(release, "SKILL.md")); err != nil {
		t.Fatalf("release copy missing: %v", err)
	}
	left, _ := os.ReadDir(filepath.Dir(release))
	if len(left) != 1 {
		t.Errorf("staging folder left next to the release: %d entries", len(left))
	}
}

func TestClawHubInstall_RefusesWhatTheFeedHashDoesNotMatch(t *testing.T) {
	site := useFakeClawHub(t)
	feed := site.feed.Load().([]byte)
	site.feed.Store(bytes.ReplaceAll(feed, []byte("d93023e42fc21a78891b1bddfc6a6a7b3c05dc5a96b8cd3ea3c50ce8528d43d3"), []byte(strings.Repeat("0", 64))))
	s, sourceDir := newTestServer(t)

	rr := clawhubInstall(t, s, map[string]any{"id": "@steipete/blogwatcher"})
	if rr.Code != http.StatusBadGateway || clawhubErrorCode(t, rr) != "clawhub_integrity_mismatch" {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "blogwatcher")); !os.IsNotExist(err) {
		t.Fatal("a skill was installed from an archive that failed its checksum")
	}
	if _, err := os.Stat(filepath.Join(site.storeDir, "releases")); !os.IsNotExist(err) {
		t.Errorf("a release folder was written for an archive that failed its checksum (err=%v)", err)
	}
}

func TestClawHubInstall_ExistingSkillNeedsForce(t *testing.T) {
	site := useFakeClawHub(t)
	s, sourceDir := newTestServer(t)
	dest := filepath.Join(sourceDir, "blogwatcher")
	os.MkdirAll(dest, 0o755)
	os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("---\nname: blogwatcher\n---\nmine\n"), 0o644)

	rr := clawhubInstall(t, s, map[string]any{"id": "@steipete/blogwatcher"})
	if rr.Code != http.StatusConflict || clawhubErrorCode(t, rr) != "clawhub_exists" || site.downloads.Load() != 0 {
		t.Fatalf("status %d, downloads %d: %s", rr.Code, site.downloads.Load(), rr.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "SKILL.md")); !strings.Contains(string(got), "mine") {
		t.Fatal("existing skill changed without force")
	}

	rr = clawhubInstall(t, s, map[string]any{"id": "@steipete/blogwatcher", "force": true})
	if rr.Code != http.StatusOK {
		t.Fatalf("force: status %d: %s", rr.Code, rr.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "SKILL.md")); strings.Contains(string(got), "mine") {
		t.Fatal("force did not replace the skill")
	}
}

func TestClawHubInstall_RefusedRequests(t *testing.T) {
	site := useFakeClawHub(t)
	s, _ := newTestServer(t)
	for name, tc := range map[string]struct {
		body   map[string]any
		status int
		code   string
	}{
		"bad id":          {map[string]any{"id": "../etc"}, http.StatusBadRequest, ""},
		"not in feed":     {map[string]any{"id": "@nobody/nothing"}, http.StatusNotFound, "clawhub_not_found"},
		"github-backed":   {map[string]any{"id": "@aws/agents-build"}, http.StatusUnprocessableEntity, "clawhub_not_installable"},
		"version changed": {map[string]any{"id": "@steipete/blogwatcher", "version": "0.9.0"}, http.StatusConflict, "clawhub_version_changed"},
	} {
		t.Run(name, func(t *testing.T) {
			rr := clawhubInstall(t, s, tc.body)
			if rr.Code != tc.status || (tc.code != "" && clawhubErrorCode(t, rr) != tc.code) {
				t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
			}
		})
	}
	if site.downloads.Load() != 0 {
		t.Errorf("refused requests downloaded %d times", site.downloads.Load())
	}
}

func TestClawHubInstall_FailedInstallRemovesOnlyANewReleaseCopy(t *testing.T) {
	site := useFakeClawHub(t)
	s, _ := newTestServer(t)
	release := filepath.Join(site.storeDir, "releases", "@steipete", "blogwatcher@1.0.0")
	prev := installClawHubRelease
	t.Cleanup(func() { installClawHubRelease = prev })
	installClawHubRelease = func(*install.Source, string, install.InstallOptions) (*install.InstallResult, error) {
		return nil, errors.New("audit blocked the skill")
	}

	// Nothing names the copy this request made, so it goes.
	rr := clawhubInstall(t, s, map[string]any{"id": "@steipete/blogwatcher"})
	if rr.Code != http.StatusInternalServerError || site.downloads.Load() != 1 {
		t.Fatalf("status %d, downloads %d: %s", rr.Code, site.downloads.Load(), rr.Body.String())
	}
	if _, err := os.Stat(release); !os.IsNotExist(err) {
		t.Fatalf("new release copy kept although nothing names it (err=%v)", err)
	}

	// A copy that was there before may be another skill's source, so it stays.
	if err := os.MkdirAll(release, 0o755); err != nil {
		t.Fatal(err)
	}
	rr = clawhubInstall(t, s, map[string]any{"id": "@steipete/blogwatcher"})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(release, "SKILL.md")); err != nil {
		t.Fatalf("earlier release copy removed: %v", err)
	}
}
