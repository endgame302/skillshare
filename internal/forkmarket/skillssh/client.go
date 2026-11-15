package skillssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Board names a skills.sh leaderboard.
type Board string

const (
	AllTime  Board = "all"
	Trending Board = "trending"
	Hot      Board = "hot"
)

// Boards lists every leaderboard in the order skills.sh shows them.
var Boards = []Board{AllTime, Trending, Hot}

func (b Board) path() (string, bool) {
	switch b {
	case AllTime:
		return "/", true
	case Trending:
		return "/trending", true
	case Hot:
		return "/hot", true
	}
	return "", false
}

// MinQueryLen is the shortest query skills.sh's search accepts.
const MinQueryLen = 2

// MaxSearchLimit caps how many results one search asks for.
const MaxSearchLimit = 100

var (
	// ErrUnknownBoard is returned for a board name other than all, trending or hot.
	ErrUnknownBoard = errors.New("unknown skills.sh list")
	// ErrQueryTooShort is returned for a query shorter than MinQueryLen.
	ErrQueryTooShort = fmt.Errorf("search needs at least %d characters", MinQueryLen)
)

const (
	boardTTL     = time.Hour
	searchTTL    = 10 * time.Minute
	maxPageBytes = 8 << 20
	maxJSONBytes = 2 << 20
	maxSearches  = 200
	userAgent    = "skillshare"
)

// Result is a list and when it was read from skills.sh.
type Result struct {
	Skills    []Skill
	FetchedAt time.Time
	// Stale is set when skills.sh could not be read and an expired copy was served instead.
	Stale bool
}

type entry struct {
	skills    []Skill
	fetchedAt time.Time
}

// Client fetches skills.sh and keeps each answer in memory for a while (an hour for a list,
// ten minutes for a search), so browsing the dashboard does not hit skills.sh on every click.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	now     func() time.Time

	mu       sync.Mutex
	boards   map[Board]entry
	searches map[string]entry
}

// New returns a client for https://skills.sh with a 20 second timeout.
func New() *Client {
	return &Client{
		BaseURL:  siteURL,
		HTTP:     &http.Client{Timeout: 20 * time.Second},
		now:      time.Now,
		boards:   map[Board]entry{},
		searches: map[string]entry{},
	}
}

// Board returns a leaderboard. A copy younger than an hour is served from memory; when skills.sh
// cannot be reached an older copy is served with Stale set. A page that no longer parses is
// reported as ErrFormatChanged even when an older copy exists, so the change shows.
func (c *Client) Board(ctx context.Context, b Board) (Result, error) {
	path, ok := b.path()
	if !ok {
		return Result{}, ErrUnknownBoard
	}
	c.mu.Lock()
	cached, hit := c.boards[b]
	c.mu.Unlock()
	if hit && c.now().Sub(cached.fetchedAt) < boardTTL {
		return Result{Skills: cached.skills, FetchedAt: cached.fetchedAt}, nil
	}

	body, err := c.get(ctx, path, "text/html", maxPageBytes)
	if err != nil {
		if hit {
			return Result{Skills: cached.skills, FetchedAt: cached.fetchedAt, Stale: true}, nil
		}
		return Result{}, err
	}
	skills, err := ParseBoard(body)
	if err != nil {
		return Result{}, err
	}
	e := entry{skills: skills, fetchedAt: c.now()}
	c.mu.Lock()
	c.boards[b] = e
	c.mu.Unlock()
	return Result{Skills: e.skills, FetchedAt: e.fetchedAt}, nil
}

// Search asks skills.sh's search API. limit is clamped to 1..MaxSearchLimit.
func (c *Client) Search(ctx context.Context, query string, limit int) (Result, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < MinQueryLen {
		return Result{}, ErrQueryTooShort
	}
	limit = min(max(limit, 1), MaxSearchLimit)
	key := strconv.Itoa(limit) + "\x00" + strings.ToLower(query)

	c.mu.Lock()
	cached, hit := c.searches[key]
	c.mu.Unlock()
	if hit && c.now().Sub(cached.fetchedAt) < searchTTL {
		return Result{Skills: cached.skills, FetchedAt: cached.fetchedAt}, nil
	}

	q := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}
	body, err := c.get(ctx, "/api/search?"+q.Encode(), "application/json", maxJSONBytes)
	if err != nil {
		return Result{}, err
	}
	skills, err := ParseSearch(body)
	if err != nil {
		return Result{}, err
	}
	e := entry{skills: skills, fetchedAt: c.now()}
	c.mu.Lock()
	c.storeSearch(key, e)
	c.mu.Unlock()
	return Result{Skills: e.skills, FetchedAt: e.fetchedAt}, nil
}

// storeSearch keeps at most maxSearches answers: expired ones go first, then all of them.
func (c *Client) storeSearch(key string, e entry) {
	if len(c.searches) >= maxSearches {
		for k, old := range c.searches {
			if c.now().Sub(old.fetchedAt) >= searchTTL {
				delete(c.searches, k)
			}
		}
		if len(c.searches) >= maxSearches {
			clear(c.searches)
		}
	}
	c.searches[key] = e
}

// HTTPError is a non-2xx answer from skills.sh.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("skills.sh answered %d: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("skills.sh answered %d", e.Status)
}

func (c *Client) get(ctx context.Context, path, accept string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach skills.sh: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("could not read skills.sh: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &HTTPError{Status: resp.StatusCode, Body: errorText(body)}
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("skills.sh answer is larger than %d MB", limit>>20)
	}
	return body, nil
}

// errorText returns the message of a small JSON {"error": "..."} body, or nothing.
func errorText(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if len(body) > 4096 || !decodeAt(body, &e) {
		return ""
	}
	return strings.TrimSpace(e.Error)
}
