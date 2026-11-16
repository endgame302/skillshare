package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"skillshare/internal/forkmarket/ghrepo"
	"skillshare/internal/forkmarket/plugincat"
)

func ensureForkPluginsRoutes(t *testing.T, s *Server) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/fork/plugins/catalog?id=nope", nil)
	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		s.registerForkPluginCatalogRoutes()
	}
}

func forkPluginsErrorCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"error_code"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rr.Body.String(), err)
	}
	return body.Code
}

type fakePluginRunner struct {
	lookPath    string
	lookPathErr error
	run         func(args []string) ([]byte, error)
	calls       int
}

func (f *fakePluginRunner) LookPath() (string, error) {
	if f.lookPathErr != nil {
		return "", f.lookPathErr
	}
	return f.lookPath, nil
}

func (f *fakePluginRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls++
	return f.run(args)
}

func seedTestPluginCatalog(t *testing.T, cacheDir, commit, sha string) *plugincat.Client {
	t.Helper()
	catJSON, err := json.Marshal(map[string]any{
		"name": "claude-plugins-official",
		"plugins": []any{
			map[string]any{
				"name":   "local-plug",
				"source": "./plugins/local-plug",
			},
			map[string]any{
				"name": "ext-plug",
				"source": map[string]string{
					"source": "github",
					"repo":   "owner/ext-plug",
					"sha":    sha,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(commit))
	}))
	defer apiSrv.Close()

	rawSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(catJSON)
	}))
	defer rawSrv.Close()

	client := plugincat.New(cacheDir)
	client.GH = &ghrepo.Client{
		HTTP:  apiSrv.Client(),
		API:   apiSrv.URL,
		Raw:   rawSrv.URL,
		Token: func() string { return "" },
	}
	if _, err := client.Catalog(context.Background(), "official", true); err != nil {
		t.Fatalf("seed Catalog: %v", err)
	}
	return client
}

func serveForkPluginHandoff(t *testing.T, s *Server, remoteAddr string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	// The dashboard is opened by localhost; upstream's plugin guard checks the Host too.
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:19420/api/fork/plugins/handoff", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	return rr
}

func TestForkPluginsCatalogAndHandoff(t *testing.T) {
	const (
		commit = "1111111111111111111111111111111111111111"
		sha    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	cacheDir := t.TempDir()
	seeded := seedTestPluginCatalog(t, cacheDir, commit, sha)

	oldClientFactory := newPluginCatalogClient
	oldRunner := forkClaudeRunner
	t.Cleanup(func() {
		newPluginCatalogClient = oldClientFactory
		forkClaudeRunner = oldRunner
	})
	newPluginCatalogClient = func() *plugincat.Client {
		c := plugincat.New(cacheDir)
		c.GH = seeded.GH
		return c
	}

	marketDir := t.TempDir()
	mFile := filepath.Join(marketDir, plugincat.CatalogFile)
	if err := os.MkdirAll(filepath.Dir(mFile), 0o755); err != nil {
		t.Fatal(err)
	}
	mJSON, _ := json.Marshal(map[string]any{
		"name": "claude-plugins-official",
		"plugins": []any{
			map[string]any{
				"name": "ext-plug",
				"source": map[string]string{
					"source": "github",
					"repo":   "owner/ext-plug",
					"sha":    sha,
				},
			},
		},
	})
	if err := os.WriteFile(mFile, mJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	listJSON, _ := json.Marshal([]map[string]string{{
		"name":            "claude-plugins-official",
		"source":          "github",
		"repo":            "anthropics/claude-plugins-official",
		"installLocation": marketDir,
	}})

	runner := &fakePluginRunner{
		lookPath: "/usr/local/bin/claude",
		run: func(args []string) ([]byte, error) {
			if args[0] == "--version" {
				return []byte("2.1.323 (Claude Code)"), nil
			}
			if args[1] == "marketplace" && args[2] == "list" {
				return listJSON, nil
			}
			if args[1] == "marketplace" && args[2] == "update" {
				return []byte(`{"outcome":"ok","message":"Updated"}`), nil
			}
			if args[1] == "install" {
				return []byte(`{"command":"install","outcome":"ok","message":"Installed"}`), nil
			}
			return nil, errors.New("unexpected")
		},
	}
	forkClaudeRunner = runner

	s, _ := newTestServer(t)
	ensureForkPluginsRoutes(t, s)

	// 1. Catalog GET ok.
	req := httptest.NewRequest(http.MethodGet, "/api/fork/plugins/catalog?id=official", nil)
	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("catalog GET status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var gotCat plugincat.Catalog
	if err := json.Unmarshal(rr.Body.Bytes(), &gotCat); err != nil || gotCat.Commit != commit || len(gotCat.Entries) != 2 {
		t.Fatalf("catalog = %+v, err = %v", gotCat, err)
	}

	// 2. Catalog GET unknown id -> 400.
	req = httptest.NewRequest(http.MethodGet, "/api/fork/plugins/catalog?id=nope", nil)
	rr = httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || forkPluginsErrorCode(t, rr) != "plugins_market.unknown_catalog" {
		t.Fatalf("unknown id = %d %s", rr.Code, rr.Body.String())
	}

	// 3. Handoff dry run -> 200 with plan steps.
	rr = serveForkPluginHandoff(t, s, "127.0.0.1:1234", map[string]any{
		"catalog": "official",
		"name":    "ext-plug",
		"commit":  commit,
		"sha":     sha,
		"dryRun":  true,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("dryRun status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var dryResp struct {
		Plan plugincat.Plan `json:"plan"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &dryResp); err != nil || len(dryResp.Plan.Steps) != 2 {
		t.Fatalf("dryResp = %+v, err = %v", dryResp, err)
	}

	// 4. Path entry -> 422 plugins_market.not_native.
	rr = serveForkPluginHandoff(t, s, "127.0.0.1:1234", map[string]any{
		"catalog": "official",
		"name":    "local-plug",
		"commit":  commit,
		"dryRun":  true,
	})
	if rr.Code != http.StatusUnprocessableEntity || forkPluginsErrorCode(t, rr) != "plugins_market.not_native" {
		t.Fatalf("not_native = %d %s", rr.Code, rr.Body.String())
	}

	// 5. Wrong commit or sha -> 409 plugins_market.catalog_changed.
	rr = serveForkPluginHandoff(t, s, "127.0.0.1:1234", map[string]any{
		"catalog": "official",
		"name":    "ext-plug",
		"commit":  "0000000000000000000000000000000000000000",
		"sha":     sha,
	})
	if rr.Code != http.StatusConflict || forkPluginsErrorCode(t, rr) != "plugins_market.catalog_changed" {
		t.Fatalf("wrong commit = %d %s", rr.Code, rr.Body.String())
	}
	rr = serveForkPluginHandoff(t, s, "127.0.0.1:1234", map[string]any{
		"catalog": "official",
		"name":    "ext-plug",
		"commit":  commit,
		"sha":     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	if rr.Code != http.StatusConflict || forkPluginsErrorCode(t, rr) != "plugins_market.catalog_changed" {
		t.Fatalf("wrong sha = %d %s", rr.Code, rr.Body.String())
	}

	// 6. Real run -> 200 with result.
	rr = serveForkPluginHandoff(t, s, "127.0.0.1:1234", map[string]any{
		"catalog": "official",
		"name":    "ext-plug",
		"commit":  commit,
		"sha":     sha,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("real run status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var runResp struct {
		Plan   plugincat.Plan   `json:"plan"`
		Result plugincat.Result `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &runResp); err != nil || runResp.Result.Message != "Installed" {
		t.Fatalf("runResp = %+v, err = %v", runResp, err)
	}

	// 7. Claude missing real run -> 412.
	forkClaudeRunner = &fakePluginRunner{lookPathErr: errors.New("missing")}
	rr = serveForkPluginHandoff(t, s, "127.0.0.1:1234", map[string]any{
		"catalog": "official",
		"name":    "ext-plug",
		"commit":  commit,
		"sha":     sha,
	})
	if rr.Code != http.StatusPreconditionFailed || forkPluginsErrorCode(t, rr) != "plugins_market.claude_missing" {
		t.Fatalf("claude missing = %d %s", rr.Code, rr.Body.String())
	}

	// 8. Non-loopback RemoteAddr refused (403) without running claude.
	guardRunner := &fakePluginRunner{lookPath: "/usr/local/bin/claude"}
	forkClaudeRunner = guardRunner
	rr = serveForkPluginHandoff(t, s, "203.0.113.5:1234", map[string]any{
		"catalog": "official",
		"name":    "ext-plug",
		"commit":  commit,
		"sha":     sha,
	})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-loopback status = %d, want 403, body = %s", rr.Code, rr.Body.String())
	}
	if guardRunner.calls != 0 {
		t.Fatalf("claude runner was called %d times on refused request", guardRunner.calls)
	}
}
