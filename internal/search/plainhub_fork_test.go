package search

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"skillshare/internal/install"
	"skillshare/internal/plainhub"
)

func stubPlainRepo(t *testing.T, fn func(hub string, opts install.ParseOptions) (*plainhub.Index, error)) {
	t.Helper()
	orig := loadPlainRepo
	loadPlainRepo = fn
	t.Cleanup(func() { loadPlainRepo = orig })
}

func TestSearchFromIndexURLPlainRepo(t *testing.T) {
	var asked []string
	stubPlainRepo(t, func(hub string, _ install.ParseOptions) (*plainhub.Index, error) {
		asked = append(asked, hub)
		return &plainhub.Index{Skills: []plainhub.Skill{
			{Name: "pdf", Description: "Read and fill PDF forms", Source: "github.com/anthropics/skills/skills/pdf"},
			{Name: "docx", Description: "Edit Word documents", Source: "github.com/anthropics/skills/skills/docx"},
		}}, nil
	})

	all, err := SearchFromIndexURL("", 0, "anthropics/skills", install.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Name != "docx" || all[1].Owner != "anthropics" || all[1].Repo != "skills" {
		t.Fatalf("results = %+v", all)
	}

	hits, err := SearchFromIndexURL("spreadsheet", 0, "https://github.com/anthropics/skills", install.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("an unrelated query matched %+v", hits)
	}
	hits, err = SearchFromIndexURL("Fill PDF", 0, "https://github.com/anthropics/skills", install.ParseOptions{})
	if err != nil || len(hits) != 1 || hits[0].Source != "github.com/anthropics/skills/skills/pdf" {
		t.Fatalf("hits = %+v, err %v", hits, err)
	}
	if len(asked) != 3 {
		t.Errorf("loader asked %d times", len(asked))
	}
}

func TestSearchFromIndexURLPlainRepoError(t *testing.T) {
	stubPlainRepo(t, func(string, install.ParseOptions) (*plainhub.Index, error) {
		return nil, errors.New("could not read GitHub repo owner/missing")
	})
	if _, err := SearchFromIndexURL("", 0, "owner/missing", install.ParseOptions{}); err == nil || !strings.Contains(err.Error(), "owner/missing") {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchFromIndexURLIndexFileSkipsPlainRepo(t *testing.T) {
	stubPlainRepo(t, func(hub string, _ install.ParseOptions) (*plainhub.Index, error) {
		t.Fatalf("a hub index file went to the repo indexer: %s", hub)
		return nil, nil
	})
	path := filepath.Join(t.TempDir(), "skillshare-hub.json")
	if err := os.WriteFile(path, []byte(`{"skills":[{"name":"a","source":"owner/repo/a"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := SearchFromIndexURL("", 0, path, install.ParseOptions{})
	if err != nil || len(results) != 1 {
		t.Fatalf("results %+v err %v", results, err)
	}
}

type capFetcher struct{ skills []install.SkillInfo }

func (capFetcher) Head(*install.Source, install.ParseOptions) (string, error) { return "abc1234", nil }
func (f capFetcher) Discover(*install.Source) ([]install.SkillInfo, error)    { return f.skills, nil }

func TestPlainRepoNote(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if note := PlainRepoNote("owner/big"); note != "" {
		t.Errorf("note before any load = %q", note)
	}
	l := plainhub.Default()
	l.Fetcher = capFetcher{skills: []install.SkillInfo{{Name: "a", Path: "a"}, {Name: "b", Path: "b"}, {Name: "c", Path: "c"}}}
	l.Limit = 2
	if _, err := l.Load("owner/big", install.ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	if note := PlainRepoNote("owner/big"); !strings.Contains(note, "3 skills") || !strings.Contains(note, "first 2") {
		t.Errorf("note = %q", note)
	}
	l.Limit = 0
	if _, err := l.Load("owner/small", install.ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	if note := PlainRepoNote("owner/small"); note != "" {
		t.Errorf("uncapped note = %q", note)
	}
	if note := PlainRepoNote("https://example.com/hub.json"); note != "" {
		t.Errorf("index file note = %q", note)
	}
}
