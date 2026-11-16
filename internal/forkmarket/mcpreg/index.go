// Fork-only (endgame302/skillshare): the trimmed entries the dashboard lists, and how a page of
// them is picked.

package mcpreg

import (
	"cmp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Entry is one server as the dashboard lists it: a trimmed record, small enough to keep the
// official registry's ~65,000 of them.
type Entry struct {
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version,omitempty"`
	Namespace   string   `json:"namespace,omitempty"`
	Status      string   `json:"status,omitempty"` // active or deprecated
	PublishedAt string   `json:"publishedAt,omitempty"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
	Repository  string   `json:"repository,omitempty"`
	Website     string   `json:"website,omitempty"`
	Kinds       []string `json:"kinds,omitempty"`  // each option's Kind, once, in option order
	Local       bool     `json:"local,omitempty"`  // has a package or container option
	Remote      bool     `json:"remote,omitempty"` // has a remote option
	Installable bool     `json:"installable"`      // some option is not refused (missing inputs do not count)
	Blocked     string   `json:"blocked,omitempty"`
	License     string   `json:"license,omitempty"`
	Stars       int      `json:"stars,omitempty"`
	PushedAt    string   `json:"pushedAt,omitempty"`
	Official    bool     `json:"official,omitempty"` // a GitHub registry entry also in the official index
	Category    string   `json:"category,omitempty"`
	Pulls       int      `json:"pulls,omitempty"`
	Image       string   `json:"image,omitempty"`
	DockerBuilt bool     `json:"dockerBuilt,omitempty"` // an image under mcp/, built and signed by Docker
	Tags        []string `json:"tags,omitempty"`
}

const maxDescription = 400

func cut(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= maxDescription {
		return s
	}
	return string([]rune(s)[:maxDescription]) + "…"
}

// summarize fills the fields that come from the install options.
func (e *Entry) summarize(opts []Option) {
	e.Blocked = ""
	for _, o := range opts {
		if !slices.Contains(e.Kinds, o.Kind) && o.Kind != "none" {
			e.Kinds = append(e.Kinds, o.Kind)
		}
		switch o.Kind {
		case "remote", "sse", "docker-remote":
			e.Remote = true
		case "none":
		default:
			e.Local = true
		}
		if o.Refused == "" {
			e.Installable = true
		}
	}
	if !e.Installable && len(opts) > 0 {
		e.Blocked = opts[0].Refused
	}
}

// EntryFromServer trims a registry record.
func EntryFromServer(r ServerResponse) Entry {
	s := r.Server
	meta := r.Official()
	e := Entry{
		Name:        s.Name,
		Title:       strings.TrimSpace(s.Title),
		Description: cut(s.Description),
		Version:     s.Version,
		Namespace:   Namespace(s.Name),
		Status:      meta.Status,
		PublishedAt: meta.PublishedAt,
		UpdatedAt:   meta.UpdatedAt,
		Website:     s.WebsiteURL,
	}
	if e.Status == "" {
		e.Status = "active"
	}
	if e.Title == "" {
		e.Title = s.Name[strings.LastIndexByte(s.Name, '/')+1:]
	}
	if s.Repository != nil {
		e.Repository = s.Repository.URL
	}
	if gh := s.GitHub(); gh != nil {
		e.License, e.Stars, e.PushedAt = gh.License, gh.Stars, gh.PushedAt
	}
	e.summarize(Translate(s, nil))
	return e
}

// EntryFromDocker trims a Docker catalog entry.
func EntryFromDocker(d DockerEntry) Entry {
	e := Entry{
		Name:        d.Name,
		Title:       strings.TrimSpace(d.Title),
		Description: cut(d.Description),
		Status:      "active",
		PublishedAt: d.DateAdded,
		Repository:  d.Upstream,
		License:     d.Metadata.License,
		Stars:       d.Metadata.GithubStars,
		Category:    d.Metadata.Category,
		Pulls:       d.Metadata.Pulls,
		Image:       d.Image,
		DockerBuilt: strings.HasPrefix(d.Image, "mcp/"),
		Tags:        d.Metadata.Tags,
	}
	if e.Title == "" {
		e.Title = d.Name
	}
	if e.Repository == "" {
		e.Repository = d.Source
	}
	e.summarize(TranslateDocker(d, nil))
	return e
}

// BulkThreshold is how many entries make a namespace a bulk publisher. 45 namespaces held 21%
// of the official registry's active entries in November 2026, many of them generated.
const BulkThreshold = 50

// Query selects and orders entries. The zero value lists everything except bulk publishers and
// deprecated entries, in name order, 50 at a time.
type Query struct {
	Text            string // whitespace-separated terms; all must match name, title, description, category or tags
	Kind            string // "", "local" or "remote"
	InstallableOnly bool
	ShowBulk        bool
	ShowDeprecated  bool
	Sort            string // name, updated, stars, pulls; "" is relevance with Text, else name
	Offset, Limit   int
}

// Page is one page of a filtered list.
type Page struct {
	Items            []Entry `json:"items"`
	Total            int     `json:"total"`
	HiddenBulk       int     `json:"hiddenBulk"`
	HiddenDeprecated int     `json:"hiddenDeprecated"`
	BulkThreshold    int     `json:"bulkThreshold"`
}

// Status says how fresh a source's list is.
type Status struct {
	State     string    `json:"state"` // ready, loading, or error (nothing to show and the last attempt failed)
	Entries   int       `json:"entries"`
	Complete  bool      `json:"complete"` // official: the first crawl has finished; others: loaded
	Syncing   bool      `json:"syncing"`
	FetchedAt time.Time `json:"fetchedAt,omitzero"`
	Error     string    `json:"error,omitempty"`
	RetryAt   time.Time `json:"retryAt,omitzero"`
	Stale     bool      `json:"stale"`
}

// NamespaceCounts counts entries per namespace.
func NamespaceCounts(entries []Entry) map[string]int {
	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Namespace]++
	}
	return counts
}

func (e Entry) matches(terms []string) bool {
	hay := strings.ToLower(e.Name + "\n" + e.Title + "\n" + e.Description + "\n" + e.Category + "\n" + strings.Join(e.Tags, " "))
	for _, t := range terms {
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

func relevance(e Entry, text string, first string) int {
	name, title := strings.ToLower(e.Name), strings.ToLower(e.Title)
	short := strings.ToLower(e.Name[strings.LastIndexByte(e.Name, '/')+1:])
	switch {
	case name == text || title == text || short == text:
		return 100
	case strings.HasPrefix(name, text) || strings.HasPrefix(title, text) || strings.HasPrefix(short, text):
		return 50
	case strings.Contains(name, first):
		return 20
	case strings.Contains(title, first):
		return 10
	}
	return 1
}

// Filter applies q to entries; counts (from NamespaceCounts) enables the bulk publisher filter.
func Filter(entries []Entry, q Query, counts map[string]int) Page {
	text := strings.ToLower(strings.TrimSpace(q.Text))
	terms := strings.Fields(text)
	p := Page{Items: []Entry{}, BulkThreshold: BulkThreshold}
	var hits []Entry
	for _, e := range entries {
		if len(terms) > 0 && !e.matches(terms) {
			continue
		}
		if (q.Kind == "local" && !e.Local) || (q.Kind == "remote" && !e.Remote) || (q.InstallableOnly && !e.Installable) {
			continue
		}
		if e.Status == "deprecated" && !q.ShowDeprecated {
			p.HiddenDeprecated++
			continue
		}
		if counts != nil && counts[e.Namespace] >= BulkThreshold && !q.ShowBulk {
			p.HiddenBulk++
			continue
		}
		hits = append(hits, e)
	}
	byName := func(a, b Entry) int { return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) }
	switch {
	case q.Sort == "updated":
		slices.SortStableFunc(hits, func(a, b Entry) int { return cmp.Or(cmp.Compare(b.UpdatedAt, a.UpdatedAt), byName(a, b)) })
	case q.Sort == "stars":
		slices.SortStableFunc(hits, func(a, b Entry) int { return cmp.Or(cmp.Compare(b.Stars, a.Stars), byName(a, b)) })
	case q.Sort == "pulls":
		slices.SortStableFunc(hits, func(a, b Entry) int { return cmp.Or(cmp.Compare(b.Pulls, a.Pulls), byName(a, b)) })
	case q.Sort == "" && len(terms) > 0:
		score := make(map[string]int, len(hits))
		for _, e := range hits {
			score[e.Name] = relevance(e, text, terms[0])
		}
		slices.SortStableFunc(hits, func(a, b Entry) int { return cmp.Or(cmp.Compare(score[b.Name], score[a.Name]), byName(a, b)) })
	default:
		slices.SortStableFunc(hits, byName)
	}
	p.Total = len(hits)
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 100)
	offset := min(max(q.Offset, 0), p.Total)
	p.Items = append(p.Items, hits[offset:min(offset+limit, p.Total)]...)
	return p
}
