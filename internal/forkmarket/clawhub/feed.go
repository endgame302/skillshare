// Package clawhub lists the skills in ClawHub's public feed and installs a ClawHub-hosted
// skill only after its archive matches the feed's integrity hash (fork-only,
// endgame302/skillshare).
//
// The feed (https://clawhub.ai/v1/feeds/skills) is a weekly snapshot of skills by verified
// publishers. Each entry names one install candidate: either a ClawHub-hosted release
// ("public-clawhub", downloaded as a zip) or a GitHub folder at a pinned commit
// ("public-github"). Only the first kind is installed here; see HostedCandidate.
package clawhub

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Feed is ClawHub's skills feed, schema version 1.
type Feed struct {
	SchemaVersion int     `json:"schemaVersion"`
	ID            string  `json:"id"`
	GeneratedAt   string  `json:"generatedAt"`
	Sequence      int64   `json:"sequence"`
	ExpiresAt     string  `json:"expiresAt"`
	Description   string  `json:"description"`
	Entries       []Entry `json:"entries"`
}

// Entry is one skill in the feed. ID is "@owner/slug".
type Entry struct {
	Type        string    `json:"type"`
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
	State       string    `json:"state"`
	Featured    bool      `json:"featured"`
	Publisher   Publisher `json:"publisher"`
	Install     struct {
		Candidates []Candidate `json:"candidates"`
	} `json:"install"`
}

// Publisher is who published an entry and how far ClawHub vouches for them.
type Publisher struct {
	ID    string `json:"id"`
	Trust string `json:"trust"`
}

// Candidate is one way to install an entry. Integrity is "sha256:<64 hex>".
type Candidate struct {
	SourceRef string        `json:"sourceRef"`
	Package   string        `json:"package"`
	Version   string        `json:"version"`
	Integrity string        `json:"integrity"`
	GitHub    *GitHubSource `json:"github,omitempty"`
}

// GitHubSource is the pinned GitHub folder of a "public-github" candidate.
type GitHubSource struct {
	Repo        string `json:"repo"`
	Path        string `json:"path"`
	Commit      string `json:"commit"`
	ContentHash string `json:"contentHash"`
}

const (
	sourceHosted = "public-clawhub"
	sourceGitHub = "public-github"
)

// Reasons an entry cannot be installed, as Item.Blocked carries them.
const (
	BlockedGitHub      = "github"       // a GitHub folder; its content hash is not checked here yet
	BlockedUnavailable = "unavailable"  // the entry's state is not "available"
	BlockedIntegrity   = "no_integrity" // no usable sha256 integrity value
	BlockedUnsupported = "unsupported"  // no candidate this client knows
)

// ParseFeed decodes a feed and keeps the skill entries whose id is a valid @owner/slug.
func ParseFeed(data []byte) (*Feed, error) {
	var f Feed
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("ClawHub feed is not valid JSON: %w", err)
	}
	if f.SchemaVersion != 1 {
		return nil, fmt.Errorf("ClawHub feed schema version %d is not supported", f.SchemaVersion)
	}
	kept := f.Entries[:0]
	for _, e := range f.Entries {
		if e.Type != "skill" {
			continue
		}
		if _, _, ok := ParseID(e.ID); !ok {
			continue
		}
		kept = append(kept, e)
	}
	f.Entries = kept
	return &f, nil
}

// Expiry returns when the feed says it expires, or the zero time when it does not say.
func (f *Feed) Expiry() time.Time {
	t, err := time.Parse(time.RFC3339Nano, f.ExpiresAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Lookup returns the entry with the given "@owner/slug" id.
func (f *Feed) Lookup(id string) (Entry, bool) {
	for _, e := range f.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// A handle or slug becomes a folder name and a query parameter, so only plain names pass.
var idPattern = regexp.MustCompile(`^@([A-Za-z0-9][A-Za-z0-9_.-]{0,99})/([A-Za-z0-9][A-Za-z0-9_.-]{0,199})$`)

// ParseID splits "@owner/slug". The slug must also be a safe folder name on this system.
func ParseID(id string) (owner, slug string, ok bool) {
	m := idPattern.FindStringSubmatch(id)
	if m == nil || strings.Contains(m[2], "..") || !filepath.IsLocal(m[2]) {
		return "", "", false
	}
	return m[1], m[2], true
}

var integrityPattern = regexp.MustCompile(`^sha256:([0-9a-fA-F]{64})$`)

// ParseIntegrity returns the lower-case hex digest of a "sha256:<hex>" value.
func ParseIntegrity(v string) (string, bool) {
	m := integrityPattern.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return "", false
	}
	return strings.ToLower(m[1]), true
}

// HostedCandidate returns the ClawHub-hosted release to install, or why there is none.
// GitHub-backed entries carry a hash of the folder's files rather than of an archive, which
// this package does not compute yet, so they are listed but not installed.
func (e Entry) HostedCandidate() (Candidate, string) {
	if e.State != "available" {
		return Candidate{}, BlockedUnavailable
	}
	github := false
	for _, c := range e.Install.Candidates {
		switch c.SourceRef {
		case sourceHosted:
			if c.Package != e.ID || !validVersion(c.Version) {
				continue
			}
			if _, ok := ParseIntegrity(c.Integrity); !ok {
				return Candidate{}, BlockedIntegrity
			}
			return c, ""
		case sourceGitHub:
			github = true
		}
	}
	if github {
		return Candidate{}, BlockedGitHub
	}
	return Candidate{}, BlockedUnsupported
}

// Origin names a release for people: "clawhub://@owner/slug@version". The operation log records
// it; the installed skill's source is the release folder (see Release.Dir).
func Origin(id, version string) string {
	return "clawhub://" + id + "@" + version
}

// Item is an entry as the dashboard lists it.
type Item struct {
	ID          string        `json:"id"`
	Owner       string        `json:"owner"`
	Slug        string        `json:"slug"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Version     string        `json:"version"`
	Featured    bool          `json:"featured"`
	Publisher   string        `json:"publisher"`
	Trust       string        `json:"trust"`
	Source      string        `json:"source"` // "clawhub" or "github"
	GitHub      *GitHubSource `json:"github,omitempty"`
	Integrity   string        `json:"integrity,omitempty"`
	Installable bool          `json:"installable"`
	Blocked     string        `json:"blocked,omitempty"`
}

// Items converts the feed's entries for the dashboard, in feed order.
func (f *Feed) Items() []Item {
	items := make([]Item, 0, len(f.Entries))
	for _, e := range f.Entries {
		owner, slug, _ := ParseID(e.ID)
		it := Item{
			ID:          e.ID,
			Owner:       owner,
			Slug:        slug,
			Title:       strings.TrimSpace(e.Title),
			Description: strings.TrimSpace(e.Description),
			Version:     e.Version,
			Featured:    e.Featured,
			Publisher:   e.Publisher.ID,
			Trust:       e.Publisher.Trust,
			Source:      "clawhub",
		}
		if it.Title == "" {
			it.Title = slug
		}
		for _, c := range e.Install.Candidates {
			if c.SourceRef == sourceGitHub && c.GitHub != nil {
				it.Source = "github"
				gh := *c.GitHub
				it.GitHub = &gh
			}
		}
		cand, blocked := e.HostedCandidate()
		it.Installable = blocked == ""
		it.Blocked = blocked
		if it.Installable {
			it.Source = "clawhub"
			it.GitHub = nil
			it.Integrity = cand.Integrity
			it.Version = cand.Version
		}
		items = append(items, it)
	}
	return items
}
