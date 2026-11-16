// Fork-only (endgame302/skillshare): registry entries become skillshare MCP servers, or say why not.

package mcpreg

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"skillshare/internal/mcp"
)

func loadPage(t *testing.T, file string) ListResponse {
	t.Helper()
	data, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var page ListResponse
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func fixtureServer(t *testing.T, name string) ServerJSON {
	t.Helper()
	for _, file := range []string{"official-page1.json", "official-page2.json", "github-page.json"} {
		for _, r := range loadPage(t, file).Servers {
			if r.Server.Name == name {
				return r.Server
			}
		}
	}
	t.Fatalf("no fixture server %s", name)
	return ServerJSON{}
}

func inline(t *testing.T, js string) ServerJSON {
	t.Helper()
	var s ServerJSON
	if err := json.Unmarshal([]byte(js), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func hasWarning(o Option, code string) bool {
	return slices.ContainsFunc(o.Warnings, func(w Warning) bool { return w.Code == code })
}

func warning(o Option, code string) Warning {
	for _, w := range o.Warnings {
		if w.Code == code {
			return w
		}
	}
	return Warning{}
}

func TestTranslateNpmWithSecret(t *testing.T) {
	opts := Translate(fixtureServer(t, "io.github.brave/brave-search-mcp-server"), nil)
	if len(opts) != 1 {
		t.Fatalf("options = %d", len(opts))
	}
	o := opts[0]
	if o.Refused != "" || o.Server == nil {
		t.Fatalf("refused %s %s", o.Refused, o.Detail)
	}
	if got := strings.Join(o.Command, " "); got != "npx -y @brave/brave-search-mcp-server@2.1.3" {
		t.Fatalf("command = %q", got)
	}
	if o.Server.Env["BRAVE_API_KEY"] != (mcp.Value{FromEnv: "BRAVE_API_KEY"}) {
		t.Fatalf("env = %+v", o.Server.Env)
	}
	if len(o.EnvVars) != 1 || o.EnvVars[0].Name != "BRAVE_API_KEY" || o.EnvVars[0].Use != "env" {
		t.Fatalf("envVars = %+v", o.EnvVars)
	}
	if !hasWarning(o, WarnFirstRun) || o.Name != "brave-search-mcp-server" || o.Kind != "npm" || o.Runtime != "npx" {
		t.Fatalf("option = %+v", o)
	}
}

func TestTranslateInputsAndOptionalSecrets(t *testing.T) {
	s := fixtureServer(t, "com.pulsemcp/remote-filesystem")
	o := Translate(s, nil)[0]
	if o.Server != nil || !slices.Contains(o.Missing, "env.GCS_BUCKET") {
		t.Fatalf("want GCS_BUCKET missing, got %+v", o.Missing)
	}
	if strings.Count(strings.Join(o.Command, " "), "-y") != 1 {
		t.Fatalf("-y repeated: %v", o.Command)
	}
	o = Translate(s, map[string]string{"env.GCS_BUCKET": "b"})[0]
	if o.Server == nil {
		t.Fatalf("refused %s %s missing %v", o.Refused, o.Detail, o.Missing)
	}
	env := o.Server.Env
	if env["GCS_BUCKET"].Literal != "b" || env["GCS_MAKE_PUBLIC"].Literal != "false" {
		t.Fatalf("env = %+v", env)
	}
	if _, ok := env["GCS_PRIVATE_KEY"]; ok || !slices.ContainsFunc(o.Skipped, func(e EnvVar) bool { return e.Name == "GCS_PRIVATE_KEY" }) {
		t.Fatalf("optional secret written: %+v skipped %+v", env, o.Skipped)
	}
	if _, ok := env["GOOGLE_APPLICATION_CREDENTIALS"]; ok {
		t.Fatal("optional input without a value was written")
	}
}

func TestTranslateRefusals(t *testing.T) {
	cases := []struct {
		name string
		idx  int
		want string
		det  string
	}{
		{"io.github.bytedance/mcp-server-filesystem", 0, RefuseNoVersion, ""},
		{"io.github.bytedance/mcp-server-filesystem", 1, RefusePackageHTTP, ""},
		{"io.github.bytedance/mcp-server-filesystem", 2, RefusePackageHTTP, ""},
		{"io.github.Evozim/chroot-filesystem-jail-mcp", 0, RefuseSSE, ""},
		{"io.github.fieldcure/filesystem", 0, RefuseUnsupported, "nuget"},
		{"dev.xakpc/sqlite-mcp-sidecar", 0, RefusePackageHTTP, ""},
		{"ai.smithery/afgong-sqlite-mcp-server", 0, RefuseSmithery, ""},
		{"io.github.arifulislamat/database-mcp-sqlite", 2, RefuseCargo, ""},
	}
	for _, c := range cases {
		o := Translate(fixtureServer(t, c.name), nil)[c.idx]
		if o.Refused != c.want || o.Server != nil || (c.det != "" && o.Detail != c.det) {
			t.Errorf("%s[%d]: refused %q detail %q, want %q", c.name, c.idx, o.Refused, o.Detail, c.want)
		}
	}
}

func TestTranslatePypiAndOCI(t *testing.T) {
	opts := Translate(fixtureServer(t, "io.github.arifulislamat/database-mcp-sqlite"), nil)
	if opts[0].Refused != "" || opts[1].Refused != "" {
		t.Fatalf("npm/pypi refused: %s %s", opts[0].Refused, opts[1].Refused)
	}
	if got := strings.Join(opts[1].Command, " "); got != "uvx database-mcp-sqlite@0.2.0" {
		t.Fatalf("pypi command = %q", got)
	}
	oci := Translate(fixtureServer(t, "io.github.dinesh-nalla-se/playwright-mcp"), nil)[1]
	want := "docker run -i --rm -e YOUR_API_KEY docker.io/dineshn51331/playwright-mcp:1.0.0"
	if oci.Kind != "oci" || !strings.HasPrefix(strings.Join(oci.Command, " "), want) || !hasWarning(oci, WarnTag) {
		t.Fatalf("oci = %v %+v", oci.Command, oci.Warnings)
	}
	if oci.Server == nil || oci.Server.Env["YOUR_API_KEY"].FromEnv != "YOUR_API_KEY" {
		t.Fatalf("oci server = %+v", oci.Server)
	}
}

func TestTranslateBearerRemote(t *testing.T) {
	o := Translate(fixtureServer(t, "io.github.netdata/mcp-server"), nil)[0]
	if o.Server == nil {
		t.Fatalf("refused %s %s", o.Refused, o.Detail)
	}
	if o.Server.BearerToken == nil || o.Server.BearerToken.FromEnv != "NETDATA_CLOUD_API_TOKEN" || len(o.Server.Headers) != 0 {
		t.Fatalf("server = %+v", o.Server)
	}
	if o.Host != "app.netdata.cloud" || o.Domain != "netdata.cloud" || !hasWarning(o, WarnRemote) || hasWarning(o, WarnDomainMismatch) {
		t.Fatalf("option = %+v", o)
	}
	if o.Server.Transport != "" || o.Server.URL != "https://app.netdata.cloud/api/v1/mcp" {
		t.Fatalf("server = %+v", o.Server)
	}
}

func TestTranslateInlineCases(t *testing.T) {
	remote := func(url string) string {
		return `{"name":"com.example/x","remotes":[{"type":"streamable-http","url":"` + url + `"}]}`
	}
	refusals := map[string]struct {
		js   string
		want string
	}{
		"http remote":        {remote("http://example.com/mcp"), RefuseInsecureHTTP},
		"secret query":       {remote("https://example.com/mcp?api_key=x"), RefuseBadURL},
		"user info":          {remote("https://u:p@example.com/mcp"), RefuseBadURL},
		"smithery host":      {remote("https://server.smithery.ai/x/mcp"), RefuseSmithery},
		"run.tools host":     {remote("https://brave.run.tools"), RefuseSmithery},
		"unknown transport":  {`{"name":"a/b","remotes":[{"type":"websocket","url":"wss://x"}]}`, RefuseTransport},
		"secret env literal": {`{"name":"a/b","packages":[{"registryType":"npm","identifier":"p","version":"1.0.0","transport":{"type":"stdio"},"environmentVariables":[{"name":"API_KEY","isSecret":true,"isRequired":true,"value":"abc"}]}]}`, RefuseSecretLiteral},
		"secret arg":         {`{"name":"a/b","packages":[{"registryType":"npm","identifier":"p","version":"1.0.0","transport":{"type":"stdio"},"packageArguments":[{"type":"positional","valueHint":"key","isSecret":true}]}]}`, RefuseSecretArg},
		"secret mixed":       {`{"name":"a/b","remotes":[{"type":"streamable-http","url":"https://a.example.com","headers":[{"name":"X-Key","isSecret":true,"isRequired":true,"value":"abc {k}"}]}]}`, RefuseSecretMixed},
		"secret url var":     {`{"name":"a/b","remotes":[{"type":"streamable-http","url":"https://a.example.com/{tok}","variables":{"tok":{"isSecret":true}}}]}`, RefuseSecretArg},
		"mcpb":               {`{"name":"a/b","packages":[{"registryType":"mcpb","identifier":"https://x/y.mcpb","transport":{"type":"stdio"}}]}`, RefuseMCPB},
		"custom registry":    {`{"name":"a/b","packages":[{"registryType":"npm","registryBaseUrl":"https://npm.example.com","identifier":"p","version":"1.0.0","transport":{"type":"stdio"}}]}`, RefuseCustomRegistry},
		"latest image":       {`{"name":"a/b","packages":[{"registryType":"oci","identifier":"docker.io/a/b:latest","transport":{"type":"stdio"}}]}`, RefuseNoVersion},
		"nothing":            {`{"name":"a/b"}`, RefuseNoInstallData},
		"line break":         {`{"name":"a/b","packages":[{"registryType":"npm","identifier":"p","version":"1.0.0","transport":{"type":"stdio"},"packageArguments":[{"type":"positional","value":"a\nb"}]}]}`, RefuseInvalid},
	}
	for name, c := range refusals {
		o := Translate(inline(t, c.js), nil)[0]
		if o.Refused != c.want || o.Server != nil {
			t.Errorf("%s: refused %q (%s), want %q", name, o.Refused, o.Detail, c.want)
		}
	}

	if o := Translate(inline(t, remote("http://localhost:3000/mcp")), nil)[0]; o.Server == nil {
		t.Errorf("localhost http refused: %s", o.Refused)
	}
	if o := Translate(inline(t, `{"name":"a/b","packages":[{"registryType":"npm","identifier":"p","version":"1.0.0","transport":{"type":"stdio"},"packageArguments":[{"type":"named","name":"--port","value":"8080"},{"type":"named","name":"--read-only"}]}]}`), nil)[0]; strings.Join(o.Command, " ") != "npx -y p@1.0.0 --port 8080 --read-only" {
		t.Errorf("named args = %v", o.Command)
	}

	hint := inline(t, `{"name":"a/b","packages":[{"registryType":"npm","identifier":"p","version":"1.0.0","transport":{"type":"stdio"},"packageArguments":[{"type":"positional","valueHint":"dir","isRequired":true},{"type":"named","name":"--mode","choices":["ro","rw"],"isRequired":true}]}]}`)
	o := Translate(hint, nil)[0]
	if !slices.Equal(o.Missing, []string{"package.0", "package.1"}) || o.Server != nil || len(o.Inputs) != 2 || o.Inputs[0].Label != "dir" {
		t.Fatalf("missing %v inputs %+v", o.Missing, o.Inputs)
	}
	o = Translate(hint, map[string]string{"package.0": "/data", "package.1": "rx"})[0]
	if !slices.Equal(o.Missing, []string{"package.1"}) {
		t.Fatalf("choice outside the list accepted: %v", o.Missing)
	}
	o = Translate(hint, map[string]string{"package.0": "/data", "package.1": "ro"})[0]
	if o.Server == nil || strings.Join(o.Server.Args, " ") != "-y p@1.0.0 /data --mode ro" {
		t.Fatalf("args = %+v", o.Server)
	}

	tenant := inline(t, `{"name":"a/b","remotes":[{"type":"streamable-http","url":"https://{tenant}.example.com/mcp","variables":{"tenant":{"isRequired":true}}}]}`)
	if o := Translate(tenant, nil)[0]; !slices.Equal(o.Missing, []string{"url.tenant"}) {
		t.Fatalf("missing = %v", o.Missing)
	}
	if o := Translate(tenant, map[string]string{"url.tenant": "acme"})[0]; o.Server == nil || o.Server.URL != "https://acme.example.com/mcp" {
		t.Fatalf("url = %+v", o)
	}

	mismatch := Translate(inline(t, `{"name":"com.example/x","remotes":[{"type":"streamable-http","url":"https://api.other.io/mcp"}]}`), nil)[0]
	if w := warning(mismatch, WarnDomainMismatch); w.Params["host"] != "api.other.io" || w.Params["namespace"] != "example.com" {
		t.Fatalf("domain mismatch = %+v", mismatch.Warnings)
	}
	repo := Translate(inline(t, `{"name":"io.github.alice/x","repository":{"url":"https://github.com/bob/x"},"remotes":[{"type":"streamable-http","url":"https://x.example.com"}]}`), nil)[0]
	if w := warning(repo, WarnRepoMismatch); w.Params["namespace"] != "alice" || w.Params["repo"] != "bob/x" {
		t.Fatalf("repo mismatch = %+v", repo.Warnings)
	}
	broad := Translate(inline(t, `{"name":"a/b","packages":[{"registryType":"npm","identifier":"p","version":"1.0.0","transport":{"type":"stdio"},"environmentVariables":[{"name":"GITHUB_PERSONAL_ACCESS_TOKEN","isSecret":true,"isRequired":true}]}]}`), nil)[0]
	if w := warning(broad, WarnBroadToken); w.Params["names"] != "GITHUB_PERSONAL_ACCESS_TOKEN" {
		t.Fatalf("broad token = %+v", broad.Warnings)
	}
	header := Translate(inline(t, `{"name":"a/b","remotes":[{"type":"streamable-http","url":"https://a.example.com","headers":[{"name":"X-Api-Key","isSecret":true,"isRequired":true}]}]}`), nil)[0]
	if header.Server == nil || header.Server.Headers["X-Api-Key"].FromEnv != "X_API_KEY" || header.EnvVars[0].Use != "header:X-Api-Key" {
		t.Fatalf("header = %+v", header)
	}
}

func TestSuggestNameAndDomains(t *testing.T) {
	for in, want := range map[string]string{
		"io.github.brave/brave-search-mcp-server": "brave-search-mcp-server",
		"io.github.netdata/mcp-server":            "netdata-mcp-server",
		"ac.inference.sh/mcp":                     "inference-mcp",
		"microsoft/markitdown":                    "markitdown",
		"github-official":                         "github-official",
		"io.github.X/My Server!":                  "my-server-",
		"":                                        "server",
	} {
		if got := SuggestName(in); got != want {
			t.Errorf("SuggestName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"app.netdata.cloud": "netdata.cloud", "a.b.co.uk": "b.co.uk", "127.0.0.1": "127.0.0.1", "localhost": "localhost"} {
		if got := Domain(in); got != want {
			t.Errorf("Domain(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"com.microsoft": "microsoft.com", "ac.inference.sh": "inference.ac", "io.github.alice": "", "microsoft": ""} {
		if got := NamespaceDomain(in); got != want {
			t.Errorf("NamespaceDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitHubMeta(t *testing.T) {
	gh := fixtureServer(t, "microsoft/markitdown").GitHub()
	if gh == nil || gh.License != "MIT License" || gh.Stars != 195628 {
		t.Fatalf("github meta = %+v", gh)
	}
	if Namespace("io.github.a/b") != "io.github.a" || Namespace("x") != "x" {
		t.Fatal("Namespace")
	}
}
