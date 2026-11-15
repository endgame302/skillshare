package plainhub

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skillshare/internal/install"
)

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	onDisk := filepath.Join(dir, "team", "hub")
	if err := os.MkdirAll(filepath.Dir(onDisk), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(onDisk, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		hub  string
		want bool
	}{
		{"anthropics/skills", true},
		{"  anthropics/skills  ", true},
		{"anthropics/skills/skills", true},
		{"github.com/anthropics/skills", true},
		{"https://github.com/anthropics/skills", true},
		{"https://github.com/anthropics/skills/", true},
		{"https://github.com/anthropics/skills.git", true},
		{"https://github.com/anthropics/skills/tree/main/skills", true},
		{"http://github.com/anthropics/skills", true},

		{"", false},
		{"skills", false},
		{"https://raw.githubusercontent.com/runkids/skillshare-hub/main/skillshare-hub.json", false},
		{"https://github.com/runkids/skillshare-hub/blob/main/skillshare-hub.json", false},
		{"https://github.com/owner/repo/blob/main/README", false},
		{"https://github.com/owner/repo/raw/main/hub", false},
		{"https://github.com/owner/repo/blob/main/skills/pdf/SKILL.md", false},
		{"owner/repo/hub.json", false},
		{"https://example.com/hub.JSON?token=x", false},
		{"https://example.com/hubs/team", false},
		{"https://gitlab.com/owner/repo", false},
		{"gitlab.com/owner/repo", false},
		{"git@github.com:owner/repo.git", false},
		{"ssh://git@github.com/owner/repo.git", false},
		{"file:///tmp/repo", false},
		{"./hub", false},
		{"/srv/hub", false},
		{onDisk, false},
	}
	for _, tt := range tests {
		if got := Detect(tt.hub); got != tt.want {
			t.Errorf("Detect(%q) = %v, want %v", tt.hub, got, tt.want)
		}
	}
}

// fakeFetcher serves a fixed commit and discovers skills in a local fixture
// tree with the same discovery an install uses.
type fakeFetcher struct {
	commit    string
	dir       string
	headErr   error
	heads     int
	discovers int
	srcs      []string
}

func (f *fakeFetcher) Head(src *install.Source, _ install.ParseOptions) (string, error) {
	f.heads++
	return f.commit, f.headErr
}

func (f *fakeFetcher) Discover(src *install.Source) ([]install.SkillInfo, error) {
	f.discovers++
	f.srcs = append(f.srcs, src.Raw)
	d, err := install.DiscoverLocal(&install.Source{Type: install.SourceTypeLocalPath, Path: f.dir, Name: filepath.Base(f.dir)})
	if err != nil {
		return nil, err
	}
	return d.Skills, nil
}

func writeSkill(t *testing.T, root, rel, frontmatter string) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "\n---\n\n# Skill\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureRepo mirrors the layout of a skills repo such as anthropics/skills:
// skills under skills/, a nested group, a README and a non-skill folder.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeSkill(t, root, "skills/pdf", "name: pdf\ndescription: Read and fill PDF forms")
	writeSkill(t, root, "skills/docx", "name: docx\ndescription: \"Edit Word documents\"")
	writeSkill(t, root, "skills/design/canvas", "name: canvas\ndescription: Make posters")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# skills\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "spec"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func newLoader(t *testing.T, f Fetcher) *Loader {
	t.Helper()
	return &Loader{
		CacheDir: filepath.Join(t.TempDir(), "plainhub"),
		Fetcher:  f,
		Limit:    Limit,
		Now:      func() time.Time { return time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC) },
	}
}

func TestLoadBuildsIndexFromRepoTree(t *testing.T) {
	f := &fakeFetcher{commit: "abc1234", dir: fixtureRepo(t)}
	l := newLoader(t, f)

	idx, err := l.Load("anthropics/skills", install.ParseOptions{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx.Repo != "anthropics/skills" || idx.Commit != "abc1234" || idx.Total != 3 || idx.Capped() {
		t.Fatalf("index meta = repo %q commit %q total %d capped %v", idx.Repo, idx.Commit, idx.Total, idx.Capped())
	}
	want := []Skill{
		{Name: "canvas", Description: "Make posters", Source: "github.com/anthropics/skills/skills/design/canvas"},
		{Name: "docx", Description: "Edit Word documents", Source: "github.com/anthropics/skills/skills/docx"},
		{Name: "pdf", Description: "Read and fill PDF forms", Source: "github.com/anthropics/skills/skills/pdf"},
	}
	if fmt.Sprint(idx.Skills) != fmt.Sprint(want) {
		t.Fatalf("skills =\n%v\nwant\n%v", idx.Skills, want)
	}
	// Each source must parse back to the one skill in the same repo.
	for _, s := range idx.Skills {
		src, err := install.ParseSource(s.Source)
		if err != nil || src.CloneURL != "https://github.com/anthropics/skills.git" || filepath.Base(src.Subdir) != s.Name {
			t.Errorf("source %q parses to clone %q subdir %q (err %v)", s.Source, src.CloneURL, src.Subdir, err)
		}
	}
}

func TestLoadKeepsWebURLRefInSources(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "pdf", "name: pdf\ndescription: PDFs")
	writeSkill(t, root, ".", "name: bundle\ndescription: The folder itself")
	f := &fakeFetcher{commit: "abc1234", dir: root}
	l := newLoader(t, f)

	idx, err := l.Load("https://github.com/anthropics/skills/tree/v2/skills/", install.ParseOptions{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx.Repo != "anthropics/skills/skills@v2" {
		t.Errorf("repo = %q", idx.Repo)
	}
	got := map[string]string{}
	for _, s := range idx.Skills {
		got[s.Name] = s.Source
	}
	if got["pdf"] != "https://github.com/anthropics/skills/tree/v2/skills/pdf" {
		t.Errorf("pdf source = %q", got["pdf"])
	}
	// A SKILL.md at the hub's own path installs as the hub's source itself.
	if src := got[filepath.Base(root)]; src != "https://github.com/anthropics/skills/tree/v2/skills" {
		t.Errorf("root skill source = %q (all: %v)", src, got)
	}
}

func TestLoadCachesPerCommit(t *testing.T) {
	f := &fakeFetcher{commit: "abc1234", dir: fixtureRepo(t)}
	l := newLoader(t, f)

	if _, err := l.Load("anthropics/skills", install.ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	// Another spelling of the same repo, and a fresh loader on the same
	// directory: the commit is unchanged, so the repo is not read again.
	again := &Loader{CacheDir: l.CacheDir, Fetcher: f, Limit: Limit, Now: time.Now}
	idx, err := again.Load("https://github.com/Anthropics/skills", install.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if f.discovers != 1 || f.heads != 2 || len(idx.Skills) != 3 {
		t.Fatalf("discovers %d heads %d skills %d; want 1, 2, 3", f.discovers, f.heads, len(idx.Skills))
	}

	// A new commit on the remote rebuilds the index.
	writeSkill(t, f.dir, "skills/xlsx", "name: xlsx\ndescription: Sheets")
	f.commit = "def5678"
	idx, err = l.Load("anthropics/skills", install.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if f.discovers != 2 || idx.Commit != "def5678" || len(idx.Skills) != 4 {
		t.Fatalf("after new commit: discovers %d commit %q skills %d", f.discovers, idx.Commit, len(idx.Skills))
	}

	// Peek reads what Load stored, without asking the remote.
	heads := f.heads
	peeked, ok := l.Peek("github.com/anthropics/skills")
	if !ok || peeked.Commit != "def5678" || f.heads != heads {
		t.Fatalf("Peek = %v %v, heads %d -> %d", peeked, ok, heads, f.heads)
	}
	if _, ok := l.Peek("anthropics/other"); ok {
		t.Error("Peek found an index for a repo never loaded")
	}

	// One file per repo, overwritten on rebuild.
	entries, _ := os.ReadDir(l.CacheDir)
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "anthropics_skills-") {
		t.Errorf("cache dir holds %v", entries)
	}
}

func TestLoadIgnoresUnreadableCache(t *testing.T) {
	f := &fakeFetcher{commit: "abc1234", dir: fixtureRepo(t)}
	l := newLoader(t, f)
	src, _ := parse("anthropics/skills")
	if err := os.MkdirAll(l.CacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"not json", `{"version":99,"key":"` + cacheKey(src) + `","commit":"abc1234","skills":[]}`} {
		if err := os.WriteFile(l.cachePath(cacheKey(src)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		idx, err := l.Load("anthropics/skills", install.ParseOptions{})
		if err != nil || len(idx.Skills) != 3 {
			t.Fatalf("Load over %q: %v, %v", body, idx, err)
		}
	}
	if f.discovers != 2 {
		t.Errorf("discovers = %d, want a rebuild for each bad file", f.discovers)
	}
}

func TestLoadCapsSkills(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 7; i++ {
		writeSkill(t, root, fmt.Sprintf("skills/s%d", i), fmt.Sprintf("name: s%d\ndescription: number %d", i, i))
	}
	f := &fakeFetcher{commit: "abc1234", dir: root}
	l := newLoader(t, f)
	l.Limit = 5

	idx, err := l.Load("owner/big", install.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !idx.Capped() || idx.Total != 7 || len(idx.Skills) != 5 || idx.Limit != 5 {
		t.Fatalf("capped %v total %d listed %d limit %d", idx.Capped(), idx.Total, len(idx.Skills), idx.Limit)
	}
	// The first five by path, so the cut is the same on every build.
	for i, s := range idx.Skills {
		if s.Name != fmt.Sprintf("s%d", i) {
			t.Errorf("skill %d = %s", i, s.Name)
		}
	}
	if peeked, ok := l.Peek("owner/big"); !ok || !peeked.Capped() {
		t.Error("the cap is not kept in the cache")
	}
}

func TestLoadErrors(t *testing.T) {
	t.Run("repo cannot be read", func(t *testing.T) {
		f := &fakeFetcher{headErr: errors.New("git failed (exit 128): repository not found or authentication required")}
		l := newLoader(t, f)
		_, err := l.Load("owner/missing", install.ParseOptions{})
		if err == nil || !strings.Contains(err.Error(), "owner/missing") || !strings.Contains(err.Error(), "public") ||
			!strings.Contains(err.Error(), "repository not found") {
			t.Fatalf("err = %v", err)
		}
		if f.discovers != 0 {
			t.Error("a repo whose commit could not be read was cloned")
		}
	})
	t.Run("no skills", func(t *testing.T) {
		f := &fakeFetcher{commit: "abc1234", dir: t.TempDir()}
		l := newLoader(t, f)
		_, err := l.Load("runkids/skillshare-hub", install.ParseOptions{})
		if err == nil || !strings.Contains(err.Error(), "no SKILL.md") || !strings.Contains(err.Error(), "skillshare-hub.json") {
			t.Fatalf("err = %v", err)
		}
		if _, ok := l.Peek("runkids/skillshare-hub"); ok {
			t.Error("an empty index was cached")
		}
	})
	t.Run("not a repo", func(t *testing.T) {
		l := newLoader(t, &fakeFetcher{})
		if _, err := l.Load("https://example.com/hub.json", install.ParseOptions{}); err == nil {
			t.Fatal("Load accepted a hub index URL")
		}
	})
}

// The git fetcher against a real local repository: ls-remote for the commit,
// then the install discovery clone. file:// keeps it off the network.
func TestGitFetcherReadsLocalRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := fixtureRepo(t)
	gitRun := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gitRun("init", "-q")
	gitRun("add", "-A")
	gitRun("commit", "-q", "-m", "skills")
	head := gitRun("rev-parse", "HEAD")

	src, err := install.ParseSource("file://" + filepath.ToSlash(repo))
	if err != nil {
		t.Fatal(err)
	}
	commit, err := gitFetcher{}.Head(src, install.ParseOptions{})
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if !strings.HasPrefix(head, commit) || commit == "" {
		t.Errorf("Head = %q, repo HEAD %q", commit, head)
	}
	skills, err := gitFetcher{}.Discover(src)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var names []string
	for _, s := range skills {
		names = append(names, s.Name+"="+s.Path)
	}
	if got := strings.Join(names, ","); !strings.Contains(got, "pdf=skills/pdf") || !strings.Contains(got, "canvas=skills/design/canvas") || len(skills) != 3 {
		t.Errorf("discovered %s", got)
	}
}
