// Fork-only (endgame302/skillshare): the MCP marketplace routes.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"skillshare/internal/forkmarket/mcpreg"
)

// useFakeMCPMarket points the market at local sites: Docker's catalog from the package's
// fixture, and registries that are down.
func useFakeMCPMarket(t *testing.T) {
	t.Helper()
	catalog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "../forkmarket/mcpreg/testdata/docker-catalog.yaml")
	}))
	t.Cleanup(catalog.Close)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(down.Close)

	m := mcpreg.NewMarket(t.TempDir())
	m.Docker.URL = catalog.URL
	m.Official.Base, m.Curated.Base = down.URL, down.URL
	m.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	forkMCPMarketMu.Lock()
	old := forkMCPMarketInst
	forkMCPMarketInst = m
	forkMCPMarketMu.Unlock()
	t.Cleanup(func() {
		forkMCPMarketMu.Lock()
		forkMCPMarketInst = old
		forkMCPMarketMu.Unlock()
	})
}

func serveForkMCPMarket(t *testing.T, s *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	return rr
}

func mcpMarketErrorCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"error_code"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rr.Body.String(), err)
	}
	return body.Code
}

func TestForkMCPMarketServers(t *testing.T) {
	useFakeMCPMarket(t)
	s, _ := newTestServer(t)

	rr := serveForkMCPMarket(t, s, http.MethodGet, "/api/fork/mcpmarket/servers?source=docker&q=brave&limit=10", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var list struct {
		Source      string         `json:"source"`
		Items       []mcpreg.Entry `json:"items"`
		Total       int            `json:"total"`
		DockerFound *bool          `json:"dockerFound"`
		Status      mcpreg.Status  `json:"status"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Source != "docker" || list.Total == 0 || list.Items[0].Name != "brave" || list.DockerFound == nil || *list.DockerFound || list.Status.State != "ready" {
		t.Fatalf("list = %+v", list)
	}

	for target, code := range map[string]string{
		"/api/fork/mcpmarket/servers?source=nope":             "mcpmarket_unknown_source",
		"/api/fork/mcpmarket/servers?source=docker&limit=abc": "mcpmarket_bad_request",
		"/api/fork/mcpmarket/servers?source=docker&offset=-1": "mcpmarket_bad_request",
	} {
		rr := serveForkMCPMarket(t, s, http.MethodGet, target, "")
		if rr.Code != http.StatusBadRequest || mcpMarketErrorCode(t, rr) != code {
			t.Fatalf("%s: %d %s", target, rr.Code, rr.Body.String())
		}
	}

	// A registry that cannot be reached, with nothing cached.
	rr = serveForkMCPMarket(t, s, http.MethodGet, "/api/fork/mcpmarket/servers?source=curated", "")
	if rr.Code != http.StatusBadGateway || mcpMarketErrorCode(t, rr) != "mcpmarket_unavailable" {
		t.Fatalf("curated down: %d %s", rr.Code, rr.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/fork/mcpmarket/servers?source=docker", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr = httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("cross-site: %d", rr.Code)
	}
}

func TestForkMCPMarketPlan(t *testing.T) {
	useFakeMCPMarket(t)
	s, _ := newTestServer(t)

	rr := serveForkMCPMarket(t, s, http.MethodPost, "/api/fork/mcpmarket/plan", `{"source":"docker","name":"brave"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var plan mcpreg.PlanResult
	if err := json.Unmarshal(rr.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Options) != 1 || plan.Options[0].Server == nil || plan.Options[0].Server.Command != "docker" || plan.DockerFound {
		t.Fatalf("plan = %+v", plan)
	}
	missing := false
	for _, w := range plan.Options[0].Warnings {
		missing = missing || w.Code == mcpreg.WarnDockerMissing
	}
	if !missing {
		t.Fatalf("no docker_missing warning: %+v", plan.Options[0].Warnings)
	}

	var many []string
	for i := range 65 {
		many = append(many, fmt.Sprintf(`"k%d":"v"`, i))
	}
	for body, want := range map[string]struct {
		status int
		code   string
	}{
		`{"source":"docker","name":""}`: {http.StatusBadRequest, "mcpmarket_bad_request"},
		`not json`:                      {http.StatusBadRequest, "mcpmarket_bad_request"},
		`{"source":"docker","name":"brave","values":{` + strings.Join(many, ",") + `}}`: {http.StatusBadRequest, "mcpmarket_bad_request"},
		`{"source":"docker","name":"no-such-server"}`:                                   {http.StatusNotFound, "mcpmarket_not_found"},
		`{"source":"glama","name":"x"}`:                                                 {http.StatusBadRequest, "mcpmarket_unknown_source"},
	} {
		rr := serveForkMCPMarket(t, s, http.MethodPost, "/api/fork/mcpmarket/plan", body)
		if rr.Code != want.status || mcpMarketErrorCode(t, rr) != want.code {
			t.Fatalf("%.60s: %d %s", body, rr.Code, rr.Body.String())
		}
	}
}
