package server

// Fork-only (endgame302/skillshare): the ClawHub marketplace routes.

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"skillshare/internal/forkmarket/clawhub"
	"skillshare/internal/install"
)

const (
	clawhubTestdata   = "../forkmarket/clawhub/testdata"
	fakeGitHubRepo    = "aws/agent-toolkit-for-aws"
	fakeGitHubPath    = "plugins/aws-agents/skills/agents-build"
	fakeGitHubCommit  = "b4416dddac9b6a0cc5412136e6dbf8f07ffdb31c"
	fakeGitHubTreeSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	oldFeedGitHubHash = "d79709bf46ca8cd0fffe392a914cd56539b4b7c0ff0fe34c9583f24057300195"
)

var fakeGitHubFiles = map[string]string{
	"SKILL.md":            "---\nname: agents-build\n---\n# Agents Build\n",
	"references/guide.md": "guide\n",
}

func fakeGitBlobSHA(content string) string {
	h := sha1.New()
	h.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00" + content))
	return hex.EncodeToString(h.Sum(nil))
}

func fakeGitHubFeedHash() string {
	return clawhub.FolderHash([]clawhub.FolderFile{
		{Path: "SKILL.md", Data: []byte(fakeGitHubFiles["SKILL.md"])},
		{Path: "references/guide.md", Data: []byte(fakeGitHubFiles["references/guide.md"])},
	})
}

// fakeClawHubSite serves the sample feed, the real blogwatcher archive, and a GitHub folder
// over local TLS.
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
	ghTreePath := "/repos/" + fakeGitHubRepo + "/git/trees/" + fakeGitHubCommit + ":" + fakeGitHubPath
	ghRawPrefix := "/" + fakeGitHubRepo + "/" + fakeGitHubCommit + "/" + fakeGitHubPath + "/"
	treeEntries := []map[string]any{
		{"path": "SKILL.md", "mode": "100644", "type": "blob", "sha": fakeGitBlobSHA(fakeGitHubFiles["SKILL.md"]), "size": len(fakeGitHubFiles["SKILL.md"])},
		{"path": "references", "mode": "040000", "type": "tree", "sha": strings.Repeat("c", 40)},
		{"path": "references/guide.md", "mode": "100644", "type": "blob", "sha": fakeGitBlobSHA(fakeGitHubFiles["references/guide.md"]), "size": len(fakeGitHubFiles["references/guide.md"])},
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/feeds/skills":
			site.feedHits.Add(1)
			if site.feedFails.Load() {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			w.Write(site.feed.Load().([]byte))
		case r.URL.Path == "/.well-known/clawhub.json":
			w.Write([]byte(`{"apiBase":"https://` + r.Host + `"}`))
		case r.URL.Path == "/api/v1/download":
			site.downloads.Add(1)
			site.query.Store(r.URL.RawQuery)
			w.Header().Set("Content-Type", "application/zip")
			w.Write(archive)
		case r.URL.Path == ghTreePath:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"sha":       fakeGitHubTreeSHA,
				"truncated": false,
				"tree":      treeEntries,
			})
		case strings.HasPrefix(r.URL.Path, ghRawPrefix):
			rel := strings.TrimPrefix(r.URL.Path, ghRawPrefix)
			body, ok := fakeGitHubFiles[rel]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	prev := newClawHubClient
	newClawHubClient = func() *clawhub.Client {
		c := clawhub.New(site.cacheDir, site.storeDir)
		c.HTTP, c.Registry = srv.Client(), srv.URL
		c.GitHubAPI, c.GitHubRaw = srv.URL, srv.URL
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

func TestClawHubInstall_GitHubEntryIsVerifiedThenInstalledPinned(t *testing.T) {
	site := useFakeClawHub(t)
	feed := site.feed.Load().([]byte)
	site.feed.Store(bytes.ReplaceAll(feed, []byte(oldFeedGitHubHash), []byte(fakeGitHubFeedHash())))
	s, sourceDir := newTestServer(t)

	prev := installClawHubRelease
	t.Cleanup(func() { installClawHubRelease = prev })
	installClawHubRelease = func(source *install.Source, dest string, opts install.InstallOptions) (*install.InstallResult, error) {
		if source.Type != install.SourceTypeGitHub {
			t.Errorf("source.Type = %v, want GitHub", source.Type)
		}
		if source.CloneURL != "https://github.com/"+fakeGitHubRepo+".git" {
			t.Errorf("source.CloneURL = %q", source.CloneURL)
		}
		if source.Subdir != fakeGitHubPath {
			t.Errorf("source.Subdir = %q", source.Subdir)
		}
		if source.Branch != fakeGitHubCommit {
			t.Errorf("source.Branch = %q", source.Branch)
		}
		if source.Name != "agents-build" {
			t.Errorf("source.Name = %q", source.Name)
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(fakeGitHubFiles["SKILL.md"]), 0o644); err != nil {
			return nil, err
		}
		st := install.LoadMetadataOrNew(opts.SourceDir)
		st.Set("agents-build", &install.MetadataEntry{
			Source:   source.Raw,
			Type:     "github",
			Branch:   source.Branch,
			TreeHash: fakeGitHubTreeSHA,
			Subdir:   source.Subdir,
		})
		if err := st.Save(opts.SourceDir); err != nil {
			return nil, err
		}
		return &install.InstallResult{SkillName: "agents-build", Action: "installed"}, nil
	}

	rr := clawhubInstall(t, s, map[string]any{"id": "@aws/agents-build"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		SkillName string `json:"skillName"`
		Action    string `json:"action"`
		Origin    string `json:"origin"`
		Version   string `json:"version"`
		GitHub    string `json:"github"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Version != fakeGitHubCommit || resp.Origin != "clawhub://@aws/agents-build@"+fakeGitHubCommit {
		t.Errorf("resp = %+v", resp)
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "agents-build", "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md missing: %v", err)
	}
}

func TestClawHubInstall_GitHubHashMismatchInstallsNothing(t *testing.T) {
	site := useFakeClawHub(t)
	feed := site.feed.Load().([]byte)
	site.feed.Store(bytes.ReplaceAll(feed, []byte(oldFeedGitHubHash), []byte(strings.Repeat("0", 64))))
	s, _ := newTestServer(t)

	prev := installClawHubRelease
	t.Cleanup(func() { installClawHubRelease = prev })
	called := false
	installClawHubRelease = func(*install.Source, string, install.InstallOptions) (*install.InstallResult, error) {
		called = true
		return nil, errors.New("should not be called")
	}

	rr := clawhubInstall(t, s, map[string]any{"id": "@aws/agents-build"})
	if rr.Code != http.StatusBadGateway || clawhubErrorCode(t, rr) != "clawhub_integrity_mismatch" {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if called {
		t.Fatal("installClawHubRelease was called despite content hash mismatch")
	}
}

func TestClawHubInstall_GitHubInstalledTreeMustMatch(t *testing.T) {
	site := useFakeClawHub(t)
	feed := site.feed.Load().([]byte)
	site.feed.Store(bytes.ReplaceAll(feed, []byte(oldFeedGitHubHash), []byte(fakeGitHubFeedHash())))
	s, sourceDir := newTestServer(t)

	prev := installClawHubRelease
	t.Cleanup(func() { installClawHubRelease = prev })
	installClawHubRelease = func(source *install.Source, dest string, opts install.InstallOptions) (*install.InstallResult, error) {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(fakeGitHubFiles["SKILL.md"]), 0o644); err != nil {
			return nil, err
		}
		st := install.LoadMetadataOrNew(opts.SourceDir)
		st.Set("agents-build", &install.MetadataEntry{
			Source:   source.Raw,
			Type:     "github",
			Branch:   source.Branch,
			TreeHash: strings.Repeat("b", 40),
			Subdir:   source.Subdir,
		})
		if err := st.Save(opts.SourceDir); err != nil {
			return nil, err
		}
		return &install.InstallResult{SkillName: "agents-build", Action: "installed"}, nil
	}

	rr := clawhubInstall(t, s, map[string]any{"id": "@aws/agents-build"})
	if rr.Code != http.StatusBadGateway || clawhubErrorCode(t, rr) != "clawhub_integrity_mismatch" {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	dest := filepath.Join(sourceDir, "agents-build")
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("mismatched installed folder was not removed (err=%v)", err)
	}
	if install.LoadMetadataOrNew(sourceDir).GetByPath("agents-build") != nil {
		t.Fatal("metadata entry was left behind after tree mismatch")
	}
}
