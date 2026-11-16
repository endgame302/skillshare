// Package mcpreg lists MCP servers from the official MCP Registry, GitHub's MCP registry and
// Docker's MCP catalog, and translates one entry into a skillshare MCP server definition
// (fork-only, endgame302/skillshare).
//
// The official registry (https://registry.modelcontextprotocol.io, API v0.1) is the source of
// record; GitHub's registry (https://api.mcp.github.com) is a curated subset in the same schema
// with GitHub data added; Docker's catalog is a separate, reviewed list of container images and
// remote servers. Nothing here starts a server: an entry becomes a portable mcp.Server that the
// dashboard saves through skillshare's own MCP API.
package mcpreg

import (
	"encoding/json"
	"strings"
)

// Keys of the _meta objects the registries add.
const (
	MetaOfficial  = "io.modelcontextprotocol.registry/official"
	MetaPublisher = "io.modelcontextprotocol.registry/publisher-provided"
)

// ListResponse is one page of GET /v0.1/servers.
type ListResponse struct {
	Servers  []ServerResponse `json:"servers"`
	Metadata struct {
		NextCursor string `json:"nextCursor,omitempty"`
		Count      int    `json:"count,omitempty"`
	} `json:"metadata"`
}

// ServerResponse is one record: a server.json and the registry's own metadata about it.
type ServerResponse struct {
	Server ServerJSON                 `json:"server"`
	Meta   map[string]json.RawMessage `json:"_meta,omitempty"`
}

// OfficialMeta is what the registry says about a record: its status and dates.
type OfficialMeta struct {
	Status          string `json:"status,omitempty"` // active, deprecated, deleted
	StatusChangedAt string `json:"statusChangedAt,omitempty"`
	PublishedAt     string `json:"publishedAt,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
	IsLatest        bool   `json:"isLatest"`
}

// Official decodes the registry's metadata; the zero value when it is absent or invalid.
func (r ServerResponse) Official() OfficialMeta {
	var m OfficialMeta
	if raw, ok := r.Meta[MetaOfficial]; ok {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

// ServerJSON is a server.json document (schema 2025-12-11, also read for 2025-09-29).
type ServerJSON struct {
	Schema      string                     `json:"$schema,omitempty"`
	Name        string                     `json:"name"`
	Title       string                     `json:"title,omitempty"`
	Description string                     `json:"description,omitempty"`
	Version     string                     `json:"version,omitempty"`
	WebsiteURL  string                     `json:"websiteUrl,omitempty"`
	Repository  *Repository                `json:"repository,omitempty"`
	Packages    []Package                  `json:"packages,omitempty"`
	Remotes     []Transport                `json:"remotes,omitempty"`
	Meta        map[string]json.RawMessage `json:"_meta,omitempty"`
}

// GitHubMeta is the repository data GitHub's registry adds to each server.
type GitHubMeta struct {
	License          string   `json:"license,omitempty"`
	Stars            int      `json:"stargazerCount,omitempty"`
	PushedAt         string   `json:"pushedAt,omitempty"`
	NameWithOwner    string   `json:"nameWithOwner,omitempty"`
	IsInOrganization bool     `json:"isInOrganization,omitempty"`
	Topics           []string `json:"topics,omitempty"`
	PrimaryLanguage  string   `json:"primaryLanguage,omitempty"`
}

// GitHub decodes the GitHub data under the publisher-provided metadata; nil when absent.
func (s ServerJSON) GitHub() *GitHubMeta {
	raw, ok := s.Meta[MetaPublisher]
	if !ok {
		return nil
	}
	var pp struct {
		GitHub *GitHubMeta `json:"github"`
	}
	if json.Unmarshal(raw, &pp) != nil {
		return nil
	}
	return pp.GitHub
}

// Repository is where a server's source lives.
type Repository struct {
	URL       string `json:"url,omitempty"`
	Source    string `json:"source,omitempty"`
	Subfolder string `json:"subfolder,omitempty"`
	ID        string `json:"id,omitempty"`
}

// Package is one way to run a server locally from a package registry.
type Package struct {
	RegistryType         string          `json:"registryType"`
	RegistryBaseURL      string          `json:"registryBaseUrl,omitempty"`
	Identifier           string          `json:"identifier"`
	Version              string          `json:"version,omitempty"`
	FileSHA256           string          `json:"fileSha256,omitempty"`
	RuntimeHint          string          `json:"runtimeHint,omitempty"`
	Transport            Transport       `json:"transport"`
	RuntimeArguments     []Argument      `json:"runtimeArguments,omitempty"`
	PackageArguments     []Argument      `json:"packageArguments,omitempty"`
	EnvironmentVariables []KeyValueInput `json:"environmentVariables,omitempty"`
}

// Transport is a package's transport or one entry of remotes.
type Transport struct {
	Type      string           `json:"type"`
	URL       string           `json:"url,omitempty"`
	Headers   []KeyValueInput  `json:"headers,omitempty"`
	Variables map[string]Input `json:"variables,omitempty"`
}

// Input is a value a server needs: fixed by the publisher (Value) or given by the user.
type Input struct {
	Description string   `json:"description,omitempty"`
	Value       string   `json:"value,omitempty"`
	Default     string   `json:"default,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Format      string   `json:"format,omitempty"`
	IsRequired  bool     `json:"isRequired,omitempty"`
	IsSecret    bool     `json:"isSecret,omitempty"`
	Choices     []string `json:"choices,omitempty"`
}

// KeyValueInput is an environment variable or an HTTP header.
type KeyValueInput struct {
	Input
	Name      string           `json:"name"`
	Variables map[string]Input `json:"variables,omitempty"`
}

// Argument is a positional or named command-line argument.
type Argument struct {
	Input
	Type       string           `json:"type"`
	Name       string           `json:"name,omitempty"`
	ValueHint  string           `json:"valueHint,omitempty"`
	IsRepeated bool             `json:"isRepeated,omitempty"`
	Variables  map[string]Input `json:"variables,omitempty"`
}

// Namespace is the part of a registry name before the first "/" ("io.github.alice" for
// "io.github.alice/weather"); the whole name when it has no "/".
func Namespace(name string) string {
	if i := strings.IndexByte(name, '/'); i >= 0 {
		return name[:i]
	}
	return name
}
