// Fork-only (endgame302/skillshare): a local index of the official MCP Registry, built by one
// resumable crawl and then kept fresh with hourly incremental passes, as the registry asks
// aggregators to do (pull about hourly, use updated_since, keep your own copy).

package mcpreg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultOfficialBase is the official MCP Registry.
	DefaultOfficialBase = "https://registry.modelcontextprotocol.io"
	// OfficialSyncEvery is how often the index is brought up to date.
	OfficialSyncEvery = time.Hour

	userAgent      = "skillshare" // generic; never personal data
	maxPageBytes   = 8 << 20
	requestTimeout = 30 * time.Second
	maxPages       = 5000
	indexVersion   = 1
	officialFile   = "official-index.json.gz"
	maxDetails     = 256
	maxBackoff     = time.Hour
	firstBackoff   = 30 * time.Second
)

// ErrNotFound means the source does not list the server asked for.
var ErrNotFound = errors.New("the registry does not list this server")

// ErrSyncRunning means a pass over the official registry is already running.
var ErrSyncRunning = errors.New("the MCP Registry index is already being updated")

// newHTTPClient follows at most five redirects, and only to https.
func newHTTPClient(who string) *http.Client {
	return &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("%s redirected to a non-https address", who)
		}
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	}}
}

// StatusError is a source's answer other than 200; RetryAt is set from Retry-After.
type StatusError struct {
	Who     string
	Code    int
	RetryAt time.Time
}

func (e *StatusError) Error() string { return fmt.Sprintf("%s answered HTTP %d", e.Who, e.Code) }

// get fetches rawURL and returns at most limit bytes of a 200 answer. extra headers are added.
func get(ctx context.Context, client *http.Client, rawURL, who string, now func() time.Time, limit int64, extra map[string]string) (*http.Response, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("could not reach %s: %w", who, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return resp, nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return resp, nil, &StatusError{Who: who, Code: resp.StatusCode, RetryAt: retryAfter(resp.Header.Get("Retry-After"), now())}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return resp, nil, fmt.Errorf("could not read %s's answer: %w", who, err)
	}
	if int64(len(data)) > limit {
		return resp, nil, fmt.Errorf("%s answered with more than %d bytes", who, limit)
	}
	return resp, data, nil
}

// getJSON fetches rawURL and decodes a 200 answer into v.
func getJSON(ctx context.Context, client *http.Client, rawURL, who string, now func() time.Time, v any) error {
	_, data, err := get(ctx, client, rawURL, who, now, maxPageBytes, map[string]string{"Accept": "application/json"})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s answered with JSON this version cannot read: %w", who, err)
	}
	return nil
}

func retryAfter(h string, now time.Time) time.Time {
	h = strings.TrimSpace(h)
	if h == "" {
		return time.Time{}
	}
	if s, err := strconv.Atoi(h); err == nil && s >= 0 {
		return now.Add(time.Duration(min(s, 86400)) * time.Second)
	}
	if t, err := http.ParseTime(h); err == nil {
		return t
	}
	return time.Time{}
}

func isStatus(err error, code int) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == code
}

// backoff is when to try again after the given number of failures in a row.
func backoff(now time.Time, failures int, err error) time.Time {
	d := firstBackoff << min(max(failures-1, 0), 8)
	at := now.Add(min(d, maxBackoff))
	var se *StatusError
	if errors.As(err, &se) && se.RetryAt.After(at) {
		at = se.RetryAt
	}
	return at
}

// indexState is the index as it is saved.
type indexState struct {
	Version      int              `json:"version"`
	Entries      map[string]Entry `json:"entries"`
	Cursor       string           `json:"cursor,omitempty"` // next page of an unfinished first crawl
	Complete     bool             `json:"complete"`
	CrawlStarted time.Time        `json:"crawlStarted,omitzero"`
	SyncedAt     time.Time        `json:"syncedAt,omitzero"`  // updated_since of the next incremental pass
	CheckedAt    time.Time        `json:"checkedAt,omitzero"` // when the last pass finished
}

// Official keeps a trimmed local index of the official MCP Registry under Dir.
type Official struct {
	Base         string
	HTTP         *http.Client
	Dir          string // the index lives in Dir/official-index.json.gz
	Now          func() time.Time
	PageDelay    time.Duration // pause between pages
	PersistEvery int           // pages between saves during the first crawl

	mu       sync.Mutex
	loaded   bool
	state    indexState
	dirty    bool
	sorted   []Entry
	counts   map[string]int
	syncing  bool
	lastErr  string
	failures int
	retryAt  time.Time
	details  map[string]*ServerResponse
}

// NewOfficial returns the official registry's index, kept under dir.
func NewOfficial(dir string) *Official {
	return &Official{
		Base:         DefaultOfficialBase,
		HTTP:         newHTTPClient("The MCP Registry"),
		Dir:          dir,
		Now:          time.Now,
		PageDelay:    100 * time.Millisecond,
		PersistEvery: 25,
	}
}

func (o *Official) path() string { return filepath.Join(o.Dir, officialFile) }

// ensure loads the saved index once; an unreadable file or another version starts over.
func (o *Official) ensure() {
	o.mu.Lock()
	loaded := o.loaded
	o.mu.Unlock()
	if loaded {
		return
	}
	var st indexState
	err := readGzipJSON(o.path(), &st)
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.loaded {
		return
	}
	if err != nil || st.Version != indexVersion || st.Entries == nil {
		st = indexState{Version: indexVersion, Entries: map[string]Entry{}}
	}
	o.state, o.loaded, o.dirty = st, true, true
}

// view returns the entries sorted by name, the entries per namespace and the status. The slice
// and map are shared: callers must not modify them.
func (o *Official) view() ([]Entry, map[string]int, Status) {
	o.ensure()
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.dirty {
		sorted := make([]Entry, 0, len(o.state.Entries))
		for _, e := range o.state.Entries {
			sorted = append(sorted, e)
		}
		slices.SortFunc(sorted, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
		o.sorted, o.counts, o.dirty = sorted, NamespaceCounts(sorted), false
	}
	return o.sorted, o.counts, o.statusLocked()
}

// Snapshot returns the indexed entries sorted by name (shared: callers must not modify them)
// and the index's status.
func (o *Official) Snapshot() ([]Entry, Status) {
	entries, _, st := o.view()
	return entries, st
}

func (o *Official) statusLocked() Status {
	st := Status{
		Entries:   len(o.state.Entries),
		Complete:  o.state.Complete,
		Syncing:   o.syncing,
		FetchedAt: o.state.CheckedAt,
		Error:     o.lastErr,
		RetryAt:   o.retryAt,
	}
	switch {
	case st.Complete:
		st.State = "ready"
	case st.Entries == 0 && !st.Syncing && st.Error != "":
		st.State = "error"
	default:
		st.State = "loading"
	}
	st.Stale = st.Complete && st.Error != "" && o.Now().Sub(o.state.CheckedAt) > 2*OfficialSyncEvery
	return st
}

// Lookup returns the indexed entry with this name.
func (o *Official) Lookup(name string) (Entry, bool) {
	o.ensure()
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.state.Entries[name]
	return e, ok
}

// complete reports whether the first crawl has finished.
func (o *Official) complete() bool {
	o.ensure()
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.state.Complete
}

// Due reports whether a pass should start: never while one runs; when the first crawl has not
// finished, OfficialSyncEvery has passed since the last pass, or force; and, unless force, not
// before the retry time a failure set.
func (o *Official) Due(force bool) bool {
	o.ensure()
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.dueLocked(force)
}

func (o *Official) dueLocked(force bool) bool {
	if o.syncing {
		return false
	}
	now := o.Now()
	if !force && now.Before(o.retryAt) {
		return false
	}
	return force || !o.state.Complete || now.Sub(o.state.CheckedAt) >= OfficialSyncEvery
}

// Start runs a pass in the background when Due(force), and reports whether it started one.
func (o *Official) Start(force bool) bool {
	o.ensure()
	o.mu.Lock()
	if !o.dueLocked(force) {
		o.mu.Unlock()
		return false
	}
	o.syncing = true
	o.mu.Unlock()
	go func() { _ = o.pass(context.Background()) }()
	return true
}

// Sync runs one pass to completion or to its first error: the first crawl, from where it
// stopped, while the index is not complete, else an incremental pass.
func (o *Official) Sync(ctx context.Context) error {
	o.ensure()
	o.mu.Lock()
	if o.syncing {
		o.mu.Unlock()
		return ErrSyncRunning
	}
	o.syncing = true
	o.mu.Unlock()
	return o.pass(ctx)
}

// pass runs with syncing set and clears it when done.
func (o *Official) pass(ctx context.Context) error {
	o.mu.Lock()
	complete := o.state.Complete
	o.mu.Unlock()
	var err error
	if complete {
		err = o.incremental(ctx)
	} else {
		err = o.crawl(ctx)
	}
	o.mu.Lock()
	if err == nil {
		o.failures, o.lastErr, o.retryAt = 0, "", time.Time{}
	} else {
		o.failures++
		o.lastErr, o.retryAt = err.Error(), backoff(o.Now(), o.failures, err)
	}
	o.syncing = false
	o.mu.Unlock()
	o.persist()
	return err
}

// persist saves the index; a cache that cannot be written only costs a crawl.
func (o *Official) persist() {
	o.mu.Lock()
	data, err := json.Marshal(o.state)
	o.mu.Unlock()
	if err == nil {
		_ = writeGzip(o.path(), data)
	}
}

// pages walks a listing from cursor, calling apply with each page and the next cursor, until
// the listing ends.
func (o *Official) pages(ctx context.Context, q url.Values, cursor string, apply func(*ListResponse, string)) error {
	for n := 1; ; n++ {
		v := url.Values{}
		for k, vs := range q {
			v[k] = vs
		}
		if cursor != "" {
			v.Set("cursor", cursor)
		}
		var page ListResponse
		if err := getJSON(ctx, o.HTTP, strings.TrimRight(o.Base, "/")+"/v0.1/servers?"+v.Encode(), "the MCP Registry", o.Now, &page); err != nil {
			return err
		}
		next := page.Metadata.NextCursor
		if next != "" && next == cursor {
			return errors.New("the MCP Registry returned the same page twice")
		}
		apply(&page, next)
		if next == "" {
			return nil
		}
		if n >= maxPages {
			return fmt.Errorf("the MCP Registry listing is longer than %d pages", maxPages)
		}
		if o.PersistEvery > 0 && n%o.PersistEvery == 0 {
			o.persist()
		}
		cursor = next
		if o.PageDelay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(o.PageDelay):
			}
		}
	}
}

// crawl lists the latest version of every server, resuming from the saved cursor.
func (o *Official) crawl(ctx context.Context) error {
	o.mu.Lock()
	if o.state.CrawlStarted.IsZero() {
		o.state.CrawlStarted = o.Now().UTC()
	}
	cursor := o.state.Cursor
	o.mu.Unlock()
	q := url.Values{"limit": {"100"}, "version": {"latest"}}
	err := o.pages(ctx, q, cursor, func(page *ListResponse, next string) {
		o.mu.Lock()
		defer o.mu.Unlock()
		for _, r := range page.Servers {
			if r.Server.Name == "" {
				continue
			}
			if r.Official().Status == "deleted" {
				delete(o.state.Entries, r.Server.Name)
				continue
			}
			o.state.Entries[r.Server.Name] = EntryFromServer(r)
		}
		o.state.Cursor, o.dirty = next, true
	})
	if err != nil {
		return err
	}
	o.mu.Lock()
	o.state.Complete, o.state.Cursor = true, ""
	o.state.SyncedAt, o.state.CheckedAt = o.state.CrawlStarted, o.Now().UTC()
	o.mu.Unlock()
	return nil
}

// incremental applies every record changed since the last pass: without a version filter the
// registry returns old versions and deleted records too.
func (o *Official) incremental(ctx context.Context) error {
	start := o.Now().UTC()
	o.mu.Lock()
	since := o.state.SyncedAt.UTC().Format(time.RFC3339Nano)
	o.mu.Unlock()
	q := url.Values{"limit": {"100"}, "updated_since": {since}}
	err := o.pages(ctx, q, "", func(page *ListResponse, _ string) {
		o.mu.Lock()
		defer o.mu.Unlock()
		for _, r := range page.Servers {
			name := r.Server.Name
			if name == "" {
				continue
			}
			meta := r.Official()
			e, ok := o.state.Entries[name]
			switch {
			case meta.Status == "deleted":
				if ok && (e.Version == r.Server.Version || meta.IsLatest) {
					delete(o.state.Entries, name)
				}
			case meta.IsLatest:
				o.state.Entries[name] = EntryFromServer(r)
			case ok && e.Version == r.Server.Version && meta.Status != "":
				e.Status = meta.Status
				o.state.Entries[name] = e
			}
		}
		o.dirty = true
		clear(o.details)
	})
	if err != nil {
		return err
	}
	o.mu.Lock()
	// A minute's overlap covers clock skew between here and the registry.
	o.state.SyncedAt, o.state.CheckedAt = start.Add(-time.Minute), o.Now().UTC()
	o.mu.Unlock()
	return nil
}

// Detail fetches one server's full record for the version the index holds ("latest" when it
// holds none), and keeps it in memory.
func (o *Official) Detail(ctx context.Context, name string) (*ServerResponse, error) {
	o.ensure()
	o.mu.Lock()
	version := "latest"
	if e, ok := o.state.Entries[name]; ok && e.Version != "" {
		version = e.Version
	}
	key := name + "@" + version
	if d, ok := o.details[key]; ok {
		o.mu.Unlock()
		return d, nil
	}
	o.mu.Unlock()

	// PathEscape keeps the name's "/" as %2F, which the registry expects.
	u := strings.TrimRight(o.Base, "/") + "/v0.1/servers/" + url.PathEscape(name) + "/versions/" + url.PathEscape(version)
	var r ServerResponse
	if err := getJSON(ctx, o.HTTP, u, "the MCP Registry", o.Now, &r); err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if r.Server.Name == "" || r.Official().Status == "deleted" {
		return nil, ErrNotFound
	}
	o.mu.Lock()
	if o.details == nil || len(o.details) >= maxDetails {
		o.details = map[string]*ServerResponse{}
	}
	o.details[key] = &r
	o.mu.Unlock()
	return &r, nil
}
