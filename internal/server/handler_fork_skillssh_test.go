package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"skillshare/internal/forkmarket/skillssh"
	"skillshare/internal/install"
)

// useFakeSkillssh points the skills.sh client at a local server for the test.
func useFakeSkillssh(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	site := httptest.NewServer(handler)
	t.Cleanup(site.Close)
	old := forkSkillssh
	forkSkillssh = skillssh.New()
	forkSkillssh.BaseURL = site.URL
	t.Cleanup(func() { forkSkillssh = old })
}

func serveForkSkillssh(t *testing.T, s *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	return rr
}

func skillsshErrorCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"error_code"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rr.Body.String(), err)
	}
	return body.Code
}

func TestForkSkillsshBoard(t *testing.T) {
	useFakeSkillssh(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trending":
			w.Write([]byte(`<script>self.__next_f.push([1,"{\"initialSkills\":[{\"source\":\"a/b\",\"skillId\":\"c\",\"name\":\"c\",\"installs\":3}]}"])</script>`))
		case "/hot":
			w.Write([]byte(`<html><body>a new design</body></html>`))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	s, _ := newTestServer(t)

	rr := serveForkSkillssh(t, s, http.MethodGet, "/api/fork/skillssh/board?name=trending", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("trending: %d %s", rr.Code, rr.Body.String())
	}
	var list struct {
		Board  string           `json:"board"`
		Skills []skillssh.Skill `json:"skills"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Board != "trending" || len(list.Skills) != 1 || list.Skills[0].ID != "a/b/c" || !list.Skills[0].Installable {
		t.Fatalf("trending = %+v", list)
	}

	for target, want := range map[string]struct {
		status int
		code   string
	}{
		"/api/fork/skillssh/board?name=hot":    {http.StatusBadGateway, "skillssh.format_changed"},
		"/api/fork/skillssh/board?name=all":    {http.StatusBadGateway, "skillssh.unreachable"},
		"/api/fork/skillssh/board?name=weekly": {http.StatusBadRequest, "skillssh.unknown_board"},
	} {
		rr := serveForkSkillssh(t, s, http.MethodGet, target, "")
		if rr.Code != want.status || skillsshErrorCode(t, rr) != want.code {
			t.Errorf("%s: %d %s, want %d %s", target, rr.Code, rr.Body.String(), want.status, want.code)
		}
	}
}

func TestForkSkillsshSearch(t *testing.T) {
	var gotQuery string
	useFakeSkillssh(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"skills":[{"id":"a/b/c","source":"a/b","skillId":"c","name":"c","installs":9}]}`))
	})
	s, _ := newTestServer(t)

	rr := serveForkSkillssh(t, s, http.MethodGet, "/api/fork/skillssh/search?q=react&limit=20", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"skillId":"c"`) {
		t.Fatalf("search: %d %s", rr.Code, rr.Body.String())
	}
	if gotQuery != "limit=20&q=react" {
		t.Errorf("asked skills.sh for %q", gotQuery)
	}

	rr = serveForkSkillssh(t, s, http.MethodGet, "/api/fork/skillssh/search?q=r", "")
	if rr.Code != http.StatusBadRequest || skillsshErrorCode(t, rr) != "skillssh.query_too_short" {
		t.Fatalf("one character: %d %s", rr.Code, rr.Body.String())
	}
}

func TestForkSkillsshRefusesOtherSites(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/fork/skillssh/board?name=all", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("cross-site request: %d, want 403", rr.Code)
	}
}

func TestForkSkillsshResolveValidates(t *testing.T) {
	s, _ := newTestServer(t)
	for _, body := range []string{
		`{"source":"open.feishu.cn","skillId":"x"}`,
		`{"source":"a/b","skillId":" "}`,
		`{"source":"../../etc","skillId":"x"}`,
		`{"source":"https://example.com/a/b","skillId":"x"}`,
		`not json`,
	} {
		rr := serveForkSkillssh(t, s, http.MethodPost, "/api/fork/skillssh/resolve", body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", body, rr.Code, rr.Body.String())
		}
	}
}

func writeSkill(t *testing.T, dir, frontmatterName string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+frontmatterName+"\ndescription: test\n---\n# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMatchSkillsshSkill(t *testing.T) {
	repo := t.TempDir()
	// As in vercel-labs/agent-skills: the skills.sh id is the SKILL.md name, not the folder.
	writeSkill(t, filepath.Join(repo, "skills", "react-best-practices"), "vercel-react-best-practices")
	writeSkill(t, filepath.Join(repo, "skills", "composition-patterns"), "vercel-composition-patterns")
	writeSkill(t, filepath.Join(repo, "skills", "web-design"), "Web Design Guidelines")
	// As in google-labs-code/stitch-skills: skills.sh drops the colons from these names.
	writeSkill(t, filepath.Join(repo, "plugins", "stitch-build", "skills", "react-components"), `"react:components"`)
	writeSkill(t, filepath.Join(repo, "plugins", "stitch", "skills", "react-components"), `"stitch::react-components"`)

	source, err := install.ParseSource(repo)
	if err != nil {
		t.Fatal(err)
	}
	d, err := install.DiscoverLocal(source)
	if err != nil {
		t.Fatal(err)
	}
	defer install.CleanupDiscovery(d)

	for id, wantPath := range map[string]string{
		"vercel-react-best-practices": "skills/react-best-practices",
		"VERCEL-COMPOSITION-PATTERNS": "skills/composition-patterns",
		"web-design-guidelines":       "skills/web-design",
		"react-best-practices":        "skills/react-best-practices", // the folder name also matches
		"reactcomponents":             "plugins/stitch-build/skills/react-components",
		"stitchreact-components":      "plugins/stitch/skills/react-components",
	} {
		sk, ok := matchSkillsshSkill(d, id)
		if !ok || sk.Path != wantPath {
			t.Errorf("match(%q) = %+v, %v; want %s", id, sk, ok, wantPath)
		}
	}
	for _, id := range []string{"missing", "", "---"} {
		if sk, ok := matchSkillsshSkill(d, id); ok {
			t.Errorf("match(%q) = %+v, want no match", id, sk)
		}
	}
}

// A git discovery clones into <RepoPath>/repo, unlike a local one; resolve only ever sees git.
func TestMatchSkillsshSkillInAClone(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, filepath.Join(tmp, "repo", "skills", "react-best-practices"), "vercel-react-best-practices")
	d := &install.DiscoveryResult{
		RepoPath: tmp,
		Source:   &install.Source{Type: install.SourceTypeGitHub},
		Skills:   []install.SkillInfo{{Name: "react-best-practices", Path: "skills/react-best-practices"}},
	}
	if sk, ok := matchSkillsshSkill(d, "vercel-react-best-practices"); !ok || sk.Name != "react-best-practices" {
		t.Fatalf("match = %+v, %v; want the frontmatter name read from the clone", sk, ok)
	}
}
