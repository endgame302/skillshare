// Fork-only (endgame302/skillshare): turn one registry or catalog entry into install options,
// each a portable skillshare MCP server, or the reason it cannot be one.

package mcpreg

import (
	"fmt"
	"maps"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"skillshare/internal/mcp"
)

// Option is one way to install an entry: a package, a remote endpoint or a Docker image.
type Option struct {
	ID       string       `json:"id"`                // "package:<i>", "remote:<i>", "docker"; "none" when the entry has nothing to install
	Kind     string       `json:"kind"`              // npm, pypi, oci, nuget, mcpb, cargo, remote, sse, docker, docker-remote, poci, none, ...
	Label    string       `json:"label"`             // what it is, for people
	Runtime  string       `json:"runtime,omitempty"` // npx, uvx, docker
	Name     string       `json:"name"`              // suggested skillshare server name
	Refused  string       `json:"refused,omitempty"` // one Refuse* code
	Detail   string       `json:"detail,omitempty"`  // extra words for Refused
	Server   *mcp.Server  `json:"server,omitempty"`  // nil when refused or a required input has no value
	Command  []string     `json:"command,omitempty"` // command then args, for display
	URL      string       `json:"url,omitempty"`
	Host     string       `json:"host,omitempty"`
	Domain   string       `json:"domain,omitempty"`
	Inputs   []InputField `json:"inputs,omitempty"`  // non-secret values the user may or must give
	Missing  []string     `json:"missing,omitempty"` // keys of required inputs that have no value
	EnvVars  []EnvVar     `json:"envVars,omitempty"` // variables written as fromEnv: the user must set them
	Skipped  []EnvVar     `json:"skipped,omitempty"` // optional secret settings that were not written
	Warnings []Warning    `json:"warnings,omitempty"`
}

// InputField is a non-secret value the user may type; Value is what was used.
type InputField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Default     string   `json:"default,omitempty"`
	Format      string   `json:"format,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Value       string   `json:"value,omitempty"`
}

// EnvVar is a variable the server reads from the user's environment. Use is "env",
// "header:<Name>" or "bearer".
type EnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Use         string `json:"use"`
}

// Warning is something the user should know before adding a server; Params fill its text.
type Warning struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params,omitempty"`
}

// Reasons an option cannot be installed.
const (
	RefuseSSE            = "sse"
	RefuseMCPB           = "mcpb"
	RefuseCargo          = "cargo"
	RefuseUnsupported    = "unsupported_package"
	RefuseTransport      = "unsupported_transport"
	RefusePackageHTTP    = "package_http"
	RefuseNoVersion      = "no_version"
	RefuseCustomRegistry = "custom_registry"
	RefuseSmithery       = "smithery"
	RefuseInsecureHTTP   = "insecure_http"
	RefuseBadURL         = "bad_url"
	RefuseSecretLiteral  = "secret_literal"
	RefuseSecretArg      = "secret_arg"
	RefuseSecretMixed    = "secret_mixed"
	RefuseDockerVolumes  = "docker_volumes"
	RefuseDockerTemplate = "docker_template"
	RefusePOCI           = "poci"
	RefuseNoInstallData  = "no_install_data"
	RefuseInvalid        = "invalid"
)

// Warnings Translate and TranslateDocker add.
const (
	WarnRemote          = "remote"            // host, domain
	WarnDomainMismatch  = "domain_mismatch"   // host, namespace
	WarnBroadToken      = "broad_token"       // names
	WarnFirstRun        = "first_run"         // runtime
	WarnTag             = "tag"               // image
	WarnRepoMismatch    = "repo_mismatch"     // namespace, repo
	WarnDockerSelfBuilt = "docker_self_built" // image
	WarnOAuth           = "oauth"             // providers
	WarnAllowHosts      = "allow_hosts"       // hosts
)

// Warnings the marketplace adds, which need more than the entry itself.
const (
	WarnDeprecated    = "deprecated"
	WarnBulk          = "bulk_publisher" // count
	WarnDockerMissing = "docker_missing"
	WarnNotInOfficial = "not_in_official"
)

var (
	// sensitive copies skillshare's own test for names that must be environment references.
	sensitive  = regexp.MustCompile(`(?i)(token|secret|password|api.?key|authorization|credential)`)
	envNameRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	wholeRef   = regexp.MustCompile(`^\{([A-Za-z_][A-Za-z0-9_]*)\}$`)
	bearerRef  = regexp.MustCompile(`(?i)^bearer\s+\{([A-Za-z_][A-Za-z0-9_]*)\}$`)
	templateRe = regexp.MustCompile(`\{([^{}\s]+)\}`)
	broadToken = regexp.MustCompile(`(?i)(PERSONAL_ACCESS_TOKEN|^GH_TOKEN$|^GITHUB_TOKEN$|^AWS_|SECRET_ACCESS_KEY|SERVICE_ACCOUNT|PRIVATE_KEY|DATABASE_URL|CONNECTION_STRING|APPLICATION_CREDENTIALS|ADMIN)`)
	// Docker's templates: {{config.property}} and ${VAR}.
	dockerTpl    = regexp.MustCompile(`\{\{([A-Za-z0-9_.-]+)\}\}`)
	dollarRef    = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)
	dollarBearer = regexp.MustCompile(`(?i)^bearer\s+\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)
)

// builder collects one option's server settings and findings.
type builder struct {
	opt      *Option
	values   map[string]string
	env      map[string]mcp.Value
	headers  map[string]mcp.Value
	bearer   *mcp.Value
	envOrder []string
}

func newBuilder(o *Option, values map[string]string) *builder {
	return &builder{opt: o, values: values}
}

// refuse keeps the first reason only.
func (b *builder) refuse(code, detail string) {
	if b.opt.Refused == "" {
		b.opt.Refused, b.opt.Detail = code, detail
	}
}

func (b *builder) warn(code string, params map[string]string) {
	for _, w := range b.opt.Warnings {
		if w.Code == code && maps.Equal(w.Params, params) {
			return
		}
	}
	b.opt.Warnings = append(b.opt.Warnings, Warning{Code: code, Params: params})
}

func (b *builder) input(f InputField) {
	for i, have := range b.opt.Inputs {
		if have.Key == f.Key {
			b.opt.Inputs[i] = f
			return
		}
	}
	b.opt.Inputs = append(b.opt.Inputs, f)
}

func (b *builder) missing(key string) {
	if !slices.Contains(b.opt.Missing, key) {
		b.opt.Missing = append(b.opt.Missing, key)
	}
}

// given is the user's value for key, else def.
func (b *builder) given(key, def string) string {
	if v := b.values[key]; v != "" {
		return v
	}
	return def
}

// check marks a required input without a value, or a value outside the input's choices.
func (b *builder) check(key string, in Input, v string) {
	if (v == "" && in.IsRequired) || (v != "" && len(in.Choices) > 0 && !slices.Contains(in.Choices, v)) {
		b.missing(key)
	}
}

// literal refuses a value no config file should hold.
func (b *builder) literal(s string) string {
	if len(s) > 4096 || strings.ContainsAny(s, "\n\r\x00") {
		b.refuse(RefuseInvalid, "a value contains a line break or is too long")
	}
	return s
}

func (b *builder) setEnv(name string, v mcp.Value) {
	if b.env == nil {
		b.env = map[string]mcp.Value{}
	}
	if _, ok := b.env[name]; !ok {
		b.envOrder = append(b.envOrder, name)
	}
	b.env[name] = v
}

func (b *builder) setHeader(name string, v mcp.Value) {
	if b.headers == nil {
		b.headers = map[string]mcp.Value{}
	}
	b.headers[name] = v
}

// resolve fills {var} templates from vars, as the schema says: unknown names stay as written.
// A secret variable is never filled; secret reports that one was used.
func (b *builder) resolve(s string, vars map[string]Input, prefix string) (out string, secret bool) {
	out = templateRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		v, ok := vars[name]
		if !ok {
			return m
		}
		if v.IsSecret {
			secret = true
			return m
		}
		if v.Value != "" {
			return v.Value
		}
		key := prefix + "." + name
		val := b.given(key, v.Default)
		b.input(InputField{Key: key, Label: name, Description: v.Description, Default: v.Default, Format: v.Format, Choices: v.Choices, Required: v.IsRequired, Value: val})
		b.check(key, v, val)
		return val
	})
	return out, secret
}

// finish validates the server with skillshare's own rules and keeps it when nothing is missing.
func (b *builder) finish(server mcp.Server) {
	o := b.opt
	var broad []string
	for _, e := range o.EnvVars {
		if broadToken.MatchString(e.Name) && !slices.Contains(broad, e.Name) {
			broad = append(broad, e.Name)
		}
	}
	if len(broad) > 0 {
		b.warn(WarnBroadToken, map[string]string{"names": strings.Join(broad, ", ")})
	}
	if o.Refused != "" || len(o.Missing) > 0 {
		return
	}
	server.Env, server.Headers, server.BearerToken = b.env, b.headers, b.bearer
	if err := server.Validate(o.Name); err != nil {
		b.refuse(RefuseInvalid, err.Error())
		return
	}
	o.Server = &server
}

// Translate returns one option per package, then one per remote, of a registry entry. values
// maps input keys to what the user typed; nil is fine.
func Translate(s ServerJSON, values map[string]string) []Option {
	name := SuggestName(s.Name)
	ns := Namespace(s.Name)
	var opts []Option
	for i, p := range s.Packages {
		opts = append(opts, translatePackage(i, p, name, values))
	}
	for i, r := range s.Remotes {
		opts = append(opts, translateRemote(ns, i, r, name, values))
	}
	if len(opts) == 0 {
		opts = append(opts, Option{ID: "none", Kind: "none", Name: name, Refused: RefuseNoInstallData})
	}
	mismatch := repoMismatch(ns, s.Repository)
	for i := range opts {
		o := &opts[i]
		// Smithery's hosted proxy shut down on 2026-10-31; its republished entries point at it.
		if ns == "ai.smithery" {
			o.Refused, o.Detail, o.Server = RefuseSmithery, "", nil
		}
		if mismatch != nil {
			newBuilder(o, nil).warn(mismatch.Code, mismatch.Params)
		}
	}
	return opts
}

func translatePackage(i int, p Package, name string, values map[string]string) Option {
	o := Option{ID: fmt.Sprintf("package:%d", i), Name: name}
	b := newBuilder(&o, values)
	rt := strings.ToLower(strings.TrimSpace(p.RegistryType))
	ver := strings.TrimSpace(p.Version)
	o.Kind = rt
	o.Label = strings.TrimSpace(p.RegistryType + " " + p.Identifier)
	switch rt {
	case "npm":
		o.Runtime, o.Label = "npx", "npm "+p.Identifier+"@"+ver
	case "pypi":
		o.Runtime, o.Label = "uvx", "PyPI "+p.Identifier+"@"+ver
	case "oci":
		o.Runtime = "docker"
	case "mcpb":
		b.refuse(RefuseMCPB, "")
		return o
	case "cargo":
		b.refuse(RefuseCargo, "")
		return o
	default:
		if rt == "" {
			o.Kind = "unknown"
		}
		b.refuse(RefuseUnsupported, p.RegistryType)
		return o
	}
	if t := strings.ToLower(p.Transport.Type); t != "" && t != "stdio" {
		b.refuse(RefusePackageHTTP, p.Transport.Type)
		return o
	}
	image := ""
	switch rt {
	case "npm", "pypi":
		if ver == "" || strings.EqualFold(ver, "latest") {
			b.refuse(RefuseNoVersion, "")
			return o
		}
		base := strings.TrimRight(strings.TrimSpace(p.RegistryBaseURL), "/")
		ok := base == "" || (rt == "npm" && base == "https://registry.npmjs.org") ||
			(rt == "pypi" && (base == "https://pypi.org" || base == "https://pypi.org/simple"))
		if !ok {
			b.refuse(RefuseCustomRegistry, p.RegistryBaseURL)
			return o
		}
	case "oci":
		var pinned bool
		image, pinned = ociImage(p)
		o.Label = "OCI " + image
		if !pinned {
			b.refuse(RefuseNoVersion, "")
			return o
		}
		if !strings.Contains(image, "@sha256:") {
			b.warn(WarnTag, map[string]string{"image": image})
		}
	}
	b.warn(WarnFirstRun, map[string]string{"runtime": o.Runtime})

	runtimeArgs := b.args(p.RuntimeArguments, "runtime")
	packageArgs := b.args(p.PackageArguments, "package")
	b.envVars(p.EnvironmentVariables)

	var command string
	var args []string
	switch rt {
	case "npm":
		command = "npx"
		if !slices.Contains(runtimeArgs, "-y") && !slices.Contains(runtimeArgs, "--yes") {
			args = append(args, "-y")
		}
		args = append(args, runtimeArgs...)
		args = append(args, p.Identifier+"@"+ver)
	case "pypi":
		command = "uvx"
		args = append(args, runtimeArgs...)
		args = append(args, p.Identifier+"@"+ver)
	case "oci":
		command = "docker"
		args = append([]string{"run", "-i", "--rm"}, runtimeArgs...)
		for _, n := range b.envOrder {
			args = append(args, "-e", n)
		}
		args = append(args, image)
	}
	args = append(args, packageArgs...)
	o.Command = append([]string{command}, args...)
	b.finish(mcp.Server{Command: command, Args: args})
	return o
}

// ociImage is the image reference to run, and whether it is pinned by a tag other than latest
// or by a digest.
func ociImage(p Package) (string, bool) {
	image := strings.TrimSpace(p.Identifier)
	if image == "" {
		return "", false
	}
	last := image[strings.LastIndex(image, "/")+1:]
	hasDigest := strings.Contains(image, "@")
	hasTag := strings.Contains(last, ":")
	if !hasDigest && !hasTag && strings.TrimSpace(p.Version) != "" {
		image += ":" + strings.TrimSpace(p.Version)
		hasTag = true
	}
	if host := registryHost(p.RegistryBaseURL); host != "" && host != "docker.io" && host != "index.docker.io" && host != "registry-1.docker.io" {
		first := ""
		if i := strings.IndexByte(image, '/'); i >= 0 {
			first = image[:i]
		}
		if first == "" || (!strings.ContainsAny(first, ".:") && first != "localhost") {
			image = host + "/" + image
		}
	}
	if !hasDigest && (!hasTag || strings.HasSuffix(strings.ToLower(image), ":latest")) {
		return image, false
	}
	return image, true
}

func registryHost(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// args builds an argument list as separate words; nothing is ever joined into a shell string.
func (b *builder) args(list []Argument, listName string) []string {
	var out []string
	for i, a := range list {
		key := fmt.Sprintf("%s.%d", listName, i)
		if a.IsSecret {
			if a.Value != "" {
				b.refuse(RefuseSecretLiteral, "")
			} else {
				b.refuse(RefuseSecretArg, "")
			}
			continue
		}
		named := strings.EqualFold(a.Type, "named")
		var v string
		if a.Value != "" {
			r, secret := b.resolve(a.Value, a.Variables, key)
			if secret {
				b.refuse(RefuseSecretArg, "")
				continue
			}
			v = r
		} else {
			// A named argument with nothing to fill in is a switch, such as --read-only.
			if named && a.Name != "" && a.Default == "" && a.ValueHint == "" && len(a.Choices) == 0 && !a.IsRequired && (a.Format == "" || a.Format == "boolean") {
				out = append(out, b.literal(a.Name))
				continue
			}
			v = b.given(key, a.Default)
			label := a.ValueHint
			if label == "" {
				label = a.Name
			}
			if label == "" {
				label = key
			}
			b.input(InputField{Key: key, Label: label, Description: a.Description, Default: a.Default, Format: a.Format, Choices: a.Choices, Required: a.IsRequired, Value: v})
			b.check(key, a.Input, v)
		}
		if v == "" {
			continue
		}
		if named && a.Name != "" {
			out = append(out, b.literal(a.Name))
		}
		out = append(out, b.literal(v))
	}
	return out
}

// envVars writes a package's environment: secrets as references, the rest as literals.
func (b *builder) envVars(list []KeyValueInput) {
	for _, e := range list {
		name := e.Name
		if !envNameRe.MatchString(name) {
			b.refuse(RefuseInvalid, "invalid environment variable name "+name)
			continue
		}
		key := "env." + name
		if e.IsSecret || sensitive.MatchString(name) {
			if v := strings.TrimSpace(e.Value); v != "" && !wholeRef.MatchString(v) {
				b.refuse(RefuseSecretLiteral, "")
				continue
			}
			ev := EnvVar{Name: name, Description: e.Description, Required: e.IsRequired, Use: "env"}
			if !e.IsRequired {
				b.opt.Skipped = append(b.opt.Skipped, ev)
				continue
			}
			b.setEnv(name, mcp.Value{FromEnv: name})
			b.opt.EnvVars = append(b.opt.EnvVars, ev)
			continue
		}
		var v string
		if e.Value != "" {
			r, secret := b.resolve(e.Value, e.Variables, key)
			if secret {
				b.refuse(RefuseSecretMixed, "")
				continue
			}
			v = r
		} else {
			v = b.given(key, e.Default)
			b.input(InputField{Key: key, Label: name, Description: e.Description, Default: e.Default, Format: e.Format, Choices: e.Choices, Required: e.IsRequired, Value: v})
			b.check(key, e.Input, v)
		}
		if v != "" {
			b.setEnv(name, mcp.Value{Literal: b.literal(v)})
		}
	}
}

func translateRemote(ns string, i int, r Transport, name string, values map[string]string) Option {
	o := Option{ID: fmt.Sprintf("remote:%d", i), Kind: "remote", Name: name, Label: r.URL, URL: r.URL}
	b := newBuilder(&o, values)
	switch strings.ToLower(r.Type) {
	case "streamable-http":
	case "sse":
		o.Kind = "sse"
		b.refuse(RefuseSSE, "")
		return o
	default:
		b.refuse(RefuseTransport, r.Type)
		return o
	}
	u, secret := b.resolve(r.URL, r.Variables, "url")
	if secret {
		b.refuse(RefuseSecretArg, "")
		return o
	}
	o.URL, o.Label = u, u
	if !b.checkURL(u) {
		return o
	}
	if nd := NamespaceDomain(ns); nd != "" && o.Domain != "" && nd != o.Domain {
		b.warn(WarnDomainMismatch, map[string]string{"host": o.Host, "namespace": nd})
	}
	b.httpHeaders(r.Headers)
	b.finish(mcp.Server{URL: u})
	return o
}

// checkURL applies the rules for a remote endpoint and records its host.
func (b *builder) checkURL(raw string) bool {
	b.literal(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		b.refuse(RefuseBadURL, "")
		return false
	}
	host := strings.ToLower(u.Hostname())
	b.opt.Host, b.opt.Domain = host, Domain(host)
	if u.User != nil || u.Fragment != "" {
		b.refuse(RefuseBadURL, "")
		return false
	}
	for key := range u.Query() {
		if sensitive.MatchString(key) {
			b.refuse(RefuseBadURL, "")
			return false
		}
	}
	if host == "server.smithery.ai" || strings.HasSuffix(host, ".smithery.ai") || host == "run.tools" || strings.HasSuffix(host, ".run.tools") {
		b.refuse(RefuseSmithery, "")
		return false
	}
	if u.Scheme == "http" && !localHost(host) {
		b.refuse(RefuseInsecureHTTP, "")
		return false
	}
	b.warn(WarnRemote, map[string]string{"host": host, "domain": b.opt.Domain})
	return b.opt.Refused == ""
}

func localHost(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "127.0.0.1" || host == "::1"
}

// httpHeaders writes a remote's headers: secrets as references, the rest as literals.
func (b *builder) httpHeaders(list []KeyValueInput) {
	for _, h := range list {
		if h.Name == "" || strings.ContainsAny(h.Name, "\r\n\x00: ") {
			b.refuse(RefuseInvalid, "invalid header name "+h.Name)
			continue
		}
		key := "header." + h.Name
		secretVar := false
		for _, m := range templateRe.FindAllStringSubmatch(h.Value, -1) {
			if v, ok := h.Variables[m[1]]; ok && v.IsSecret {
				secretVar = true
			}
		}
		if h.IsSecret || sensitive.MatchString(h.Name) || secretVar {
			b.secretHeader(h)
			continue
		}
		var v string
		if h.Value != "" {
			v, _ = b.resolve(h.Value, h.Variables, key)
		} else {
			v = b.given(key, h.Default)
			b.input(InputField{Key: key, Label: h.Name, Description: h.Description, Default: h.Default, Format: h.Format, Choices: h.Choices, Required: h.IsRequired, Value: v})
			b.check(key, h.Input, v)
		}
		if v != "" {
			b.setHeader(h.Name, mcp.Value{Literal: b.literal(v)})
		}
	}
}

func (b *builder) secretHeader(h KeyValueInput) {
	val := strings.TrimSpace(h.Value)
	var env, use string
	switch {
	case strings.EqualFold(h.Name, "Authorization") && bearerRef.MatchString(val):
		env, use = bearerRef.FindStringSubmatch(val)[1], "bearer"
	case wholeRef.MatchString(val):
		env, use = wholeRef.FindStringSubmatch(val)[1], "header:"+h.Name
	case val == "":
		env, use = envFromHeader(h.Name), "header:"+h.Name
	case strings.Contains(val, "{"):
		b.refuse(RefuseSecretMixed, "")
		return
	default:
		b.refuse(RefuseSecretLiteral, "")
		return
	}
	desc := h.Description
	if desc == "" {
		desc = h.Variables[env].Description
	}
	ev := EnvVar{Name: env, Description: desc, Required: h.IsRequired, Use: use}
	if !h.IsRequired {
		b.opt.Skipped = append(b.opt.Skipped, ev)
		return
	}
	if use == "bearer" {
		b.bearer = &mcp.Value{FromEnv: env}
	} else {
		b.setHeader(h.Name, mcp.Value{FromEnv: env})
	}
	b.opt.EnvVars = append(b.opt.EnvVars, ev)
}

// envFromHeader names the variable for a header that gives none: X-Api-Key → X_API_KEY.
func envFromHeader(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	s := sb.String()
	if s != "" && s[0] >= '0' && s[0] <= '9' {
		s = "_" + s
	}
	return s
}

// repoMismatch warns when an io.github namespace's repository belongs to another GitHub owner:
// the registry checks the namespace, not the repository link.
func repoMismatch(ns string, repo *Repository) *Warning {
	if !strings.HasPrefix(ns, "io.github.") || repo == nil {
		return nil
	}
	owner := strings.TrimPrefix(ns, "io.github.")
	u, err := url.Parse(repo.URL)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || strings.EqualFold(parts[0], owner) {
		return nil
	}
	return &Warning{Code: WarnRepoMismatch, Params: map[string]string{"namespace": owner, "repo": parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")}}
}

// TranslateDocker returns the single option of a Docker catalog entry.
func TranslateDocker(e DockerEntry, values map[string]string) []Option {
	o := Option{ID: "docker", Name: SuggestName(e.Name)}
	b := newBuilder(&o, values)
	switch e.Type {
	case "poci":
		o.Kind, o.Label = "poci", "Docker "+e.Name
		b.refuse(RefusePOCI, "")
	case "remote":
		b.dockerRemote(e)
	case "server":
		b.dockerServer(e)
	default:
		o.Kind = e.Type
		b.refuse(RefuseNoInstallData, "")
	}
	return []Option{o}
}

func (b *builder) dockerRemote(e DockerEntry) {
	o := b.opt
	o.Kind = "docker-remote"
	if e.Remote == nil || e.Remote.URL == "" {
		b.refuse(RefuseNoInstallData, "")
		return
	}
	raw := e.Remote.URL
	o.URL, o.Label = raw, raw
	switch tt := strings.ToLower(e.Remote.TransportType); tt {
	case "", "streamable-http":
	case "sse":
		b.refuse(RefuseSSE, "")
		return
	default:
		b.refuse(RefuseTransport, e.Remote.TransportType)
		return
	}
	if strings.Contains(raw, "{{") || strings.Contains(raw, "${") {
		b.refuse(RefuseDockerTemplate, "")
		return
	}
	if !b.checkURL(raw) {
		return
	}
	b.dockerOAuth(e)
	for _, k := range slices.Sorted(maps.Keys(e.Remote.Headers)) {
		v := strings.TrimSpace(e.Remote.Headers[k])
		switch {
		case strings.EqualFold(k, "Authorization") && dollarBearer.MatchString(v):
			env := dollarBearer.FindStringSubmatch(v)[1]
			b.bearer = &mcp.Value{FromEnv: env}
			o.EnvVars = append(o.EnvVars, EnvVar{Name: env, Description: secretDescription(e, env), Required: true, Use: "bearer"})
		case dollarRef.MatchString(v):
			env := dollarRef.FindStringSubmatch(v)[1]
			b.setHeader(k, mcp.Value{FromEnv: env})
			o.EnvVars = append(o.EnvVars, EnvVar{Name: env, Description: secretDescription(e, env), Required: true, Use: "header:" + k})
		case strings.Contains(v, "${") || strings.Contains(v, "{{"):
			b.refuse(RefuseDockerTemplate, "")
			return
		case sensitive.MatchString(k):
			b.refuse(RefuseSecretLiteral, "")
			return
		default:
			b.setHeader(k, mcp.Value{Literal: b.literal(v)})
		}
	}
	b.finish(mcp.Server{URL: raw})
}

func (b *builder) dockerServer(e DockerEntry) {
	o := b.opt
	o.Kind, o.Runtime, o.Label = "docker", "docker", "Docker "+e.Image
	if e.Image == "" {
		b.refuse(RefuseNoInstallData, "")
		return
	}
	// Mounting host folders needs the user's own paths and consent; the MCP page can do that.
	if len(e.Volumes) > 0 {
		b.refuse(RefuseDockerVolumes, strings.Join(e.Volumes, ", "))
		return
	}
	if strings.Contains(e.User, "{{") {
		b.refuse(RefuseDockerTemplate, "")
		return
	}
	b.warn(WarnFirstRun, map[string]string{"runtime": "docker"})
	if !strings.Contains(e.Image, "@sha256:") {
		b.warn(WarnTag, map[string]string{"image": e.Image})
	}
	// Only images under mcp/ are built, signed and given an SBOM by Docker.
	if !strings.HasPrefix(e.Image, "mcp/") {
		b.warn(WarnDockerSelfBuilt, map[string]string{"image": e.Image})
	}
	b.dockerOAuth(e)
	if len(e.AllowHosts) > 0 {
		b.warn(WarnAllowHosts, map[string]string{"hosts": strings.Join(e.AllowHosts, ", ")})
	}
	for _, s := range e.Secrets {
		if !envNameRe.MatchString(s.Env) {
			b.refuse(RefuseInvalid, "invalid environment variable name "+s.Env)
			return
		}
		b.setEnv(s.Env, mcp.Value{FromEnv: s.Env})
		o.EnvVars = append(o.EnvVars, EnvVar{Name: s.Env, Description: s.Description, Required: true, Use: "env"})
	}
	for _, ev := range e.Env {
		if !envNameRe.MatchString(ev.Name) {
			b.refuse(RefuseInvalid, "invalid environment variable name "+ev.Name)
			return
		}
		v := ev.Value
		if m := dockerTpl.FindStringSubmatch(v); m != nil && m[0] == v {
			if sensitive.MatchString(ev.Name) {
				b.setEnv(ev.Name, mcp.Value{FromEnv: ev.Name})
				o.EnvVars = append(o.EnvVars, EnvVar{Name: ev.Name, Description: dockerProperty(e, m[1]).Description, Required: true, Use: "env"})
				continue
			}
			if val := b.dockerInput(e, m[1]); val != "" {
				b.setEnv(ev.Name, mcp.Value{Literal: b.literal(val)})
			}
			continue
		}
		if strings.Contains(v, "{{") || strings.Contains(v, "$") {
			b.refuse(RefuseDockerTemplate, "")
			return
		}
		if sensitive.MatchString(ev.Name) {
			b.refuse(RefuseSecretLiteral, "")
			return
		}
		b.setEnv(ev.Name, mcp.Value{Literal: b.literal(v)})
	}
	var tail []string
	for _, item := range e.Command {
		had, empty := dockerTpl.MatchString(item), false
		out := dockerTpl.ReplaceAllStringFunc(item, func(m string) string {
			v := b.dockerInput(e, m[2:len(m)-2])
			if v == "" {
				empty = true
			}
			return v
		})
		// Templates with filters, such as {{x|volume}}, are Docker's own.
		if strings.Contains(dockerTpl.ReplaceAllString(item, ""), "{{") {
			b.refuse(RefuseDockerTemplate, "")
			return
		}
		if had && empty {
			continue
		}
		tail = append(tail, b.literal(out))
	}
	args := []string{"run", "-i", "--rm"}
	if e.DisableNetwork {
		args = append(args, "--network", "none")
	}
	if e.User != "" {
		args = append(args, "--user", e.User)
	}
	for _, n := range b.envOrder {
		args = append(args, "-e", n)
	}
	args = append(args, e.Image)
	args = append(args, tail...)
	o.Command = append([]string{"docker"}, args...)
	b.finish(mcp.Server{Command: "docker", Args: args})
}

// dockerInput is the value of a {{config.property}} template: the user's, else its default.
func (b *builder) dockerInput(e DockerEntry, ref string) string {
	key := "config." + ref
	_, prop := splitRef(ref)
	p := dockerProperty(e, ref)
	f := InputField{Key: key, Label: prop, Description: p.Description, Required: dockerRequired(e, ref)}
	if p.Default != nil {
		f.Default = fmt.Sprint(p.Default)
	}
	f.Value = b.given(key, f.Default)
	b.input(f)
	if f.Value == "" && f.Required {
		b.missing(key)
	}
	return f.Value
}

func splitRef(ref string) (config, prop string) {
	if i := strings.IndexByte(ref, '.'); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ref
}

func dockerProperty(e DockerEntry, ref string) DockerProperty {
	cfg, prop := splitRef(ref)
	for _, c := range e.Config {
		if c.Name == cfg {
			return c.Properties[prop]
		}
	}
	return DockerProperty{}
}

func dockerRequired(e DockerEntry, ref string) bool {
	cfg, prop := splitRef(ref)
	for _, c := range e.Config {
		if c.Name == cfg {
			return slices.Contains(c.Required, prop)
		}
	}
	return false
}

func secretDescription(e DockerEntry, env string) string {
	for _, s := range e.Secrets {
		if s.Env == env {
			return s.Description
		}
	}
	return ""
}

func (b *builder) dockerOAuth(e DockerEntry) {
	if e.OAuth == nil {
		return
	}
	var names []string
	for _, p := range e.OAuth.Providers {
		if p.Provider != "" && !slices.Contains(names, p.Provider) {
			names = append(names, p.Provider)
		}
	}
	if len(names) > 0 {
		b.warn(WarnOAuth, map[string]string{"providers": strings.Join(names, ", ")})
	}
}

// genericNames say nothing on their own, so SuggestName puts the owner in front.
var genericNames = map[string]bool{"": true, "mcp": true, "server": true, "mcp-server": true, "mcpserver": true, "main": true, "api": true, "remote": true, "app": true}

// SuggestName turns a registry name into a skillshare server name: the part after the last
// "/", with the owner in front when that part is generic.
func SuggestName(registryName string) string {
	last := registryName
	slash := strings.LastIndexByte(registryName, '/')
	if slash >= 0 {
		last = registryName[slash+1:]
	}
	name := cleanName(last)
	if genericNames[name] && slash >= 0 {
		if owner := cleanName(ownerLabel(Namespace(registryName))); owner != "" {
			name = strings.TrimSuffix(owner+"-"+name, "-")
		}
	}
	if len(name) > 128 {
		name = name[:128]
	}
	if name == "" {
		return "server"
	}
	return name
}

func cleanName(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('-')
		}
	}
	return strings.TrimLeft(sb.String(), "-.")
}

func ownerLabel(ns string) string {
	for _, p := range []string{"io.github.", "io.gitlab."} {
		if strings.HasPrefix(ns, p) {
			return ns[len(p):]
		}
	}
	if parts := strings.Split(ns, "."); len(parts) >= 2 {
		return parts[1]
	}
	return ns
}

// twoLevelSuffixes are public suffixes of two labels, so Domain keeps three labels for them.
var twoLevelSuffixes = map[string]bool{
	"co.uk": true, "org.uk": true, "ac.uk": true, "gov.uk": true, "com.au": true, "net.au": true, "org.au": true,
	"co.jp": true, "ne.jp": true, "or.jp": true, "com.br": true, "com.cn": true, "com.tw": true, "co.in": true,
	"co.kr": true, "co.nz": true, "com.mx": true, "com.sg": true, "com.tr": true,
}

// Domain is a host's registrable domain, roughly: its last two labels (three under a known
// two-label suffix). IP addresses and single labels come back unchanged.
func Domain(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if net.ParseIP(host) != nil {
		return host
	}
	labels := strings.Split(host, ".")
	if len(labels) <= 2 {
		return host
	}
	n := 2
	if twoLevelSuffixes[strings.Join(labels[len(labels)-2:], ".")] {
		n = 3
	}
	return strings.Join(labels[len(labels)-n:], ".")
}

// NamespaceDomain is the domain a reverse-DNS namespace claims ("com.microsoft" →
// "microsoft.com"); "" for GitHub and GitLab namespaces and names without a dot.
func NamespaceDomain(ns string) string {
	if !strings.Contains(ns, ".") || strings.HasPrefix(ns, "io.github.") || strings.HasPrefix(ns, "io.gitlab.") {
		return ""
	}
	labels := strings.Split(ns, ".")
	slices.Reverse(labels)
	return Domain(strings.Join(labels, "."))
}
