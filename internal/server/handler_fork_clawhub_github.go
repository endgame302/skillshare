package server

// Fork-only (endgame302/skillshare): verify a GitHub-hosted ClawHub entry against the feed's
// content hash, install it pinned to that commit, and confirm the installed git tree matches.

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"skillshare/internal/forkmarket/clawhub"
	"skillshare/internal/install"
	"skillshare/internal/sourcefs"
)

// installClawHubGitHub installs a GitHub-hosted entry. Its folder's stored bytes are checked
// first (a git checkout cannot be: core.autocrlf rewrites line endings), then skillshare's own
// GitHub install pins the same commit, and its recorded tree id must be the tree checked.
func (s *Server) installClawHubGitHub(w http.ResponseWriter, r *http.Request, start time.Time, client *clawhub.Client, entry clawhub.Entry, cand clawhub.Candidate, slug string, force bool) {
	folder, err := client.VerifyGitHub(r.Context(), cand)
	if err != nil {
		writeClawHubFetchError(w, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.trackProjectLock()()
	before := s.skillsStoreOnDisk()
	sourceDir := s.cfg.EffectiveSkillsSource()
	if !force && clawhubSkillExists(sourceDir, slug) {
		writeClawHubExists(w, slug)
		return
	}

	raw := "github.com/" + folder.Repo + "/" + folder.Path
	source, err := install.ParseSourceWithOptions(raw, s.parseOpts())
	if err != nil || source.Type != install.SourceTypeGitHub || source.Subdir != folder.Path ||
		!strings.EqualFold(source.CloneURL, "https://github.com/"+folder.Repo+".git") || source.Branch != "" {
		writeCodedError(w, http.StatusUnprocessableEntity, "clawhub_not_installable",
			(&clawhub.NotInstallableError{Reason: clawhub.BlockedUnsupported}).Error(),
			map[string]string{"reason": clawhub.BlockedUnsupported})
		return
	}
	source.Branch = folder.Commit
	source.Name = slug

	logArgs := map[string]any{
		"source":    clawhub.Origin(entry.ID, folder.Commit),
		"github":    raw + "@" + folder.Commit,
		"mode":      s.installLogMode(),
		"force":     force,
		"threshold": s.auditThreshold(),
		"scope":     "ui",
	}

	opts := install.InstallOptions{
		Name:           slug,
		Force:          force,
		AuditThreshold: s.auditThreshold(),
		SourceDir:      sourceDir,
		SourceFollow:   s.skillsWalk().Follow,
	}
	if s.IsProjectMode() {
		opts.AuditProjectRoot = s.projectRoot
	}
	dest := filepath.Join(sourceDir, slug)
	result, err := installClawHubRelease(source, dest, opts)
	if err != nil {
		logArgs["failed_skills"] = []string{slug}
		s.writeOpsLog("install", "error", start, logArgs, err.Error())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	store, err := install.LoadMetadata(sourceDir)
	var e *install.MetadataEntry
	if err == nil {
		e = store.GetByPath(slug)
	}
	if err != nil || e == nil || e.TreeHash != folder.Tree || e.Branch != folder.Commit {
		if root, rootErr := sourcefs.Open(sourceDir, s.skillsWalk().Follow); rootErr == nil {
			_ = root.RemoveAll(slug)
			root.Close()
		}
		if err == nil && store != nil {
			store.Remove(slug)
			_ = store.Save(sourceDir)
		}
		s.reloadSkillsStore()
		s.reconcileInstalledSkills(before, sourceDir)
		const mismatchMsg = "the installed folder is not the one checked against ClawHub's feed, so it was removed"
		logArgs["failed_skills"] = []string{slug}
		s.writeOpsLog("install", "error", start, logArgs, mismatchMsg)
		writeCodedError(w, http.StatusBadGateway, "clawhub_integrity_mismatch", mismatchMsg, nil)
		return
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
		"origin":    clawhub.Origin(entry.ID, folder.Commit),
		"version":   folder.Commit,
		"github":    raw,
	})
}
