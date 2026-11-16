// Fork-only (endgame302/skillshare): plans and executes native Claude CLI plugin hand-off.

package plugincat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"skillshare/internal/gitenv"
)

const (
	versionTimeout = 30 * time.Second
	listTimeout    = 30 * time.Second
	syncTimeout    = 3 * time.Minute
	installTimeout = 5 * time.Minute
)

// Runner runs the Claude CLI. ExecRunner is the real one; tests use a fake.
type Runner interface {
	// LookPath returns where claude is, or an error when it is not on PATH.
	LookPath() (string, error)
	// Run runs the claude found by LookPath with args; stdout is returned even when it exits non-zero (err set then).
	Run(ctx context.Context, args ...string) (stdout []byte, err error)
}

type ExecRunner struct{}

func (ExecRunner) LookPath() (string, error) {
	return exec.LookPath("claude")
}

func (ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cmd.Dir = home
	}
	cmd.Env = append(gitenv.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.Bytes(), err
}

var (
	ErrClaudeMissing  = errors.New("the claude CLI is not on PATH on the machine running skillshare")
	ErrMarketConflict = errors.New("Claude already has a marketplace with this name from another source")
	ErrCatalogMoved   = errors.New("Claude's copy of the catalog pins a different version than the one shown")
	ErrNotNative      = errors.New("this entry is not installed through Claude Code")
)

// StepError is a claude command that failed; Message is Claude's own JSON "message" (empty when it printed none).
type StepError struct {
	Step        string // "add", "update", "install"
	Message     string
	FailureCode string
}

func (e *StepError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("claude plugin %s failed", e.Step)
}

type Step struct {
	Name string   `json:"name"` // "add", "update", "install"
	Args []string `json:"args"` // argv after "claude"
}

type Plan struct {
	Claude        string `json:"claude"` // path of claude; "" when missing (then no steps)
	ClaudeVersion string `json:"claudeVersion,omitempty"`
	Market        string `json:"market"`
	PluginID      string `json:"pluginId"`   // NAME@MARKET
	Registered    bool   `json:"registered"` // Claude already has this marketplace from this repo
	Steps         []Step `json:"steps"`
	Import        string `json:"import"` // "skillshare plugin import NAME@MARKET --from claude", shown to the user
}

type Result struct {
	PluginID string `json:"pluginId"`
	Message  string `json:"message,omitempty"` // Claude's install message
}

type marketplaceItem struct {
	Name            string `json:"name"`
	Source          string `json:"source"`
	Repo            string `json:"repo"`
	URL             string `json:"url"`
	InstallLocation string `json:"installLocation"`
}

func PlanHandoff(ctx context.Context, r Runner, cat *Catalog, e Entry) (*Plan, error) {
	if e.Install != "native" {
		return nil, ErrNotNative
	}
	if cat == nil || cat.Market == "" {
		return nil, errors.New("catalog marketplace name is required")
	}

	pluginID := e.Name + "@" + cat.Market
	importCmd := "skillshare plugin import " + pluginID + " --from claude"

	claudePath, err := r.LookPath()
	if err != nil {
		return &Plan{
			Market:   cat.Market,
			PluginID: pluginID,
			Steps:    []Step{},
			Import:   importCmd,
		}, nil
	}

	vCtx, vCancel := context.WithTimeout(ctx, versionTimeout)
	vOut, vErr := r.Run(vCtx, "--version")
	vCancel()
	var version string
	if vErr == nil {
		version = trimVersionLine(vOut)
	}

	lCtx, lCancel := context.WithTimeout(ctx, listTimeout)
	lOut, lErr := r.Run(lCtx, "plugin", "marketplace", "list", "--json")
	lCancel()
	if lErr != nil {
		return nil, fmt.Errorf("listing Claude marketplaces failed: %w", lErr)
	}
	markets, err := parseMarketplaceList(lOut)
	if err != nil {
		return nil, err
	}

	plan := &Plan{
		Claude:        claudePath,
		ClaudeVersion: version,
		Market:        cat.Market,
		PluginID:      pluginID,
		Import:        importCmd,
	}

	var matched *marketplaceItem
	for i := range markets {
		if markets[i].Name == cat.Market {
			matched = &markets[i]
			break
		}
	}

	if matched == nil {
		plan.Steps = []Step{
			{Name: "add", Args: []string{"plugin", "marketplace", "add", cat.Repo, "--scope", "user", "--json"}},
			{Name: "install", Args: []string{"plugin", "install", pluginID, "--scope", "user", "--json"}},
		}
		return plan, nil
	}

	if !sameMarketplaceSource(*matched, cat.Repo) {
		origin := matched.Repo
		if origin == "" {
			origin = matched.URL
		}
		if origin == "" {
			origin = matched.Source
		}
		return nil, fmt.Errorf("%w: %s comes from %s", ErrMarketConflict, cat.Market, origin)
	}

	plan.Registered = true
	plan.Steps = []Step{
		{Name: "update", Args: []string{"plugin", "marketplace", "update", cat.Market, "--json"}},
		{Name: "install", Args: []string{"plugin", "install", pluginID, "--scope", "user", "--json"}},
	}
	return plan, nil
}

func RunHandoff(ctx context.Context, r Runner, cat *Catalog, e Entry, plan *Plan) (*Result, error) {
	if e.Install != "native" {
		return nil, ErrNotNative
	}
	if cat == nil || cat.Market == "" {
		return nil, errors.New("catalog marketplace name is required")
	}
	if plan == nil || plan.Claude == "" {
		return nil, ErrClaudeMissing
	}

	var syncStep, installStep *Step
	for i := range plan.Steps {
		switch plan.Steps[i].Name {
		case "add", "update":
			syncStep = &plan.Steps[i]
		case "install":
			installStep = &plan.Steps[i]
		}
	}
	if syncStep == nil || installStep == nil {
		return nil, ErrClaudeMissing
	}

	sCtx, sCancel := context.WithTimeout(ctx, syncTimeout)
	sOut, sErr := r.Run(sCtx, syncStep.Args...)
	sCancel()
	if _, err := checkStepOutput(syncStep.Name, sOut, sErr); err != nil {
		return nil, err
	}

	lCtx, lCancel := context.WithTimeout(ctx, listTimeout)
	lOut, lErr := r.Run(lCtx, "plugin", "marketplace", "list", "--json")
	lCancel()
	if lErr != nil {
		return nil, fmt.Errorf("listing Claude marketplaces failed: %w", lErr)
	}
	markets, err := parseMarketplaceList(lOut)
	if err != nil {
		return nil, err
	}

	var installLoc string
	for _, m := range markets {
		if m.Name == cat.Market {
			installLoc = m.InstallLocation
			break
		}
	}
	if installLoc == "" {
		return nil, fmt.Errorf("Claude marketplace %q was not found after %s", cat.Market, syncStep.Name)
	}
	if !filepath.IsAbs(installLoc) {
		return nil, fmt.Errorf("Claude returned a non-absolute installLocation %q", installLoc)
	}

	data, err := readCatalogFile(filepath.Join(installLoc, CatalogFile))
	if err != nil {
		return nil, fmt.Errorf("reading Claude's copy of the catalog failed: %w", err)
	}
	_, claudeEntries, _, err := ParseCatalog(data)
	if err != nil {
		return nil, fmt.Errorf("parsing Claude's copy of the catalog failed: %w", err)
	}

	var claudeEntry *Entry
	for i := range claudeEntries {
		if claudeEntries[i].Name == e.Name {
			claudeEntry = &claudeEntries[i]
			break
		}
	}
	if claudeEntry == nil {
		return nil, fmt.Errorf("Claude's copy of the catalog has no plugin named %s", e.Name)
	}

	if e.Source.SHA != "" {
		if claudeEntry.Source.SHA != e.Source.SHA {
			return nil, fmt.Errorf("%w (shown %s, Claude has %s)", ErrCatalogMoved, shortSHA(e.Source.SHA), shortSHA(claudeEntry.Source.SHA))
		}
	} else {
		got, want := claudeEntry.Source, e.Source
		if got.Kind != want.Kind ||
			got.URL != want.URL ||
			got.Repo != want.Repo ||
			got.Path != want.Path ||
			got.Ref != want.Ref ||
			got.Package != want.Package {
			return nil, ErrCatalogMoved
		}
	}

	iCtx, iCancel := context.WithTimeout(ctx, installTimeout)
	iOut, iErr := r.Run(iCtx, installStep.Args...)
	iCancel()
	msg, err := checkStepOutput(installStep.Name, iOut, iErr)
	if err != nil {
		return nil, err
	}
	return &Result{PluginID: plan.PluginID, Message: msg}, nil
}

func trimVersionLine(out []byte) string {
	s := strings.ReplaceAll(string(out), "\r\n", "\n")
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[:nl]
	}
	s = strings.TrimSpace(s)
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

func parseMarketplaceList(out []byte) ([]marketplaceItem, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, errors.New("Claude marketplace list did not return a JSON array")
	}
	var items []marketplaceItem
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, fmt.Errorf("parsing Claude marketplace list: %w", err)
	}
	return items, nil
}

func sameMarketplaceSource(item marketplaceItem, repo string) bool {
	switch item.Source {
	case "github":
		return strings.EqualFold(item.Repo, repo)
	case "git":
		u := strings.TrimRight(item.URL, "/")
		if len(u) >= 4 && strings.EqualFold(u[len(u)-4:], ".git") {
			u = u[:len(u)-4]
		}
		u = strings.TrimRight(u, "/")
		return strings.EqualFold(u, "https://github.com/"+repo)
	default:
		return false
	}
}

func readCatalogFile(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCatalogBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxCatalogBytes {
		return nil, fmt.Errorf("catalog file exceeded %d bytes", maxCatalogBytes)
	}
	return data, nil
}

func checkStepOutput(stepName string, stdout []byte, runErr error) (string, error) {
	var parsed struct {
		Outcome     string `json:"outcome"`
		Message     string `json:"message"`
		FailureCode string `json:"failureCode"`
	}
	found := false
	lines := strings.Split(string(stdout), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "{") {
			if json.Unmarshal([]byte(line), &parsed) == nil {
				found = true
				break
			}
		}
	}
	if !found || runErr != nil || parsed.Outcome != "ok" {
		return "", &StepError{
			Step:        stepName,
			Message:     parsed.Message,
			FailureCode: parsed.FailureCode,
		}
	}
	return parsed.Message, nil
}

func shortSHA(s string) string {
	if s == "" {
		return "none"
	}
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
