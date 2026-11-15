package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"skillshare/internal/config"
)

// Fork: `search --hub owner/repo` reads a plain GitHub repo when no saved hub
// has that label.
func TestResolveHubURLPlainRepo(t *testing.T) {
	cwd := t.TempDir()
	path := config.ProjectConfigPath(cwd)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "hub:\n  hubs:\n    - label: acme/skills\n      url: https://example.com/acme.json\n"
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		input, want, wantErr string
	}{
		{input: "anthropics/skills", want: "anthropics/skills"},
		{input: "github.com/anthropics/skills/skills", want: "github.com/anthropics/skills/skills"},
		// A saved label still wins over the repo of the same name.
		{input: "acme/skills", want: "https://example.com/acme.json"},
		{input: "unknown", wantErr: "not found"},
		{input: "owner/repo/hub.json", wantErr: "not found"},
	}
	for _, tt := range tests {
		got, err := resolveHubURL(tt.input, false, modeProject, cwd, "https://example.com/default.json")
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("resolveHubURL(%q) = %q, %v; want error %q", tt.input, got, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("resolveHubURL(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
		}
	}
}
