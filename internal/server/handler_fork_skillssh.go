// Fork-only (endgame302/skillshare): the skills.sh marketplace on the dashboard's Hubs page.
// Lists and searches come from skills.sh; installing goes through the normal install routes,
// with resolve mapping a skills.sh skill id to the folder that holds it in the GitHub repo.

package server

import (
	"errors"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"skillshare/internal/forkmarket/skillssh"
	"skillshare/internal/install"
	"skillshare/internal/utils"
)

// forkSkillssh keeps skills.sh answers in memory for the life of the server.
var forkSkillssh = skillssh.New()

func (s *Server) registerForkSkillsshRoutes() {
	s.mux.HandleFunc("GET /api/fork/skillssh/board", requireSameOrigin(s.handleForkSkillsshBoard))
	s.mux.HandleFunc("GET /api/fork/skillssh/search", requireSameOrigin(s.handleForkSkillsshSearch))
	s.mux.HandleFunc("POST /api/fork/skillssh/resolve", requireSameOrigin(s.handleForkSkillsshResolve))
}

type forkSkillsshList struct {
	Board     string           `json:"board,omitempty"`
	Skills    []skillssh.Skill `json:"skills"`
	FetchedAt time.Time        `json:"fetchedAt"`
	Stale     bool             `json:"stale,omitempty"`
}

func writeForkSkillsshError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, skillssh.ErrUnknownBoard):
		writeCodedError(w, http.StatusBadRequest, "skillssh.unknown_board", err.Error(), nil)
	case errors.Is(err, skillssh.ErrQueryTooShort):
		writeCodedError(w, http.StatusBadRequest, "skillssh.query_too_short", err.Error(), nil)
	case errors.Is(err, skillssh.ErrFormatChanged):
		writeCodedError(w, http.StatusBadGateway, "skillssh.format_changed", err.Error(), nil)
	default:
		writeCodedError(w, http.StatusBadGateway, "skillssh.unreachable", err.Error(), nil)
	}
}

func (s *Server) handleForkSkillsshBoard(w http.ResponseWriter, r *http.Request) {
	board := skillssh.Board(r.URL.Query().Get("name"))
	if board == "" {
		board = skillssh.AllTime
	}
	res, err := forkSkillssh.Board(r.Context(), board)
	if err != nil {
		writeForkSkillsshError(w, err)
		return
	}
	writeJSON(w, forkSkillsshList{Board: string(board), Skills: res.Skills, FetchedAt: res.FetchedAt, Stale: res.Stale})
}

func (s *Server) handleForkSkillsshSearch(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
		limit = l
	}
	res, err := forkSkillssh.Search(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		writeForkSkillsshError(w, err)
		return
	}
	writeJSON(w, forkSkillsshList{Skills: res.Skills, FetchedAt: res.FetchedAt})
}

// handleForkSkillsshResolve finds the skill a skills.sh entry names in its GitHub repo. skills.sh
// ids come from the SKILL.md name, which need not match the folder skillshare installs by (the
// id vercel-react-best-practices lives in skills/react-best-practices), so the repo is
// discovered and each skill's frontmatter name is read.
func (s *Server) handleForkSkillsshResolve(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	parseOpts := s.parseOpts()
	s.mu.RUnlock()

	var body struct {
		Source  string `json:"source"`
		SkillID string `json:"skillId"`
	}
	if err := decodeJSON(w, r, &body, defaultJSONBodyLimit); err != nil {
		if !errors.Is(err, errBodyTooLarge) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return
	}
	body.Source, body.SkillID = strings.TrimSpace(body.Source), strings.TrimSpace(body.SkillID)
	if body.SkillID == "" || !skillssh.IsGitHubRepo(body.Source) {
		writeError(w, http.StatusBadRequest, "source must be a GitHub owner/repo and skillId is required")
		return
	}
	source, err := install.ParseSourceWithOptions(body.Source, parseOpts)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source: "+err.Error())
		return
	}
	discovery, err := discoverInstallSource(source)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer install.CleanupDiscovery(discovery)

	skill, ok := matchSkillsshSkill(discovery, body.SkillID)
	if !ok {
		writeCodedError(w, http.StatusNotFound, "skillssh.skill_not_found",
			"no skill named "+body.SkillID+" in "+body.Source, map[string]string{"skill": body.SkillID, "source": body.Source})
		return
	}
	writeJSON(w, map[string]any{
		"skill": map[string]string{"name": skill.Name, "path": skill.Path, "description": skill.Description, "kind": "skill"},
	})
}

// matchSkillsshSkill picks the discovered skill whose SKILL.md name is the skills.sh id, else the
// one whose install name or folder is. Names compare on their letters and digits only: skills.sh
// lower-cases names and drops some punctuation (react:components is listed as reactcomponents).
func matchSkillsshSkill(d *install.DiscoveryResult, skillID string) (install.SkillInfo, bool) {
	want := skillsshKey(skillID)
	if want == "" {
		return install.SkillInfo{}, false
	}
	// Skill paths are relative to the checkout: RepoPath itself for a local source, and
	// RepoPath/repo for a clone (install's unexported discoverySourceRoot).
	root := d.RepoPath
	if d.Source == nil || d.Source.Type != install.SourceTypeLocalPath {
		root = filepath.Join(d.RepoPath, "repo")
	}
	for _, sk := range d.Skills {
		if name, err := utils.ParseSkillName(filepath.Join(root, filepath.FromSlash(sk.Path))); err == nil && skillsshKey(name) == want {
			return sk, true
		}
	}
	for _, sk := range d.Skills {
		if skillsshKey(sk.Name) == want || (sk.Path != "." && skillsshKey(path.Base(sk.Path)) == want) {
			return sk, true
		}
	}
	return install.SkillInfo{}, false
}

func skillsshKey(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}
