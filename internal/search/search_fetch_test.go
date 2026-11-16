package search

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSkillDoc struct {
	path    string
	sha     string
	content string
}

type fakeRepoData struct {
	owner         string
	repo          string
	stars         int
	defaultBranch string
	skills        []fakeSkillDoc
}

type fakeGitHub struct {
	repos map[string]fakeRepoData // keyed by owner/repo

	mu            sync.Mutex
	counts        map[string]int
	failGraphQL   bool
	nullStarAlias string
	nullBlobAlias string
	nullBlobUsed  bool
	// failCodeSearch makes /search/code answer 500 when q contains it.
	failCodeSearch string
}

func newFakeGitHubData() *fakeGitHub {
	makeDoc := func(name, desc string) string {
		return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n# %s\n", name, desc, name)
	}

	addSkills := func(prefix, repoSlug string, count int) []fakeSkillDoc {
		docs := make([]fakeSkillDoc, 0, count)
		for i := 1; i <= count; i++ {
			name := fmt.Sprintf("%s-%02d", prefix, i)
			desc := fmt.Sprintf("Comprehensive pdf toolkit skill %s-%02d for document workflows", prefix, i)
			docs = append(docs, fakeSkillDoc{
				path:    fmt.Sprintf("skills/%s/SKILL.md", name),
				sha:     fmt.Sprintf("sha-%s-%s-%02d", repoSlug, prefix, i),
				content: makeDoc(name, desc),
			})
		}
		return docs
	}

	dupContent := makeDoc("dup-pdf-skill", "Shared duplicate pdf conversion skill across repositories")

	anthropicsSkills := append(addSkills("anthropic-pdf", "anthropics-skills", 3), fakeSkillDoc{
		path:    "skills/dup-pdf-skill/SKILL.md",
		sha:     "sha-anthropics-dup",
		content: dupContent,
	})
	acmeSkills := append(addSkills("acme-pdf", "acme-pdf-tools", 7), fakeSkillDoc{
		path:    "skills/dup-pdf-skill/SKILL.md",
		sha:     "sha-acme-dup",
		content: dupContent,
	})
	epsilonSkills := []fakeSkillDoc{
		{
			path:    "skills/good-pdf/SKILL.md",
			sha:     "sha-epsilon-good",
			content: makeDoc("good-pdf", "Valid pdf extraction helper with descriptive frontmatter"),
		},
		{
			path:    "skills/bad-pdf/SKILL.md",
			sha:     "sha-epsilon-bad",
			content: makeDoc("\"Bad Skill Name\"", "Invalid name skill that should be dropped during broad search"),
		},
	}

	repos := map[string]fakeRepoData{
		"anthropics/skills": {
			owner:         "anthropics",
			repo:          "skills",
			stars:         5000,
			defaultBranch: "main",
			skills:        anthropicsSkills,
		},
		"vercel-labs/skills": {
			owner:         "vercel-labs",
			repo:          "skills",
			stars:         3000,
			defaultBranch: "main",
			skills:        addSkills("vercel-pdf", "vercel-skills", 3),
		},
		"acme/pdf-tools": {
			owner:         "acme",
			repo:          "pdf-tools",
			stars:         200,
			defaultBranch: "main",
			skills:        acmeSkills,
		},
		"beta/doc-kit": {
			owner:         "beta",
			repo:          "doc-kit",
			stars:         200,
			defaultBranch: "main",
			skills:        addSkills("beta-doc", "beta-doc-kit", 8),
		},
		"gamma/pdf-utils": {
			owner:         "gamma",
			repo:          "pdf-utils",
			stars:         120,
			defaultBranch: "main",
			skills:        addSkills("gamma-util", "gamma-pdf-utils", 8),
		},
		"delta/zero-repo": {
			owner:         "delta",
			repo:          "zero-repo",
			stars:         0,
			defaultBranch: "main",
			skills:        addSkills("zero-pdf", "delta-zero-repo", 2),
		},
		"epsilon/bad-skills": {
			owner:         "epsilon",
			repo:          "bad-skills",
			stars:         50,
			defaultBranch: "main",
			skills:        epsilonSkills,
		},
		"zeta/tree-only": {
			owner:         "zeta",
			repo:          "tree-only",
			stars:         400,
			defaultBranch: "main",
			skills:        addSkills("tree-pdf", "zeta-tree-only", 3),
		},
	}

	return &fakeGitHub{
		repos:  repos,
		counts: make(map[string]int),
	}
}

func (f *fakeGitHub) inc(kind string) {
	f.mu.Lock()
	f.counts[kind]++
	f.mu.Unlock()
}

func (f *fakeGitHub) resetCounts() {
	f.mu.Lock()
	f.counts = make(map[string]int)
	f.nullBlobUsed = false
	f.mu.Unlock()
}

func (f *fakeGitHub) count(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[kind]
}

func (f *fakeGitHub) totalRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0
	for _, n := range f.counts {
		total += n
	}
	return total
}

func (f *fakeGitHub) findSkill(fullName, path string) (fakeSkillDoc, bool) {
	repo, ok := f.repos[fullName]
	if !ok {
		return fakeSkillDoc{}, false
	}
	for _, s := range repo.skills {
		if s.path == path {
			return s, true
		}
	}
	return fakeSkillDoc{}, false
}

func (f *fakeGitHub) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/search/code":
			f.inc("search/code")
			q := r.URL.Query().Get("q")
			f.mu.Lock()
			fail := f.failCodeSearch != "" && strings.Contains(q, f.failCodeSearch)
			f.mu.Unlock()
			if fail {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}

			var targetRepos []string
			for _, field := range strings.Fields(q) {
				if strings.HasPrefix(field, "repo:") {
					targetRepos = append(targetRepos, strings.TrimPrefix(field, "repo:"))
				}
			}
			if len(targetRepos) == 0 {
				targetRepos = []string{
					"acme/pdf-tools",
					"beta/doc-kit",
					"gamma/pdf-utils",
					"delta/zero-repo",
					"epsilon/bad-skills",
				}
			}

			var items []gitHubCodeItem
			for _, fullName := range targetRepos {
				repo, ok := f.repos[fullName]
				if !ok {
					continue
				}
				for _, s := range repo.skills {
					items = append(items, gitHubCodeItem{
						Name: "SKILL.md",
						Path: s.path,
						SHA:  s.sha,
						Repository: gitHubRepository{
							FullName: fullName,
							Fork:     false,
						},
					})
				}
			}

			_ = json.NewEncoder(w).Encode(gitHubSearchResponse{
				TotalCount: len(items),
				Items:      items,
			})

		case r.Method == http.MethodGet && r.URL.Path == "/search/repositories":
			f.inc("search/repositories")
			repo := f.repos["zeta/tree-only"]
			_ = json.NewEncoder(w).Encode(gitHubRepositorySearchResponse{
				Items: []gitHubRepositorySearchItem{
					{
						FullName:        "zeta/tree-only",
						StargazersCount: repo.stars,
						DefaultBranch:   repo.defaultBranch,
					},
				},
			})

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/"):
			rest := strings.TrimPrefix(r.URL.Path, "/repos/")
			parts := strings.SplitN(rest, "/", 4)
			if len(parts) < 2 {
				http.NotFound(w, r)
				return
			}
			fullName := parts[0] + "/" + parts[1]
			repo, ok := f.repos[fullName]
			if !ok {
				http.NotFound(w, r)
				return
			}

			switch {
			case len(parts) == 2:
				f.inc("repos")
				_ = json.NewEncoder(w).Encode(map[string]int{
					"stargazers_count": repo.stars,
				})
			case len(parts) == 4 && parts[2] == "git" && strings.HasPrefix(parts[3], "trees/"):
				f.inc("git/trees")
				entries := make([]gitHubTreeItem, 0, len(repo.skills)+1)
				entries = append(entries, gitHubTreeItem{Path: "README.md", Type: "blob", SHA: "sha-readme"})
				for _, s := range repo.skills {
					entries = append(entries, gitHubTreeItem{
						Path: s.path,
						Type: "blob",
						SHA:  s.sha,
					})
				}
				_ = json.NewEncoder(w).Encode(gitHubTreeResponse{Tree: entries})
			case len(parts) == 4 && parts[2] == "contents":
				f.inc("contents")
				doc, found := f.findSkill(fullName, parts[3])
				if !found {
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(gitHubContentResponse{
					Content:  base64.StdEncoding.EncodeToString([]byte(doc.content)),
					Encoding: "base64",
					SHA:      doc.sha,
				})
			default:
				http.NotFound(w, r)
			}

		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			var req graphQLRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			f.mu.Lock()
			fail := f.failGraphQL
			nullStar := f.nullStarAlias
			nullBlob := f.nullBlobAlias
			f.mu.Unlock()

			if strings.Contains(req.Query, "stargazerCount") {
				f.inc("graphql_stars")
				if fail {
					http.Error(w, "bad gateway", http.StatusBadGateway)
					return
				}
				data := make(map[string]any)
				for i := 0; ; i++ {
					oRaw, okO := req.Variables[fmt.Sprintf("o%d", i)]
					nRaw, okN := req.Variables[fmt.Sprintf("n%d", i)]
					if !okO || !okN {
						break
					}
					alias := fmt.Sprintf("r%d", i)
					if nullStar == alias {
						data[alias] = nil
						continue
					}
					fullName := fmt.Sprintf("%s/%s", oRaw, nRaw)
					repo, exists := f.repos[fullName]
					if !exists {
						data[alias] = nil
						continue
					}
					data[alias] = map[string]int{"stargazerCount": repo.stars}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
				return
			}

			f.inc("graphql_blobs")
			if fail {
				http.Error(w, "bad gateway", http.StatusBadGateway)
				return
			}
			data := make(map[string]any)
			for i := 0; ; i++ {
				oRaw, okO := req.Variables[fmt.Sprintf("o%d", i)]
				nRaw, okN := req.Variables[fmt.Sprintf("n%d", i)]
				eRaw, okE := req.Variables[fmt.Sprintf("e%d", i)]
				if !okO || !okN || !okE {
					break
				}
				alias := fmt.Sprintf("b%d", i)

				shouldNull := false
				if nullBlob == alias {
					f.mu.Lock()
					if !f.nullBlobUsed {
						f.nullBlobUsed = true
						shouldNull = true
					}
					f.mu.Unlock()
				}
				if shouldNull {
					data[alias] = nil
					continue
				}

				fullName := fmt.Sprintf("%s/%s", oRaw, nRaw)
				path := strings.TrimPrefix(fmt.Sprint(eRaw), "HEAD:")
				doc, exists := f.findSkill(fullName, path)
				if !exists {
					data[alias] = map[string]any{"object": nil}
					continue
				}
				data[alias] = map[string]any{
					"object": map[string]any{
						"oid":      doc.sha,
						"text":     doc.content,
						"isBinary": false,
					},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func setupSearchTestEnv(t *testing.T, srv *httptest.Server, token string) string {
	t.Helper()
	origBase := githubAPIBase
	origToken := githubToken
	origCacheDir := searchCacheDir
	origNow := timeNow

	cacheDir := t.TempDir()
	githubAPIBase = srv.URL
	githubToken = func() string { return token }
	searchCacheDir = func() string { return cacheDir }

	t.Cleanup(func() {
		githubAPIBase = origBase
		githubToken = origToken
		searchCacheDir = origCacheDir
		timeNow = origNow
	})

	return cacheDir
}

func assertEqualSearchResults(t *testing.T, got, want []SearchResult) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Source != want[i].Source ||
			got[i].Name != want[i].Name ||
			got[i].Description != want[i].Description ||
			got[i].Stars != want[i].Stars ||
			got[i].Skill != want[i].Skill {
			t.Fatalf("result[%d] mismatch:\ngot:  %+v\nwant: %+v", i, got[i], want[i])
		}
	}
}

func TestSearch_GraphQLMatchesREST(t *testing.T) {
	fake := newFakeGitHubData()
	srv := fake.server(t)

	setupSearchTestEnv(t, srv, "test")
	gqlResults, err := Search("pdf", 50)
	if err != nil {
		t.Fatalf("GraphQL Search failed: %v", err)
	}
	if len(gqlResults) < 30 {
		t.Fatalf("expected >= 30 filtered results, got %d", len(gqlResults))
	}
	if fake.count("contents") != 0 {
		t.Errorf("expected 0 /contents/ calls, got %d", fake.count("contents"))
	}
	if fake.count("repos") != 0 {
		t.Errorf("expected 0 /repos/ star calls, got %d", fake.count("repos"))
	}
	if fake.count("graphql_stars") != 1 {
		t.Errorf("expected 1 stars GraphQL call, got %d", fake.count("graphql_stars"))
	}
	// 38 SKILL.md files.
	if want := (38 + metadataGraphQLBatchSize - 1) / metadataGraphQLBatchSize; fake.count("graphql_blobs") != want {
		t.Errorf("expected %d blob GraphQL calls for 38 items, got %d", want, fake.count("graphql_blobs"))
	}

	fake.resetCounts()
	setupSearchTestEnv(t, srv, "")
	restResults, err := Search("pdf", 50)
	if err != nil {
		t.Fatalf("REST Search failed: %v", err)
	}
	if fake.count("graphql_stars") != 0 || fake.count("graphql_blobs") != 0 {
		t.Errorf("expected 0 GraphQL calls without token, got stars=%d blobs=%d", fake.count("graphql_stars"), fake.count("graphql_blobs"))
	}
	if fake.count("contents") == 0 || fake.count("repos") == 0 {
		t.Errorf("expected REST calls without token, got contents=%d repos=%d", fake.count("contents"), fake.count("repos"))
	}

	assertEqualSearchResults(t, gqlResults, restResults)
}

func TestSearch_GraphQL502FallsBackToREST(t *testing.T) {
	fake := newFakeGitHubData()
	srv := fake.server(t)

	setupSearchTestEnv(t, srv, "")
	want, err := Search("pdf", 50)
	if err != nil {
		t.Fatalf("REST baseline Search failed: %v", err)
	}

	fake.resetCounts()
	fake.mu.Lock()
	fake.failGraphQL = true
	fake.mu.Unlock()

	setupSearchTestEnv(t, srv, "test")
	got, err := Search("pdf", 50)
	if err != nil {
		t.Fatalf("GraphQL 502 fallback Search failed: %v", err)
	}
	if fake.count("contents") == 0 || fake.count("repos") == 0 {
		t.Errorf("expected REST fallback calls on 502, got contents=%d repos=%d", fake.count("contents"), fake.count("repos"))
	}

	assertEqualSearchResults(t, got, want)
}

func TestSearch_NullAliasFallsBackPerItem(t *testing.T) {
	fake := newFakeGitHubData()
	srv := fake.server(t)

	setupSearchTestEnv(t, srv, "test")
	want, err := Search("pdf", 50)
	if err != nil {
		t.Fatalf("baseline Search failed: %v", err)
	}

	fake.resetCounts()
	fake.mu.Lock()
	fake.nullStarAlias = "r0"
	fake.nullBlobAlias = "b0"
	fake.mu.Unlock()

	setupSearchTestEnv(t, srv, "test")
	got, err := Search("pdf", 50)
	if err != nil {
		t.Fatalf("null-alias Search failed: %v", err)
	}
	if fake.count("contents") != 1 {
		t.Errorf("expected 1 /contents/ fallback call, got %d", fake.count("contents"))
	}
	if fake.count("repos") != 1 {
		t.Errorf("expected 1 /repos/ star fallback call, got %d", fake.count("repos"))
	}

	assertEqualSearchResults(t, got, want)
}

func TestSearch_DiskCache(t *testing.T) {
	fake := newFakeGitHubData()
	srv := fake.server(t)

	setupSearchTestEnv(t, srv, "test")
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	timeNow = func() time.Time { return now }

	first, err := Search("pdf", 20)
	if err != nil {
		t.Fatalf("cold Search failed: %v", err)
	}
	if len(first) != 20 {
		t.Fatalf("expected 20 results, got %d", len(first))
	}

	// Repeat search hits queries.json with zero HTTP requests.
	fake.resetCounts()
	repeat, err := Search("pdf", 20)
	if err != nil {
		t.Fatalf("repeat Search failed: %v", err)
	}
	if fake.totalRequests() != 0 {
		t.Errorf("expected 0 requests on repeat search, got %d", fake.totalRequests())
	}
	assertEqualSearchResults(t, repeat, first)

	// Smaller limit slice from the same cached query also makes zero HTTP requests.
	fake.resetCounts()
	top5, err := Search("pdf", 5)
	if err != nil {
		t.Fatalf("Search(pdf, 5) failed: %v", err)
	}
	if fake.totalRequests() != 0 {
		t.Errorf("expected 0 requests for Search(pdf, 5), got %d", fake.totalRequests())
	}
	assertEqualSearchResults(t, top5, first[:5])

	// 11 minutes later: queries.json expired, stars.json and metadata.json still valid.
	fake.resetCounts()
	now = now.Add(11 * time.Minute)
	after11m, err := Search("pdf", 20)
	if err != nil {
		t.Fatalf("Search after 11m failed: %v", err)
	}
	if fake.count("search/code") == 0 {
		t.Errorf("expected candidate searches to run after query TTL")
	}
	if fake.count("graphql_stars") != 0 || fake.count("graphql_blobs") != 0 {
		t.Errorf("expected 0 GraphQL calls after 11m, got stars=%d blobs=%d", fake.count("graphql_stars"), fake.count("graphql_blobs"))
	}
	if fake.count("contents") != 0 || fake.count("repos") != 0 {
		t.Errorf("expected 0 REST enrichment calls after 11m, got contents=%d repos=%d", fake.count("contents"), fake.count("repos"))
	}
	assertEqualSearchResults(t, after11m, first)

	// 25 hours later: stars.json expired, metadata.json never expires.
	fake.resetCounts()
	now = now.Add(25 * time.Hour)
	after25h, err := Search("pdf", 20)
	if err != nil {
		t.Fatalf("Search after 25h failed: %v", err)
	}
	if fake.count("graphql_stars") != 1 {
		t.Errorf("expected 1 stars GraphQL call after 25h, got %d", fake.count("graphql_stars"))
	}
	if fake.count("graphql_blobs") != 0 || fake.count("contents") != 0 {
		t.Errorf("expected 0 SKILL.md fetches after 25h, got graphql_blobs=%d contents=%d", fake.count("graphql_blobs"), fake.count("contents"))
	}
	assertEqualSearchResults(t, after25h, first)
}

func TestSearch_CorruptCacheIgnored(t *testing.T) {
	fake := newFakeGitHubData()
	srv := fake.server(t)
	cacheDir := setupSearchTestEnv(t, srv, "test")

	if err := os.WriteFile(filepath.Join(cacheDir, "queries.json"), []byte("{corrupt"), 0o644); err != nil {
		t.Fatalf("write corrupt queries.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "metadata.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("write corrupt metadata.json: %v", err)
	}

	results, err := Search("pdf", 20)
	if err != nil {
		t.Fatalf("Search with corrupt cache failed: %v", err)
	}
	if len(results) != 20 {
		t.Fatalf("expected 20 results, got %d", len(results))
	}
}

func TestSearch_RepoScopedQuery(t *testing.T) {
	fake := newFakeGitHubData()
	srv := fake.server(t)
	setupSearchTestEnv(t, srv, "test")

	results, err := Search("acme/pdf-tools", 20)
	if err != nil {
		t.Fatalf("repo-scoped Search failed: %v", err)
	}
	if len(results) != 8 {
		t.Fatalf("expected 8 results for acme/pdf-tools, got %d", len(results))
	}
	if fake.count("search/code") != 1 {
		t.Errorf("expected 1 /search/code call, got %d", fake.count("search/code"))
	}
	if fake.count("search/repositories") != 0 {
		t.Errorf("expected 0 /search/repositories calls, got %d", fake.count("search/repositories"))
	}
	if fake.count("git/trees") != 0 {
		t.Errorf("expected 0 /git/trees calls, got %d", fake.count("git/trees"))
	}
	for _, r := range results {
		if r.Owner != "acme" || r.Repo != "pdf-tools" {
			t.Errorf("unexpected result outside acme/pdf-tools: %+v", r)
		}
		if r.Stars != 200 {
			t.Errorf("expected Stars=200 for %s, got %d", r.Source, r.Stars)
		}
	}
}

// A failed side search leaves results out, so the next search must ask
// GitHub again instead of serving the incomplete list from the cache.
func TestSearch_IncompleteResultsAreNotCached(t *testing.T) {
	fake := newFakeGitHubData()
	srv := fake.server(t)
	setupSearchTestEnv(t, srv, "test")

	fake.mu.Lock()
	fake.failCodeSearch = "repo:anthropics/skills"
	fake.mu.Unlock()
	if _, err := Search("pdf", 20); err != nil {
		t.Fatalf("Search with a failing side search: %v", err)
	}

	fake.mu.Lock()
	fake.failCodeSearch = ""
	fake.mu.Unlock()
	fake.resetCounts()
	if _, err := Search("pdf", 20); err != nil {
		t.Fatalf("second Search: %v", err)
	}
	if fake.count("search/code") == 0 {
		t.Error("second search was served from the cache after an incomplete first search")
	}

	fake.resetCounts()
	if _, err := Search("pdf", 20); err != nil {
		t.Fatalf("third Search: %v", err)
	}
	if fake.totalRequests() != 0 {
		t.Errorf("complete search was not cached: %d requests on repeat", fake.totalRequests())
	}
}
