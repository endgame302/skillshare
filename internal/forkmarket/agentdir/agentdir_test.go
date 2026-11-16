package agentdir

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"skillshare/internal/forkmarket/ghrepo"
)

func TestMatchRepoRules(t *testing.T) {
	cases := []struct {
		source  string
		entry   ghrepo.Entry
		wantOK  bool
		wantCat string
		wantDir string
	}{
		{"voltagent", ghrepo.Entry{Path: "categories/01-core-development/api-designer.md", Type: "blob", Mode: "100644", Size: 100}, true, "core-development", "categories/01-core-development"},
		{"voltagent", ghrepo.Entry{Path: "categories/misc/Helper.MD", Type: "blob", Mode: "100755", Size: 100}, true, "misc", "categories/misc"},
		{"voltagent", ghrepo.Entry{Path: "categories/01-core-development/README.md", Type: "blob", Mode: "100644"}, false, "", ""},
		{"voltagent", ghrepo.Entry{Path: "categories/01-core-development/CONTRIBUTING.md", Type: "blob", Mode: "100644"}, false, "", ""},
		{"voltagent", ghrepo.Entry{Path: "categories/01-core-development/CLAUDE.md", Type: "blob", Mode: "100644"}, false, "", ""},
		{"voltagent", ghrepo.Entry{Path: "categories/01-core-development/.hidden.md", Type: "blob", Mode: "100644"}, false, "", ""},
		{"voltagent", ghrepo.Entry{Path: "categories/01-core-development/sub/a.md", Type: "blob", Mode: "100644"}, false, "", ""},
		{"voltagent", ghrepo.Entry{Path: "categories/a.md", Type: "blob", Mode: "100644"}, false, "", ""},
		{"voltagent", ghrepo.Entry{Path: "categories/01-core-development/notes.txt", Type: "blob", Mode: "100644"}, false, "", ""},
		{"voltagent", ghrepo.Entry{Path: "categories/01-core-development/link.md", Type: "blob", Mode: "120000"}, false, "", ""},
		{"voltagent", ghrepo.Entry{Path: "categories/01-core-development/big.md", Type: "blob", Mode: "100644", Size: maxAgentFileBytes + 1}, false, "", ""},
		{"wshobson", ghrepo.Entry{Path: "plugins/backend/agents/architect.md", Type: "blob", Mode: "100644", Size: 100}, true, "backend", "plugins/backend/agents"},
		{"wshobson", ghrepo.Entry{Path: "plugins/backend/agents/README.md", Type: "blob", Mode: "100644"}, false, "", ""},
		{"wshobson", ghrepo.Entry{Path: "plugins/backend/skills/architect.md", Type: "blob", Mode: "100644"}, false, "", ""},
		{"wshobson", ghrepo.Entry{Path: "plugins/backend/agents/sub/a.md", Type: "blob", Mode: "100644"}, false, "", ""},
		{"wshobson", ghrepo.Entry{Path: "plugins/backend/agents/.hidden.md", Type: "blob", Mode: "100644"}, false, "", ""},
		{"wshobson", ghrepo.Entry{Path: "plugins/backend/agents/link.md", Type: "blob", Mode: "120000"}, false, "", ""},
	}

	for _, tc := range cases {
		var got matchedEntry
		var ok bool
		if tc.source == "voltagent" {
			got, ok = matchVoltagent(tc.entry)
		} else {
			got, ok = matchWshobson(tc.entry)
		}
		if ok != tc.wantOK || (ok && (got.category != tc.wantCat || got.dir != tc.wantDir)) {
			t.Errorf("%s match(%+v) = (%+v, %v), want (%s, %s, %v)", tc.source, tc.entry, got, ok, tc.wantCat, tc.wantDir, tc.wantOK)
		}
	}
}

func TestFrontmatterAndClean(t *testing.T) {
	// BOM + CRLF + tools as comma string + <example> + literal \n.
	bomCRLF := "\xef\xbb\xbf---\r\nname: API Designer\r\ndescription: Helps\n  with   APIs<example>ignored</example>\r\ntools: Read, Write , , Bash\r\nmodel: sonnet\r\n---\r\nBody\r\n"
	fm := parseFrontmatter([]byte(bomCRLF))
	if fm.Name != "API Designer" || fm.Model != "sonnet" {
		t.Fatalf("fm = %+v", fm)
	}
	if len(fm.Tools) != 3 || fm.Tools[0] != "Read" || fm.Tools[1] != "Write" || fm.Tools[2] != "Bash" {
		t.Fatalf("tools = %+v", fm.Tools)
	}
	if got := cleanDescription(fm.Description); got != "Helps with APIs" {
		t.Fatalf("cleanDescription = %q", got)
	}

	// Tools as YAML list and case-insensitive <EXAMPLE>.
	listYAML := "---\nname: Reviewer\ndescription: Line1\n  Line2 <EXAMPLE>drop me\ntools:\n  - Grep\n  - Glob\n---\n"
	fmList := parseFrontmatter([]byte(listYAML))
	if len(fmList.Tools) != 2 || fmList.Tools[0] != "Grep" || fmList.Tools[1] != "Glob" {
		t.Fatalf("list tools = %+v", fmList.Tools)
	}
	if got := cleanDescription(fmList.Description); got != "Line1 Line2" {
		t.Fatalf("cleanDescription = %q", got)
	}

	// Missing frontmatter and invalid YAML.
	if got := parseFrontmatter([]byte("# Plain markdown\n")); got.Name != "" || got.Description != "" {
		t.Fatalf("missing frontmatter = %+v", got)
	}
	if got := parseFrontmatter([]byte("---\n: [bad yaml\n---\n")); got.Name != "" || got.Description != "" {
		t.Fatalf("invalid yaml = %+v", got)
	}
	// YAML that does not parse still gives its simple one-line keys.
	sloppy := "---\nname: \"sloppy-agent\"\ndescription: Use when: things break\nmodel: opus\nnote: [unclosed\n---\n"
	if got := parseFrontmatter([]byte(sloppy)); got.Name != "sloppy-agent" || got.Description != "Use when: things break" || got.Model != "opus" {
		t.Fatalf("sloppy yaml = %+v", got)
	}

	// 700-rune description cut with ellipsis.
	long := strings.Repeat("a", 700)
	cleaned := cleanDescription(long)
	if !strings.HasSuffix(cleaned, "…") {
		t.Fatalf("cleaned missing ellipsis: %q", cleaned)
	}
	if n := utf8.RuneCountInString(cleaned); n > maxDescriptionRunes {
		t.Fatalf("cleaned rune count = %d, want <= %d", n, maxDescriptionRunes)
	}
}

func TestListVoltagentCachingAndStale(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	var headCalls, treeCalls, rawCalls atomic.Int32
	var failHead atomic.Bool

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failHead.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/commits/HEAD"):
			headCalls.Add(1)
			w.Write([]byte(commit))
		case strings.Contains(r.URL.Path, "/git/trees/"):
			treeCalls.Add(1)
			json.NewEncoder(w).Encode(map[string]any{
				"truncated": false,
				"tree": []ghrepo.Entry{
					{Path: "categories/02-ops/zeta.md", Type: "blob", Mode: "100644", Size: 50},
					{Path: "categories/01-core/alpha.md", Type: "blob", Mode: "100644", Size: 50},
					{Path: "categories/01-core/README.md", Type: "blob", Mode: "100644", Size: 50},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiSrv.Close()

	rawSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawCalls.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/alpha.md"):
			w.Write([]byte("---\nname: Alpha Agent\ndescription: First agent\n---\n"))
		case strings.HasSuffix(r.URL.Path, "/zeta.md"):
			w.Write([]byte("# No frontmatter"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rawSrv.Close()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c := New(t.TempDir())
	c.GH.API = apiSrv.URL
	c.GH.Raw = rawSrv.URL
	c.GH.Token = func() string { return "" }
	c.Now = func() time.Time { return now }

	l, err := c.List(context.Background(), "voltagent", false)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(l.Agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(l.Agents))
	}
	if l.Agents[0].File != "alpha.md" || l.Agents[0].Title != "Alpha Agent" || l.Agents[0].Category != "core" || l.Agents[0].Dir != "categories/01-core" {
		t.Fatalf("agent[0] = %+v", l.Agents[0])
	}
	wantPage := "https://github.com/VoltAgent/awesome-claude-code-subagents/blob/" + commit + "/categories/01-core/alpha.md"
	if l.Agents[0].PageURL != wantPage {
		t.Fatalf("PageURL = %q, want %q", l.Agents[0].PageURL, wantPage)
	}
	if l.Agents[1].File != "zeta.md" || l.Agents[1].Title != "zeta" {
		t.Fatalf("agent[1] = %+v", l.Agents[1])
	}
	if headCalls.Load() != 1 || treeCalls.Load() != 1 || rawCalls.Load() != 2 {
		t.Fatalf("calls = (%d, %d, %d), want (1, 1, 2)", headCalls.Load(), treeCalls.Load(), rawCalls.Load())
	}

	// Within TTL: no HTTP requests.
	now = now.Add(time.Hour)
	if _, err := c.List(context.Background(), "voltagent", false); err != nil {
		t.Fatalf("cached List error: %v", err)
	}
	if headCalls.Load() != 1 || treeCalls.Load() != 1 || rawCalls.Load() != 2 {
		t.Fatalf("cached calls = (%d, %d, %d), want (1, 1, 2)", headCalls.Load(), treeCalls.Load(), rawCalls.Load())
	}

	// Past TTL with same commit: only Head request.
	now = now.Add(12 * time.Hour)
	l2, err := c.List(context.Background(), "voltagent", false)
	if err != nil || len(l2.Agents) != 2 {
		t.Fatalf("expired same commit: %+v, err=%v", l2, err)
	}
	if headCalls.Load() != 2 || treeCalls.Load() != 1 || rawCalls.Load() != 2 {
		t.Fatalf("same-commit calls = (%d, %d, %d), want (2, 1, 2)", headCalls.Load(), treeCalls.Load(), rawCalls.Load())
	}

	// refresh=true with same commit: only Head request.
	l3, err := c.List(context.Background(), "voltagent", true)
	if err != nil || len(l3.Agents) != 2 {
		t.Fatalf("refresh same commit: %+v, err=%v", l3, err)
	}
	if headCalls.Load() != 3 || treeCalls.Load() != 1 || rawCalls.Load() != 2 {
		t.Fatalf("refresh calls = (%d, %d, %d), want (3, 1, 2)", headCalls.Load(), treeCalls.Load(), rawCalls.Load())
	}

	// Failing API with cached listing returns stale listing.
	failHead.Store(true)
	l4, err := c.List(context.Background(), "voltagent", true)
	if err != nil {
		t.Fatalf("stale List returned error: %v", err)
	}
	if !l4.Stale || l4.StaleReason == "" || len(l4.Agents) != 2 {
		t.Fatalf("stale listing = %+v", l4)
	}

	// Failing API with no cache returns error.
	cFresh := New(t.TempDir())
	cFresh.GH.API = apiSrv.URL
	cFresh.GH.Raw = rawSrv.URL
	cFresh.GH.Token = func() string { return "" }
	if _, err := cFresh.List(context.Background(), "voltagent", false); err == nil {
		t.Fatalf("failing API without cache succeeded, want error")
	}
}

func TestAitmpl(t *testing.T) {
	var mode string
	var gotIfNoneMatch string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		switch mode {
		case "304":
			w.WriteHeader(http.StatusNotModified)
		case "v2":
			w.Write([]byte(`{"schemaVersion":"2.0.0","source":"https://github.com/davila7/claude-code-templates","components":[]}`))
		case "badsource":
			w.Write([]byte(`{"schemaVersion":"1.0.0","source":"https://gitlab.com/a/b","components":[]}`))
		default:
			w.Header().Set("ETag", `"etag-1"`)
			w.Write([]byte(`{
				"schemaVersion":"1.2.0",
				"generatedAt":"2026-01-01T00:00:00Z",
				"source":"https://github.com/davila7/claude-code-templates",
				"count":4,
				"components":[
					{
						"id":"agents/security/auditor",
						"type":"agents",
						"name":"Security Auditor",
						"category":"security",
						"path":"security/auditor.md",
						"description":"Audits code",
						"downloads":42,
						"urls":{"page":"https://www.aitmpl.com/agents/security/auditor"},
						"scan":{"tool":"SkillSpector","severity":"low","score":21},
						"source":null
					},
					{
						"id":"agents/bad/path",
						"type":"agents",
						"name":"Bad Path",
						"category":"bad",
						"path":"bad/sub/x.md"
					},
					{
						"id":"agents/ext/one",
						"type":"agents",
						"name":"External",
						"category":"ext",
						"path":"ext/one.md",
						"source":{"repo":"other/repo"}
					},
					{
						"id":"skills/x/y",
						"type":"skills",
						"name":"Not Agent",
						"category":"x",
						"path":"x/y.md"
					}
				]
			}`))
		}
	}))
	defer srv.Close()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c := New(t.TempDir())
	c.HTTP = srv.Client()
	c.AitmplURL = srv.URL
	c.Now = func() time.Time { return now }

	l, err := c.List(context.Background(), "aitmpl", false)
	if err != nil {
		t.Fatalf("aitmpl List err: %v", err)
	}
	if len(l.Agents) != 1 {
		t.Fatalf("got %d agents, want 1: %+v", len(l.Agents), l.Agents)
	}
	a := l.Agents[0]
	if a.ID != "aitmpl:agents/security/auditor" || a.Dir != "cli-tool/components/agents/security" || a.File != "auditor.md" {
		t.Fatalf("agent = %+v", a)
	}
	if a.Scan == nil || a.Scan.Score == nil || *a.Scan.Score != 21 || a.Downloads == nil || *a.Downloads != 42 {
		t.Fatalf("scan/downloads = %+v / %+v", a.Scan, a.Downloads)
	}

	// After TTL, sends If-None-Match and 304 keeps agents.
	now = now.Add(7 * time.Hour)
	mode = "304"
	l2, err := c.List(context.Background(), "aitmpl", false)
	if err != nil || len(l2.Agents) != 1 {
		t.Fatalf("304 List = %+v, err=%v", l2, err)
	}
	if gotIfNoneMatch != `"etag-1"` {
		t.Errorf("If-None-Match = %q, want \"etag-1\"", gotIfNoneMatch)
	}

	// Schema v2 and non-GitHub source errors.
	for _, m := range []string{"v2", "badsource"} {
		mode = m
		cErr := New(t.TempDir())
		cErr.HTTP = srv.Client()
		cErr.AitmplURL = srv.URL
		if _, err := cErr.List(context.Background(), "aitmpl", false); err == nil {
			t.Errorf("mode %s succeeded, want error", m)
		}
	}
}

func TestUnknownSource(t *testing.T) {
	c := New(t.TempDir())
	if _, err := c.List(context.Background(), "nope", false); !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("err = %v, want ErrUnknownSource", err)
	}
}
