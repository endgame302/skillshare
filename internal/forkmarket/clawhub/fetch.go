package clawhub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// NotInstallableError is an entry with no ClawHub-hosted release to install; Reason is one of
// the Blocked* values.
type NotInstallableError struct{ Reason string }

func (e *NotInstallableError) Error() string {
	switch e.Reason {
	case BlockedGitHub:
		return "this skill is hosted on GitHub; installing it from the ClawHub list is not supported yet"
	case BlockedIntegrity:
		return ErrNoIntegrity.Error()
	case BlockedUnavailable:
		return "ClawHub lists this skill as unavailable"
	default:
		return "ClawHub offers no release of this skill that can be installed here"
	}
}

// A version becomes part of a folder name.
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

func validVersion(v string) bool {
	return versionPattern.MatchString(v) && filepath.IsLocal("x@"+v)
}

// Release is a hosted release, verified and unpacked next to its folder in the release store.
// Place moves it into that folder; Discard drops it if it was not placed.
type Release struct {
	// Dir is <StoreDir>/releases/@owner/slug@version. The installed skill records it as its
	// source, so update and a reinstall from the records copy it again like any local folder,
	// and the path itself names the ClawHub origin.
	Dir     string
	Slug    string
	Version string
	Origin  string // clawhub://@owner/slug@version
	// Fresh is set by Place when it created Dir rather than replacing an earlier copy of the
	// same release, which an installed skill may still name as its source.
	Fresh   bool
	staging string
}

// Place moves the unpacked release into Dir, replacing an earlier copy of the same release, so
// what is installed is what was just verified. Callers serialize Place with installs that read
// Dir.
func (r *Release) Place() error {
	_, statErr := os.Stat(r.Dir)
	r.Fresh = errors.Is(statErr, os.ErrNotExist)
	if !r.Fresh {
		if err := os.RemoveAll(r.Dir); err != nil {
			return err
		}
	}
	if err := os.Rename(r.staging, r.Dir); err != nil {
		return err
	}
	r.staging = ""
	return nil
}

// Discard removes the unpacked release if Place did not move it.
func (r *Release) Discard() {
	if r.staging != "" {
		os.RemoveAll(r.staging)
		r.staging = ""
	}
}

// ReleaseDir is where a release is kept.
func (c *Client) ReleaseDir(owner, slug, version string) string {
	return filepath.Join(c.StoreDir, "releases", "@"+owner, slug+"@"+version)
}

// Fetch downloads the hosted release of e, verifies it against the feed's integrity, checks its
// entries, and unpacks it into a staging folder beside its release folder. Nothing is written
// unless every check passes.
func (c *Client) Fetch(ctx context.Context, snap *Snapshot, e Entry) (*Release, error) {
	cand, blocked := e.HostedCandidate()
	if blocked != "" {
		return nil, &NotInstallableError{Reason: blocked}
	}
	owner, slug, _ := ParseID(e.ID)
	data, err := c.Download(ctx, snap.APIBase, owner, slug, cand.Version)
	if err != nil {
		return nil, err
	}
	files, err := Verify(data, cand.Integrity)
	if err != nil {
		return nil, err
	}
	if err := Check(files); err != nil {
		return nil, err
	}

	final := c.ReleaseDir(owner, slug, cand.Version)
	parent := filepath.Dir(final)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	pruneStaging(parent, c.Now())
	staging, err := os.MkdirTemp(parent, ".staging-*")
	if err != nil {
		return nil, err
	}
	if err := extract(files, staging); err != nil {
		os.RemoveAll(staging)
		return nil, err
	}
	return &Release{Dir: final, Slug: slug, Version: cand.Version, Origin: Origin(e.ID, cand.Version), staging: staging}, nil
}

// pruneStaging removes staging folders an earlier run left behind, if it stopped before
// cleaning up.
func pruneStaging(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, d := range entries {
		info, err := d.Info()
		if err != nil || !d.IsDir() || now.Sub(info.ModTime()) < 24*time.Hour {
			continue
		}
		if matched, _ := filepath.Match(".staging-*", d.Name()); matched {
			os.RemoveAll(filepath.Join(dir, d.Name()))
		}
	}
}

// IsIntegrityError reports whether err means the archive or its hash cannot be trusted.
func IsIntegrityError(err error) bool {
	return errors.Is(err, ErrIntegrityMismatch) || errors.Is(err, ErrNoIntegrity)
}
