// Fork-only (endgame302/skillshare): Docker's MCP catalog, fetched with a conditional GET and
// kept on disk.

package mcpreg

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultDockerCatalogURL is Docker's published MCP catalog.
	DefaultDockerCatalogURL = "https://desktop.docker.com/mcp/catalog/v3/catalog.yaml"
	// DockerTTL is how long a fetched copy is used before asking again.
	DockerTTL = 6 * time.Hour

	dockerFile     = "docker-catalog.yaml"
	dockerMetaFile = "docker-catalog.meta.json.gz"
	maxDockerBytes = 16 << 20
)

// dockerMeta says when the saved catalog was fetched and how to ask whether it changed.
type dockerMeta struct {
	FetchedAt    time.Time `json:"fetchedAt"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"lastModified,omitempty"`
}

// Docker is Docker's MCP catalog, kept on disk for DockerTTL.
type Docker struct {
	URL  string
	HTTP *http.Client
	Dir  string
	Now  func() time.Time

	mu      sync.Mutex
	loaded  bool
	catalog *DockerCatalog
	entries []Entry
	meta    dockerMeta
	lastErr string
	retryAt time.Time
}

// NewDocker returns Docker's catalog, cached under dir.
func NewDocker(dir string) *Docker {
	return &Docker{URL: DefaultDockerCatalogURL, HTTP: newHTTPClient("Docker's MCP catalog"), Dir: dir, Now: time.Now}
}

func (d *Docker) use(c *DockerCatalog) {
	d.catalog = c
	d.entries = make([]Entry, 0, len(c.Registry))
	for _, e := range c.Registry {
		d.entries = append(d.entries, EntryFromDocker(e))
	}
	slices.SortFunc(d.entries, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
}

func (d *Docker) status() Status {
	st := Status{Entries: len(d.entries), Error: d.lastErr, RetryAt: d.retryAt}
	if d.catalog != nil {
		st.FetchedAt, st.Complete = d.meta.FetchedAt, true
		st.Stale = d.lastErr != "" && d.Now().Sub(d.meta.FetchedAt) >= DockerTTL
	}
	switch {
	case d.catalog != nil:
		st.State = "ready"
	case d.lastErr != "":
		st.State = "error"
	default:
		st.State = "loading"
	}
	return st
}

// Load returns the entries sorted by name and the status, with the same caching rules as
// Curated.Load.
func (d *Docker) Load(ctx context.Context, refresh bool) ([]Entry, Status, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.loaded {
		d.loaded = true
		var meta dockerMeta
		if readGzipJSON(filepath.Join(d.Dir, dockerMetaFile), &meta) == nil && !meta.FetchedAt.IsZero() {
			if data, err := os.ReadFile(filepath.Join(d.Dir, dockerFile)); err == nil {
				if c, err := ParseDockerCatalog(data); err == nil {
					d.use(c)
					d.meta = meta
				}
			}
		}
	}
	now := d.Now()
	fresh := d.catalog != nil && now.Sub(d.meta.FetchedAt) < DockerTTL
	waiting := !refresh && now.Before(d.retryAt)
	if (fresh && !refresh) || waiting {
		if d.catalog == nil {
			return nil, d.status(), errors.New(d.lastErr)
		}
		return d.entries, d.status(), nil
	}
	if err := d.fetch(ctx, now); err != nil {
		d.lastErr, d.retryAt = err.Error(), now.Add(failedRetry)
		if d.catalog == nil {
			return nil, d.status(), err
		}
		return d.entries, d.status(), nil
	}
	d.lastErr, d.retryAt = "", time.Time{}
	return d.entries, d.status(), nil
}

// fetch asks for the catalog, conditionally when a copy exists; nothing is written unless the
// answer parses.
func (d *Docker) fetch(ctx context.Context, now time.Time) error {
	extra := map[string]string{"Accept": "application/yaml, text/yaml, */*"}
	if d.catalog != nil {
		if d.meta.ETag != "" {
			extra["If-None-Match"] = d.meta.ETag
		}
		if d.meta.LastModified != "" {
			extra["If-Modified-Since"] = d.meta.LastModified
		}
	}
	resp, data, err := get(ctx, d.HTTP, d.URL, "Docker's MCP catalog", d.Now, maxDockerBytes, extra)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotModified {
		if d.catalog == nil {
			return errors.New("Docker's MCP catalog answered 304 to a request without a cached copy")
		}
		d.meta.FetchedAt = now.UTC()
		_ = writeGzipJSON(filepath.Join(d.Dir, dockerMetaFile), d.meta)
		return nil
	}
	c, err := ParseDockerCatalog(data)
	if err != nil {
		return err
	}
	d.use(c)
	d.meta = dockerMeta{FetchedAt: now.UTC(), ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}
	if writeFileAtomic(filepath.Join(d.Dir, dockerFile), data) == nil {
		_ = writeGzipJSON(filepath.Join(d.Dir, dockerMetaFile), d.meta)
	}
	return nil
}

// Entry returns the catalog entry with this name.
func (d *Docker) Entry(ctx context.Context, name string) (DockerEntry, error) {
	if _, _, err := d.Load(ctx, false); err != nil {
		return DockerEntry{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.catalog.Registry[name]
	if !ok {
		return DockerEntry{}, ErrNotFound
	}
	return e, nil
}
