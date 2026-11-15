// Package plainhub lets a plain GitHub skills repo serve as a hub (fork-only,
// endgame302/skillshare). A saved hub normally points at a skillshare-hub.json
// index. For a repo reference such as "anthropics/skills" there is no index, so
// one is built from the repo's SKILL.md files, with the same discovery an
// install uses, and kept on disk per repo and commit.
package plainhub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"skillshare/internal/config"
	"skillshare/internal/git"
	"skillshare/internal/install"
)

// Limit is how many skills one repo contributes at most, so a huge monorepo
// stays quick to load and browse. The index records how many there were.
const Limit = 500

// cacheVersion changes when the cached file's layout or meaning does, so an
// older file is rebuilt instead of misread.
const cacheVersion = 1

// Skill is one entry of a generated index, in the hub index's field names.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
}

// Index is a generated hub index and what it was built from.
type Index struct {
	Version int    `json:"version"`
	Key     string `json:"key"`
	// Repo names the repo for people: owner/repo, then the subdirectory and ref when given.
	Repo      string    `json:"repo"`
	Commit    string    `json:"commit"`
	IndexedAt time.Time `json:"indexedAt"`
	// Total counts the skills found before Limit cut the list.
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Skills []Skill `json:"skills"`
}

// Capped reports whether the repo had more skills than the index lists.
func (i *Index) Capped() bool { return i.Total > len(i.Skills) }

// Fetcher reads a repo from its remote. The git implementation is the default;
// tests substitute their own so they never touch the network.
type Fetcher interface {
	// Head returns the commit the source's ref (or the default branch) points at.
	// It may settle a web URL's ref on src.
	Head(src *install.Source, opts install.ParseOptions) (string, error)
	// Discover lists the skills in the source at its ref.
	Discover(src *install.Source) ([]install.SkillInfo, error)
}

// Loader builds and caches indexes. The zero value is not usable; see Default.
type Loader struct {
	CacheDir string
	Fetcher  Fetcher
	Limit    int
	Now      func() time.Time
}

// Default uses git and skillshare's cache directory.
func Default() *Loader {
	return &Loader{
		CacheDir: filepath.Join(config.CacheDir(), "plainhub"),
		Fetcher:  gitFetcher{},
		Limit:    Limit,
		Now:      time.Now,
	}
}

// Load returns the index for hub with the default loader.
func Load(hub string, opts install.ParseOptions) (*Index, error) {
	return Default().Load(hub, opts)
}

// Peek returns the last index built for hub with the default loader, without
// reaching the network.
func Peek(hub string) (*Index, bool) {
	return Default().Peek(hub)
}

// blobOrRaw matches a GitHub link to one file, which is never a repo hub.
var blobOrRaw = regexp.MustCompile(`^(?:https?://)?github\.com/[^/]+/[^/]+/(?:blob|raw)/`)

// Detect reports whether hub names a GitHub repo rather than a hub index file:
// owner/repo, github.com/owner/repo or https://github.com/owner/repo, with an
// optional subdirectory or /tree/<ref>/<path>. A .json file, a GitHub blob or
// raw link, an SSH or file:// source and a path that exists on disk keep their
// usual meaning.
func Detect(hub string) bool {
	_, ok := parse(hub)
	return ok
}

func parse(hub string) (*install.Source, bool) {
	// A trailing slash, as a pasted browser URL often has, would stop the GitHub
	// pattern from matching.
	s := strings.TrimRight(strings.TrimSpace(hub), "/")
	lower := strings.ToLower(s)
	if s == "" || install.IsSSHURL(s) || strings.HasPrefix(lower, "file://") {
		return nil, false
	}
	file := lower
	if i := strings.IndexAny(file, "?#"); i >= 0 {
		file = file[:i]
	}
	if strings.HasSuffix(strings.TrimRight(file, "/"), ".json") {
		return nil, false
	}
	if _, err := os.Stat(s); err == nil {
		return nil, false
	}
	src, err := install.ParseSource(s)
	if err != nil || src.Type != install.SourceTypeGitHub || src.ExplicitSkill || blobOrRaw.MatchString(src.Raw) {
		return nil, false
	}
	return src, true
}

// Load returns the index for hub. It asks the remote for the current commit
// and rebuilds only when that differs from the cached index, so refreshing a
// hub picks up new commits while an unchanged repo is never cloned again.
func (l *Loader) Load(hub string, opts install.ParseOptions) (*Index, error) {
	src, ok := parse(hub)
	if !ok {
		return nil, fmt.Errorf("%q is not a GitHub repo", hub)
	}
	key := cacheKey(src)
	mu := lockFor(key)
	mu.Lock()
	defer mu.Unlock()

	commit, err := l.Fetcher.Head(src, opts)
	if err != nil {
		return nil, repoError(repoLabel(src), err)
	}
	// Head may have settled a web URL's ref and subdirectory.
	label := repoLabel(src)
	if cached, ok := l.read(key); ok && cached.Commit == commit {
		return cached, nil
	}
	found, err := l.Fetcher.Discover(src)
	if err != nil {
		return nil, repoError(label, err)
	}
	idx := build(src, key, label, commit, found, l.Limit, l.Now())
	if idx.Total == 0 {
		return nil, fmt.Errorf("no SKILL.md files found in GitHub repo %s; if it holds a skillshare-hub.json, add that file's raw URL as the hub instead", label)
	}
	// Best effort: a cache that cannot be written only costs a rebuild next time.
	_ = l.write(idx)
	return idx, nil
}

// Peek returns the last index built for hub, read from the cache only.
func (l *Loader) Peek(hub string) (*Index, bool) {
	src, ok := parse(hub)
	if !ok {
		return nil, false
	}
	return l.read(cacheKey(src))
}

// build turns discovered skills into an index, sorted by path and cut at limit
// (0 means no limit). Each source installs that one skill from the repo, at the
// ref the hub names.
func build(src *install.Source, key, label, commit string, found []install.SkillInfo, limit int, now time.Time) *Index {
	sorted := append([]install.SkillInfo(nil), found...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	idx := &Index{
		Version:   cacheVersion,
		Key:       key,
		Repo:      label,
		Commit:    commit,
		IndexedAt: now.UTC(),
		Total:     len(sorted),
		Limit:     limit,
		Skills:    make([]Skill, 0, len(sorted)),
	}
	for _, sk := range sorted {
		if limit > 0 && len(idx.Skills) >= limit {
			break
		}
		idx.Skills = append(idx.Skills, Skill{
			Name:        sk.Name,
			Description: strings.TrimSpace(sk.Description),
			Source:      skillSource(src, sk.Path),
		})
	}
	return idx
}

// skillSource is the install source of the skill at path inside src, in the
// form discovery's own installs record for a GitHub repo: the hub's source
// with the path appended. A web URL keeps its /tree/<ref>/, so the skill
// installs from the same ref.
func skillSource(src *install.Source, path string) string {
	base := strings.TrimRight(src.Raw, "/")
	if path == "" || path == "." {
		return base
	}
	return base + "/" + path
}

func repoLabel(src *install.Source) string {
	label := src.GitHubOwner() + "/" + src.GitHubRepo()
	if src.Subdir != "" {
		label += "/" + src.Subdir
	}
	if src.Branch != "" {
		label += "@" + src.Branch
	}
	return label
}

// repoError says what most often goes wrong with a repo hub, then git's own
// account of it.
func repoError(label string, err error) error {
	return fmt.Errorf("could not read GitHub repo %s — check that it exists and is public, or set GITHUB_TOKEN for a private repo: %w", label, err)
}

// cacheKey identifies the hub's repo, ref and subdirectory as written, so two
// spellings of one repo share an index. It is computed before any network
// call, which keeps Peek offline.
func cacheKey(src *install.Source) string {
	return strings.ToLower(src.CloneURL) + "#" + src.Branch + "//" + src.Subdir
}

var locks sync.Map

// lockFor serializes loads of one key, so two requests for a hub that is not
// cached yet clone it once.
func lockFor(key string) *sync.Mutex {
	mu, _ := locks.LoadOrStore(key, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

var unsafeName = regexp.MustCompile(`[^a-z0-9._-]+`)

// cachePath is one file per key, overwritten on each rebuild, so the cache
// holds the latest commit of each repo and never grows with history.
func (l *Loader) cachePath(key string) string {
	sum := sha256.Sum256([]byte(key))
	base := strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(key, "#", 2)[0], "https://github.com/"), ".git")
	name := strings.Trim(unsafeName.ReplaceAllString(base, "_"), "_.")
	if len(name) > 60 {
		name = name[:60]
	}
	return filepath.Join(l.CacheDir, name+"-"+hex.EncodeToString(sum[:])[:12]+".json")
}

func (l *Loader) read(key string) (*Index, bool) {
	data, err := os.ReadFile(l.cachePath(key))
	if err != nil {
		return nil, false
	}
	var idx Index
	if json.Unmarshal(data, &idx) != nil || idx.Version != cacheVersion || idx.Key != key {
		return nil, false
	}
	return &idx, true
}

func (l *Loader) write(idx *Index) error {
	if err := os.MkdirAll(l.CacheDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(l.CacheDir, ".plainhub-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmp.Name(), l.cachePath(idx.Key)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// gitFetcher reads the repo with git, as installs do: ls-remote for the
// commit, then a shallow (sparse for a subdirectory) clone for discovery.
// Neither uses the GitHub REST API, so its rate limit does not apply.
type gitFetcher struct{}

func (gitFetcher) Head(src *install.Source, opts install.ParseOptions) (string, error) {
	// A missing or private repo makes GitHub ask for credentials. Git Credential
	// Manager would then wait on a login prompt nobody sees, for minutes;
	// GCM_INTERACTIVE=never makes it use stored credentials only and fail at once.
	env := append(install.AuthEnvForURL(src.CloneURL, opts), "GCM_INTERACTIVE=never")
	if src.HasAmbiguousWebRef() {
		// Settling a /tree/<ref>/ URL lists the remote's refs without that
		// guard, so first make sure the repo can be read at all.
		if _, err := git.GetRemoteRefHashWithEnv(src.CloneURL, "", env); err != nil {
			return "", err
		}
		if err := (&install.RemoteRefs{}).Resolve(src); err != nil {
			return "", err
		}
	}
	return git.GetRemoteRefHashWithEnv(src.CloneURL, src.Branch, env)
}

func (gitFetcher) Discover(src *install.Source) ([]install.SkillInfo, error) {
	var (
		d   *install.DiscoveryResult
		err error
	)
	if src.HasSubdir() {
		d, err = install.DiscoverFromGitSubdir(src)
	} else {
		d, err = install.DiscoverFromGit(src)
	}
	if err != nil {
		return nil, err
	}
	defer install.CleanupDiscovery(d)
	return d.Skills, nil
}
