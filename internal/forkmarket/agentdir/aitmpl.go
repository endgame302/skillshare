// Fork-only (endgame302/skillshare): reads the aitmpl.com v1 agent catalog.

package agentdir

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"skillshare/internal/forkmarket/ghrepo"
)

const (
	aitmplTimeout      = 30 * time.Second
	maxAitmplBytes     = 16 << 20 // 16 MiB
	aitmplGitHubPrefix = "https://github.com/"
	aitmplPagePrefix   = "https://www.aitmpl.com/"
	aitmplDirPrefix    = "cli-tool/components/agents/"
)

var aitmplSegRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type aitmplEnvelope struct {
	SchemaVersion string            `json:"schemaVersion"`
	GeneratedAt   string            `json:"generatedAt"`
	Source        string            `json:"source"`
	Count         int               `json:"count"`
	Components    []aitmplComponent `json:"components"`
}

type aitmplComponent struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Category    string          `json:"category"`
	Path        string          `json:"path"`
	Description string          `json:"description"`
	Downloads   *int            `json:"downloads"`
	URLs        *aitmplURLs     `json:"urls"`
	Scan        *Scan           `json:"scan"`
	Source      json.RawMessage `json:"source"`
}

type aitmplURLs struct {
	Page string `json:"page"`
}

func (c *Client) listAitmpl(ctx context.Context, refresh bool) (*Listing, error) {
	old := c.readCache("aitmpl")
	if old != nil && !refresh && c.now().Before(old.Listing.FetchedAt.Add(aitmplCacheTTL)) {
		l := old.Listing
		l.Agents = nonNilAgents(l.Agents)
		return &l, nil
	}

	var oldETag string
	if old != nil {
		oldETag = old.ETag
	}

	listing, etag, err := c.fetchAitmpl(ctx, old, oldETag)
	if err != nil {
		if old != nil {
			l := old.Listing
			l.Agents = nonNilAgents(l.Agents)
			l.Stale = true
			l.StaleReason = err.Error()
			return &l, nil
		}
		return nil, err
	}
	c.writeCache("aitmpl", listing, etag)
	return listing, nil
}

func (c *Client) fetchAitmpl(ctx context.Context, old *diskCache, oldETag string) (*Listing, string, error) {
	u := c.AitmplURL
	if u == "" {
		u = DefaultAitmplURL
	}

	reqCtx, cancel := context.WithTimeout(ctx, aitmplTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "skillshare")
	req.Header.Set("Accept", "application/json")
	if oldETag != "" {
		req.Header.Set("If-None-Match", oldETag)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching aitmpl catalog failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		if old == nil {
			return nil, "", errors.New("aitmpl answered HTTP 304 without a cached catalog")
		}
		l := old.Listing
		l.Agents = nonNilAgents(l.Agents)
		l.FetchedAt = c.now().UTC()
		l.Stale = false
		l.StaleReason = ""
		etag := resp.Header.Get("ETag")
		if etag == "" {
			etag = oldETag
		}
		return &l, etag, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("aitmpl catalog answered HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAitmplBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("reading aitmpl catalog failed: %w", err)
	}
	if int64(len(body)) > maxAitmplBytes {
		return nil, "", fmt.Errorf("aitmpl catalog exceeds %d bytes", maxAitmplBytes)
	}

	listing, err := parseAitmplCatalog(body, c.now().UTC())
	if err != nil {
		return nil, "", err
	}
	return listing, resp.Header.Get("ETag"), nil
}

func parseAitmplCatalog(body []byte, fetchedAt time.Time) (*Listing, error) {
	var env aitmplEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parsing aitmpl catalog failed: %w", err)
	}
	major, _, _ := strings.Cut(env.SchemaVersion, ".")
	if major != "1" {
		return nil, fmt.Errorf("unsupported aitmpl schemaVersion %q", env.SchemaVersion)
	}
	if !strings.HasPrefix(env.Source, aitmplGitHubPrefix) {
		return nil, errors.New("aitmpl's catalog names no GitHub repository")
	}
	repo := strings.TrimPrefix(env.Source, aitmplGitHubPrefix)
	if !ghrepo.ValidRepo(repo) {
		return nil, errors.New("aitmpl's catalog names no GitHub repository")
	}

	agents := make([]Agent, 0, len(env.Components))
	for _, comp := range env.Components {
		if agent, ok := convertAitmplComponent(comp, repo); ok {
			agents = append(agents, agent)
		}
	}

	return &Listing{
		Source:      "aitmpl",
		Repo:        repo,
		GeneratedAt: env.GeneratedAt,
		Agents:      agents,
		FetchedAt:   fetchedAt,
	}, nil
}

func convertAitmplComponent(comp aitmplComponent, repo string) (Agent, bool) {
	if comp.Type != "agents" {
		return Agent{}, false
	}
	if rawSrc := bytes.TrimSpace(comp.Source); len(rawSrc) > 0 && !bytes.Equal(rawSrc, []byte("null")) {
		return Agent{}, false
	}

	parts := strings.Split(comp.Path, "/")
	if len(parts) != 2 {
		return Agent{}, false
	}
	pathCat, file := parts[0], parts[1]
	if !aitmplSegRe.MatchString(pathCat) || !aitmplSegRe.MatchString(file) {
		return Agent{}, false
	}
	if len(file) <= 3 || !strings.HasSuffix(strings.ToLower(file), ".md") {
		return Agent{}, false
	}
	cat := comp.Category
	if cat == "" {
		cat = pathCat
	} else if cat != pathCat {
		return Agent{}, false
	}

	name := file[:len(file)-3]
	title := strings.TrimSpace(comp.Name)
	if title == "" {
		title = name
	}

	var pageURL string
	if comp.URLs != nil && strings.HasPrefix(comp.URLs.Page, aitmplPagePrefix) {
		pageURL = comp.URLs.Page
	}

	return Agent{
		ID:          "aitmpl:" + comp.ID,
		Name:        name,
		Title:       title,
		Description: cleanDescription(comp.Description),
		Category:    cat,
		Repo:        repo,
		Dir:         aitmplDirPrefix + cat,
		File:        file,
		Downloads:   comp.Downloads,
		Scan:        comp.Scan,
		PageURL:     pageURL,
	}, true
}
