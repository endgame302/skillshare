package server

// Fork-only (endgame302/skillshare): browse ClawHub's skills feed on the Hubs page and install
// a ClawHub-hosted skill after checking its archive against the feed's integrity hash.

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"skillshare/internal/config"
	"skillshare/internal/forkmarket/clawhub"
	"skillshare/internal/install"
)

// newClawHubClient is replaced in tests. The feed is a cache; verified releases are data, since
// installed skills name them as their source.
var newClawHubClient = func() *clawhub.Client {
	return clawhub.New(filepath.Join(config.CacheDir(), "clawhub"), filepath.Join(config.DataDir(), "clawhub"))
}

// installClawHubRelease is install.Install; tests replace it to make an install fail.
var installClawHubRelease = install.Install

func (s *Server) registerForkClawHubRoutes() {
	// The feed is fetched from clawhub.ai on a cache miss, so only the dashboard may ask.
	s.mux.HandleFunc("GET /api/fork/clawhub/feed", requireSameOrigin(s.handleClawHubFeed))
	s.mux.HandleFunc("POST /api/fork/clawhub/install", s.handleClawHubInstall)
}

func (s *Server) handleClawHubFeed(w http.ResponseWriter, r *http.Request) {
	snap, err := newClawHubClient().Feed(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeCodedError(w, http.StatusBadGateway, "clawhub_feed_unavailable", err.Error(), nil)
		return
	}
	body := map[string]any{
		"items":       snap.Feed.Items(),
		"generatedAt": snap.Feed.GeneratedAt,
		"expiresAt":   snap.Feed.ExpiresAt,
		"fetchedAt":   snap.FetchedAt.Format(time.RFC3339),
		"registry":    clawhub.DefaultRegistry,
		"stale":       snap.Stale,
	}
	if snap.Stale {
		body["staleReason"] = snap.FetchErr.Error()
	}
	writeJSON(w, body)
}

// handleClawHubInstall installs the entry the request names, as the cached feed describes it:
// the download host, version and hash come from the feed, never from the request.
func (s *Server) handleClawHubInstall(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var body struct {
		ID      string `json:"id"`
		Version string `json:"version"`
		Force   bool   `json:"force"`
	}
	if err := decodeJSON(w, r, &body, defaultJSONBodyLimit); err != nil {
		if !errors.Is(err, errBodyTooLarge) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return
	}
	if _, _, ok := clawhub.ParseID(body.ID); !ok {
		writeError(w, http.StatusBadRequest, "id must be a ClawHub skill id such as @owner/slug")
		return
	}

	client := newClawHubClient()
	snap, err := client.Feed(r.Context(), false)
	if err != nil {
		writeCodedError(w, http.StatusBadGateway, "clawhub_feed_unavailable", err.Error(), nil)
		return
	}
	entry, ok := snap.Feed.Lookup(body.ID)
	if !ok {
		writeCodedError(w, http.StatusNotFound, "clawhub_not_found", "ClawHub's feed no longer lists "+body.ID, nil)
		return
	}
	cand, blocked := entry.HostedCandidate()
	if blocked != "" {
		writeCodedError(w, http.StatusUnprocessableEntity, "clawhub_not_installable",
			(&clawhub.NotInstallableError{Reason: blocked}).Error(), map[string]string{"reason": blocked})
		return
	}
	if body.Version != "" && body.Version != cand.Version {
		writeCodedError(w, http.StatusConflict, "clawhub_version_changed",
			"ClawHub's feed now lists version "+cand.Version+"; refresh the list", map[string]string{"version": cand.Version})
		return
	}
	_, slug, _ := clawhub.ParseID(entry.ID)

	// Refuse before downloading when the name is taken; checked again under the lock.
	s.mu.RLock()
	skillsDir := s.cfg.EffectiveSkillsSource()
	s.mu.RUnlock()
	if !body.Force && clawhubSkillExists(skillsDir, slug) {
		writeClawHubExists(w, slug)
		return
	}

	// Download, verify and unpack without the server lock: it can take a while. The release then
	// moves into its own folder of the store, which the installed skill records as its source.
	rel, err := client.Fetch(r.Context(), snap, entry)
	if err != nil {
		var dl *clawhub.DownloadError
		var ni *clawhub.NotInstallableError
		switch {
		case clawhub.IsIntegrityError(err):
			writeCodedError(w, http.StatusBadGateway, "clawhub_integrity_mismatch", err.Error(), nil)
		case errors.Is(err, clawhub.ErrNoSkillFile):
			writeCodedError(w, http.StatusUnprocessableEntity, "clawhub_no_skill_file", err.Error(), nil)
		case errors.Is(err, clawhub.ErrUnsafeArchive):
			writeCodedError(w, http.StatusUnprocessableEntity, "clawhub_unsafe_archive", err.Error(), nil)
		case errors.As(err, &ni):
			writeCodedError(w, http.StatusUnprocessableEntity, "clawhub_not_installable", err.Error(), map[string]string{"reason": ni.Reason})
		case errors.As(err, &dl):
			writeCodedError(w, http.StatusBadGateway, "clawhub_download_failed", err.Error(), nil)
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	defer rel.Discard()

	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.trackProjectLock()()
	before := s.skillsStoreOnDisk()
	sourceDir := s.cfg.EffectiveSkillsSource()
	if !body.Force && clawhubSkillExists(sourceDir, slug) {
		writeClawHubExists(w, slug)
		return
	}
	// Under the lock, so no other install is copying from the folder it replaces.
	if err := rel.Place(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to keep the ClawHub release: "+err.Error())
		return
	}
	// A copy this request created is removed unless a skill ends up naming it as its source.
	keep := !rel.Fresh
	defer func() {
		if !keep {
			os.RemoveAll(rel.Dir)
		}
	}()
	logArgs := map[string]any{
		"source":    rel.Origin,
		"mode":      s.installLogMode(),
		"force":     body.Force,
		"threshold": s.auditThreshold(),
		"scope":     "ui",
	}

	// The release folder goes through skillshare's own local install, so the copy, audit and
	// records are those of any other local install: update and a reinstall from the records copy
	// the folder again, and its path ends in the ClawHub origin, @owner/slug@version.
	source := &install.Source{Type: install.SourceTypeLocalPath, Path: rel.Dir, Raw: rel.Dir, Name: slug}
	opts := install.InstallOptions{
		Name:           slug,
		Force:          body.Force,
		AuditThreshold: s.auditThreshold(),
		SourceDir:      sourceDir,
		SourceFollow:   s.skillsWalk().Follow,
	}
	if s.IsProjectMode() {
		opts.AuditProjectRoot = s.projectRoot
	}
	result, err := installClawHubRelease(source, filepath.Join(sourceDir, slug), opts)
	if err != nil {
		logArgs["failed_skills"] = []string{slug}
		s.writeOpsLog("install", "error", start, logArgs, err.Error())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	keep = true
	// The local install records no version; the ClawHub release is the skill's version.
	if store, err := install.LoadMetadata(sourceDir); err == nil {
		if e := store.GetByPath(slug); e != nil {
			e.Version = rel.Version
			if err := store.Save(sourceDir); err != nil {
				result.Warnings = append(result.Warnings, "failed to record the ClawHub version: "+err.Error())
			}
		}
	}

	s.reloadSkillsStore()
	s.reconcileInstalledSkills(before, sourceDir)
	logArgs["skill_count"] = 1
	logArgs["installed_skills"] = []string{result.SkillName}
	s.writeOpsLog("install", "ok", start, logArgs, "")

	writeJSON(w, map[string]any{
		"skillName": result.SkillName,
		"action":    result.Action,
		"warnings":  result.Warnings,
		"origin":    rel.Origin,
		"version":   rel.Version,
	})
}

func clawhubSkillExists(sourceDir, slug string) bool {
	_, err := os.Lstat(filepath.Join(sourceDir, slug))
	return err == nil
}

func writeClawHubExists(w http.ResponseWriter, slug string) {
	writeCodedError(w, http.StatusConflict, "clawhub_exists",
		"a skill named "+slug+" is already installed", map[string]string{"name": slug})
}
