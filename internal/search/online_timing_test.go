package search

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingTransport counts the requests sent through http.DefaultTransport,
// grouped by kind.
type countingTransport struct {
	next  http.RoundTripper
	total atomic.Int64
	mu    sync.Mutex
	kinds map[string]int
	start time.Time
	spans []string
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.total.Add(1)
	kind := req.Method + " " + req.URL.Host
	switch p := req.URL.Path; {
	case strings.HasPrefix(p, "/search/code"):
		kind = "search/code"
	case strings.HasPrefix(p, "/search/repositories"):
		kind = "search/repositories"
	case strings.Contains(p, "/git/trees/"):
		kind = "git/trees"
	case strings.Contains(p, "/contents/"):
		kind = "contents"
	case p == "/graphql":
		kind = "graphql"
	case strings.HasPrefix(p, "/repos/"):
		kind = "repos"
	}
	c.mu.Lock()
	c.kinds[kind]++
	offset := time.Since(c.start)
	c.mu.Unlock()
	resp, err := c.next.RoundTrip(req)
	c.mu.Lock()
	c.spans = append(c.spans, fmt.Sprintf("%-20s %6dms +%6dms", kind, offset.Milliseconds(), (time.Since(c.start)-offset).Milliseconds()))
	c.mu.Unlock()
	return resp, err
}

func (c *countingTransport) reset() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.kinds
	c.kinds = map[string]int{}
	c.spans = nil
	c.start = time.Now()
	c.total.Store(0)
	return out
}

// TestOnlineSearchTiming measures real GitHub searches: a cold search, the
// same search again, and a second query. It needs network access and a token:
//
//	SKILLSHARE_ONLINE_SEARCH=pdf GITHUB_TOKEN=$(gh auth token) go test ./internal/search -run TestOnlineSearchTiming -v
func TestOnlineSearchTiming(t *testing.T) {
	query := os.Getenv("SKILLSHARE_ONLINE_SEARCH")
	if query == "" {
		t.Skip("set SKILLSHARE_ONLINE_SEARCH=<query> to run against GitHub")
	}
	second := os.Getenv("SKILLSHARE_ONLINE_SEARCH_2")
	if second == "" {
		second = "excel"
	}
	// Start cold, and leave the user's own search cache alone.
	cacheDir, origCacheDir := t.TempDir(), searchCacheDir
	searchCacheDir = func() string { return cacheDir }
	ct := &countingTransport{next: http.DefaultTransport, kinds: map[string]int{}}
	orig := http.DefaultTransport
	http.DefaultTransport = ct
	t.Cleanup(func() {
		http.DefaultTransport = orig
		searchCacheDir = origCacheDir
	})

	var first []SearchResult
	for i, q := range []string{query, query, second} {
		ct.reset()
		start := time.Now()
		results, err := Search(q, 20)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		ct.mu.Lock()
		kinds := ct.kinds
		ct.mu.Unlock()
		total := 0
		for _, n := range kinds {
			total += n
		}
		t.Logf("run %d %q: %d results in %s, %d requests %v", i+1, q, len(results), elapsed.Round(time.Millisecond), total, kinds)
		if i != 1 {
			ct.mu.Lock()
			for _, span := range ct.spans {
				t.Logf("  %s", span)
			}
			ct.mu.Unlock()
		}
		if i == 0 {
			first = results
			for j, r := range results {
				t.Logf("  %2d %-40s %6d  %s", j+1, r.Source, r.Stars, truncate(r.Description, 60))
			}
		}
		if i == 1 && !sameResults(first, results) {
			t.Errorf("repeat search returned different results")
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func sameResults(a, b []SearchResult) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Source != b[i].Source || a[i].Name != b[i].Name || a[i].Description != b[i].Description || a[i].Stars != b[i].Stars {
			return false
		}
	}
	return true
}
