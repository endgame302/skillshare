// Fork-only (endgame302/skillshare): Docker's MCP catalog (catalog.yaml, version 3).

package mcpreg

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// DockerCatalog is Docker's published MCP catalog.
type DockerCatalog struct {
	Version  int                    `yaml:"version"`
	Name     string                 `yaml:"name"`
	Registry map[string]DockerEntry `yaml:"registry"`
}

// DockerEntry is one server of the catalog: a container image (type server), a remote server
// (type remote) or a tool container only Docker's gateway runs (type poci).
type DockerEntry struct {
	Name           string         `yaml:"-" json:"name"`
	Title          string         `yaml:"title" json:"title,omitempty"`
	Description    string         `yaml:"description" json:"description,omitempty"`
	Type           string         `yaml:"type" json:"type,omitempty"`
	DateAdded      string         `yaml:"dateAdded" json:"dateAdded,omitempty"`
	Image          string         `yaml:"image" json:"image,omitempty"`
	Source         string         `yaml:"source" json:"source,omitempty"`
	Upstream       string         `yaml:"upstream" json:"upstream,omitempty"`
	Icon           string         `yaml:"icon" json:"icon,omitempty"`
	Secrets        []DockerSecret `yaml:"secrets" json:"secrets,omitempty"`
	Env            []DockerEnv    `yaml:"env" json:"env,omitempty"`
	Command        []string       `yaml:"command" json:"command,omitempty"`
	Volumes        []string       `yaml:"volumes" json:"volumes,omitempty"`
	User           string         `yaml:"user" json:"user,omitempty"`
	DisableNetwork bool           `yaml:"disableNetwork" json:"disableNetwork,omitempty"`
	AllowHosts     []string       `yaml:"allowHosts" json:"allowHosts,omitempty"`
	Config         []DockerConfig `yaml:"config" json:"config,omitempty"`
	OAuth          *DockerOAuth   `yaml:"oauth" json:"oauth,omitempty"`
	Remote         *DockerRemote  `yaml:"remote" json:"remote,omitempty"`
	Metadata       DockerMetadata `yaml:"metadata" json:"metadata"`
}

// DockerSecret is a secret the server reads from an environment variable.
type DockerSecret struct {
	Name        string `yaml:"name" json:"name"`
	Env         string `yaml:"env" json:"env"`
	Example     string `yaml:"example" json:"example,omitempty"`
	Description string `yaml:"description" json:"description,omitempty"`
}

// DockerEnv is an environment variable; Value may hold a {{config.property}} template.
type DockerEnv struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}

// DockerConfig describes the settings an entry's templates refer to, as a JSON Schema object.
type DockerConfig struct {
	Name        string                    `yaml:"name" json:"name"`
	Description string                    `yaml:"description" json:"description,omitempty"`
	Properties  map[string]DockerProperty `yaml:"properties" json:"properties,omitempty"`
	Required    []string                  `yaml:"required" json:"required,omitempty"`
}

// DockerProperty is one setting of a DockerConfig.
type DockerProperty struct {
	Type        string `yaml:"type" json:"type,omitempty"`
	Description string `yaml:"description" json:"description,omitempty"`
	Default     any    `yaml:"default" json:"default,omitempty"`
}

// DockerOAuth names the OAuth providers Docker's gateway can sign in with.
type DockerOAuth struct {
	Providers []struct {
		Provider string `yaml:"provider" json:"provider"`
		Secret   string `yaml:"secret" json:"secret,omitempty"`
		Env      string `yaml:"env" json:"env,omitempty"`
	} `yaml:"providers" json:"providers,omitempty"`
}

// DockerRemote is a remote server's endpoint; header values may hold ${VAR} references.
type DockerRemote struct {
	TransportType string            `yaml:"transport_type" json:"transportType"`
	URL           string            `yaml:"url" json:"url"`
	Headers       map[string]string `yaml:"headers" json:"headers,omitempty"`
}

// DockerMetadata is the catalog's popularity and classification data.
type DockerMetadata struct {
	Pulls       int      `yaml:"pulls" json:"pulls,omitempty"`
	Stars       int      `yaml:"stars" json:"stars,omitempty"`
	GithubStars int      `yaml:"githubStars" json:"githubStars,omitempty"`
	Category    string   `yaml:"category" json:"category,omitempty"`
	Tags        []string `yaml:"tags" json:"tags,omitempty"`
	License     string   `yaml:"license" json:"license,omitempty"`
	Owner       string   `yaml:"owner" json:"owner,omitempty"`
}

// ParseDockerCatalog decodes catalog.yaml version 3, sets each entry's Name from its key and
// drops entries with an empty key.
func ParseDockerCatalog(data []byte) (*DockerCatalog, error) {
	var c DockerCatalog
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("Docker's MCP catalog is not valid YAML: %w", err)
	}
	if c.Version != 3 {
		return nil, fmt.Errorf("Docker's MCP catalog version %d is not supported", c.Version)
	}
	for name, e := range c.Registry {
		if name == "" {
			delete(c.Registry, name)
			continue
		}
		e.Name = name
		c.Registry[name] = e
	}
	return &c, nil
}
