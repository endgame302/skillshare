package clawhub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFolderHash_MatchesClawHub(t *testing.T) {
	samples := map[string]string{
		"SKILL.md": "---\nname: kat\n---\n# KAT\n", "references/api.md": "api\n", "references/API-v2.md": "v2\n",
		"scripts/_helper.py": "print(1)\n", "scripts/run-all.sh": "a\n", "scripts/run_all.sh": "b\n",
		"scripts/run.all.sh": "c\n", "Zeta.txt": "z", "alpha.txt": "", "a-b.txt": "1", "a_b.txt": "2",
		"ab.txt": "3", "10.md": "ten", "9.md": "nine", "docs/x.md": "d", "docs-old/x.md": "e",
		"docs_old/x.md": "f", ".env.example": "KEY=\n", "README.md": "r", "readme.txt": "r2",
		"café.md": "accent", "cafe.md": "plain", "b c.md": "space",
	}
	files := make([]FolderFile, 0, len(samples))
	for p, content := range samples {
		files = append(files, FolderFile{Path: p, Data: []byte(content)})
	}
	const want = "b6edc56a5f4b8fceeeb5b34031bda121d7bfda0363974096a9a7d48e4f4d3427"
	if got := FolderHash(files); got != want {
		t.Fatalf("FolderHash = %s, want %s", got, want)
	}

	// Byte order produces a different digest; localeCompare (ICU root collation) must be kept.
	keys := make([]string, 0, len(samples))
	for k := range samples {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		sum := sha256.Sum256([]byte(samples[k]))
		lines[i] = k + "\x00" + strconv.Itoa(len(samples[k])) + "\x00" + hex.EncodeToString(sum[:])
	}
	byteOrderSum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	if hex.EncodeToString(byteOrderSum[:]) == want {
		t.Fatal("byte order hash unexpectedly matched ICU root collation hash")
	}
}

type fakeGitHubTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size,omitempty"`
}

type fakeGitHubServer struct {
	srv        *httptest.Server
	status     atomic.Int32
	truncated  atomic.Bool
	treeSHA    string
	entries    atomic.Value // []fakeGitHubTreeEntry
	blobs      atomic.Value // map[string]string
	treeHits   atomic.Int32
	blobHits   atomic.Int32
	outsideHit atomic.Bool
}

func newFakeGitHubServer(t *testing.T, repo, folderPath, commit string, initial map[string]string) *fakeGitHubServer {
	t.Helper()
	f := &fakeGitHubServer{treeSHA: strings.Repeat("f", 40)}
	f.status.Store(http.StatusOK)
	f.setFiles(initial)

	expectedTreePath := "/repos/" + repo + "/git/trees/" + commit + ":" + folderPath
	expectedRawPrefix := "/" + repo + "/" + commit + "/" + folderPath + "/"

	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			f.treeHits.Add(1)
			if r.URL.Path != expectedTreePath {
				f.outsideHit.Store(true)
				http.NotFound(w, r)
				return
			}
			if code := int(f.status.Load()); code != http.StatusOK {
				http.Error(w, "status", code)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"sha":       f.treeSHA,
				"truncated": f.truncated.Load(),
				"tree":      f.entries.Load().([]fakeGitHubTreeEntry),
			})
			return
		}
		if strings.HasPrefix(r.URL.Path, expectedRawPrefix) {
			f.blobHits.Add(1)
			rel := strings.TrimPrefix(r.URL.Path, expectedRawPrefix)
			blobs := f.blobs.Load().(map[string]string)
			body, ok := blobs[rel]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(body))
			return
		}
		f.outsideHit.Store(true)
		http.NotFound(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHubServer) setFiles(files map[string]string) {
	blobs := make(map[string]string, len(files))
	keys := make([]string, 0, len(files))
	for k, v := range files {
		blobs[k] = v
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var entries []fakeGitHubTreeEntry
	seenDirs := map[string]bool{}
	for _, k := range keys {
		if idx := strings.LastIndex(k, "/"); idx > 0 {
			dir := k[:idx]
			if !seenDirs[dir] {
				seenDirs[dir] = true
				entries = append(entries, fakeGitHubTreeEntry{Path: dir, Mode: "040000", Type: "tree", SHA: strings.Repeat("d", 40)})
			}
		}
		entries = append(entries, fakeGitHubTreeEntry{
			Path: k,
			Mode: "100644",
			Type: "blob",
			SHA:  gitBlobSHA([]byte(files[k])),
			Size: int64(len(files[k])),
		})
	}
	f.blobs.Store(blobs)
	f.entries.Store(entries)
}

func (f *fakeGitHubServer) client(t *testing.T) *Client {
	t.Helper()
	c := New(t.TempDir(), t.TempDir())
	c.HTTP = f.srv.Client()
	c.GitHubAPI = f.srv.URL
	c.GitHubRaw = f.srv.URL
	return c
}

func TestVerifyGitHub(t *testing.T) {
	const (
		repo       = "aws/agent-toolkit-for-aws"
		folderPath = "plugins/aws-agents/skills/agents-build"
		commit     = "b4416dddac9b6a0cc5412136e6dbf8f07ffdb31c"
	)
	baseFiles := map[string]string{
		"SKILL.md":        "---\nname: agents-build\n---\n# Build\n",
		"references/a.md": "ref a\n",
		".env.example":    "FOO=bar\n",
	}
	baseHash := FolderHash([]FolderFile{
		{Path: "SKILL.md", Data: []byte(baseFiles["SKILL.md"])},
		{Path: "references/a.md", Data: []byte(baseFiles["references/a.md"])},
		{Path: ".env.example", Data: []byte(baseFiles[".env.example"])},
	})
	makeCand := func(hash string) Candidate {
		return Candidate{
			SourceRef: "public-github",
			Package:   "@aws/agents-build",
			Version:   commit,
			Integrity: "sha256:" + hash,
			GitHub: &GitHubSource{
				Repo:        repo,
				Path:        folderPath,
				Commit:      commit,
				ContentHash: hash,
			},
		}
	}

	t.Run("success", func(t *testing.T) {
		f := newFakeGitHubServer(t, repo, folderPath, commit, baseFiles)
		folder, err := f.client(t).VerifyGitHub(context.Background(), makeCand(baseHash))
		if err != nil {
			t.Fatalf("VerifyGitHub: %v", err)
		}
		if folder.Tree != f.treeSHA || folder.Files != 3 || folder.Repo != repo || folder.Path != folderPath || folder.Commit != commit {
			t.Errorf("folder = %+v", folder)
		}
		if f.outsideHit.Load() || f.treeHits.Load() != 1 || f.blobHits.Load() != 3 {
			t.Errorf("outside=%v treeHits=%d blobHits=%d", f.outsideHit.Load(), f.treeHits.Load(), f.blobHits.Load())
		}
	})

	t.Run("content hash mismatch", func(t *testing.T) {
		f := newFakeGitHubServer(t, repo, folderPath, commit, baseFiles)
		_, err := f.client(t).VerifyGitHub(context.Background(), makeCand(strings.Repeat("0", 64)))
		if !IsIntegrityError(err) {
			t.Fatalf("err = %v, want integrity error", err)
		}
	})

	t.Run("blob bytes differ from sha", func(t *testing.T) {
		f := newFakeGitHubServer(t, repo, folderPath, commit, baseFiles)
		blobs := map[string]string{
			"SKILL.md":        baseFiles["SKILL.md"],
			"references/a.md": "tampered",
			".env.example":    baseFiles[".env.example"],
		}
		f.blobs.Store(blobs)
		_, err := f.client(t).VerifyGitHub(context.Background(), makeCand(baseHash))
		if !IsIntegrityError(err) {
			t.Fatalf("err = %v, want integrity error", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		f := newFakeGitHubServer(t, repo, folderPath, commit, baseFiles)
		entries := append(f.entries.Load().([]fakeGitHubTreeEntry), fakeGitHubTreeEntry{Path: "link", Mode: "120000", Type: "blob", SHA: strings.Repeat("a", 40), Size: 5})
		f.entries.Store(entries)
		_, err := f.client(t).VerifyGitHub(context.Background(), makeCand(baseHash))
		if !errors.Is(err, ErrUnsafeArchive) || f.blobHits.Load() != 0 {
			t.Fatalf("err = %v, blobHits = %d", err, f.blobHits.Load())
		}
	})

	t.Run("submodule", func(t *testing.T) {
		f := newFakeGitHubServer(t, repo, folderPath, commit, baseFiles)
		entries := append(f.entries.Load().([]fakeGitHubTreeEntry), fakeGitHubTreeEntry{Path: "sub", Mode: "160000", Type: "commit", SHA: strings.Repeat("a", 40)})
		f.entries.Store(entries)
		_, err := f.client(t).VerifyGitHub(context.Background(), makeCand(baseHash))
		if !errors.Is(err, ErrUnsafeArchive) || f.blobHits.Load() != 0 {
			t.Fatalf("err = %v, blobHits = %d", err, f.blobHits.Load())
		}
	})

	t.Run("truncated", func(t *testing.T) {
		f := newFakeGitHubServer(t, repo, folderPath, commit, baseFiles)
		f.truncated.Store(true)
		_, err := f.client(t).VerifyGitHub(context.Background(), makeCand(baseHash))
		if !errors.Is(err, ErrUnsafeArchive) || f.blobHits.Load() != 0 {
			t.Fatalf("err = %v, blobHits = %d", err, f.blobHits.Load())
		}
	})

	t.Run("dotdot path", func(t *testing.T) {
		f := newFakeGitHubServer(t, repo, folderPath, commit, baseFiles)
		entries := append(f.entries.Load().([]fakeGitHubTreeEntry), fakeGitHubTreeEntry{Path: "../escape", Mode: "100644", Type: "blob", SHA: strings.Repeat("a", 40), Size: 5})
		f.entries.Store(entries)
		_, err := f.client(t).VerifyGitHub(context.Background(), makeCand(baseHash))
		if !errors.Is(err, ErrUnsafeArchive) || f.blobHits.Load() != 0 {
			t.Fatalf("err = %v, blobHits = %d", err, f.blobHits.Load())
		}
	})

	t.Run("no SKILL.md", func(t *testing.T) {
		f := newFakeGitHubServer(t, repo, folderPath, commit, map[string]string{"README.md": "hi"})
		_, err := f.client(t).VerifyGitHub(context.Background(), makeCand(baseHash))
		if !errors.Is(err, ErrNoSkillFile) || f.blobHits.Load() != 0 {
			t.Fatalf("err = %v, blobHits = %d", err, f.blobHits.Load())
		}
	})

	t.Run("listing 404", func(t *testing.T) {
		f := newFakeGitHubServer(t, repo, folderPath, commit, baseFiles)
		f.status.Store(http.StatusNotFound)
		_, err := f.client(t).VerifyGitHub(context.Background(), makeCand(baseHash))
		var de *DownloadError
		if !errors.As(err, &de) || de.Status != http.StatusNotFound {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("listing 403 rate limit", func(t *testing.T) {
		f := newFakeGitHubServer(t, repo, folderPath, commit, baseFiles)
		f.status.Store(http.StatusForbidden)
		_, err := f.client(t).VerifyGitHub(context.Background(), makeCand(baseHash))
		var de *DownloadError
		if !errors.As(err, &de) || de.Status != http.StatusForbidden {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("git lfs pointer", func(t *testing.T) {
		// The hash covers the pointer, but a checkout with the LFS filter (Git for Windows
		// sets it up) would install the file it points to, which nothing checked.
		files := map[string]string{
			"SKILL.md":         baseFiles["SKILL.md"],
			"assets/model.bin": "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("a", 64) + "\nsize 1048576\n",
		}
		f := newFakeGitHubServer(t, repo, folderPath, commit, files)
		hash := FolderHash([]FolderFile{
			{Path: "SKILL.md", Data: []byte(files["SKILL.md"])},
			{Path: "assets/model.bin", Data: []byte(files["assets/model.bin"])},
		})
		_, err := f.client(t).VerifyGitHub(context.Background(), makeCand(hash))
		if !errors.Is(err, ErrUnsafeArchive) {
			t.Fatalf("err = %v, want unsafe (LFS pointer)", err)
		}
	})

	t.Run("too many bytes", func(t *testing.T) {
		f := newFakeGitHubServer(t, repo, folderPath, commit, baseFiles)
		entries := f.entries.Load().([]fakeGitHubTreeEntry)
		entries[0].Size = MaxExtractBytes + 1
		f.entries.Store(entries)
		_, err := f.client(t).VerifyGitHub(context.Background(), makeCand(baseHash))
		if !errors.Is(err, ErrUnsafeArchive) || f.blobHits.Load() != 0 {
			t.Fatalf("err = %v, blobHits = %d", err, f.blobHits.Load())
		}
	})
}
