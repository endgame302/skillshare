// Fork-only (endgame302/skillshare): one front for the three sources, used by the dashboard's
// routes: list a page of one source, and plan the install of one entry.

package mcpreg

import (
	"context"
	"errors"
	"maps"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
)

// ErrUnknownSource is a source name other than the three below.
var ErrUnknownSource = errors.New("unknown MCP marketplace source")

// The marketplace's sources.
const (
	SourceOfficial = "official"
	SourceCurated  = "curated"
	SourceDocker   = "docker"
)

// Market lists and plans entries of the official registry, GitHub's registry and Docker's catalog.
type Market struct {
	Official *Official
	Curated  *Curated
	Docker   *Docker
	LookPath func(string) (string, error) // exec.LookPath; tests replace it
	GOOS     string
}

// NewMarket keeps every source's files under dir.
func NewMarket(dir string) *Market {
	return &Market{
		Official: NewOfficial(dir),
		Curated:  NewCurated(dir),
		Docker:   NewDocker(dir),
		LookPath: exec.LookPath,
		GOOS:     runtime.GOOS,
	}
}

func (m *Market) dockerFound() bool {
	_, err := m.LookPath("docker")
	return err == nil
}

// ListResult is one page of one source.
type ListResult struct {
	Source string `json:"source"`
	Page
	Status      Status `json:"status"`
	DockerFound *bool  `json:"dockerFound,omitempty"` // docker source only
}

// List returns one page of a source. The official registry is listed as far as it is indexed,
// never waiting for the network; a pass starts in the background when one is due. A query's own
// Sort wins over each source's default; with Text and no Sort the order is relevance.
func (m *Market) List(ctx context.Context, source string, q Query, refresh bool) (*ListResult, error) {
	defaultSort := func(s string) {
		if q.Sort == "" && q.Text == "" {
			q.Sort = s
		}
	}
	res := &ListResult{Source: source}
	switch source {
	case SourceOfficial:
		m.Official.Start(refresh)
		entries, counts, st := m.Official.view()
		defaultSort("name")
		res.Page, res.Status = Filter(entries, q, counts), st
	case SourceCurated:
		entries, st, err := m.Curated.Load(ctx, refresh)
		if err != nil {
			return nil, err
		}
		defaultSort("stars")
		res.Page, res.Status = Filter(entries, q, nil), st
		// Which entries the official registry lists is only known once it is fully indexed.
		if m.Official.complete() {
			for i := range res.Items {
				_, res.Items[i].Official = m.Official.Lookup(res.Items[i].Name)
			}
		}
	case SourceDocker:
		entries, st, err := m.Docker.Load(ctx, refresh)
		if err != nil {
			return nil, err
		}
		defaultSort("pulls")
		res.Page, res.Status = Filter(entries, q, nil), st
		found := m.dockerFound()
		res.DockerFound = &found
	default:
		return nil, ErrUnknownSource
	}
	return res, nil
}

// PlanResult is one entry and the ways to install it, filled with the user's values.
type PlanResult struct {
	Source      string       `json:"source"`
	Entry       Entry        `json:"entry"`
	Server      *ServerJSON  `json:"server,omitempty"` // the registry record (official, curated)
	Docker      *DockerEntry `json:"docker,omitempty"`
	Options     []Option     `json:"options"`
	OS          string       `json:"os"`
	DockerFound bool         `json:"dockerFound"`
}

// Plan translates one entry, with the user's input values, into install options, and adds what
// the marketplace knows beyond the entry: deprecation, bulk publishers, a namespace the official
// registry has not verified, and a missing Docker.
func (m *Market) Plan(ctx context.Context, source, name string, values map[string]string) (*PlanResult, error) {
	res := &PlanResult{Source: source, OS: m.GOOS, DockerFound: m.dockerFound()}
	var extra []Warning
	switch source {
	case SourceOfficial:
		r, err := m.Official.Detail(ctx, name)
		if err != nil {
			return nil, err
		}
		entry, ok := m.Official.Lookup(name)
		if !ok {
			entry = EntryFromServer(*r)
		}
		res.Entry, res.Server, res.Options = entry, &r.Server, Translate(r.Server, values)
		if _, counts, _ := m.Official.view(); counts[entry.Namespace] >= BulkThreshold {
			extra = append(extra, Warning{Code: WarnBulk, Params: map[string]string{"count": strconv.Itoa(counts[entry.Namespace])}})
		}
	case SourceCurated:
		r, err := m.Curated.Server(ctx, name)
		if err != nil {
			return nil, err
		}
		res.Entry, res.Server, res.Options = EntryFromServer(*r), &r.Server, Translate(r.Server, values)
		if m.Official.complete() {
			_, res.Entry.Official = m.Official.Lookup(name)
			if !res.Entry.Official {
				extra = append(extra, Warning{Code: WarnNotInOfficial})
			}
		}
	case SourceDocker:
		e, err := m.Docker.Entry(ctx, name)
		if err != nil {
			return nil, err
		}
		res.Entry, res.Docker, res.Options = EntryFromDocker(e), &e, TranslateDocker(e, values)
	default:
		return nil, ErrUnknownSource
	}
	if res.Entry.Status == "deprecated" {
		extra = append([]Warning{{Code: WarnDeprecated}}, extra...)
	}
	for i := range res.Options {
		o := &res.Options[i]
		add := slices.Clone(extra)
		if o.Runtime == "docker" && !res.DockerFound {
			add = append(add, Warning{Code: WarnDockerMissing})
		}
		for _, w := range add {
			if !slices.ContainsFunc(o.Warnings, func(have Warning) bool { return have.Code == w.Code && maps.Equal(have.Params, w.Params) }) {
				o.Warnings = append(o.Warnings, w)
			}
		}
	}
	return res, nil
}
