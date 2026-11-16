package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"skillshare/internal/forkmarket/agentdir"
	"skillshare/internal/forkmarket/ghrepo"
)

func serveForkAgents(t *testing.T, s *Server, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	return rr
}

func forkAgentsErrorCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"error_code"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rr.Body.String(), err)
	}
	return body.Code
}

func ensureForkAgentsRoute(t *testing.T, s *Server) {
	t.Helper()
	if rr := serveForkAgents(t, s, "/api/fork/agents/listing?source=nope"); rr.Code == http.StatusNotFound {
		s.registerForkAgentsRoutes()
	}
}

func TestForkAgentsListing(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	var apiStatus = http.StatusOK

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiStatus != http.StatusOK {
			w.WriteHeader(apiStatus)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/commits/HEAD"):
			w.Write([]byte(commit))
		case strings.Contains(r.URL.Path, "/git/trees/"):
			json.NewEncoder(w).Encode(map[string]any{
				"truncated": false,
				"tree": []ghrepo.Entry{
					{Path: "categories/01-core/helper.md", Type: "blob", Mode: "100644", Size: 40},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiSrv.Close()

	rawSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("---\nname: Helper\ndescription: Helps out\n---\n"))
	}))
	defer rawSrv.Close()

	aitmplSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"schemaVersion":"1.0.0","source":"https://github.com/davila7/claude-code-templates","components":[]}`))
	}))
	defer aitmplSrv.Close()

	oldFactory := newAgentDirClient
	t.Cleanup(func() { newAgentDirClient = oldFactory })

	newAgentDirClient = func() *agentdir.Client {
		c := agentdir.New(t.TempDir())
		c.GH.API = apiSrv.URL
		c.GH.Raw = rawSrv.URL
		c.GH.Token = func() string { return "" }
		c.HTTP = aitmplSrv.Client()
		c.AitmplURL = aitmplSrv.URL
		return c
	}

	s, _ := newTestServer(t)
	ensureForkAgentsRoute(t, s)

	// 200 with agents.
	rr := serveForkAgents(t, s, "/api/fork/agents/listing?source=voltagent")
	if rr.Code != http.StatusOK {
		t.Fatalf("voltagent status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var listing agentdir.Listing
	if err := json.Unmarshal(rr.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.Source != "voltagent" || len(listing.Agents) != 1 || listing.Agents[0].Title != "Helper" {
		t.Fatalf("listing = %+v", listing)
	}

	// 400 unknown source.
	rr = serveForkAgents(t, s, "/api/fork/agents/listing?source=nope")
	if rr.Code != http.StatusBadRequest || forkAgentsErrorCode(t, rr) != "agents.unknown_source" {
		t.Fatalf("nope = %d %s", rr.Code, rr.Body.String())
	}

	// 502 upstream 500.
	apiStatus = http.StatusInternalServerError
	rr = serveForkAgents(t, s, "/api/fork/agents/listing?source=voltagent")
	if rr.Code != http.StatusBadGateway || forkAgentsErrorCode(t, rr) != "agents.unavailable" {
		t.Fatalf("upstream 500 = %d %s", rr.Code, rr.Body.String())
	}

	// 502 upstream 403 rate limited.
	apiStatus = http.StatusForbidden
	rr = serveForkAgents(t, s, "/api/fork/agents/listing?source=voltagent")
	if rr.Code != http.StatusBadGateway || forkAgentsErrorCode(t, rr) != "agents.rate_limited" {
		t.Fatalf("upstream 403 = %d %s", rr.Code, rr.Body.String())
	}
}

func TestForkAgentsRefusesOtherSites(t *testing.T) {
	s, _ := newTestServer(t)
	oldFactory := newAgentDirClient
	t.Cleanup(func() { newAgentDirClient = oldFactory })
	newAgentDirClient = func() *agentdir.Client {
		c := agentdir.New(t.TempDir())
		c.GH.Token = func() string { return "" }
		return c
	}
	ensureForkAgentsRoute(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/fork/agents/listing?source=nope", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("cross-site status = %d, want 403", rr.Code)
	}
}
