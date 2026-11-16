package clawhub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultRegistry is ClawHub's site; its robots.txt allows the feed and disallows /api/,
	// which is only used for a download the user asked for.
	DefaultRegistry  = "https://clawhub.ai"
	DefaultGitHubAPI = "https://api.github.com"
	DefaultGitHubRaw = "https://raw.githubusercontent.com"
	feedPath         = "/v1/feeds/skills"
	wellKnownPath    = "/.well-known/clawhub.json"
	downloadPath     = "/api/v1/download"

	userAgent       = "skillshare"
	maxFeedBytes    = 8 << 20
	maxWellKnown    = 64 << 10
	feedTimeout     = 20 * time.Second
	downloadTimeout = 60 * time.Second
	cacheFile       = "skills-feed.json"

	// fallbackTTL applies when the feed names no usable expiry; maxTTL bounds one that does.
	fallbackTTL = time.Hour
	maxTTL      = 14 * 24 * time.Hour
)

// Client fetches ClawHub's feed, keeps it on disk until it expires, and downloads archives.
type Client struct {
	HTTP      *http.Client
	Registry  string // site root: the feed and .well-known/clawhub.json live here
	GitHubAPI string // https://api.github.com
	GitHubRaw string // https://raw.githubusercontent.com
	CacheDir  string // where the feed is kept
	StoreDir  string // where verified releases are kept; see Release.Dir
	Now       func() time.Time
}

// New returns a client for clawhub.ai that caches the feed under cacheDir and keeps releases
// under storeDir.
func New(cacheDir, storeDir string) *Client {
	return &Client{
		HTTP: &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("ClawHub redirected to a non-https address")
			}
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		}},
		Registry:  DefaultRegistry,
		GitHubAPI: DefaultGitHubAPI,
		GitHubRaw: DefaultGitHubRaw,
		CacheDir:  cacheDir,
		StoreDir:  storeDir,
		Now:       time.Now,
	}
}

// Snapshot is the feed as it was last fetched.
type Snapshot struct {
	Feed      *Feed
	FetchedAt time.Time
	// APIBase is where downloads go, as the registry's .well-known/clawhub.json names it.
	APIBase string
	// Stale is set when the cached feed has expired and fetching a new one failed;
	// FetchErr says why.
	Stale    bool
	FetchErr error
}

// ValidUntil is when the snapshot should be fetched again.
func (s *Snapshot) ValidUntil() time.Time {
	exp := s.Feed.Expiry()
	if exp.IsZero() || !exp.After(s.FetchedAt) {
		return s.FetchedAt.Add(fallbackTTL)
	}
	if limit := s.FetchedAt.Add(maxTTL); exp.After(limit) {
		return limit
	}
	return exp
}

type cached struct {
	FetchedAt time.Time       `json:"fetchedAt"`
	APIBase   string          `json:"apiBase,omitempty"`
	Feed      json.RawMessage `json:"feed"`
}

// Feed returns the cached feed while it is valid, and otherwise fetches it. refresh fetches
// even a valid one. When a fetch fails and an older copy exists, that copy is returned with
// Stale set.
func (c *Client) Feed(ctx context.Context, refresh bool) (*Snapshot, error) {
	old := c.readCache()
	if old != nil && !refresh && c.Now().Before(old.ValidUntil()) {
		return old, nil
	}
	raw, feed, err := c.fetchFeed(ctx)
	if err != nil {
		if old != nil {
			old.Stale, old.FetchErr = true, err
			return old, nil
		}
		return nil, err
	}
	snap := &Snapshot{Feed: feed, FetchedAt: c.Now().UTC(), APIBase: c.apiBase(ctx)}
	if snap.APIBase == "" && old != nil {
		snap.APIBase = old.APIBase
	}
	c.writeCache(snap, raw)
	return snap, nil
}

func (c *Client) cachePath() string { return filepath.Join(c.CacheDir, cacheFile) }

func (c *Client) readCache() *Snapshot {
	data, err := os.ReadFile(c.cachePath())
	if err != nil {
		return nil
	}
	var cc cached
	if json.Unmarshal(data, &cc) != nil || cc.FetchedAt.IsZero() {
		return nil
	}
	feed, err := ParseFeed(cc.Feed)
	if err != nil {
		return nil
	}
	return &Snapshot{Feed: feed, FetchedAt: cc.FetchedAt, APIBase: cc.APIBase}
}

// writeCache replaces the cache file in one step, so a concurrent reader sees the old or the
// new feed. A cache that cannot be written only costs a fetch next time.
func (c *Client) writeCache(snap *Snapshot, raw []byte) {
	data, err := json.Marshal(cached{FetchedAt: snap.FetchedAt, APIBase: snap.APIBase, Feed: raw})
	if err != nil || os.MkdirAll(c.CacheDir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(c.CacheDir, cacheFile+".*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil || os.Rename(tmp.Name(), c.cachePath()) != nil {
		os.Remove(tmp.Name())
	}
}

func (c *Client) get(ctx context.Context, rawURL, accept string, limit int64) (*http.Response, []byte, error) {
	return c.getWithHeaders(ctx, rawURL, accept, nil, limit)
}

func (c *Client) getWithHeaders(ctx context.Context, rawURL, accept string, headers map[string]string, limit int64) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return resp, nil, err
	}
	if int64(len(body)) > limit {
		return resp, nil, fmt.Errorf("the response is larger than %d bytes", limit)
	}
	return resp, body, nil
}

func (c *Client) fetchFeed(ctx context.Context) ([]byte, *Feed, error) {
	ctx, cancel := context.WithTimeout(ctx, feedTimeout)
	defer cancel()
	resp, body, err := c.get(ctx, strings.TrimRight(c.Registry, "/")+feedPath, "application/json", maxFeedBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot reach ClawHub: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("ClawHub's feed answered HTTP %d", resp.StatusCode)
	}
	feed, err := ParseFeed(body)
	if err != nil {
		return nil, nil, err
	}
	return body, feed, nil
}

// apiBase reads the download host from the registry's .well-known/clawhub.json. It returns ""
// when that file cannot be read or names something other than a plain https address.
func (c *Client) apiBase(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, feedTimeout)
	defer cancel()
	resp, body, err := c.get(ctx, strings.TrimRight(c.Registry, "/")+wellKnownPath, "application/json", maxWellKnown)
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	var wk struct {
		APIBase string `json:"apiBase"`
	}
	if json.Unmarshal(body, &wk) != nil {
		return ""
	}
	return cleanBase(wk.APIBase)
}

func cleanBase(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return ""
	}
	return strings.TrimRight(u.String(), "/")
}

// DownloadError is a download ClawHub refused or failed; Status is its HTTP status, or 0.
type DownloadError struct {
	Status int
	Msg    string
}

func (e *DownloadError) Error() string { return e.Msg }

// Download fetches the archive of one release. owner is always passed: ClawHub answers 409 for
// a slug two publishers share. apiBase "" means the registry itself.
func (c *Client) Download(ctx context.Context, apiBase, owner, slug, version string) ([]byte, error) {
	base := cleanBase(apiBase)
	if base == "" {
		base = strings.TrimRight(c.Registry, "/")
	}
	q := url.Values{"slug": {slug}, "ownerHandle": {owner}, "version": {version}}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	resp, body, err := c.get(ctx, base+downloadPath+"?"+q.Encode(), "application/zip", MaxArchiveBytes)
	if err != nil {
		return nil, &DownloadError{Msg: "downloading from ClawHub failed: " + err.Error()}
	}
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, &DownloadError{resp.StatusCode, "ClawHub no longer offers this version"}
	case resp.StatusCode == http.StatusForbidden:
		return nil, &DownloadError{resp.StatusCode, "ClawHub blocks downloads of this version"}
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &DownloadError{resp.StatusCode, "ClawHub is rate limiting downloads; try again later"}
	default:
		return nil, &DownloadError{resp.StatusCode, fmt.Sprintf("ClawHub answered HTTP %d", resp.StatusCode)}
	}
	if !strings.HasPrefix(string(body), "PK\x03\x04") {
		return nil, &DownloadError{Msg: "ClawHub did not send a zip archive"}
	}
	return body, nil
}
