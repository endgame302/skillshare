package search

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"skillshare/internal/config"
)

const (
	searchCacheVersion    = 1
	metadataCacheFile     = "metadata.json"
	starsCacheFile        = "stars.json"
	queriesCacheFile      = "queries.json"
	maxMetadataCacheItems = 5000
	maxQueriesCacheItems  = 50
	starsCacheTTL         = 24 * time.Hour
	queriesCacheTTL       = 10 * time.Minute
)

var (
	searchCacheDir = func() string { return filepath.Join(config.CacheDir(), "search") }
	timeNow        = time.Now
	searchCacheMu  sync.Mutex
)

type metadataCacheEntry struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Valid       bool      `json:"valid"`
	StoredAt    time.Time `json:"storedAt"`
}

type metadataCacheFileData struct {
	Version int                           `json:"version"`
	Entries map[string]metadataCacheEntry `json:"entries"`
}

type starsCacheEntry struct {
	Stars    int       `json:"stars"`
	StoredAt time.Time `json:"storedAt"`
}

type starsCacheFileData struct {
	Version int                        `json:"version"`
	Entries map[string]starsCacheEntry `json:"entries"`
}

type cachedSearchResult struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Source      string   `json:"source"`
	Skill       string   `json:"skill"`
	Stars       int      `json:"stars"`
	Owner       string   `json:"owner"`
	Repo        string   `json:"repo"`
	Path        string   `json:"path"`
	Tags        []string `json:"tags,omitempty"`
	RiskScore   *int     `json:"riskScore,omitempty"`
	RiskLabel   string   `json:"riskLabel,omitempty"`
	Score       float64  `json:"score"`
}

type queriesCacheEntry struct {
	Results  []cachedSearchResult `json:"results"`
	StoredAt time.Time            `json:"storedAt"`
}

type queriesCacheFileData struct {
	Version int                          `json:"version"`
	Entries map[string]queriesCacheEntry `json:"entries"`
}

func readSearchCacheBytes(name string) ([]byte, bool) {
	dir := searchCacheDir()
	if dir == "" {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, false
	}
	return data, true
}

// writeSearchCacheFile atomically writes v as JSON inside searchCacheDir.
func writeSearchCacheFile(name string, v any) error {
	dir := searchCacheDir()
	if dir == "" {
		return fmt.Errorf("empty search cache dir")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, name+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func readMetadataCacheFile() metadataCacheFileData {
	var file metadataCacheFileData
	if data, ok := readSearchCacheBytes(metadataCacheFile); ok {
		if err := json.Unmarshal(data, &file); err != nil || file.Version != searchCacheVersion {
			file = metadataCacheFileData{}
		}
	}
	file.Version = searchCacheVersion
	if file.Entries == nil {
		file.Entries = make(map[string]metadataCacheEntry)
	}
	return file
}

func loadMetadataCache(shas []string) map[string]skillMetadata {
	if len(shas) == 0 {
		return nil
	}
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()

	file := readMetadataCacheFile()
	if len(file.Entries) == 0 {
		return nil
	}
	out := make(map[string]skillMetadata, len(shas))
	for _, sha := range shas {
		if sha == "" {
			continue
		}
		if e, ok := file.Entries[sha]; ok {
			out[sha] = skillMetadata{
				Name:        e.Name,
				Description: e.Description,
				Valid:       e.Valid,
			}
		}
	}
	return out
}

func saveMetadataCache(updates map[string]skillMetadata) {
	if len(updates) == 0 {
		return
	}
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()

	file := readMetadataCacheFile()
	now := timeNow()
	for sha, meta := range updates {
		if sha == "" {
			continue
		}
		file.Entries[sha] = metadataCacheEntry{
			Name:        meta.Name,
			Description: meta.Description,
			Valid:       meta.Valid,
			StoredAt:    now,
		}
	}

	if len(file.Entries) > maxMetadataCacheItems {
		type keyTime struct {
			key string
			t   time.Time
		}
		items := make([]keyTime, 0, len(file.Entries))
		for k, e := range file.Entries {
			items = append(items, keyTime{key: k, t: e.StoredAt})
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i].t.Before(items[j].t)
		})
		drop := len(file.Entries) - maxMetadataCacheItems
		for i := 0; i < drop; i++ {
			delete(file.Entries, items[i].key)
		}
	}

	_ = writeSearchCacheFile(metadataCacheFile, file)
}

func readStarsCacheFile() starsCacheFileData {
	var file starsCacheFileData
	if data, ok := readSearchCacheBytes(starsCacheFile); ok {
		if err := json.Unmarshal(data, &file); err != nil || file.Version != searchCacheVersion {
			file = starsCacheFileData{}
		}
	}
	file.Version = searchCacheVersion
	if file.Entries == nil {
		file.Entries = make(map[string]starsCacheEntry)
	}
	return file
}

func repoCacheKey(id repoID) string {
	return id.owner + "/" + id.repo
}

func loadStarsCache(repos []repoID) map[repoID]int {
	if len(repos) == 0 {
		return nil
	}
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()

	file := readStarsCacheFile()
	if len(file.Entries) == 0 {
		return nil
	}
	now := timeNow()
	out := make(map[repoID]int, len(repos))
	for _, id := range repos {
		if e, ok := file.Entries[repoCacheKey(id)]; ok {
			age := now.Sub(e.StoredAt)
			if age >= 0 && age < starsCacheTTL {
				out[id] = e.Stars
			}
		}
	}
	return out
}

func saveStarsCache(updates map[repoID]int) {
	if len(updates) == 0 {
		return
	}
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()

	file := readStarsCacheFile()
	now := timeNow()
	for k, e := range file.Entries {
		age := now.Sub(e.StoredAt)
		if age < 0 || age >= starsCacheTTL {
			delete(file.Entries, k)
		}
	}
	for id, stars := range updates {
		file.Entries[repoCacheKey(id)] = starsCacheEntry{
			Stars:    stars,
			StoredAt: now,
		}
	}

	_ = writeSearchCacheFile(starsCacheFile, file)
}

func readQueriesCacheFile() queriesCacheFileData {
	var file queriesCacheFileData
	if data, ok := readSearchCacheBytes(queriesCacheFile); ok {
		if err := json.Unmarshal(data, &file); err != nil || file.Version != searchCacheVersion {
			file = queriesCacheFileData{}
		}
	}
	file.Version = searchCacheVersion
	if file.Entries == nil {
		file.Entries = make(map[string]queriesCacheEntry)
	}
	return file
}

func loadQueryCache(query string) ([]SearchResult, bool) {
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()

	file := readQueriesCacheFile()
	entry, ok := file.Entries[query]
	if !ok {
		return nil, false
	}
	now := timeNow()
	age := now.Sub(entry.StoredAt)
	if age < 0 || age >= queriesCacheTTL {
		return nil, false
	}
	return fromCachedSearchResults(entry.Results), true
}

func saveQueryCache(query string, results []SearchResult) {
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()

	file := readQueriesCacheFile()
	now := timeNow()
	for k, e := range file.Entries {
		age := now.Sub(e.StoredAt)
		if age < 0 || age >= queriesCacheTTL {
			delete(file.Entries, k)
		}
	}

	file.Entries[query] = queriesCacheEntry{
		Results:  toCachedSearchResults(results),
		StoredAt: now,
	}

	if len(file.Entries) > maxQueriesCacheItems {
		type keyTime struct {
			key string
			t   time.Time
		}
		items := make([]keyTime, 0, len(file.Entries))
		for k, e := range file.Entries {
			items = append(items, keyTime{key: k, t: e.StoredAt})
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i].t.Before(items[j].t)
		})
		drop := len(file.Entries) - maxQueriesCacheItems
		for i := 0; i < drop; i++ {
			delete(file.Entries, items[i].key)
		}
	}

	_ = writeSearchCacheFile(queriesCacheFile, file)
}

func toCachedSearchResults(results []SearchResult) []cachedSearchResult {
	out := make([]cachedSearchResult, len(results))
	for i, r := range results {
		var tags []string
		if len(r.Tags) > 0 {
			tags = append([]string(nil), r.Tags...)
		}
		var riskScore *int
		if r.RiskScore != nil {
			v := *r.RiskScore
			riskScore = &v
		}
		out[i] = cachedSearchResult{
			Name:        r.Name,
			Description: r.Description,
			Source:      r.Source,
			Skill:       r.Skill,
			Stars:       r.Stars,
			Owner:       r.Owner,
			Repo:        r.Repo,
			Path:        r.Path,
			Tags:        tags,
			RiskScore:   riskScore,
			RiskLabel:   r.RiskLabel,
			Score:       r.Score,
		}
	}
	return out
}

func fromCachedSearchResults(cached []cachedSearchResult) []SearchResult {
	out := make([]SearchResult, len(cached))
	for i, c := range cached {
		var tags []string
		if len(c.Tags) > 0 {
			tags = append([]string(nil), c.Tags...)
		}
		var riskScore *int
		if c.RiskScore != nil {
			v := *c.RiskScore
			riskScore = &v
		}
		out[i] = SearchResult{
			Name:        c.Name,
			Description: c.Description,
			Source:      c.Source,
			Skill:       c.Skill,
			Stars:       c.Stars,
			Owner:       c.Owner,
			Repo:        c.Repo,
			Path:        c.Path,
			Tags:        tags,
			RiskScore:   riskScore,
			RiskLabel:   c.RiskLabel,
			Score:       c.Score,
		}
	}
	return out
}
