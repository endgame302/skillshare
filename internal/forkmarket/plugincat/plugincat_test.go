package plugincat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"skillshare/internal/forkmarket/ghrepo"
)

func TestTranslate(t *testing.T) {
	const sha40 = "09bf1539d41f9ff355ba3eb5d05d4a75813423bb"
	cases := []struct {
		name    string
		raw     string
		src     Source
		install string
		pinned  bool
		problem string
	}{
		{
			name:    "string path",
			raw:     `"./plugins/agent-sdk-dev"`,
			src:     Source{Kind: "path", Path: "plugins/agent-sdk-dev"},
			install: "add",
			pinned:  true,
		},
		{
			name:    "url external with sha",
			raw:     `{"source":"url","url":"https://github.com/SalesforceAIResearch/agentforce-adlc.git","sha":"` + sha40 + `"}`,
			src:     Source{Kind: "url", URL: "https://github.com/SalesforceAIResearch/agentforce-adlc.git", SHA: sha40},
			install: "native",
			pinned:  true,
		},
		{
			name:    "git-subdir",
			raw:     `{"source":"git-subdir","url":"https://github.com/42Crunch-AI/x.git","path":"claude","ref":"main","sha":"` + sha40 + `"}`,
			src:     Source{Kind: "git-subdir", URL: "https://github.com/42Crunch-AI/x.git", Path: "claude", Ref: "main", SHA: sha40},
			install: "native",
			pinned:  true,
		},
		{
			name:    "github",
			raw:     `{"source":"github","repo":"owner/name","ref":"v1","sha":"` + sha40 + `"}`,
			src:     Source{Kind: "github", Repo: "owner/name", Ref: "v1", SHA: sha40},
			install: "native",
			pinned:  true,
		},
		{
			name:    "npm",
			raw:     `{"source":"npm","package":"@scope/name","version":"1.2.3"}`,
			src:     Source{Kind: "npm", Package: "@scope/name", Version: "1.2.3"},
			install: "native",
			pinned:  false,
		},
		{
			name:    "command",
			raw:     `{"source":"command","command":"xcrun agent plugin path --plugin-format claude","mode":"link"}`,
			src:     Source{Kind: "command", Command: "xcrun agent plugin path --plugin-format claude"},
			install: "manual",
			pinned:  false,
		},
		{
			name:    "local object",
			raw:     `{"source":"local","path":"./plugins/local-one"}`,
			src:     Source{Kind: "path", Path: "plugins/local-one"},
			install: "add",
			pinned:  true,
		},
		{
			name:    "dot-url",
			raw:     `{"source":"url","url":"./plugins/dot"}`,
			src:     Source{Kind: "path", Path: "plugins/dot"},
			install: "add",
			pinned:  true,
		},
		{
			name:    "dot slash",
			raw:     `"./"`,
			src:     Source{Kind: "path", Path: "."},
			install: "add",
			pinned:  true,
		},
		{
			name:    "dot",
			raw:     `"."`,
			src:     Source{Kind: "path", Path: "."},
			install: "add",
			pinned:  true,
		},
		{
			name:    "parent escape",
			raw:     `"../x"`,
			src:     Source{Kind: "path"},
			install: "none",
			pinned:  false,
			problem: "path escapes the catalog",
		},
		{
			name:    "abs path",
			raw:     `"/abs"`,
			src:     Source{Kind: "path"},
			install: "none",
			pinned:  false,
			problem: "path escapes the catalog",
		},
		{
			name:    "backslashes",
			raw:     `"a\\b"`,
			src:     Source{Kind: "path"},
			install: "none",
			pinned:  false,
			problem: "path uses backslashes",
		},
		{
			name:    "non-40-hex sha dropped",
			raw:     `{"source":"github","repo":"owner/name","sha":"09BF1539"}`,
			src:     Source{Kind: "github", Repo: "owner/name"},
			install: "native",
			pinned:  false,
		},
		{
			name:    "non-string source unknown",
			raw:     `{"source":42}`,
			src:     Source{Kind: "unknown"},
			install: "none",
			pinned:  false,
			problem: "unknown source",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, install, pinned, problem := Translate(json.RawMessage(tc.raw))
			if src != tc.src || install != tc.install || pinned != tc.pinned || problem != tc.problem {
				t.Fatalf("got (%+v, %q, %v, %q), want (%+v, %q, %v, %q)",
					src, install, pinned, problem, tc.src, tc.install, tc.pinned, tc.problem)
			}
		})
	}
}

func TestParseCatalog(t *testing.T) {
	longDesc := strings.Repeat("a ", 400) // 800 chars -> collapsed then cut to 600 runes
	payload := map[string]any{
		"name": "claude-plugins-official",
		"renames": map[string]string{
			"old-name": "new-name",
			"bad name": "good-name",
			"good":     "bad/name",
		},
		"plugins": []any{
			map[string]any{
				"name":        "first",
				"description": "  hello   world  ",
				"author":      "Plain Author",
				"tags":        []string{"t1", " t2 "},
				"homepage":    "https://example.com/first",
				"source":      "./plugins/first",
			},
			map[string]any{
				"name":        "first", // duplicate -> skipped
				"description": "dup",
				"source":      "./plugins/dup",
			},
			map[string]any{
				"name":   "bad/name", // invalid -> skipped
				"source": "./plugins/bad",
			},
			map[string]any{
				"name":        "second",
				"description": longDesc,
				"author":      map[string]string{"name": "Object Author", "email": "a@b.c"},
				"keywords":    []string{"k1"},
				"tags":        []string{"ignored"},
				"homepage":    "javascript:alert(1)",
				"source":      map[string]string{"source": "github", "repo": "o/r"},
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	market, entries, renames, err := ParseCatalog(data)
	if err != nil {
		t.Fatalf("ParseCatalog error: %v", err)
	}
	if market != "claude-plugins-official" {
		t.Fatalf("market = %q", market)
	}
	if !reflect.DeepEqual(renames, map[string]string{"old-name": "new-name"}) {
		t.Fatalf("renames = %#v", renames)
	}
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(entries))
	}

	if entries[0].Author != "Plain Author" || entries[0].Description != "hello world" ||
		!reflect.DeepEqual(entries[0].Keywords, []string{"t1", "t2"}) ||
		entries[0].Homepage != "https://example.com/first" {
		t.Fatalf("entry[0] = %+v", entries[0])
	}

	descRunes := []rune(entries[1].Description)
	if len(descRunes) != maxDescriptionRunes || descRunes[len(descRunes)-1] != '…' {
		t.Fatalf("entry[1].Description runes = %d, last = %q", len(descRunes), descRunes[len(descRunes)-1])
	}
	if entries[1].Author != "Object Author" || !reflect.DeepEqual(entries[1].Keywords, []string{"k1"}) ||
		entries[1].Homepage != "" {
		t.Fatalf("entry[1] = %+v", entries[1])
	}

	if _, _, _, err := ParseCatalog([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if _, _, _, err := ParseCatalog([]byte(`{"name":"bad/market","plugins":[]}`)); err == nil {
		t.Fatal("expected error for invalid market name")
	}
}

func TestClientCatalogAndCache(t *testing.T) {
	const (
		head1 = "1111111111111111111111111111111111111111"
		head2 = "2222222222222222222222222222222222222222"
	)
	var (
		curHead   = head1
		apiStatus = http.StatusOK
		headReqs  atomic.Int32
		rawReqs   atomic.Int32
	)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headReqs.Add(1)
		if apiStatus != http.StatusOK {
			w.WriteHeader(apiStatus)
			return
		}
		w.Write([]byte(curHead))
	}))
	defer apiSrv.Close()

	rawSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawReqs.Add(1)
		w.Write([]byte(`{"name":"claude-plugins-official","plugins":[{"name":"p1","source":"./plugins/p1"}]}`))
	}))
	defer rawSrv.Close()

	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	c := New(t.TempDir())
	c.GH = &ghrepo.Client{
		HTTP:  apiSrv.Client(),
		API:   apiSrv.URL,
		Raw:   rawSrv.URL,
		Token: func() string { return "" },
	}
	c.Now = func() time.Time { return now }

	// 1. Fresh fetch -> 1 head, 1 raw.
	cat, err := c.Catalog(context.Background(), "official", false)
	if err != nil {
		t.Fatalf("Catalog fresh: %v", err)
	}
	if cat.Commit != head1 || len(cat.Entries) != 1 || cat.Stale {
		t.Fatalf("cat = %+v", cat)
	}
	if headReqs.Load() != 1 || rawReqs.Load() != 1 {
		t.Fatalf("reqs = (%d, %d), want (1, 1)", headReqs.Load(), rawReqs.Load())
	}

	// 2. Cached within TTL -> no requests.
	now = now.Add(2 * time.Hour)
	cat2, err := c.Catalog(context.Background(), "official", false)
	if err != nil {
		t.Fatalf("Catalog cached: %v", err)
	}
	if cat2.Commit != head1 || headReqs.Load() != 1 || rawReqs.Load() != 1 {
		t.Fatalf("expected no requests within TTL, got (%d, %d)", headReqs.Load(), rawReqs.Load())
	}

	// 3. Cached() returns stored catalog without requests.
	cached := c.Cached("official")
	if cached == nil || cached.Commit != head1 || headReqs.Load() != 1 || rawReqs.Load() != 1 {
		t.Fatalf("Cached = %+v, reqs = (%d, %d)", cached, headReqs.Load(), rawReqs.Load())
	}

	// 4. After TTL with same head -> only Head request.
	now = now.Add(5 * time.Hour)
	cat3, err := c.Catalog(context.Background(), "official", false)
	if err != nil {
		t.Fatalf("Catalog after TTL same head: %v", err)
	}
	if headReqs.Load() != 2 || rawReqs.Load() != 1 || cat3.Stale {
		t.Fatalf("same head reqs = (%d, %d), stale = %v", headReqs.Load(), rawReqs.Load(), cat3.Stale)
	}

	// 5. Failure after TTL with a cache -> Stale.
	now = now.Add(7 * time.Hour)
	apiStatus = http.StatusInternalServerError
	cat4, err := c.Catalog(context.Background(), "official", false)
	if err != nil {
		t.Fatalf("Catalog stale fallback err: %v", err)
	}
	if !cat4.Stale || cat4.StaleReason == "" || cat4.Commit != head1 {
		t.Fatalf("expected stale catalog, got %+v", cat4)
	}

	// 6. New head fetches raw again.
	apiStatus = http.StatusOK
	curHead = head2
	cat5, err := c.Catalog(context.Background(), "official", true)
	if err != nil {
		t.Fatalf("Catalog refresh new head: %v", err)
	}
	if cat5.Commit != head2 || rawReqs.Load() != 2 {
		t.Fatalf("new head cat = %+v, rawReqs = %d", cat5, rawReqs.Load())
	}
}

type fakeCall struct {
	args []string
}

type fakeRunner struct {
	lookPath    string
	lookPathErr error
	handle      func(args []string) ([]byte, error)
	calls       []fakeCall
}

func (f *fakeRunner) LookPath() (string, error) {
	if f.lookPathErr != nil {
		return "", f.lookPathErr
	}
	return f.lookPath, nil
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	cp := append([]string(nil), args...)
	f.calls = append(f.calls, fakeCall{args: cp})
	return f.handle(cp)
}

func TestPlanHandoff(t *testing.T) {
	cat := &Catalog{ID: "wshobson", Repo: "wshobson/agents", Market: "claude-code-workflows"}
	nativeEntry := Entry{Name: "deja-vu", Install: "native"}
	pathEntry := Entry{Name: "local", Install: "add"}

	// 1. Not native -> ErrNotNative.
	if _, err := PlanHandoff(context.Background(), &fakeRunner{}, cat, pathEntry); !errors.Is(err, ErrNotNative) {
		t.Fatalf("expected ErrNotNative, got %v", err)
	}

	// 2. Claude missing -> Claude "" and no steps.
	rMissing := &fakeRunner{lookPathErr: errors.New("not found")}
	plan, err := PlanHandoff(context.Background(), rMissing, cat, nativeEntry)
	if err != nil {
		t.Fatalf("missing claude err: %v", err)
	}
	if plan.Claude != "" || len(plan.Steps) != 0 || plan.PluginID != "deja-vu@claude-code-workflows" {
		t.Fatalf("missing claude plan = %+v", plan)
	}

	// 3. Not registered -> add + install steps.
	rAdd := &fakeRunner{
		lookPath: "/usr/local/bin/claude",
		handle: func(args []string) ([]byte, error) {
			if args[0] == "--version" {
				return []byte("2.1.323 (Claude Code)\n"), nil
			}
			return []byte(`[]`), nil
		},
	}
	plan, err = PlanHandoff(context.Background(), rAdd, cat, nativeEntry)
	if err != nil {
		t.Fatalf("PlanHandoff add: %v", err)
	}
	if plan.Registered || plan.ClaudeVersion != "2.1.323 (Claude Code)" || len(plan.Steps) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	wantAdd := []string{"plugin", "marketplace", "add", "wshobson/agents", "--scope", "user", "--json"}
	wantInstall := []string{"plugin", "install", "deja-vu@claude-code-workflows", "--scope", "user", "--json"}
	if plan.Steps[0].Name != "add" || !reflect.DeepEqual(plan.Steps[0].Args, wantAdd) {
		t.Fatalf("step[0] = %+v", plan.Steps[0])
	}
	if plan.Steps[1].Name != "install" || !reflect.DeepEqual(plan.Steps[1].Args, wantInstall) {
		t.Fatalf("step[1] = %+v", plan.Steps[1])
	}

	// 4. Registered from same github repo (different case) -> update step.
	rUpdateGH := &fakeRunner{
		lookPath: "/usr/local/bin/claude",
		handle: func(args []string) ([]byte, error) {
			if args[0] == "--version" {
				return []byte("2.1.323"), nil
			}
			return []byte(`[{"name":"claude-code-workflows","source":"github","repo":"WShobson/Agents"}]`), nil
		},
	}
	plan, err = PlanHandoff(context.Background(), rUpdateGH, cat, nativeEntry)
	if err != nil {
		t.Fatalf("PlanHandoff update github: %v", err)
	}
	if !plan.Registered || plan.Steps[0].Name != "update" {
		t.Fatalf("plan = %+v", plan)
	}
	wantUpdate := []string{"plugin", "marketplace", "update", "claude-code-workflows", "--json"}
	if !reflect.DeepEqual(plan.Steps[0].Args, wantUpdate) {
		t.Fatalf("update args = %v", plan.Steps[0].Args)
	}

	// 5. Registered as git URL https://github.com/Owner/Repo.git -> update step.
	rUpdateGit := &fakeRunner{
		lookPath: "/usr/local/bin/claude",
		handle: func(args []string) ([]byte, error) {
			if args[0] == "--version" {
				return []byte("2.1.323"), nil
			}
			return []byte(`[{"name":"claude-code-workflows","source":"git","url":"https://github.com/WShobson/Agents.git"}]`), nil
		},
	}
	plan, err = PlanHandoff(context.Background(), rUpdateGit, cat, nativeEntry)
	if err != nil || !plan.Registered || plan.Steps[0].Name != "update" {
		t.Fatalf("PlanHandoff update git: err=%v plan=%+v", err, plan)
	}

	// 6. Registered from elsewhere -> ErrMarketConflict.
	rConflict := &fakeRunner{
		lookPath: "/usr/local/bin/claude",
		handle: func(args []string) ([]byte, error) {
			if args[0] == "--version" {
				return []byte("2.1.323"), nil
			}
			return []byte(`[{"name":"claude-code-workflows","source":"github","repo":"evil/other"}]`), nil
		},
	}
	if _, err := PlanHandoff(context.Background(), rConflict, cat, nativeEntry); !errors.Is(err, ErrMarketConflict) {
		t.Fatalf("expected ErrMarketConflict, got %v", err)
	}
}

func writeTestMarketCopy(t *testing.T, dir, market, pluginName, sha string) {
	t.Helper()
	p := filepath.Join(dir, CatalogFile)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"name": market,
		"plugins": []any{
			map[string]any{
				"name": pluginName,
				"source": map[string]string{
					"source": "github",
					"repo":   "owner/plugin",
					"sha":    sha,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunHandoff(t *testing.T) {
	const (
		shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	cat := &Catalog{ID: "official", Repo: "anthropics/claude-plugins-official", Market: "claude-plugins-official"}
	entry := Entry{
		Name:    "my-plug",
		Install: "native",
		Pinned:  true,
		Source:  Source{Kind: "github", Repo: "owner/plugin", SHA: shaA},
	}
	plan := &Plan{
		Claude:   "/usr/local/bin/claude",
		Market:   cat.Market,
		PluginID: "my-plug@claude-plugins-official",
		Steps: []Step{
			{Name: "add", Args: []string{"plugin", "marketplace", "add", cat.Repo, "--scope", "user", "--json"}},
			{Name: "install", Args: []string{"plugin", "install", "my-plug@claude-plugins-official", "--scope", "user", "--json"}},
		},
	}

	// 1. Happy path.
	marketDir := t.TempDir()
	writeTestMarketCopy(t, marketDir, cat.Market, entry.Name, shaA)
	listJSON, _ := json.Marshal([]map[string]string{{
		"name":            cat.Market,
		"source":          "github",
		"repo":            cat.Repo,
		"installLocation": marketDir,
	}})

	rHappy := &fakeRunner{
		lookPath: "/usr/local/bin/claude",
		handle: func(args []string) ([]byte, error) {
			switch args[1] {
			case "marketplace":
				if args[2] == "add" {
					return []byte(`{"command":"marketplace-add","outcome":"ok","message":"Added"}`), nil
				}
				return listJSON, nil
			case "install":
				return []byte("some preamble\n{\"command\":\"install\",\"outcome\":\"ok\",\"message\":\"Successfully installed plugin\"}\n"), nil
			}
			return nil, errors.New("unexpected")
		},
	}
	res, err := RunHandoff(context.Background(), rHappy, cat, entry, plan)
	if err != nil {
		t.Fatalf("RunHandoff happy: %v", err)
	}
	if res.PluginID != plan.PluginID || res.Message != "Successfully installed plugin" {
		t.Fatalf("res = %+v", res)
	}
	if len(rHappy.calls) != 3 || rHappy.calls[0].args[2] != "add" || rHappy.calls[1].args[2] != "list" || rHappy.calls[2].args[1] != "install" {
		t.Fatalf("call order = %+v", rHappy.calls)
	}
	for _, c := range rHappy.calls {
		for _, a := range c.args {
			if a == "-y" || a == "--yes" || a == "--accept-command" {
				t.Fatalf("forbidden flag %q in %v", a, c.args)
			}
		}
	}

	// 2. Copy pins another sha -> ErrCatalogMoved and install never runs.
	movedDir := t.TempDir()
	writeTestMarketCopy(t, movedDir, cat.Market, entry.Name, shaB)
	movedListJSON, _ := json.Marshal([]map[string]string{{
		"name":            cat.Market,
		"source":          "github",
		"repo":            cat.Repo,
		"installLocation": movedDir,
	}})
	rMoved := &fakeRunner{
		lookPath: "/usr/local/bin/claude",
		handle: func(args []string) ([]byte, error) {
			if args[1] == "marketplace" && args[2] == "add" {
				return []byte(`{"outcome":"ok"}`), nil
			}
			if args[1] == "marketplace" && args[2] == "list" {
				return movedListJSON, nil
			}
			t.Fatalf("install should not have run: %v", args)
			return nil, nil
		},
	}
	if _, err := RunHandoff(context.Background(), rMoved, cat, entry, plan); !errors.Is(err, ErrCatalogMoved) {
		t.Fatalf("expected ErrCatalogMoved, got %v", err)
	}

	// 3. Install fails with JSON outcome:"failed".
	rInstFail := &fakeRunner{
		lookPath: "/usr/local/bin/claude",
		handle: func(args []string) ([]byte, error) {
			if args[1] == "marketplace" && args[2] == "add" {
				return []byte(`{"outcome":"ok"}`), nil
			}
			if args[1] == "marketplace" && args[2] == "list" {
				return listJSON, nil
			}
			return []byte("{\"command\":\"install\",\"outcome\":\"failed\",\"message\":\"Plugin not found\",\"failureCode\":\"not_found\"}\n✘ Failed"), errors.New("exit 1")
		},
	}
	_, err = RunHandoff(context.Background(), rInstFail, cat, entry, plan)
	var se *StepError
	if !errors.As(err, &se) || se.Step != "install" || se.FailureCode != "not_found" || se.Message != "Plugin not found" {
		t.Fatalf("expected StepError(install, not_found), got %#v (%v)", se, err)
	}

	// 4. Add fails -> StepError Step "add".
	rAddFail := &fakeRunner{
		lookPath: "/usr/local/bin/claude",
		handle: func(args []string) ([]byte, error) {
			return []byte(`{"outcome":"failed","message":"network error","failureCode":"net"}`), errors.New("exit 1")
		},
	}
	_, err = RunHandoff(context.Background(), rAddFail, cat, entry, plan)
	if !errors.As(err, &se) || se.Step != "add" || se.Message != "network error" {
		t.Fatalf("expected StepError(add), got %#v (%v)", se, err)
	}
}
