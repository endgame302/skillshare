package gitenv

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// withTerminal makes stdin look like a terminal (or not) and clears
// DisablePrompts for the duration of the test.
func withTerminal(t *testing.T, tty bool) {
	t.Helper()
	origTTY, origDisabled := stdinIsTerminal, promptsDisabled.Load()
	stdinIsTerminal = func() bool { return tty }
	promptsDisabled.Store(false)
	t.Cleanup(func() {
		stdinIsTerminal = origTTY
		promptsDisabled.Store(origDisabled)
	})
}

func lookup(env []string, key string) []string {
	var values []string
	for _, item := range env {
		k, v, _ := strings.Cut(item, "=")
		if strings.EqualFold(k, key) {
			values = append(values, v)
		}
	}
	return values
}

func TestEnvironWithoutTerminalDisablesPrompts(t *testing.T) {
	withTerminal(t, false)
	// A user setting that would allow prompts is overridden: nobody can answer.
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GCM_INTERACTIVE", "always")

	env := Environ()
	if got := lookup(env, "GIT_TERMINAL_PROMPT"); !slices.Equal(got, []string{"0"}) {
		t.Errorf("GIT_TERMINAL_PROMPT = %q, want [0]", got)
	}
	if got := lookup(env, "GCM_INTERACTIVE"); !slices.Equal(got, []string{"never"}) {
		t.Errorf("GCM_INTERACTIVE = %q, want [never]", got)
	}
	if PromptsAllowed() {
		t.Error("PromptsAllowed() = true without a terminal")
	}
}

// A CLI user at a terminal may answer a credential prompt for a private
// HTTPS repository, so Environ leaves git's defaults and the user's settings.
func TestEnvironAtTerminalKeepsPrompts(t *testing.T) {
	withTerminal(t, true)
	t.Setenv("GCM_INTERACTIVE", "always")
	t.Setenv("GIT_TERMINAL_PROMPT", "") // restored after the test
	os.Unsetenv("GIT_TERMINAL_PROMPT")

	env := Environ()
	if got := lookup(env, "GIT_TERMINAL_PROMPT"); len(got) != 0 {
		t.Errorf("GIT_TERMINAL_PROMPT = %q, want unset", got)
	}
	if got := lookup(env, "GCM_INTERACTIVE"); !slices.Equal(got, []string{"always"}) {
		t.Errorf("GCM_INTERACTIVE = %q, want the user's [always]", got)
	}
	if !PromptsAllowed() {
		t.Error("PromptsAllowed() = false at a terminal")
	}
}

// The dashboard server is started from a terminal, but its user is in the
// browser.
func TestDisablePromptsOverridesTerminal(t *testing.T) {
	withTerminal(t, true)
	DisablePrompts()

	if PromptsAllowed() {
		t.Fatal("PromptsAllowed() = true after DisablePrompts")
	}
	env := Command("version").Env
	if got := lookup(env, "GIT_TERMINAL_PROMPT"); !slices.Equal(got, []string{"0"}) {
		t.Errorf("GIT_TERMINAL_PROMPT = %q, want [0]", got)
	}
	if got := lookup(env, "GCM_INTERACTIVE"); !slices.Equal(got, []string{"never"}) {
		t.Errorf("GCM_INTERACTIVE = %q, want [never]", got)
	}
}

// A missing or private HTTPS repository answers 401. Without a terminal, git
// must fail at once instead of waiting on a prompt, and the credential helper
// (Git Credential Manager in real use) must be told not to interact.
func TestGitFailsFastWhenRemoteAsksForCredentials(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	withTerminal(t, false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	dir := t.TempDir()
	global := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(global, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Keep the machine's own helpers (such as credential.helper=manager in
	// Git for Windows' system config) out of the test.
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", global)

	seen := filepath.ToSlash(filepath.Join(dir, "helper-env"))
	helper := `!f() { echo "$GCM_INTERACTIVE" > "` + seen + `"; }; f`

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := CommandContext(ctx, "-c", "credential.helper=", "-c", "credential.helper="+helper,
		"ls-remote", srv.URL+"/owner/missing.git")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("git ls-remote succeeded against a remote that demands credentials")
	}
	if elapsed > 20*time.Second {
		t.Errorf("git took %s to fail; it should not wait on a prompt", elapsed)
	}
	if !strings.Contains(stderr.String(), "terminal prompts disabled") {
		t.Errorf("stderr = %q, want git's terminal prompts disabled error", stderr.String())
	}
	got, readErr := os.ReadFile(filepath.FromSlash(seen))
	if readErr != nil {
		t.Fatalf("credential helper did not run: %v (stderr %q)", readErr, stderr.String())
	}
	if strings.TrimSpace(string(got)) != "never" {
		t.Errorf("credential helper saw GCM_INTERACTIVE=%q, want never", strings.TrimSpace(string(got)))
	}
}
