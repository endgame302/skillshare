// Fork-only (endgame302/skillshare): minimal read-only GitHub client for fork marketplaces.

package ghrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"skillshare/internal/github"
)

const (
	DefaultAPI = "https://api.github.com"
	DefaultRaw = "https://raw.githubusercontent.com"

	userAgent  = "skillshare"
	apiVersion = "2022-11-28"
	headLimit  = 1 << 10  // 1 KiB
	treeLimit  = 16 << 20 // 16 MiB
	reqTimeout = 30 * time.Second
)

// ErrRateLimited: GitHub answered 403 or 429. ErrNotFound: 404 or 422.
var (
	ErrRateLimited = errors.New("GitHub is rate limiting requests")
	ErrNotFound    = errors.New("not found on GitHub")
)

var hex40Pattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Client reads public GitHub repositories. Token is sent only to DefaultAPI itself (never to Raw or any other host).
type Client struct {
	HTTP  *http.Client
	API   string        // DefaultAPI
	Raw   string        // DefaultRaw
	Token func() string // nil means github.GetToken from "skillshare/internal/github" (env GITHUB_TOKEN, GH_TOKEN, then `gh auth token`)
}

// New returns a Client configured for GitHub's public endpoints with HTTPS-only redirects.
func New() *Client {
	return &Client{
		HTTP: &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("GitHub redirected to a non-https address")
			}
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		}},
		API: DefaultAPI,
		Raw: DefaultRaw,
	}
}

// ValidRepo reports whether repo is "owner/name": two segments of [A-Za-z0-9_.-], neither "." nor "..", owner not starting with "-".
func ValidRepo(repo string) bool {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || strings.Contains(name, "/") {
		return false
	}
	if !validRepoSegment(owner) || !validRepoSegment(name) {
		return false
	}
	return owner[0] != '-'
}

func validRepoSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		isLetter := (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
		isDigit := b >= '0' && b <= '9'
		isPunct := b == '_' || b == '.' || b == '-'
		if !isLetter && !isDigit && !isPunct {
			return false
		}
	}
	return true
}

// Entry is one git tree entry returned by GitHub's recursive tree API.
type Entry struct {
	Path string `json:"path"`
	Type string `json:"type"` // blob, tree, commit
	Mode string `json:"mode"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

func (c *Client) apiBase() string {
	if c.API != "" {
		return strings.TrimRight(c.API, "/")
	}
	return DefaultAPI
}

func (c *Client) rawBase() string {
	if c.Raw != "" {
		return strings.TrimRight(c.Raw, "/")
	}
	return DefaultRaw
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) token() string {
	if c.Token != nil {
		return c.Token()
	}
	return github.GetToken()
}

// apiHeaders builds GitHub REST API headers, attaching Authorization only when targeting DefaultAPI.
func (c *Client) apiHeaders(accept string) map[string]string {
	h := map[string]string{
		"Accept":               accept,
		"X-GitHub-Api-Version": apiVersion,
	}
	if c.apiBase() == DefaultAPI {
		if tok := c.token(); tok != "" {
			h["Authorization"] = "Bearer " + tok
		}
	}
	return h
}

func (c *Client) get(ctx context.Context, rawURL string, headers map[string]string, limit int64) (*http.Response, []byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return resp, nil, err
	}
	if int64(len(body)) > limit {
		return resp, nil, fmt.Errorf("response exceeded %d bytes", limit)
	}
	return resp, body, nil
}

func statusError(status int) error {
	switch status {
	case http.StatusForbidden, http.StatusTooManyRequests:
		return fmt.Errorf("%w; try again later, or sign in with gh or set GITHUB_TOKEN", ErrRateLimited)
	case http.StatusNotFound, http.StatusUnprocessableEntity:
		return fmt.Errorf("%w (HTTP %d)", ErrNotFound, status)
	default:
		return fmt.Errorf("GitHub answered HTTP %d", status)
	}
}

// Head resolves ref (a branch, tag, "HEAD", or a sha) to its 40-hex commit.
func (c *Client) Head(ctx context.Context, repo, ref string) (string, error) {
	if !ValidRepo(repo) {
		return "", fmt.Errorf("invalid GitHub repository %q", repo)
	}
	if strings.TrimSpace(ref) == "" {
		return "", errors.New("git ref is required")
	}

	u := c.apiBase() + "/repos/" + repo + "/commits/" + url.PathEscape(ref)
	resp, body, err := c.get(ctx, u, c.apiHeaders("application/vnd.github.sha"), headLimit)
	if err != nil {
		return "", fmt.Errorf("resolving %s@%s failed: %w", repo, ref, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", statusError(resp.StatusCode)
	}

	sha := strings.TrimSpace(string(body))
	if !hex40Pattern.MatchString(sha) {
		return "", fmt.Errorf("GitHub returned an invalid commit sha %q", sha)
	}
	return sha, nil
}

// Tree lists every entry of commit recursively.
func (c *Client) Tree(ctx context.Context, repo, commit string) ([]Entry, error) {
	if !ValidRepo(repo) {
		return nil, fmt.Errorf("invalid GitHub repository %q", repo)
	}
	if !hex40Pattern.MatchString(commit) {
		return nil, fmt.Errorf("invalid commit sha %q", commit)
	}

	u := c.apiBase() + "/repos/" + repo + "/git/trees/" + commit + "?recursive=1"
	resp, body, err := c.get(ctx, u, c.apiHeaders("application/vnd.github+json"), treeLimit)
	if err != nil {
		return nil, fmt.Errorf("listing tree for %s@%s failed: %w", repo, commit, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, statusError(resp.StatusCode)
	}

	var listing struct {
		Truncated bool    `json:"truncated"`
		Tree      []Entry `json:"tree"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("parsing GitHub tree failed: %w", err)
	}
	if listing.Truncated {
		return nil, errors.New("GitHub tree listing is truncated")
	}
	return listing.Tree, nil
}

// RawFile downloads one file at commit without sending any token.
func (c *Client) RawFile(ctx context.Context, repo, commit, path string, limit int64) ([]byte, error) {
	if !ValidRepo(repo) {
		return nil, fmt.Errorf("invalid GitHub repository %q", repo)
	}
	if !hex40Pattern.MatchString(commit) {
		return nil, fmt.Errorf("invalid commit sha %q", commit)
	}
	if path == "" {
		return nil, errors.New("file path is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("invalid byte limit %d", limit)
	}

	u := c.rawBase() + "/" + repo + "/" + commit + "/" + EscapePathSegments(path)
	resp, body, err := c.get(ctx, u, nil, limit)
	if err != nil {
		return nil, fmt.Errorf("downloading %s failed: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, statusError(resp.StatusCode)
	}
	return body, nil
}

// EscapePathSegments escapes each slash-separated path segment for use in a URL.
func EscapePathSegments(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}
