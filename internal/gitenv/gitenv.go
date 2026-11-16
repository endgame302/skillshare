// Package gitenv runs git without the caller's repository selection, and
// without credential prompts nobody can answer.
//
// Git exports GIT_DIR and related variables to hooks, so skillshare started
// from a hook would otherwise run its own git commands against the hook's
// repository instead of the directory it names (#852).
//
// A missing or private HTTPS repository makes the host ask for credentials.
// Git then asks its credential helper and the terminal; Git Credential Manager
// can wait minutes on a login prompt that a server or a piped run never
// shows. So git may prompt only when someone can answer: stdin is a terminal
// and the process has not called DisablePrompts.
package gitenv

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	"golang.org/x/term"
)

// repoVars are the variables `git rev-parse --local-env-vars` lists that pick
// a repository, work tree, index, object store, or config file.
// GIT_CONFIG_PARAMETERS and GIT_CONFIG_COUNT stay: they carry user settings
// such as credentials, and install's token auth appends to them.
var repoVars = map[string]bool{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_COMMON_DIR":                   true,
	"GIT_CONFIG":                       true,
	"GIT_DIR":                          true,
	"GIT_GRAFT_FILE":                   true,
	"GIT_IMPLICIT_WORK_TREE":           true,
	"GIT_INDEX_FILE":                   true,
	"GIT_NO_REPLACE_OBJECTS":           true,
	"GIT_OBJECT_DIRECTORY":             true,
	"GIT_PREFIX":                       true,
	"GIT_REPLACE_REF_BASE":             true,
	"GIT_SHALLOW_FILE":                 true,
	"GIT_WORK_TREE":                    true,
}

// noPromptVars stop git and its credential helper from asking the user:
// GIT_TERMINAL_PROMPT for git's own username and password prompt, and
// GCM_INTERACTIVE for Git Credential Manager's GUI, browser and terminal
// prompts (it then uses stored credentials only). The variable overrides the
// credential.interactive setting, so no -c option is needed.
var noPromptVars = []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never"}

func isNoPromptKey(key string) bool {
	return key == "GIT_TERMINAL_PROMPT" || key == "GCM_INTERACTIVE"
}

var promptsDisabled atomic.Bool

// stdinIsTerminal is a variable so tests can stand in for a terminal.
var stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// DisablePrompts makes every later git command non-interactive, even when
// stdin is a terminal. Long-running servers call it: the terminal that
// started them is not where their user is looking.
func DisablePrompts() { promptsDisabled.Store(true) }

// PromptsAllowed reports whether git may ask for credentials.
func PromptsAllowed() bool {
	return !promptsDisabled.Load() && stdinIsTerminal()
}

// Environ returns os.Environ() without the repository selection variables.
// When PromptsAllowed is false it also turns off credential prompts.
func Environ() []string {
	env := os.Environ()
	quiet := !PromptsAllowed()
	out := make([]string, 0, len(env)+len(noPromptVars))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		key = strings.ToUpper(key) // Windows names are case-insensitive.
		if repoVars[key] || (quiet && isNoPromptKey(key)) {
			continue
		}
		out = append(out, item)
	}
	if quiet {
		out = append(out, noPromptVars...)
	}
	return out
}

// Command is exec.Command("git", args...) with Env set to Environ().
// Add variables with cmd.Env = append(cmd.Env, ...).
func Command(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Env = Environ()
	return cmd
}

// CommandContext is the context-aware form of Command.
func CommandContext(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = Environ()
	return cmd
}
