// Fork-only (endgame302/skillshare): lists Claude-format subagents from curated directories.

package agentdir

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"skillshare/internal/forkmarket/ghrepo"
	"skillshare/internal/resource"
)

const (
	DefaultAitmplURL = "https://www.aitmpl.com/api/v1/catalog/agents.json"

	repoCacheTTL           = 12 * time.Hour
	aitmplCacheTTL         = 6 * time.Hour
	maxAgentFileBytes      = 256 << 10
	maxMatchedAgents       = 2000
	maxConcurrentDownloads = 8
	maxDescriptionRunes    = 600
)

// Scan is aitmpl's own static security scan of an agent (NVIDIA SkillSpector), shown as aitmpl's score, not skillshare's.
type Scan struct {
	Tool        string `json:"tool"`
	ToolVersion string `json:"toolVersion,omitempty"`
	Severity    string `json:"severity"`
	Score       *int   `json:"score,omitempty"`
	ScannedAt   string `json:"scannedAt,omitempty"`
}

// Agent is one subagent file a directory lists.
type Agent struct {
	ID          string   `json:"id"`          // "<source>:<path in repo>", unique within a listing
	Name        string   `json:"name"`        // file name without ".md": the name skillshare installs it under
	Title       string   `json:"title"`       // frontmatter name, else Name
	Description string   `json:"description"` // cleaned, at most 600 runes (see clean below)
	Category    string   `json:"category"`
	Repo        string   `json:"repo"` // owner/name holding the file
	Dir         string   `json:"dir"`  // folder of the file in Repo, slash-separated, no trailing slash
	File        string   `json:"file"` // file name in Dir, e.g. "api-designer.md"
	Tools       []string `json:"tools,omitempty"`
	Model       string   `json:"model,omitempty"`
	Downloads   *int     `json:"downloads,omitempty"` // aitmpl only
	Scan        *Scan    `json:"scan,omitempty"`      // aitmpl only
	PageURL     string   `json:"pageUrl,omitempty"`   // aitmpl page; for repo sources the GitHub blob URL at the commit
}

// Listing is one cached or fetched agent directory response.
type Listing struct {
	Source      string    `json:"source"`
	Repo        string    `json:"repo"`
	Commit      string    `json:"commit,omitempty"`      // repo sources: the commit listed
	GeneratedAt string    `json:"generatedAt,omitempty"` // aitmpl: the catalog's generatedAt
	Agents      []Agent   `json:"agents"`                // never nil
	FetchedAt   time.Time `json:"fetchedAt"`
	Stale       bool      `json:"stale"`
	StaleReason string    `json:"staleReason,omitempty"`
}

var ErrUnknownSource = errors.New("unknown agent directory")

// SourceIDs lists the known sources in display order: "voltagent", "wshobson", "aitmpl".
var SourceIDs = []string{"voltagent", "wshobson", "aitmpl"}

// Client fetches, caches, and serves subagent listings for the dashboard.
type Client struct {
	GH        *ghrepo.Client
	HTTP      *http.Client // for aitmpl
	AitmplURL string       // DefaultAitmplURL
	CacheDir  string
	Now       func() time.Time
}

// New creates a Client storing disk caches in cacheDir.
func New(cacheDir string) *Client {
	return &Client{
		GH: ghrepo.New(),
		HTTP: &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("aitmpl redirected to a non-https address")
			}
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		}},
		AitmplURL: DefaultAitmplURL,
		CacheDir:  cacheDir,
		Now:       time.Now,
	}
}

type repoSourceSpec struct {
	repo  string
	match func(ghrepo.Entry) (matchedEntry, bool)
}

var repoSources = map[string]repoSourceSpec{
	"voltagent": {repo: "VoltAgent/awesome-claude-code-subagents", match: matchVoltagent},
	"wshobson":  {repo: "wshobson/agents", match: matchWshobson},
}

// List returns the source's cached listing while it is fresh, else fetches it. refresh fetches even a fresh one.
// When fetching fails and a cached listing exists, that listing comes back with Stale set and StaleReason = err.Error().
func (c *Client) List(ctx context.Context, source string, refresh bool) (*Listing, error) {
	switch source {
	case "voltagent", "wshobson":
		return c.listRepo(ctx, source, repoSources[source], refresh)
	case "aitmpl":
		return c.listAitmpl(ctx, refresh)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownSource, source)
	}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) ghClient() *ghrepo.Client {
	if c.GH != nil {
		return c.GH
	}
	return ghrepo.New()
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) listRepo(ctx context.Context, source string, spec repoSourceSpec, refresh bool) (*Listing, error) {
	old := c.readCache(source)
	if old != nil && !refresh && c.now().Before(old.Listing.FetchedAt.Add(repoCacheTTL)) {
		l := old.Listing
		l.Agents = nonNilAgents(l.Agents)
		return &l, nil
	}

	listing, err := c.fetchRepo(ctx, source, spec, old)
	if err != nil {
		if old != nil {
			l := old.Listing
			l.Agents = nonNilAgents(l.Agents)
			l.Stale = true
			l.StaleReason = err.Error()
			return &l, nil
		}
		return nil, err
	}
	c.writeCache(source, listing, "")
	return listing, nil
}

func (c *Client) fetchRepo(ctx context.Context, source string, spec repoSourceSpec, old *diskCache) (*Listing, error) {
	gh := c.ghClient()
	commit, err := gh.Head(ctx, spec.repo, "HEAD")
	if err != nil {
		return nil, err
	}
	if old != nil && old.Listing.Commit == commit {
		l := old.Listing
		l.Repo = spec.repo
		l.Agents = nonNilAgents(l.Agents)
		l.FetchedAt = c.now().UTC()
		l.Stale = false
		l.StaleReason = ""
		return &l, nil
	}

	entries, err := gh.Tree(ctx, spec.repo, commit)
	if err != nil {
		return nil, err
	}

	matched := make([]matchedEntry, 0)
	for _, e := range entries {
		if m, ok := spec.match(e); ok {
			matched = append(matched, m)
		}
	}
	if len(matched) > maxMatchedAgents {
		return nil, fmt.Errorf("%s lists %d agents, over the %d limit", spec.repo, len(matched), maxMatchedAgents)
	}
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].entry.Path < matched[j].entry.Path
	})

	agents := make([]Agent, len(matched))
	if len(matched) > 0 {
		dlCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		jobs := make(chan int, len(matched))
		for i := range matched {
			jobs <- i
		}
		close(jobs)

		var (
			wg       sync.WaitGroup
			errOnce  sync.Once
			firstErr error
		)
		workers := maxConcurrentDownloads
		if len(matched) < workers {
			workers = len(matched)
		}
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range jobs {
					if dlCtx.Err() != nil {
						return
					}
					m := matched[idx]
					data, dlErr := gh.RawFile(dlCtx, spec.repo, commit, m.entry.Path, maxAgentFileBytes)
					if dlErr != nil {
						errOnce.Do(func() {
							firstErr = fmt.Errorf("downloading %s: %w", m.entry.Path, dlErr)
							cancel()
						})
						return
					}
					agents[idx] = buildRepoAgent(source, spec.repo, commit, m, data)
				}
			}()
		}
		wg.Wait()
		if firstErr != nil {
			return nil, firstErr
		}
	}

	return &Listing{
		Source:    source,
		Repo:      spec.repo,
		Commit:    commit,
		Agents:    agents,
		FetchedAt: c.now().UTC(),
	}, nil
}

func buildRepoAgent(source, repo, commit string, m matchedEntry, data []byte) Agent {
	name := m.file[:len(m.file)-3]
	fm := parseFrontmatter(data)
	title := strings.TrimSpace(fm.Name)
	if title == "" {
		title = name
	}
	return Agent{
		ID:          source + ":" + m.entry.Path,
		Name:        name,
		Title:       title,
		Description: cleanDescription(fm.Description),
		Category:    m.category,
		Repo:        repo,
		Dir:         m.dir,
		File:        m.file,
		Tools:       fm.Tools,
		Model:       strings.TrimSpace(fm.Model),
		PageURL:     "https://github.com/" + repo + "/blob/" + commit + "/" + ghrepo.EscapePathSegments(m.entry.Path),
	}
}

type matchedEntry struct {
	entry    ghrepo.Entry
	category string
	dir      string
	file     string
}

func isValidAgentBlob(e ghrepo.Entry) bool {
	if e.Type != "blob" {
		return false
	}
	if e.Mode != "100644" && e.Mode != "100755" {
		return false
	}
	return e.Size >= 0 && e.Size <= maxAgentFileBytes
}

func isValidAgentFile(file string) bool {
	if file == "" || strings.HasPrefix(file, ".") {
		return false
	}
	if len(file) <= 3 || !strings.HasSuffix(strings.ToLower(file), ".md") {
		return false
	}
	return !resource.ConventionalExcludes[file]
}

func matchVoltagent(e ghrepo.Entry) (matchedEntry, bool) {
	if !isValidAgentBlob(e) {
		return matchedEntry{}, false
	}
	parts := strings.Split(e.Path, "/")
	if len(parts) != 3 || parts[0] != "categories" {
		return matchedEntry{}, false
	}
	cat, file := parts[1], parts[2]
	if cat == "" || strings.HasPrefix(cat, ".") || !isValidAgentFile(file) {
		return matchedEntry{}, false
	}
	return matchedEntry{
		entry:    e,
		category: stripCategoryPrefix(cat),
		dir:      "categories/" + cat,
		file:     file,
	}, true
}

func matchWshobson(e ghrepo.Entry) (matchedEntry, bool) {
	if !isValidAgentBlob(e) {
		return matchedEntry{}, false
	}
	parts := strings.Split(e.Path, "/")
	if len(parts) != 4 || parts[0] != "plugins" || parts[2] != "agents" {
		return matchedEntry{}, false
	}
	plugin, file := parts[1], parts[3]
	if plugin == "" || strings.HasPrefix(plugin, ".") || !isValidAgentFile(file) {
		return matchedEntry{}, false
	}
	return matchedEntry{
		entry:    e,
		category: plugin,
		dir:      "plugins/" + plugin + "/agents",
		file:     file,
	}, true
}

func stripCategoryPrefix(cat string) string {
	i := 0
	for i < len(cat) && cat[i] >= '0' && cat[i] <= '9' {
		i++
	}
	if i > 0 && i < len(cat) && cat[i] == '-' && i+1 < len(cat) {
		return cat[i+1:]
	}
	return cat
}

type parsedFrontmatter struct {
	Name        string
	Description string
	Tools       []string
	Model       string
}

func parseFrontmatter(data []byte) parsedFrontmatter {
	s := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) < 2 || lines[0] != "---" {
		return parsedFrontmatter{}
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return parsedFrontmatter{}
	}
	var raw struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Tools       any    `yaml:"tools"`
		Model       string `yaml:"model"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &raw); err != nil {
		// Hand-written frontmatter is often not valid YAML (an unquoted colon in a description);
		// its one-line keys are still worth showing.
		return looseFrontmatter(lines[1:end])
	}
	return parsedFrontmatter{
		Name:        raw.Name,
		Description: raw.Description,
		Tools:       parseTools(raw.Tools),
		Model:       raw.Model,
	}
}

// looseFrontmatter reads the top-level one-line `key: value` pairs of frontmatter YAML could not parse.
func looseFrontmatter(lines []string) parsedFrontmatter {
	var fm parsedFrontmatter
	for _, line := range lines {
		key, value, ok := strings.Cut(line, ":")
		if !ok || key == "" || key != strings.TrimSpace(key) {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		switch key {
		case "name":
			fm.Name = value
		case "description":
			fm.Description = value
		case "model":
			fm.Model = value
		case "tools":
			fm.Tools = parseTools(value)
		}
	}
	return fm
}

func parseTools(v any) []string {
	var out []string
	switch x := v.(type) {
	case string:
		for _, part := range strings.Split(x, ",") {
			if s := strings.TrimSpace(part); s != "" {
				out = append(out, s)
			}
		}
	case []any:
		for _, elem := range x {
			if s, ok := elem.(string); ok {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					out = append(out, trimmed)
				}
			}
		}
	case []string:
		for _, s := range x {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

var exampleTagRe = regexp.MustCompile(`(?i)<example>`)

func cleanDescription(s string) string {
	if loc := exampleTagRe.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	s = strings.ReplaceAll(s, `\n`, " ")
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > maxDescriptionRunes {
		return string(runes[:maxDescriptionRunes-1]) + "…"
	}
	return s
}

func nonNilAgents(agents []Agent) []Agent {
	if agents == nil {
		return []Agent{}
	}
	return agents
}

type diskCache struct {
	Listing Listing `json:"listing"`
	ETag    string  `json:"etag,omitempty"`
}

func (c *Client) cachePath(source string) string {
	return filepath.Join(c.CacheDir, source+".json")
}

func (c *Client) readCache(source string) *diskCache {
	if c.CacheDir == "" {
		return nil
	}
	data, err := os.ReadFile(c.cachePath(source))
	if err != nil {
		return nil
	}
	var dc diskCache
	if json.Unmarshal(data, &dc) != nil || dc.Listing.Source != source || dc.Listing.FetchedAt.IsZero() {
		return nil
	}
	dc.Listing.Agents = nonNilAgents(dc.Listing.Agents)
	return &dc
}

// writeCache replaces the cache file atomically. A cache that cannot be written only costs a fetch next time.
func (c *Client) writeCache(source string, listing *Listing, etag string) {
	if c.CacheDir == "" || listing == nil {
		return
	}
	data, err := json.Marshal(diskCache{Listing: *listing, ETag: etag})
	if err != nil || os.MkdirAll(c.CacheDir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(c.CacheDir, source+".json.*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil || os.Rename(tmp.Name(), c.cachePath(source)) != nil {
		os.Remove(tmp.Name())
	}
}
