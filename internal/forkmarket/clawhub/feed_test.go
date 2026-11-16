package clawhub

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseFeed_Sample(t *testing.T) {
	feed, err := ParseFeed(readTestdata(t, "feed-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Entries) != 5 {
		t.Fatalf("entries = %d, want 5", len(feed.Entries))
	}
	if want := time.Date(2026, 11, 22, 0, 42, 18, 813e6, time.UTC); !feed.Expiry().Equal(want) {
		t.Errorf("Expiry = %v, want %v", feed.Expiry(), want)
	}

	items := map[string]Item{}
	for _, it := range feed.Items() {
		items[it.ID] = it
	}
	bw := items["@steipete/blogwatcher"]
	if !bw.Installable || bw.Owner != "steipete" || bw.Slug != "blogwatcher" || bw.Version != "1.0.0" ||
		bw.Trust != "official" || bw.Source != "clawhub" || bw.Integrity != blogwatcherIntegrity || bw.Featured {
		t.Errorf("blogwatcher item = %+v", bw)
	}
	if !items["@steipete/gog"].Featured || !items["@steipete/qmd"].Featured {
		t.Error("gog and qmd are featured in the sample")
	}
	gh := items["@aws/agents-build"]
	const wantCommit = "b4416dddac9b6a0cc5412136e6dbf8f07ffdb31c"
	const wantIntegrity = "sha256:d79709bf46ca8cd0fffe392a914cd56539b4b7c0ff0fe34c9583f24057300195"
	if !gh.Installable || gh.Blocked != "" || gh.Source != "github" || gh.GitHub == nil ||
		gh.GitHub.Repo != "aws/agent-toolkit-for-aws" || gh.Integrity != wantIntegrity || gh.Version != wantCommit {
		t.Errorf("GitHub-backed item = %+v", gh)
	}
	if ali := items["@alipay/alipay-aipay"]; ali.Title != "支付宝 AI 付接入" || !ali.Installable {
		t.Errorf("non-ASCII item = %+v", ali)
	}
}

func TestParseFeed_KeepsOnlyValidSkillEntries(t *testing.T) {
	feed, err := ParseFeed([]byte(`{"schemaVersion":1,"entries":[
		{"type":"plugin","id":"@a/plugin"},
		{"type":"skill","id":"@a/../b"},
		{"type":"skill","id":"@a/b/c"},
		{"type":"skill","id":"a/b"},
		{"type":"skill","id":"@a/ok"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Entries) != 1 || feed.Entries[0].ID != "@a/ok" {
		t.Fatalf("entries = %+v", feed.Entries)
	}
	for _, bad := range []string{`{"schemaVersion":2,"entries":[]}`, `{"entries":[]}`, `not json`} {
		if _, err := ParseFeed([]byte(bad)); err == nil {
			t.Errorf("ParseFeed(%s) accepted", bad)
		}
	}
}

func TestParseID(t *testing.T) {
	for id, want := range map[string][2]string{
		"@steipete/blogwatcher": {"steipete", "blogwatcher"},
		"@ant-intl/antom.v2_x":  {"ant-intl", "antom.v2_x"},
	} {
		owner, slug, ok := ParseID(id)
		if !ok || owner != want[0] || slug != want[1] {
			t.Errorf("ParseID(%q) = %q, %q, %v", id, owner, slug, ok)
		}
	}
	bad := []string{"", "steipete/x", "@/x", "@a/", "@a/b/c", "@a/..", "@a/x..y", "@../x", "@a/.x", "@a/x y", `@a/x\y`, "@a/" + strings.Repeat("x", 201)}
	if runtime.GOOS == "windows" {
		bad = append(bad, "@a/con", "@a/NUL")
	}
	for _, id := range bad {
		if _, _, ok := ParseID(id); ok {
			t.Errorf("ParseID(%q) accepted", id)
		}
	}
}

func TestInstallCandidate(t *testing.T) {
	entry := func(state, sourceRef, pkg, integrity string) Entry {
		e := Entry{Type: "skill", ID: "@a/b", Version: "1.0.0", State: state}
		e.Install.Candidates = []Candidate{{SourceRef: sourceRef, Package: pkg, Version: "1.0.0", Integrity: integrity}}
		return e
	}
	good := "sha256:" + strings.Repeat("A", 64)
	if c, blocked := entry("available", "public-clawhub", "@a/b", good).InstallCandidate(); blocked != "" || c.Version != "1.0.0" {
		t.Errorf("hosted: %+v, %q", c, blocked)
	}

	commit := strings.Repeat("a", 40)
	hashHex := strings.Repeat("a", 64)
	ghEntry := func(mut func(*Candidate)) Entry {
		c := Candidate{
			SourceRef: "public-github",
			Package:   "@a/b",
			Version:   commit,
			Integrity: good,
			GitHub: &GitHubSource{
				Repo:        "owner/repo",
				Path:        "skills/b",
				Commit:      commit,
				ContentHash: hashHex,
			},
		}
		if mut != nil {
			mut(&c)
		}
		e := Entry{Type: "skill", ID: "@a/b", Version: commit, State: "available"}
		e.Install.Candidates = []Candidate{c}
		return e
	}
	if c, blocked := ghEntry(nil).InstallCandidate(); blocked != "" || c.Version != commit {
		t.Errorf("github: %+v, %q", c, blocked)
	}

	for name, tc := range map[string]struct {
		e    Entry
		want string
	}{
		"no integrity":   {entry("available", "public-clawhub", "@a/b", ""), BlockedIntegrity},
		"base64":         {entry("available", "public-clawhub", "@a/b", "sha256-abc="), BlockedIntegrity},
		"unavailable":    {entry("deprecated", "public-clawhub", "@a/b", good), BlockedUnavailable},
		"other package":  {entry("available", "public-clawhub", "@a/c", good), BlockedUnsupported},
		"unknown source": {entry("available", "private", "@a/b", good), BlockedUnsupported},
		"no candidates":  {Entry{ID: "@a/b", State: "available"}, BlockedUnsupported},
		"unsafe version": {func() Entry {
			e := entry("available", "public-clawhub", "@a/b", good)
			e.Install.Candidates[0].Version = "../1"
			return e
		}(), BlockedUnsupported},
		"github bad commit":         {ghEntry(func(c *Candidate) { c.GitHub.Commit = "main" }), BlockedUnsupported},
		"github version mismatch":   {ghEntry(func(c *Candidate) { c.Version = strings.Repeat("b", 40) }), BlockedUnsupported},
		"github hash mismatch":      {ghEntry(func(c *Candidate) { c.GitHub.ContentHash = strings.Repeat("b", 64) }), BlockedUnsupported},
		"github traversal path":     {ghEntry(func(c *Candidate) { c.GitHub.Path = "skills/../secret" }), BlockedUnsupported},
		"github missing object":     {ghEntry(func(c *Candidate) { c.GitHub = nil }), BlockedUnsupported},
		"github three-segment repo": {ghEntry(func(c *Candidate) { c.GitHub.Repo = "owner/repo/extra" }), BlockedUnsupported},
	} {
		if _, blocked := tc.e.InstallCandidate(); blocked != tc.want {
			t.Errorf("%s: blocked = %q, want %q", name, blocked, tc.want)
		}
	}
	if d, ok := ParseIntegrity(good); !ok || d != strings.Repeat("a", 64) {
		t.Errorf("ParseIntegrity lower-cases the hex: %q, %v", d, ok)
	}
}

func TestOrigin(t *testing.T) {
	if got := Origin("@steipete/blogwatcher", "1.0.0"); got != "clawhub://@steipete/blogwatcher@1.0.0" {
		t.Fatalf("Origin = %q", got)
	}
	for v, ok := range map[string]bool{"1.0.0": true, "1.0.11-beta.1": true, "2.0.0+build.5": true, "": false, "../1": false, "1/2": false, ".1": false, "1 0": false} {
		if validVersion(v) != ok {
			t.Errorf("validVersion(%q) = %v", v, !ok)
		}
	}
}
