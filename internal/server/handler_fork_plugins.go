// Fork-only (endgame302/skillshare): plugins marketplace catalog reader and Claude CLI hand-off.

package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"skillshare/internal/config"
	"skillshare/internal/forkmarket/ghrepo"
	"skillshare/internal/forkmarket/plugincat"
)

var newPluginCatalogClient = func() *plugincat.Client {
	return plugincat.New(filepath.Join(config.CacheDir(), "plugincat"))
}

// forkClaudeRunner runs the claude CLI; tests replace it.
var forkClaudeRunner plugincat.Runner = plugincat.ExecRunner{}

// forkHandoffMu lets one hand-off run at a time.
var forkHandoffMu sync.Mutex

func (s *Server) registerForkPluginCatalogRoutes() {
	s.mux.HandleFunc("GET /api/fork/plugins/catalog", requireSameOrigin(s.handleForkPluginCatalog))
	// It runs the native Claude CLI, so only the local dashboard may ask, like upstream's plugin routes.
	s.mux.HandleFunc("POST /api/fork/plugins/handoff", s.requireLocalPlugin(requireSameOrigin(s.handleForkPluginHandoff)))
}

func (s *Server) handleForkPluginCatalog(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	qRefresh := r.URL.Query().Get("refresh")
	refresh := qRefresh == "1" || strings.EqualFold(qRefresh, "true")

	cat, err := newPluginCatalogClient().Catalog(r.Context(), id, refresh)
	if err != nil {
		switch {
		case errors.Is(err, plugincat.ErrUnknownCatalog):
			writeCodedError(w, http.StatusBadRequest, "plugins_market.unknown_catalog", err.Error(), nil)
		case errors.Is(err, ghrepo.ErrRateLimited):
			writeCodedError(w, http.StatusBadGateway, "plugins_market.rate_limited", err.Error(), nil)
		default:
			writeCodedError(w, http.StatusBadGateway, "plugins_market.unavailable", err.Error(), nil)
		}
		return
	}
	writeJSON(w, cat)
}

func (s *Server) handleForkPluginHandoff(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Catalog string `json:"catalog"`
		Name    string `json:"name"`
		Commit  string `json:"commit"`
		SHA     string `json:"sha"`
		DryRun  bool   `json:"dryRun"`
	}
	if err := decodeJSON(w, r, &body, defaultJSONBodyLimit); err != nil {
		if !errors.Is(err, errBodyTooLarge) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return
	}

	if s.IsProjectMode() {
		writeCodedError(w, http.StatusBadRequest, "plugins_market.project_mode",
			"Installing through Claude Code is available in global mode only", nil)
		return
	}

	if !isKnownPluginCatalog(body.Catalog) {
		writeCodedError(w, http.StatusBadRequest, "plugins_market.unknown_catalog",
			plugincat.ErrUnknownCatalog.Error(), nil)
		return
	}

	cat := newPluginCatalogClient().Cached(body.Catalog)
	if cat == nil {
		writeCodedError(w, http.StatusConflict, "plugins_market.catalog_changed", "Load the catalog again", nil)
		return
	}

	if body.Commit != cat.Commit {
		writeCodedError(w, http.StatusConflict, "plugins_market.catalog_changed", "Load the catalog again", nil)
		return
	}

	entry, ok := cat.Lookup(body.Name)
	if !ok {
		writeCodedError(w, http.StatusNotFound, "plugins_market.not_found",
			"Plugin "+body.Name+" was not found in the catalog", nil)
		return
	}

	if entry.Install != "native" {
		writeCodedError(w, http.StatusUnprocessableEntity, "plugins_market.not_native",
			"This entry is not installed through Claude Code",
			map[string]string{"install": entry.Install})
		return
	}

	if body.SHA != entry.Source.SHA {
		writeCodedError(w, http.StatusConflict, "plugins_market.catalog_changed", "Load the catalog again", nil)
		return
	}

	if !forkHandoffMu.TryLock() {
		writeCodedError(w, http.StatusConflict, "plugins_market.busy",
			"Another Claude Code hand-off is already running", nil)
		return
	}
	defer forkHandoffMu.Unlock()

	plan, err := plugincat.PlanHandoff(r.Context(), forkClaudeRunner, cat, entry)
	if err != nil {
		if errors.Is(err, plugincat.ErrMarketConflict) {
			writeCodedError(w, http.StatusConflict, "plugins_market.market_conflict", err.Error(),
				map[string]string{"market": cat.Market})
			return
		}
		writeCodedError(w, http.StatusBadGateway, "plugins_market.claude_failed", err.Error(), nil)
		return
	}

	if body.DryRun {
		writeJSON(w, map[string]any{"plan": plan})
		return
	}

	if plan.Claude == "" {
		writeCodedError(w, http.StatusPreconditionFailed, "plugins_market.claude_missing",
			plugincat.ErrClaudeMissing.Error(), nil)
		return
	}

	start := time.Now()
	result, err := plugincat.RunHandoff(context.WithoutCancel(r.Context()), forkClaudeRunner, cat, entry, plan)
	logArgs := map[string]any{
		"plugin":  plan.PluginID,
		"catalog": cat.Repo,
		"scope":   "ui",
	}
	if err != nil {
		s.writeOpsLog("plugin handoff", "error", start, logArgs, err.Error())
		var se *plugincat.StepError
		switch {
		case errors.Is(err, plugincat.ErrCatalogMoved):
			writeCodedError(w, http.StatusConflict, "plugins_market.catalog_moved", err.Error(), nil)
		case errors.As(err, &se):
			writeCodedError(w, http.StatusBadGateway, "plugins_market.claude_failed", err.Error(), map[string]string{
				"step":        se.Step,
				"failureCode": se.FailureCode,
			})
		default:
			writeCodedError(w, http.StatusBadGateway, "plugins_market.claude_failed", err.Error(), nil)
		}
		return
	}

	s.writeOpsLog("plugin handoff", "ok", start, logArgs, "")
	writeJSON(w, map[string]any{
		"plan":   plan,
		"result": result,
	})
}

func isKnownPluginCatalog(id string) bool {
	for _, c := range plugincat.Catalogs {
		if c.ID == id {
			return true
		}
	}
	return false
}
