// Package skillssh reads the skills.sh directory (fork-only, endgame302/skillshare): its search
// API and the All time, Trending and Hot leaderboards, which exist only as web pages.
//
// The pages are Next.js output. ParseBoard tries, in order, the legacy __NEXT_DATA__ script,
// the React Server Components flight payload (`self.__next_f.push([1,"..."])`), and finally any
// JSON object on the page that has a source and a skill id. When none of them yields a skill it
// returns ErrFormatChanged instead of an empty list, so callers can say the page changed.
package skillssh

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Skill is one skills.sh entry.
type Skill struct {
	// ID is "<source>/<skillId>", unique on skills.sh.
	ID string `json:"id"`
	// Source is a GitHub "owner/repo", or a domain for skills published through a well-known URL.
	Source  string `json:"source"`
	SkillID string `json:"skillId"`
	Name    string `json:"name"`
	// Installs is the count the list ranks by: all-time installs, this period's for Trending and Hot.
	Installs int64 `json:"installs"`
	// URL is the skill's page on skills.sh.
	URL string `json:"url"`
	// Installable reports whether Source is a GitHub owner/repo that skillshare can install from.
	Installable bool `json:"installable"`
}

// ErrFormatChanged means a page or response held none of the shapes this package knows.
var ErrFormatChanged = errors.New("skills.sh format changed: no skills found")

const siteURL = "https://skills.sh"

var githubRepo = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?/[A-Za-z0-9_.-]+$`)

// IsGitHubRepo reports whether source is a GitHub "owner/repo".
func IsGitHubRepo(source string) bool { return githubRepo.MatchString(source) }

type rawSkill struct {
	Source   string          `json:"source"`
	SkillID  string          `json:"skillId"`
	SkillID2 string          `json:"skill_id"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Installs json.RawMessage `json:"installs"`
}

func (r rawSkill) skill() (Skill, bool) {
	source := strings.TrimSpace(r.Source)
	id := strings.TrimSpace(r.SkillID)
	if id == "" {
		id = strings.TrimSpace(r.SkillID2)
	}
	// Search results carry "id" as "<source>/<skillId>"; older payloads used it as the skill id.
	if id == "" && r.ID != "" {
		id = strings.TrimPrefix(r.ID, source+"/")
	}
	if source == "" || id == "" || strings.ContainsAny(source+id, "\n\r\t ") {
		return Skill{}, false
	}
	name := strings.TrimSpace(r.Name)
	if name == "" {
		name = id
	}
	return Skill{
		ID:          source + "/" + id,
		Source:      source,
		SkillID:     id,
		Name:        name,
		Installs:    count(r.Installs),
		URL:         siteURL + "/" + escapePath(source) + "/" + url.PathEscape(id),
		Installable: githubRepo.MatchString(source),
	}, true
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// count reads a JSON number, or a numeric string, as a non-negative integer.
func count(raw json.RawMessage) int64 {
	var f float64
	if json.Unmarshal(raw, &f) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil || json.Unmarshal([]byte(s), &f) != nil {
			return 0
		}
	}
	if f < 0 {
		return 0
	}
	return int64(f)
}

// collect converts raw entries, dropping incomplete ones and repeats.
func collect(raws []rawSkill) []Skill {
	seen := map[string]bool{}
	out := make([]Skill, 0, len(raws))
	for _, r := range raws {
		s, ok := r.skill()
		if !ok || seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	return out
}

// ParseBoard reads a leaderboard page (https://skills.sh/, /trending or /hot).
func ParseBoard(html []byte) ([]Skill, error) {
	if skills := fromNextData(html); len(skills) > 0 {
		return skills, nil
	}
	flight := flightPayload(html)
	if skills := fromKey(flight, "initialSkills"); len(skills) > 0 {
		return skills, nil
	}
	if skills := embeddedObjects(flight); len(skills) > 0 {
		return skills, nil
	}
	if skills := embeddedObjects(html); len(skills) > 0 {
		return skills, nil
	}
	// Objects inside a JavaScript string the flight reader missed keep their quotes escaped.
	if skills := embeddedObjects(bytes.ReplaceAll(html, []byte(`\"`), []byte(`"`))); len(skills) > 0 {
		return skills, nil
	}
	return nil, ErrFormatChanged
}

// ParseSearch reads a /api/search response: {"skills": [...]} or a bare array.
func ParseSearch(body []byte) ([]Skill, error) {
	var wrapped struct {
		Skills *[]rawSkill `json:"skills"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Skills != nil {
		return collect(*wrapped.Skills), nil
	}
	var bare []rawSkill
	if err := json.Unmarshal(body, &bare); err == nil {
		return collect(bare), nil
	}
	return nil, ErrFormatChanged
}

var nextDataScript = regexp.MustCompile(`(?s)<script[^>]*\bid="__NEXT_DATA__"[^>]*>(.*?)</script>`)

// fromNextData reads the pages-router payload skills.sh used before the app router.
func fromNextData(html []byte) []Skill {
	m := nextDataScript.FindSubmatch(html)
	if m == nil {
		return nil
	}
	var data struct {
		Props struct {
			PageProps map[string]json.RawMessage `json:"pageProps"`
		} `json:"props"`
	}
	if json.Unmarshal(m[1], &data) != nil {
		return nil
	}
	for _, key := range []string{"initialSkills", "skills", "items"} {
		var raws []rawSkill
		if raw, ok := data.Props.PageProps[key]; ok && json.Unmarshal(raw, &raws) == nil {
			if skills := collect(raws); len(skills) > 0 {
				return skills
			}
		}
	}
	return nil
}

var flightPush = []byte(`self.__next_f.push([1,`)

// flightPayload joins the string chunks of the React Server Components payload. Each chunk is a
// JavaScript string literal written as JSON, so encoding/json decodes it.
func flightPayload(html []byte) []byte {
	var out bytes.Buffer
	rest := html
	for {
		i := bytes.Index(rest, flightPush)
		if i < 0 {
			break
		}
		rest = rest[i+len(flightPush):]
		var chunk string
		dec := json.NewDecoder(bytes.NewReader(rest))
		if dec.Decode(&chunk) != nil {
			continue
		}
		out.WriteString(chunk)
		rest = rest[dec.InputOffset():]
	}
	return out.Bytes()
}

// fromKey decodes the array after the first `"<key>":` that holds skills. In a flight payload the
// value can also be a reference ("$1f") to the row that holds it.
func fromKey(text []byte, key string) []Skill {
	marker := []byte(`"` + key + `":`)
	rest := text
	for {
		i := bytes.Index(rest, marker)
		if i < 0 {
			return nil
		}
		rest = rest[i+len(marker):]
		var raws []rawSkill
		if decodeAt(rest, &raws) {
			if skills := collect(raws); len(skills) > 0 {
				return skills
			}
			continue
		}
		var ref string
		if decodeAt(rest, &ref) && strings.HasPrefix(ref, "$") {
			if row := flightRow(text, strings.TrimPrefix(ref, "$")); row != nil && decodeAt(row, &raws) {
				if skills := collect(raws); len(skills) > 0 {
					return skills
				}
			}
		}
	}
}

// flightRow returns the text after the row header "<id>:" at the start of a line.
func flightRow(text []byte, id string) []byte {
	header := []byte(id + ":")
	if bytes.HasPrefix(text, header) {
		return text[len(header):]
	}
	if i := bytes.Index(text, append([]byte("\n"), header...)); i >= 0 {
		return text[i+1+len(header):]
	}
	return nil
}

func decodeAt(b []byte, v any) bool {
	return json.NewDecoder(bytes.NewReader(b)).Decode(v) == nil
}

// maxObjectLookback bounds how far back from a skill id the object's opening brace may be.
const maxObjectLookback = 4096

// embeddedObjects finds every JSON object that names a source and a skill id, in any key order,
// by decoding from the nearest opening brace before each skill id key.
func embeddedObjects(text []byte) []Skill {
	var raws []rawSkill
	for _, key := range [][]byte{[]byte(`"skillId":`), []byte(`"skill_id":`)} {
		rest, base := text, 0
		for {
			i := bytes.Index(rest, key)
			if i < 0 {
				break
			}
			at := base + i
			from := max(0, at-maxObjectLookback)
			if open := bytes.LastIndexByte(text[from:at], '{'); open >= 0 {
				var r rawSkill
				if decodeAt(text[from+open:], &r) {
					raws = append(raws, r)
				}
			}
			rest, base = rest[i+len(key):], at+len(key)
		}
	}
	return collect(raws)
}
