package server

import (
	"net/http"
	"strings"
	"time"

	"skillshare/internal/plainhub"
)

// plainHubInfo describes how a plain GitHub repo hub was indexed (fork-only,
// endgame302/skillshare). Only Plain is set until the hub has been loaded once.
type plainHubInfo struct {
	Plain     bool       `json:"plain"`
	Repo      string     `json:"repo,omitempty"`
	Commit    string     `json:"commit,omitempty"`
	IndexedAt *time.Time `json:"indexedAt,omitempty"`
	Listed    int        `json:"listed,omitempty"`
	Total     int        `json:"total,omitempty"`
	Capped    bool       `json:"capped,omitempty"`
}

// handleForkPlainHub reports whether a hub URL is a plain GitHub repo and, once
// GET /api/search?hub= has built its index, the commit it was built from and
// whether the skill list was cut at the cap. It reads the cache only.
func (s *Server) handleForkPlainHub(w http.ResponseWriter, r *http.Request) {
	hub := strings.TrimSpace(r.URL.Query().Get("hub"))
	info := plainHubInfo{Plain: plainhub.Detect(hub)}
	if info.Plain {
		if idx, ok := plainhub.Peek(hub); ok {
			at := idx.IndexedAt
			info.Repo = idx.Repo
			info.Commit = idx.Commit
			info.IndexedAt = &at
			info.Listed = len(idx.Skills)
			info.Total = idx.Total
			info.Capped = idx.Capped()
		}
	}
	writeJSON(w, info)
}
