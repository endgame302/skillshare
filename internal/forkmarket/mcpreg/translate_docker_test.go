// Fork-only (endgame302/skillshare): Docker catalog entries become skillshare MCP servers, or say why not.

package mcpreg

import (
	"os"
	"slices"
	"strings"
	"testing"

	"skillshare/internal/mcp"
)

func loadCatalog(t *testing.T) *DockerCatalog {
	t.Helper()
	data, err := os.ReadFile("testdata/docker-catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseDockerCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func dockerOption(t *testing.T, name string, values map[string]string) Option {
	t.Helper()
	e, ok := loadCatalog(t).Registry[name]
	if !ok {
		t.Fatalf("no catalog entry %s", name)
	}
	opts := TranslateDocker(e, values)
	if len(opts) != 1 || opts[0].ID != "docker" {
		t.Fatalf("options = %+v", opts)
	}
	return opts[0]
}

func TestParseDockerCatalog(t *testing.T) {
	c := loadCatalog(t)
	if len(c.Registry) != 13 || c.Registry["brave"].Name != "brave" {
		t.Fatalf("catalog = %d entries", len(c.Registry))
	}
	if _, err := ParseDockerCatalog([]byte("version: 2\nregistry: {}\n")); err == nil {
		t.Fatal("version 2 accepted")
	}
}

func TestTranslateDockerImage(t *testing.T) {
	c := loadCatalog(t)
	o := dockerOption(t, "brave", nil)
	want := "docker run -i --rm -e BRAVE_API_KEY -e BRAVE_MCP_TRANSPORT " + c.Registry["brave"].Image
	if o.Server == nil || strings.Join(o.Command, " ") != want {
		t.Fatalf("command = %q refused %s %s", strings.Join(o.Command, " "), o.Refused, o.Detail)
	}
	if o.Server.Env["BRAVE_API_KEY"] != (mcp.Value{FromEnv: "BRAVE_API_KEY"}) || o.Server.Env["BRAVE_MCP_TRANSPORT"].Literal != "stdio" {
		t.Fatalf("env = %+v", o.Server.Env)
	}
	if hasWarning(o, WarnDockerSelfBuilt) || hasWarning(o, WarnTag) || !hasWarning(o, WarnFirstRun) {
		t.Fatalf("warnings = %+v", o.Warnings)
	}

	gh := dockerOption(t, "github-official", nil)
	for _, code := range []string{WarnDockerSelfBuilt, WarnOAuth, WarnAllowHosts, WarnBroadToken} {
		if !hasWarning(gh, code) {
			t.Errorf("github-official lacks %s: %+v", code, gh.Warnings)
		}
	}
}

func TestTranslateDockerRemotes(t *testing.T) {
	c7 := dockerOption(t, "context7", nil)
	if c7.Kind != "docker-remote" || c7.Server == nil || c7.Server.Headers["CONTEXT7_API_KEY"] != (mcp.Value{FromEnv: "CONTEXT7_API_KEY"}) {
		t.Fatalf("context7 = %+v", c7)
	}
	apify := dockerOption(t, "apify", nil)
	if apify.Server == nil || apify.Server.BearerToken == nil || apify.Server.BearerToken.FromEnv != "APIFY_API_KEY" {
		t.Fatalf("apify = %+v", apify)
	}
	astro := dockerOption(t, "astro-docs", nil)
	if astro.Server == nil || astro.Server.URL != "https://mcp.docs.astro.build/mcp" || len(astro.EnvVars) != 0 || !hasWarning(astro, WarnRemote) {
		t.Fatalf("astro = %+v", astro)
	}
}

func TestTranslateDockerRefusals(t *testing.T) {
	for name, want := range map[string]string{
		"asana":             RefuseSSE,
		"curl":              RefusePOCI,
		"desktop-commander": RefuseDockerVolumes,
		"ast-grep":          RefuseDockerVolumes,
		"notion":            RefuseDockerTemplate,
	} {
		if o := dockerOption(t, name, nil); o.Refused != want || o.Server != nil {
			t.Errorf("%s: refused %q, want %q", name, o.Refused, want)
		}
	}
}

func TestTranslateDockerInputs(t *testing.T) {
	hub := dockerOption(t, "dockerhub", nil)
	if hub.Server == nil || slices.ContainsFunc(hub.Server.Args, func(a string) bool { return strings.HasPrefix(a, "--username") }) {
		t.Fatalf("empty optional username kept: %+v", hub.Server)
	}
	if !slices.ContainsFunc(hub.Inputs, func(f InputField) bool { return f.Key == "config.dockerhub.username" }) || hub.Server.Env["HUB_PAT_TOKEN"].FromEnv != "HUB_PAT_TOKEN" {
		t.Fatalf("inputs = %+v env = %+v", hub.Inputs, hub.Server.Env)
	}
	hub = dockerOption(t, "dockerhub", map[string]string{"config.dockerhub.username": "alice"})
	if hub.Server == nil || !slices.Contains(hub.Server.Args, "--username=alice") {
		t.Fatalf("args = %+v", hub.Server)
	}

	cb := dockerOption(t, "couchbase", nil)
	if !slices.ContainsFunc(cb.Inputs, func(f InputField) bool { return f.Key == "config.couchbase.cb_connection_string" }) {
		t.Fatalf("couchbase inputs = %+v", cb.Inputs)
	}
	if !slices.ContainsFunc(cb.EnvVars, func(e EnvVar) bool { return e.Name == "CB_PASSWORD" }) {
		t.Fatalf("couchbase env vars = %+v", cb.EnvVars)
	}
	cb = dockerOption(t, "couchbase", map[string]string{"config.couchbase.cb_connection_string": "couchbase://db"})
	if cb.Server == nil || cb.Server.Env["CB_CONNECTION_STRING"].Literal != "couchbase://db" || !slices.Contains(cb.Server.Args, "CB_CONNECTION_STRING") {
		t.Fatalf("couchbase = %+v", cb.Server)
	}

	graf := dockerOption(t, "grafana", nil)
	if graf.Server == nil {
		t.Fatalf("grafana refused %s missing %v", graf.Refused, graf.Missing)
	}
	if _, ok := graf.Server.Env["GRAFANA_URL"]; ok {
		t.Fatal("empty optional GRAFANA_URL written")
	}

	isolated := TranslateDocker(DockerEntry{Name: "x", Type: "server", Image: "mcp/x@sha256:" + strings.Repeat("a", 64), DisableNetwork: true}, nil)[0]
	if isolated.Server == nil || !slices.Contains(isolated.Server.Args, "--network") || !slices.Contains(isolated.Server.Args, "none") {
		t.Fatalf("network = %+v", isolated.Server)
	}
	required := TranslateDocker(DockerEntry{Name: "y", Type: "server", Image: "mcp/y@sha256:" + strings.Repeat("b", 64),
		Command: []string{"--path={{y.path}}"}, Config: []DockerConfig{{Name: "y", Required: []string{"path"}}}}, nil)[0]
	if !slices.Equal(required.Missing, []string{"config.y.path"}) || required.Server != nil {
		t.Fatalf("required input = %+v", required)
	}
}
