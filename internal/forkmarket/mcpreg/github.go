// Fork-only (endgame302/skillshare): GitHub's MCP registry, a curated subset of the official
// one in the same schema, with GitHub's repository data added.

package mcpreg

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultCuratedBase is GitHub's MCP registry.
	DefaultCuratedBase = "https://api.mcp.github.com"
	// CuratedTTL is how long a fetched copy is used before fetching again.
	CuratedTTL = 24 * time.Hour

	curatedFile     = "github-registry.json.gz"
	curatedVersion  = 1
	maxCuratedPages = 50
	// failedRetry keeps a failing source from being asked again on every list request.
	failedRetry = time.Minute
)

// curatedSnapshot is GitHub's registry as it is saved.
type curatedSnapshot struct {
	Version   int              `json:"version"`
	FetchedAt time.Time        `json:"fetchedAt"`
	Servers   []ServerResponse `json:"servers"`
}

// Curated is GitHub's MCP registry, kept on disk for CuratedTTL.
type Curated struct {
	Base string
	HTTP *http.Client
	Dir  string
	Now  func() time.Time

	mu      sync.Mutex
	loaded  bool
	snap    *curatedSnapshot
	entries []Entry
	byName  map[string]int
	lastErr string
	retryAt time.Time
}

// NewCurated returns GitHub's registry, cached under dir.
func NewCurated(dir string) *Curated {
	return &Curated{Base: DefaultCuratedBase, HTTP: newHTTPClient("GitHub's MCP registry"), Dir: dir, Now: time.Now}
}

func (c *Curated) path() string { return filepath.Join(c.Dir, curatedFile) }

// use makes snap the current copy.
func (c *Curated) use(snap *curatedSnapshot) {
	c.snap = snap
	c.entries = make([]Entry, 0, len(snap.Servers))
	c.byName = make(map[string]int, len(snap.Servers))
	for i, r := range snap.Servers {
		c.byName[r.Server.Name] = i
		c.entries = append(c.entries, EntryFromServer(r))
	}
}

func (c *Curated) status() Status {
	st := Status{Entries: len(c.entries), Error: c.lastErr, RetryAt: c.retryAt}
	if c.snap != nil {
		st.FetchedAt = c.snap.FetchedAt
		st.Complete = true
		st.Stale = c.lastErr != "" && c.Now().Sub(c.snap.FetchedAt) >= CuratedTTL
	}
	switch {
	case c.snap != nil:
		st.State = "ready"
	case c.lastErr != "":
		st.State = "error"
	default:
		st.State = "loading"
	}
	return st
}

// Load returns the entries, in the order fetched, and the status. It fetches when nothing is
// cached, the copy is older than CuratedTTL, or refresh is set. When that fetch fails it returns
// the older copy with the error in the status, or the error when there is no copy.
func (c *Curated) Load(ctx context.Context, refresh bool) ([]Entry, Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		var snap curatedSnapshot
		if readGzipJSON(c.path(), &snap) == nil && snap.Version == curatedVersion && !snap.FetchedAt.IsZero() {
			c.use(&snap)
		}
		c.loaded = true
	}
	now := c.Now()
	fresh := c.snap != nil && now.Sub(c.snap.FetchedAt) < CuratedTTL
	waiting := !refresh && now.Before(c.retryAt)
	if (fresh && !refresh) || waiting {
		if c.snap == nil {
			return nil, c.status(), errors.New(c.lastErr)
		}
		return c.entries, c.status(), nil
	}
	servers, err := c.fetch(ctx)
	if err != nil {
		c.lastErr, c.retryAt = err.Error(), now.Add(failedRetry)
		if c.snap == nil {
			return nil, c.status(), err
		}
		return c.entries, c.status(), nil
	}
	snap := &curatedSnapshot{Version: curatedVersion, FetchedAt: now.UTC(), Servers: servers}
	c.use(snap)
	c.lastErr, c.retryAt = "", time.Time{}
	_ = writeGzipJSON(c.path(), snap)
	return c.entries, c.status(), nil
}

// Server returns the cached record with this name.
func (c *Curated) Server(ctx context.Context, name string) (*ServerResponse, error) {
	if _, _, err := c.Load(ctx, false); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	i, ok := c.byName[name]
	if !ok {
		return nil, ErrNotFound
	}
	r := c.snap.Servers[i]
	return &r, nil
}

func (c *Curated) fetch(ctx context.Context) ([]ServerResponse, error) {
	var out []ServerResponse
	cursor := ""
	for n := 0; n < maxCuratedPages; n++ {
		v := url.Values{"limit": {"100"}}
		if cursor != "" {
			v.Set("cursor", cursor)
		}
		var page ListResponse
		if err := getJSON(ctx, c.HTTP, strings.TrimRight(c.Base, "/")+"/v0.1/servers?"+v.Encode(), "GitHub's MCP registry", c.Now, &page); err != nil {
			return nil, err
		}
		for _, r := range page.Servers {
			if r.Server.Name == "" || r.Official().Status == "deleted" {
				continue
			}
			dropReadme(&r.Server)
			out = append(out, r)
		}
		next := page.Metadata.NextCursor
		if next == "" {
			return out, nil
		}
		if next == cursor {
			return nil, errors.New("GitHub's MCP registry returned the same page twice")
		}
		cursor = next
	}
	return nil, errors.New("GitHub's MCP registry listing is longer than expected")
}

// dropReadme removes the repository README GitHub's registry includes, which is large and not
// shown; every other key stays as it was.
func dropReadme(s *ServerJSON) {
	raw, ok := s.Meta[MetaPublisher]
	if !ok {
		return
	}
	var pp map[string]json.RawMessage
	if json.Unmarshal(raw, &pp) != nil {
		return
	}
	var gh map[string]json.RawMessage
	if json.Unmarshal(pp["github"], &gh) != nil {
		return
	}
	if _, ok := gh["readme"]; !ok {
		return
	}
	delete(gh, "readme")
	ghRaw, err := json.Marshal(gh)
	if err != nil {
		return
	}
	pp["github"] = ghRaw
	if out, err := json.Marshal(pp); err == nil {
		s.Meta[MetaPublisher] = out
	}
}
