// Fork-only (endgame302/skillshare): the MCP marketplace on the dashboard's Hubs page. It lists
// servers from the official MCP Registry, GitHub's registry and Docker's catalog, and plans an
// install: one entry becomes a skillshare MCP server, which the dashboard then saves through the
// MCP routes, as the MCP page does, so nothing here writes the MCP config.

package server

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"

	"skillshare/internal/config"
	"skillshare/internal/forkmarket/mcpreg"
)

// newForkMCPMarket is replaced in tests.
var newForkMCPMarket = func() *mcpreg.Market {
	return mcpreg.NewMarket(filepath.Join(config.CacheDir(), "mcpmarket"))
}

var (
	forkMCPMarketMu   sync.Mutex
	forkMCPMarketInst *mcpreg.Market
)

// forkMCPMarket returns one market for the life of the server, so the official registry's index
// and its background pass outlive a request.
func forkMCPMarket() *mcpreg.Market {
	forkMCPMarketMu.Lock()
	defer forkMCPMarketMu.Unlock()
	if forkMCPMarketInst == nil {
		forkMCPMarketInst = newForkMCPMarket()
	}
	return forkMCPMarketInst
}

func (s *Server) registerForkMCPMarketRoutes() {
	// Lists and plans fetch from the registries on a cache miss, so only the dashboard may ask.
	s.mux.HandleFunc("GET /api/fork/mcpmarket/servers", requireSameOrigin(s.handleForkMCPMarketServers))
	s.mux.HandleFunc("POST /api/fork/mcpmarket/plan", requireSameOrigin(s.handleForkMCPMarketPlan))
}

func writeForkMCPMarketError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mcpreg.ErrUnknownSource):
		writeCodedError(w, http.StatusBadRequest, "mcpmarket_unknown_source", err.Error(), nil)
	case errors.Is(err, mcpreg.ErrNotFound):
		writeCodedError(w, http.StatusNotFound, "mcpmarket_not_found", err.Error(), nil)
	default:
		writeCodedError(w, http.StatusBadGateway, "mcpmarket_unavailable", err.Error(), nil)
	}
}

func (s *Server) handleForkMCPMarketServers(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	q := mcpreg.Query{
		Text:            v.Get("q"),
		Kind:            v.Get("kind"),
		InstallableOnly: v.Get("installable") == "1",
		ShowBulk:        v.Get("bulk") == "1",
		ShowDeprecated:  v.Get("deprecated") == "1",
		Sort:            v.Get("sort"),
	}
	for key, dst := range map[string]*int{"offset": &q.Offset, "limit": &q.Limit} {
		if raw := v.Get(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				writeCodedError(w, http.StatusBadRequest, "mcpmarket_bad_request", key+" must be a whole number", nil)
				return
			}
			*dst = n
		}
	}
	if len(q.Text) > 200 {
		writeCodedError(w, http.StatusBadRequest, "mcpmarket_bad_request", "the search text is too long", nil)
		return
	}
	res, err := forkMCPMarket().List(r.Context(), v.Get("source"), q, v.Get("refresh") == "1")
	if err != nil {
		writeForkMCPMarketError(w, err)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleForkMCPMarketPlan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string            `json:"source"`
		Name   string            `json:"name"`
		Values map[string]string `json:"values"`
	}
	if err := decodeJSON(w, r, &body, defaultJSONBodyLimit); err != nil {
		if !errors.Is(err, errBodyTooLarge) {
			writeCodedError(w, http.StatusBadRequest, "mcpmarket_bad_request", "invalid JSON body", nil)
		}
		return
	}
	bad := func(msg string) { writeCodedError(w, http.StatusBadRequest, "mcpmarket_bad_request", msg, nil) }
	if body.Name == "" || len(body.Name) > 300 {
		bad("name must name one server")
		return
	}
	if len(body.Values) > 64 {
		bad("too many values")
		return
	}
	for k, v := range body.Values {
		if len(k) > 200 || len(v) > 4096 {
			bad("a value is too long")
			return
		}
	}
	res, err := forkMCPMarket().Plan(r.Context(), body.Source, body.Name, body.Values)
	if err != nil {
		writeForkMCPMarketError(w, err)
		return
	}
	writeJSON(w, res)
}
