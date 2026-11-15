package skillssh

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures are the three skills.sh leaderboard pages and one search answer as fetched on
// 2026-11-15, gzipped because the pages are close to 1 MB of minified HTML.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if filepath.Ext(name) != ".gz" {
		return raw
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gunzip %s: %v", name, err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip %s: %v", name, err)
	}
	return out
}

func TestParseBoardFixtures(t *testing.T) {
	cases := []struct {
		file  string
		first Skill
		last  string
	}{
		{"all.html.gz", Skill{ID: "vercel-labs/skills/find-skills", Source: "vercel-labs/skills", SkillID: "find-skills", Name: "find-skills", Installs: 4368796, URL: "https://skills.sh/vercel-labs/skills/find-skills", Installable: true}, "addyosmani/web-quality-skills/accessibility"},
		{"trending.html.gz", Skill{ID: "designed-by-ai/skills/design-mobile-apps", Source: "designed-by-ai/skills", SkillID: "design-mobile-apps", Name: "design-mobile-apps", Installs: 16538, URL: "https://skills.sh/designed-by-ai/skills/design-mobile-apps", Installable: true}, "mvanhorn/last30days-skill/last30days"},
		{"hot.html.gz", Skill{ID: "lllllllama/rigorpilot-skills/explore-run", Source: "lllllllama/rigorpilot-skills", SkillID: "explore-run", Name: "explore-run", Installs: 180, URL: "https://skills.sh/lllllllama/rigorpilot-skills/explore-run", Installable: true}, "zubair-trabzada/geo-seo-claude/geo-schema"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			skills, err := ParseBoard(readFixture(t, tc.file))
			if err != nil {
				t.Fatalf("ParseBoard: %v", err)
			}
			if len(skills) != 600 {
				t.Fatalf("got %d skills, want the page's 600", len(skills))
			}
			if skills[0] != tc.first {
				t.Errorf("first = %+v\nwant    %+v", skills[0], tc.first)
			}
			if got := skills[len(skills)-1].ID; got != tc.last {
				t.Errorf("last = %q, want %q", got, tc.last)
			}
		})
	}
}

func TestParseBoardMarksNonGitHubSources(t *testing.T) {
	skills, err := ParseBoard(readFixture(t, "all.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	// Skills published from a website's well-known URL name the domain (agent.qq.com) as their source.
	var domain, github int
	for _, s := range skills {
		switch {
		case !strings.Contains(s.Source, "/"):
			domain++
			if s.Installable {
				t.Errorf("%s marked installable", s.ID)
			}
		case s.Installable:
			github++
		default:
			t.Errorf("%s marked not installable", s.ID)
		}
	}
	if domain == 0 || github == 0 {
		t.Fatalf("want both kinds of source, got %d GitHub and %d domain", github, domain)
	}
}

func TestInstallableSources(t *testing.T) {
	for source, want := range map[string]bool{
		"vercel-labs/skills":     true,
		"a/b.c":                  true,
		"open.feishu.cn":         false,
		"gitlab.com/group/repo":  false,
		"owner/repo/sub":         false,
		"-bad/repo":              false,
		"https://github.com/a/b": false,
	} {
		if got := githubRepo.MatchString(source); got != want {
			t.Errorf("%q installable = %v, want %v", source, got, want)
		}
	}
}

func TestParseBoardFormatChanged(t *testing.T) {
	pages := map[string]string{
		"empty":            ``,
		"plain html":       `<html><body><h1>Agent Skills</h1><ul><li>find-skills</li></ul></body></html>`,
		"challenge":        `<html><head><title>Just a moment...</title></head><body><script>window._cf_chl_opt={}</script></body></html>`,
		"renamed fields":   `<script>self.__next_f.push([1,"5:[\"$\",\"$L6\",null,{\"rows\":[{\"repo\":\"a/b\",\"slug\":\"c\",\"count\":3}]}]\n"])</script>`,
		"broken flight":    `<script>self.__next_f.push([1,"unterminated</script>`,
		"next data, empty": `<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"initialSkills":[]}}}</script>`,
	}
	for name, page := range pages {
		t.Run(name, func(t *testing.T) {
			skills, err := ParseBoard([]byte(page))
			if !errors.Is(err, ErrFormatChanged) {
				t.Fatalf("err = %v, want ErrFormatChanged", err)
			}
			if skills != nil {
				t.Fatalf("skills = %v, want nil", skills)
			}
		})
	}
}

func TestParseBoardShapes(t *testing.T) {
	pages := map[string]string{
		"next data": `<script id="__NEXT_DATA__" type="application/json">
			{"props":{"pageProps":{"initialSkills":[{"source":"antfu/skills","skillId":"vite","name":"vite","installs":152}]}}}
		</script>`,
		"flight": `<script>self.__next_f.push([1,"...\n[{\"source\":\"antfu/skills\",\"skillId\":\"vite\",\"name\":\"vite\",\"installs\":152}]\n"])</script>`,
		"flight split across chunks": `<script>self.__next_f.push([1,"53:[\"$\",\"$L61\",null,{\"initialSkills\":[{\"source\":\"antfu/sk"])</script>` +
			`<script>self.__next_f.push([1,"ills\",\"skillId\":\"vite\",\"name\":\"vite\",\"installs\":152}]}]\n"])</script>`,
		"flight reference":    `<script>self.__next_f.push([1,"1f:[{\"skillId\":\"vite\",\"installs\":152,\"source\":\"antfu/skills\",\"name\":\"vite\"}]\n53:[\"$\",\"$L61\",null,{\"initialSkills\":\"$1f\"}]\n"])</script>`,
		"legacy object":       `{"source":"antfu/skills","skill_id":"vite","name":"vite","installs":152}`,
		"escaped in a string": `<script>window.x = "{\"source\":\"antfu/skills\",\"skillId\":\"vite\",\"name\":\"vite\",\"installs\":152}"</script>`,
	}
	want := Skill{ID: "antfu/skills/vite", Source: "antfu/skills", SkillID: "vite", Name: "vite", Installs: 152, URL: "https://skills.sh/antfu/skills/vite", Installable: true}
	for name, page := range pages {
		t.Run(name, func(t *testing.T) {
			skills, err := ParseBoard([]byte(page))
			if err != nil {
				t.Fatalf("ParseBoard: %v", err)
			}
			if len(skills) != 1 || skills[0] != want {
				t.Fatalf("skills = %+v, want [%+v]", skills, want)
			}
		})
	}
}

func TestParseBoardDropsRepeatsAndIncompleteEntries(t *testing.T) {
	page := `<script>self.__next_f.push([1,"{\"initialSkills\":[` +
		`{\"source\":\"a/b\",\"skillId\":\"c\",\"installs\":3},` +
		`{\"source\":\"a/b\",\"skillId\":\"c\",\"installs\":3},` +
		`{\"source\":\"\",\"skillId\":\"d\"},` +
		`{\"source\":\"a/b\"},` +
		`{\"source\":\"a/b\",\"skillId\":\"e\",\"installs\":\"7\"}` +
		`]}"])</script>`
	skills, err := ParseBoard([]byte(page))
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 || skills[0].Name != "c" || skills[1].Installs != 7 {
		t.Fatalf("skills = %+v", skills)
	}
}

func TestParseSearchFixture(t *testing.T) {
	skills, err := ParseSearch(readFixture(t, "search-react.json"))
	if err != nil {
		t.Fatalf("ParseSearch: %v", err)
	}
	if len(skills) != 10 {
		t.Fatalf("got %d results, want 10", len(skills))
	}
	want := Skill{ID: "vercel-labs/agent-skills/vercel-react-best-practices", Source: "vercel-labs/agent-skills", SkillID: "vercel-react-best-practices", Name: "vercel-react-best-practices", Installs: 900588, URL: "https://skills.sh/vercel-labs/agent-skills/vercel-react-best-practices", Installable: true}
	if skills[0] != want {
		t.Errorf("first = %+v\nwant    %+v", skills[0], want)
	}
}

func TestParseSearchShapes(t *testing.T) {
	if skills, err := ParseSearch([]byte(`[{"id":"a/b/c","source":"a/b","name":"c","installs":1}]`)); err != nil || len(skills) != 1 || skills[0].SkillID != "c" {
		t.Errorf("bare array: %+v, %v", skills, err)
	}
	if skills, err := ParseSearch([]byte(`{"skills":[]}`)); err != nil || len(skills) != 0 {
		t.Errorf("no results: %+v, %v", skills, err)
	}
	for _, body := range []string{`{"results":[]}`, `<html>`, ``} {
		if _, err := ParseSearch([]byte(body)); !errors.Is(err, ErrFormatChanged) {
			t.Errorf("ParseSearch(%q) err = %v, want ErrFormatChanged", body, err)
		}
	}
}
