package ghrepo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidRepo(t *testing.T) {
	cases := []struct {
		repo string
		want bool
	}{
		{"a/b", true},
		{"A-1/b.c_d", true},
		{"a", false},
		{"a/b/c", false},
		{"../b", false},
		{"a/..", false},
		{"-a/b", false},
		{"a b/c", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := ValidRepo(tc.repo); got != tc.want {
			t.Errorf("ValidRepo(%q) = %v, want %v", tc.repo, got, tc.want)
		}
	}
}

func TestAPIHeadersAuthorization(t *testing.T) {
	c := &Client{
		API:   DefaultAPI,
		Token: func() string { return "secret-tok" },
	}
	h := c.apiHeaders("application/vnd.github+json")
	if h["Authorization"] != "Bearer secret-tok" {
		t.Fatalf("Authorization = %q, want Bearer secret-tok", h["Authorization"])
	}
	if h["X-GitHub-Api-Version"] != apiVersion {
		t.Fatalf("X-GitHub-Api-Version = %q, want %s", h["X-GitHub-Api-Version"], apiVersion)
	}

	cEmpty := &Client{
		API:   DefaultAPI,
		Token: func() string { return "" },
	}
	if got := cEmpty.apiHeaders("application/vnd.github+json")["Authorization"]; got != "" {
		t.Fatalf("empty token sent Authorization %q", got)
	}

	cCustom := &Client{
		API:   "https://example.invalid",
		Token: func() string { return "secret-tok" },
	}
	if got := cCustom.apiHeaders("application/vnd.github+json")["Authorization"]; got != "" {
		t.Fatalf("non-default API sent Authorization %q", got)
	}
}

func TestHead(t *testing.T) {
	const wantSHA = "0123456789abcdef0123456789abcdef01234567"
	var gotAccept, gotAuth, gotUA string
	var body string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := New()
	c.API = srv.URL
	c.Token = func() string { return "should-not-leak" }

	body = wantSHA + "\n"
	sha, err := c.Head(context.Background(), "owner/repo", "HEAD")
	if err != nil {
		t.Fatalf("Head error: %v", err)
	}
	if sha != wantSHA {
		t.Fatalf("Head = %q, want %q", sha, wantSHA)
	}
	if gotAccept != "application/vnd.github.sha" {
		t.Errorf("Accept = %q, want application/vnd.github.sha", gotAccept)
	}
	if gotAuth != "" {
		t.Errorf("fake server received Authorization %q", gotAuth)
	}
	if gotUA != "skillshare" {
		t.Errorf("User-Agent = %q, want skillshare", gotUA)
	}

	for _, bad := range []string{"not-a-sha", strings.ToUpper(wantSHA), wantSHA[:39], ""} {
		body = bad
		if _, err := c.Head(context.Background(), "owner/repo", "HEAD"); err == nil {
			t.Errorf("Head(%q) succeeded, want error", bad)
		}
	}
}

func TestStatusMapping(t *testing.T) {
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	defer srv.Close()

	c := New()
	c.API = srv.URL
	c.Raw = srv.URL
	c.Token = func() string { return "" }

	const sha = "0123456789abcdef0123456789abcdef01234567"
	for _, st := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		status = st
		if _, err := c.Head(context.Background(), "a/b", "HEAD"); !errors.Is(err, ErrRateLimited) {
			t.Errorf("status %d Head err = %v, want ErrRateLimited", st, err)
		}
	}
	for _, st := range []int{http.StatusNotFound, http.StatusUnprocessableEntity} {
		status = st
		if _, err := c.Head(context.Background(), "a/b", "HEAD"); !errors.Is(err, ErrNotFound) {
			t.Errorf("status %d Head err = %v, want ErrNotFound", st, err)
		}
	}
	status = http.StatusNotFound
	if _, err := c.RawFile(context.Background(), "a/b", sha, "x.md", 1024); !errors.Is(err, ErrNotFound) {
		t.Errorf("RawFile 404 err = %v, want ErrNotFound", err)
	}
}

func TestTree(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	var respBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Authorization on non-default API")
		}
		w.Write([]byte(respBody))
	}))
	defer srv.Close()

	c := New()
	c.API = srv.URL
	c.Token = func() string { return "tok" }

	respBody = `{"truncated":false,"tree":[{"path":"categories/01-dev/a.md","type":"blob","mode":"100644","sha":"abc","size":12}]}`
	entries, err := c.Tree(context.Background(), "a/b", sha)
	if err != nil {
		t.Fatalf("Tree err: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "categories/01-dev/a.md" || entries[0].Size != 12 {
		t.Fatalf("entries = %+v", entries)
	}

	respBody = `{"truncated":true,"tree":[]}`
	if _, err := c.Tree(context.Background(), "a/b", sha); err == nil {
		t.Fatalf("truncated tree succeeded, want error")
	}
}

func TestRawFile(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	var gotURI, gotAuth string
	var payload string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := New()
	c.Raw = srv.URL
	c.Token = func() string { return "secret" }

	payload = "hello"
	data, err := c.RawFile(context.Background(), "a/b", sha, "dir one/my file.md", 10)
	if err != nil {
		t.Fatalf("RawFile err: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("data = %q, want hello", data)
	}
	if !strings.Contains(gotURI, "/dir%20one/my%20file.md") {
		t.Errorf("RequestURI = %q, want escaped segments", gotURI)
	}
	if gotAuth != "" {
		t.Errorf("RawFile sent Authorization %q", gotAuth)
	}

	payload = "0123456789a"
	if _, err := c.RawFile(context.Background(), "a/b", sha, "a.md", 10); err == nil {
		t.Fatalf("RawFile over limit succeeded, want error")
	}
}
