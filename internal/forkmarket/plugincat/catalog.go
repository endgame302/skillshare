// Fork-only (endgame302/skillshare): reads and translates Claude plugin marketplace catalogs.

package plugincat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"skillshare/internal/forkmarket/ghrepo"
)

const (
	CatalogFile = ".claude-plugin/marketplace.json"

	cacheTTL            = 6 * time.Hour
	maxCatalogBytes     = 16 << 20 // 16 MiB
	maxDescriptionRunes = 600
	maxNameBytes        = 128
)

type CatalogDef struct {
	ID   string `json:"id"`
	Repo string `json:"repo"`
}

// Catalogs in display order.
var Catalogs = []CatalogDef{
	{ID: "official", Repo: "anthropics/claude-plugins-official"},
	{ID: "community", Repo: "anthropics/claude-plugins-community"},
	{ID: "wshobson", Repo: "wshobson/agents"},
}

var ErrUnknownCatalog = errors.New("unknown plugin catalog")

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// Source is an entry's source as the catalog writes it, normalised.
type Source struct {
	Kind    string `json:"kind"`           // "path", or the catalog's own word: github, url, git-subdir, npm, command, ...
	Path    string `json:"path,omitempty"` // path: slash path inside the catalog repo ("plugins/x"); git-subdir: its path
	URL     string `json:"url,omitempty"`
	Repo    string `json:"repo,omitempty"` // github
	Ref     string `json:"ref,omitempty"`
	SHA     string `json:"sha,omitempty"`     // set only when it is 40 lowercase hex
	Package string `json:"package,omitempty"` // npm
	Version string `json:"version,omitempty"` // npm
	Command string `json:"command,omitempty"` // command
}

type Entry struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName,omitempty"`
	Description string   `json:"description"`
	Category    string   `json:"category,omitempty"`
	Author      string   `json:"author,omitempty"`
	Homepage    string   `json:"homepage,omitempty"` // only http(s) URLs are kept
	Version     string   `json:"version,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Source      Source   `json:"source"`
	Install     string   `json:"install"` // "add", "native", "manual", or "none" (Problem says why)
	Pinned      bool     `json:"pinned"`  // add: always (the catalog commit pins it); native: SHA set
	Problem     string   `json:"problem,omitempty"`
}

type Catalog struct {
	ID          string            `json:"id"`
	Repo        string            `json:"repo"`
	Market      string            `json:"market"`  // the catalog's "name": Claude's marketplace name
	Commit      string            `json:"commit"`  // the catalog repo commit the file was read at
	Entries     []Entry           `json:"entries"` // never nil, catalog order
	Renames     map[string]string `json:"renames,omitempty"`
	FetchedAt   time.Time         `json:"fetchedAt"`
	Stale       bool              `json:"stale"`
	StaleReason string            `json:"staleReason,omitempty"`
}

// Lookup finds an entry by name.
func (cat *Catalog) Lookup(name string) (Entry, bool) {
	if cat == nil {
		return Entry{}, false
	}
	for _, e := range cat.Entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// ParseCatalog reads a marketplace.json. Errors: not JSON, or a "name" that does not match namePattern.
func ParseCatalog(data []byte) (market string, entries []Entry, renames map[string]string, err error) {
	var raw struct {
		Name    string                     `json:"name"`
		Renames map[string]json.RawMessage `json:"renames"`
		Plugins []json.RawMessage          `json:"plugins"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", nil, nil, fmt.Errorf("parsing marketplace catalog: %w", err)
	}
	if !validName(raw.Name) {
		return "", nil, nil, fmt.Errorf("invalid marketplace name %q", raw.Name)
	}

	entries = make([]Entry, 0, len(raw.Plugins))
	seen := make(map[string]bool, len(raw.Plugins))
	for _, item := range raw.Plugins {
		var rp struct {
			Name        string          `json:"name"`
			DisplayName string          `json:"displayName"`
			Description string          `json:"description"`
			Category    string          `json:"category"`
			Author      json.RawMessage `json:"author"`
			Homepage    string          `json:"homepage"`
			Version     string          `json:"version"`
			Keywords    []string        `json:"keywords"`
			Tags        []string        `json:"tags"`
			Source      json.RawMessage `json:"source"`
		}
		if json.Unmarshal(item, &rp) != nil {
			continue
		}
		if !validName(rp.Name) || seen[rp.Name] {
			continue
		}
		seen[rp.Name] = true

		kw := cleanKeywords(rp.Keywords)
		if len(kw) == 0 {
			kw = cleanKeywords(rp.Tags)
		}

		src, install, pinned, problem := Translate(rp.Source)
		entries = append(entries, Entry{
			Name:        rp.Name,
			DisplayName: strings.TrimSpace(rp.DisplayName),
			Description: cleanDescription(rp.Description),
			Category:    strings.TrimSpace(rp.Category),
			Author:      parseAuthor(rp.Author),
			Homepage:    cleanHomepage(rp.Homepage),
			Version:     strings.TrimSpace(rp.Version),
			Keywords:    kw,
			Source:      src,
			Install:     install,
			Pinned:      pinned,
			Problem:     problem,
		})
	}

	for k, vRaw := range raw.Renames {
		var v string
		if json.Unmarshal(vRaw, &v) == nil && validName(k) && validName(v) {
			if renames == nil {
				renames = make(map[string]string)
			}
			renames[k] = v
		}
	}

	return raw.Name, entries, renames, nil
}

// Translate turns one entry's raw "source" into a Source plus how it installs (Install, Pinned, Problem).
func Translate(raw json.RawMessage) (src Source, install string, pinned bool, problem string) {
	rawTrim := bytes.TrimSpace(raw)
	if len(rawTrim) == 0 || bytes.Equal(rawTrim, []byte("null")) {
		return Source{Kind: "unknown"}, "none", false, "unknown source"
	}
	if rawTrim[0] == '"' {
		var rel string
		if err := json.Unmarshal(rawTrim, &rel); err != nil {
			return Source{Kind: "unknown"}, "none", false, "unknown source"
		}
		return translateRel(rel)
	}
	if rawTrim[0] != '{' {
		return Source{Kind: "unknown"}, "none", false, "unknown source"
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(rawTrim, &obj); err != nil {
		return Source{Kind: "unknown"}, "none", false, "unknown source"
	}

	rawKind, ok := jsonString(obj, "source")
	kind := strings.ToLower(strings.TrimSpace(rawKind))
	if !ok || kind == "" {
		return Source{Kind: "unknown"}, "none", false, "unknown source"
	}

	if kind == "local" {
		rel, _ := jsonString(obj, "path")
		return translateRel(rel)
	}
	if kind == "url" {
		u, _ := jsonString(obj, "url")
		if u == "." || strings.HasPrefix(u, "./") {
			return translateRel(u)
		}
	}

	sha, _ := jsonString(obj, "sha")
	if !isHex40Lower(sha) {
		sha = ""
	}
	p, _ := jsonString(obj, "path")
	u, _ := jsonString(obj, "url")
	repo, _ := jsonString(obj, "repo")
	ref, _ := jsonString(obj, "ref")
	pkg, _ := jsonString(obj, "package")
	ver, _ := jsonString(obj, "version")
	cmd, _ := jsonString(obj, "command")

	src = Source{
		Kind:    kind,
		Path:    p,
		URL:     u,
		Repo:    repo,
		Ref:     ref,
		SHA:     sha,
		Package: pkg,
		Version: ver,
		Command: cmd,
	}
	if kind == "command" {
		return src, "manual", false, ""
	}
	return src, "native", src.SHA != "", ""
}

func translateRel(rel string) (Source, string, bool, string) {
	if strings.Contains(rel, "\\") {
		return Source{Kind: "path"}, "none", false, "path uses backslashes"
	}
	if rel == "" {
		return Source{Kind: "path"}, "none", false, "empty path"
	}
	if path.IsAbs(rel) {
		return Source{Kind: "path"}, "none", false, "path escapes the catalog"
	}
	cleaned := path.Clean(rel)
	if path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return Source{Kind: "path"}, "none", false, "path escapes the catalog"
	}
	return Source{Kind: "path", Path: cleaned}, "add", true, ""
}

func jsonString(m map[string]json.RawMessage, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	return s, true
}

func validName(s string) bool {
	return len(s) > 0 && len(s) <= maxNameBytes && namePattern.MatchString(s)
}

func isHex40Lower(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < 40; i++ {
		b := s[i]
		if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f')) {
			return false
		}
	}
	return true
}

func parseAuthor(raw json.RawMessage) string {
	rawTrim := bytes.TrimSpace(raw)
	if len(rawTrim) == 0 || bytes.Equal(rawTrim, []byte("null")) {
		return ""
	}
	var s string
	if json.Unmarshal(rawTrim, &s) == nil {
		return strings.TrimSpace(s)
	}
	var obj struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(rawTrim, &obj) == nil {
		return strings.TrimSpace(obj.Name)
	}
	return ""
}

func cleanHomepage(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return ""
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return ""
	}
	return s
}

func cleanDescription(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > maxDescriptionRunes {
		return string(runes[:maxDescriptionRunes-1]) + "…"
	}
	return s
}

func cleanKeywords(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	var out []string
	for _, item := range in {
		if s := strings.TrimSpace(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func nonNilEntries(entries []Entry) []Entry {
	if entries == nil {
		return []Entry{}
	}
	return entries
}

type Client struct {
	GH       *ghrepo.Client
	CacheDir string
	Now      func() time.Time
}

func New(cacheDir string) *Client {
	return &Client{
		GH:       ghrepo.New(),
		CacheDir: cacheDir,
		Now:      time.Now,
	}
}

func catalogDef(id string) (CatalogDef, bool) {
	for _, d := range Catalogs {
		if d.ID == id {
			return d, true
		}
	}
	return CatalogDef{}, false
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

// Catalog returns the cached catalog while fresh (6 h), else fetches: GH.Head(repo,"HEAD"); when the cached Commit equals
// the head, reuse its entries (no download) with FetchedAt = Now; else GH.RawFile(repo, head, CatalogFile, 16 MiB) and
// parse. On failure with a cached copy: return it with Stale and StaleReason = err.Error(); without: the error.
func (c *Client) Catalog(ctx context.Context, id string, refresh bool) (*Catalog, error) {
	def, ok := catalogDef(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCatalog, id)
	}

	old := c.readCache(id)
	if old != nil && !refresh && c.now().Before(old.FetchedAt.Add(cacheTTL)) {
		cat := *old
		cat.Entries = nonNilEntries(cat.Entries)
		cat.Stale = false
		cat.StaleReason = ""
		return &cat, nil
	}

	cat, err := c.fetch(ctx, def, old)
	if err != nil {
		if old != nil {
			cat := *old
			cat.Entries = nonNilEntries(cat.Entries)
			cat.Stale = true
			cat.StaleReason = err.Error()
			return &cat, nil
		}
		return nil, err
	}
	c.writeCache(id, cat)
	return cat, nil
}

func (c *Client) fetch(ctx context.Context, def CatalogDef, old *Catalog) (*Catalog, error) {
	gh := c.ghClient()
	head, err := gh.Head(ctx, def.Repo, "HEAD")
	if err != nil {
		return nil, err
	}
	if old != nil && old.Commit == head {
		cat := *old
		cat.ID = def.ID
		cat.Repo = def.Repo
		cat.Entries = nonNilEntries(cat.Entries)
		cat.FetchedAt = c.now().UTC()
		cat.Stale = false
		cat.StaleReason = ""
		return &cat, nil
	}

	data, err := gh.RawFile(ctx, def.Repo, head, CatalogFile, maxCatalogBytes)
	if err != nil {
		return nil, err
	}
	market, entries, renames, err := ParseCatalog(data)
	if err != nil {
		return nil, err
	}
	return &Catalog{
		ID:        def.ID,
		Repo:      def.Repo,
		Market:    market,
		Commit:    head,
		Entries:   nonNilEntries(entries),
		Renames:   renames,
		FetchedAt: c.now().UTC(),
	}, nil
}

// Cached returns the cached catalog without any network request (nil when there is none). The hand-off route uses it so
// it acts on exactly what the user was shown.
func (c *Client) Cached(id string) *Catalog {
	if _, ok := catalogDef(id); !ok {
		return nil
	}
	return c.readCache(id)
}

type diskCache struct {
	Catalog Catalog `json:"catalog"`
}

func (c *Client) cachePath(id string) string {
	return filepath.Join(c.CacheDir, id+".json")
}

func (c *Client) readCache(id string) *Catalog {
	if c.CacheDir == "" {
		return nil
	}
	data, err := os.ReadFile(c.cachePath(id))
	if err != nil {
		return nil
	}
	var dc diskCache
	if json.Unmarshal(data, &dc) != nil || dc.Catalog.ID != id || dc.Catalog.FetchedAt.IsZero() {
		return nil
	}
	dc.Catalog.Entries = nonNilEntries(dc.Catalog.Entries)
	return &dc.Catalog
}

// writeCache replaces the cache file atomically. A cache that cannot be written only costs a fetch next time.
func (c *Client) writeCache(id string, cat *Catalog) {
	if c.CacheDir == "" || cat == nil {
		return
	}
	data, err := json.Marshal(diskCache{Catalog: *cat})
	if err != nil || os.MkdirAll(c.CacheDir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(c.CacheDir, id+".json.*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil || os.Rename(tmp.Name(), c.cachePath(id)) != nil {
		os.Remove(tmp.Name())
	}
}
