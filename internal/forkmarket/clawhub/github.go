package clawhub

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	ghclient "skillshare/internal/github"
)

const (
	maxTreeBytes  = 8 << 20
	githubTimeout = 2 * time.Minute
	rawSlack      = 64 << 10
)

var hex40Pattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// FolderFile is one file of a GitHub-hosted folder: its path relative to the folder, and bytes.
type FolderFile struct {
	Path string
	Data []byte
}

// FolderHash is ClawHub's content hash of a GitHub-hosted folder (hex, lower case).
func FolderHash(files []FolderFile) string {
	sorted := make([]FolderFile, len(files))
	copy(sorted, files)
	// ClawHub sorts paths with JS localeCompare (ICU root collation), not byte order.
	col := collate.New(language.Und)
	sort.SliceStable(sorted, func(i, j int) bool {
		return col.CompareString(sorted[i].Path, sorted[j].Path) < 0
	})
	lines := make([]string, len(sorted))
	for i, f := range sorted {
		sum := sha256.Sum256(f.Data)
		lines[i] = f.Path + "\x00" + strconv.Itoa(len(f.Data)) + "\x00" + hex.EncodeToString(sum[:])
	}
	total := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(total[:])
}

// GitHubFolder is a GitHub-hosted entry's folder at its pinned commit, checked against the feed.
type GitHubFolder struct {
	Repo   string // owner/name
	Path   string // the folder in the repo
	Commit string // the full commit the feed pins
	Tree   string // git tree id of Commit:Path, as GitHub listed it
	Files  int    // files that were hashed
}

// githubAPIHeaders are the headers for GitHub's REST API. The user's GitHub token (the one the
// rest of skillshare uses: GITHUB_TOKEN, GH_TOKEN or `gh auth token`) is added only when the
// request goes to api.github.com itself, so it never reaches another host.
func githubAPIHeaders(apiBase string) map[string]string {
	h := map[string]string{"X-GitHub-Api-Version": "2022-11-28"}
	if strings.TrimRight(apiBase, "/") == DefaultGitHubAPI {
		if token := ghclient.GetToken(); token != "" {
			h["Authorization"] = "Bearer " + token
		}
	}
	return h
}

type treeBlob struct {
	path string
	sha  string
	size int64
}

// VerifyGitHub downloads the folder of a GitHub candidate at its pinned commit, checks every entry
// like an archive's (Check), each file against its git blob id, and the whole folder against the
// feed's content hash. It writes nothing.
func (c *Client) VerifyGitHub(ctx context.Context, cand Candidate) (*GitHubFolder, error) {
	if cand.GitHub == nil {
		return nil, &NotInstallableError{Reason: BlockedUnsupported}
	}
	gh := cand.GitHub

	treeURL := strings.TrimRight(c.GitHubAPI, "/") + "/repos/" + gh.Repo + "/git/trees/" + gh.Commit + ":" + url.PathEscape(gh.Path) + "?recursive=1"
	treeCtx, cancelTree := context.WithTimeout(ctx, feedTimeout)
	defer cancelTree()
	resp, body, err := c.getWithHeaders(treeCtx, treeURL, "application/vnd.github+json", githubAPIHeaders(c.GitHubAPI), maxTreeBytes)
	if err != nil {
		return nil, &DownloadError{Msg: "reading the folder from GitHub failed: " + err.Error()}
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusUnprocessableEntity:
		return nil, &DownloadError{resp.StatusCode, "GitHub no longer has this folder at the pinned commit"}
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, &DownloadError{resp.StatusCode, "GitHub is rate limiting requests; try again later, or sign in with gh or set GITHUB_TOKEN for a higher limit"}
	default:
		return nil, &DownloadError{resp.StatusCode, fmt.Sprintf("GitHub answered HTTP %d", resp.StatusCode)}
	}

	var listing struct {
		SHA       string `json:"sha"`
		Truncated bool   `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
			Size int64  `json:"size"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(body, &listing); err != nil || !hex40Pattern.MatchString(listing.SHA) {
		return nil, &DownloadError{Msg: "GitHub sent an unexpected folder listing"}
	}
	if listing.Truncated {
		return nil, unsafeArchive("GitHub listed the folder only in part")
	}

	files := make([]File, 0, len(listing.Tree))
	blobs := make([]treeBlob, 0, len(listing.Tree))
	for _, entry := range listing.Tree {
		switch {
		case entry.Type == "tree":
			files = append(files, File{Name: entry.Path + "/", Dir: true, mode: fs.ModeDir | 0o755, meth: zip.Store})
		case entry.Type == "commit":
			return nil, unsafeArchive("entry %q is a submodule", entry.Path)
		case entry.Mode == "120000":
			return nil, unsafeArchive("entry %q is a symbolic link", entry.Path)
		case entry.Type == "blob" && (entry.Mode == "100644" || entry.Mode == "100755"):
			if entry.Size < 0 {
				return nil, unsafeArchive("entry %q has a negative size", entry.Path)
			}
			if entry.Size > MaxExtractBytes {
				return nil, unsafeArchive("%q is too large", entry.Path)
			}
			files = append(files, File{Name: entry.Path, mode: 0o644, meth: zip.Store, usize: uint64(entry.Size)})
			blobs = append(blobs, treeBlob{path: entry.Path, sha: entry.SHA, size: entry.Size})
		default:
			return nil, unsafeArchive("entry %q is not a plain file or folder", entry.Path)
		}
	}
	if err := Check(files); err != nil {
		return nil, err
	}

	dlCtx, cancelDL := context.WithTimeout(ctx, githubTimeout)
	defer cancelDL()
	rawBase := strings.TrimRight(c.GitHubRaw, "/") + "/" + gh.Repo + "/" + gh.Commit + "/" + escapePathSegments(gh.Path)
	folderFiles := make([]FolderFile, 0, len(blobs))
	for _, b := range blobs {
		rawURL := rawBase + "/" + escapePathSegments(b.path)
		// Read a little past the listed size: an error page or a file that differs from the
		// listing must reach the checks below rather than fail as an oversized download.
		resp, data, err := c.get(dlCtx, rawURL, "*/*", b.size+rawSlack)
		if err != nil {
			return nil, &DownloadError{Msg: "downloading " + b.path + " from GitHub failed: " + err.Error()}
		}
		switch resp.StatusCode {
		case http.StatusOK:
		case http.StatusNotFound:
			return nil, &DownloadError{http.StatusNotFound, "GitHub no longer serves " + b.path + " at the pinned commit"}
		case http.StatusForbidden, http.StatusTooManyRequests:
			return nil, &DownloadError{resp.StatusCode, "GitHub is rate limiting requests; try again later"}
		default:
			return nil, &DownloadError{resp.StatusCode, fmt.Sprintf("GitHub answered HTTP %d", resp.StatusCode)}
		}
		if int64(len(data)) != b.size || gitBlobSHA(data) != strings.ToLower(b.sha) {
			return nil, fmt.Errorf("%w: %s differs from the file GitHub lists", ErrIntegrityMismatch, b.path)
		}
		// The hash covers an LFS pointer, but a checkout with the LFS filter (Git for Windows
		// configures it) would install the file it points to, which nothing has checked.
		if bytes.HasPrefix(data, []byte("version https://git-lfs.github.com/spec/")) {
			return nil, unsafeArchive("%q is a Git LFS pointer; the checksum does not cover the file it stands for", b.path)
		}
		folderFiles = append(folderFiles, FolderFile{Path: b.path, Data: data})
	}

	if FolderHash(folderFiles) != strings.ToLower(gh.ContentHash) {
		shortCommit := gh.Commit
		if len(shortCommit) > 12 {
			shortCommit = shortCommit[:12]
		}
		return nil, fmt.Errorf("%w (GitHub folder %s/%s at %s)", ErrIntegrityMismatch, gh.Repo, gh.Path, shortCommit)
	}

	return &GitHubFolder{
		Repo:   gh.Repo,
		Path:   gh.Path,
		Commit: gh.Commit,
		Tree:   listing.SHA,
		Files:  len(folderFiles),
	}, nil
}

func escapePathSegments(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}

func gitBlobSHA(data []byte) string {
	h := sha1.New()
	h.Write([]byte("blob "))
	h.Write([]byte(strconv.Itoa(len(data))))
	h.Write([]byte{0})
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
