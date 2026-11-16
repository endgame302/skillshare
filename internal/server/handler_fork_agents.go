// Fork-only (endgame302/skillshare): the agents marketplace on the dashboard's Hubs page.

package server

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"skillshare/internal/config"
	"skillshare/internal/forkmarket/agentdir"
	"skillshare/internal/forkmarket/ghrepo"
)

// newAgentDirClient is replaced in tests.
var newAgentDirClient = func() *agentdir.Client {
	return agentdir.New(filepath.Join(config.CacheDir(), "agentdir"))
}

func (s *Server) registerForkAgentsRoutes() {
	// A cache miss fetches from GitHub or aitmpl.com, so only the dashboard may ask.
	s.mux.HandleFunc("GET /api/fork/agents/listing", requireSameOrigin(s.handleForkAgentsListing))
}

func (s *Server) handleForkAgentsListing(w http.ResponseWriter, r *http.Request) {
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	qRefresh := r.URL.Query().Get("refresh")
	refresh := qRefresh == "1" || strings.EqualFold(qRefresh, "true")

	listing, err := newAgentDirClient().List(r.Context(), source, refresh)
	if err != nil {
		switch {
		case errors.Is(err, agentdir.ErrUnknownSource):
			writeCodedError(w, http.StatusBadRequest, "agents.unknown_source", err.Error(), nil)
		case errors.Is(err, ghrepo.ErrRateLimited):
			writeCodedError(w, http.StatusBadGateway, "agents.rate_limited", err.Error(), nil)
		default:
			writeCodedError(w, http.StatusBadGateway, "agents.unavailable", err.Error(), nil)
		}
		return
	}
	writeJSON(w, listing)
}
