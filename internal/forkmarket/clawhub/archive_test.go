package clawhub

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// blogwatcher-1.0.0.zip is @steipete/blogwatcher 1.0.0 exactly as ClawHub's download endpoint
// sent it on 2026-11-15 (ClawHub publishes every skill under MIT-0). The integrity is the one
// the feed lists for that release; the downloaded bytes themselves hash to something else.
const blogwatcherIntegrity = "sha256:d93023e42fc21a78891b1bddfc6a6a7b3c05dc5a96b8cd3ea3c50ce8528d43d3"

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type zipEntry struct {
	name string
	body string
	mode fs.FileMode
}

// streamedZip builds an archive the way ClawHub streams downloads: deflated entries followed by
// data descriptors.
func streamedZip(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		fh := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			fh.SetMode(e.mode)
		}
		fw, err := w.CreateHeader(fh)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func wholeIntegrity(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// canonicalIntegrity is the publish-time hash of a streamed archive, computed with Go's own zip
// writer rather than this package's: the same compressed entries without skill-card.md, each
// with its sizes in the local header and no data descriptor.
func canonicalIntegrity(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if f.Name == skillCardFile {
			continue
		}
		fh := f.FileHeader
		fh.Flags &^= 0x8
		fw, err := w.CreateRaw(&fh)
		if err != nil {
			t.Fatal(err)
		}
		r, err := f.OpenRaw()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(fw, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return wholeIntegrity(buf.Bytes())
}

func names(files []File) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Name)
	}
	return out
}

func TestVerify_RealClawHubDownloadMatchesFeedIntegrity(t *testing.T) {
	data := readTestdata(t, "blogwatcher-1.0.0.zip")
	if wholeIntegrity(data) == blogwatcherIntegrity {
		t.Fatal("fixture no longer exercises the re-pack: its raw bytes match the feed hash")
	}
	files, err := Verify(data, blogwatcherIntegrity)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := strings.Join(names(files), ","); got != "SKILL.md,_meta.json" {
		t.Fatalf("verified entries = %s, want SKILL.md,_meta.json (skill-card.md is not covered)", got)
	}

	dir := t.TempDir()
	if err := Unpack(data, blogwatcherIntegrity, dir); err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	skill, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || !strings.HasPrefix(string(skill), "---\nname: blogwatcher\n") {
		t.Fatalf("SKILL.md = %q, %v", skill, err)
	}
	if _, err := os.Stat(filepath.Join(dir, skillCardFile)); !os.IsNotExist(err) {
		t.Fatalf("skill-card.md was unpacked although the hash does not cover it (err=%v)", err)
	}
}

func TestVerify_RepackMatchesGoZipWriter(t *testing.T) {
	data := streamedZip(t,
		zipEntry{name: "skill-card.md", body: "# card"},
		zipEntry{name: "SKILL.md", body: "---\nname: demo\n---\nbody\n"},
		zipEntry{name: "scripts/run.sh", body: strings.Repeat("echo hi\n", 200)},
		zipEntry{name: "_meta.json", body: `{"slug":"demo"}`},
	)
	files, err := Verify(data, canonicalIntegrity(t, data))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := strings.Join(names(files), ","); got != "SKILL.md,scripts/run.sh,_meta.json" {
		t.Fatalf("verified entries = %s", got)
	}
}

func TestVerify_WholeArchiveHashKeepsEveryEntry(t *testing.T) {
	data := streamedZip(t, zipEntry{name: "skill-card.md", body: "# card"}, zipEntry{name: "SKILL.md", body: "x"})
	files, err := Verify(data, wholeIntegrity(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(files), ","); got != "skill-card.md,SKILL.md" {
		t.Fatalf("verified entries = %s", got)
	}
}

func TestVerify_RefusesMismatchAndMissingIntegrity(t *testing.T) {
	data := readTestdata(t, "blogwatcher-1.0.0.zip")
	wrong := blogwatcherIntegrity[:len(blogwatcherIntegrity)-1] + "4"
	if _, err := Verify(data, wrong); !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("wrong hash: err = %v, want ErrIntegrityMismatch", err)
	}
	for _, v := range []string{"", "sha256:", "sha512:" + strings.Repeat("a", 128), "sha256-2TAj5C/CGniJGxvd/Gpqezw=", "d93023e42fc21a78891b1bddfc6a6a7b3c05dc5a96b8cd3ea3c50ce8528d43d3"} {
		if _, err := Verify(data, v); !errors.Is(err, ErrNoIntegrity) {
			t.Errorf("integrity %q: err = %v, want ErrNoIntegrity", v, err)
		}
	}

	// One changed byte inside an entry: the re-pack no longer matches.
	tampered := bytes.Clone(data)
	i := bytes.Index(tampered, []byte("SKILL.md")) + len("SKILL.md") + 10
	tampered[i] ^= 0xff
	if _, err := Verify(tampered, blogwatcherIntegrity); !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("tampered archive: err = %v, want ErrIntegrityMismatch", err)
	}
	dir := t.TempDir()
	if err := Unpack(tampered, blogwatcherIntegrity, dir); err == nil {
		t.Fatal("Unpack accepted a tampered archive")
	}
	assertEmpty(t, dir)
}

func assertEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("%s should be empty, has %d entries", dir, len(entries))
	}
}

func TestUnpack_RefusesUnsafeEntries(t *testing.T) {
	skill := zipEntry{name: "SKILL.md", body: "---\nname: x\n---\n"}
	cases := map[string]zipEntry{
		"parent":          {name: "../evil.md", body: "x"},
		"nested parent":   {name: "docs/../../evil.md", body: "x"},
		"absolute":        {name: "/etc/evil.md", body: "x"},
		"drive letter":    {name: "C:/evil.md", body: "x"},
		"drive relative":  {name: "C:evil.md", body: "x"},
		"backslash":       {name: `docs\..\..\evil.md`, body: "x"},
		"stream":          {name: "SKILL.md:hidden", body: "x"},
		"dot segment":     {name: "./SKILL.md", body: "x"},
		"empty segment":   {name: "docs//a.md", body: "x"},
		"trailing dot":    {name: "docs./a.md", body: "x"},
		"control char":    {name: "a\x01.md", body: "x"},
		"symlink":         {name: "link", body: "../../outside", mode: fs.ModeSymlink | 0o777},
		"named pipe":      {name: "pipe", body: "", mode: fs.ModeNamedPipe | 0o644},
		"case duplicate":  {name: "skill.md", body: "x"},
		"inside a file":   {name: "SKILL.md/inner.md", body: "x"},
		"windows reserve": {name: "docs/NUL", body: "x"},
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			if name == "windows reserve" && runtime.GOOS != "windows" {
				t.Skip("reserved device names only alias on Windows")
			}
			data := streamedZip(t, skill, bad)
			dir := t.TempDir()
			err := Unpack(data, wholeIntegrity(data), dir)
			if !errors.Is(err, ErrUnsafeArchive) {
				t.Fatalf("err = %v, want ErrUnsafeArchive", err)
			}
			assertEmpty(t, dir)
		})
	}
}

func TestUnpack_RequiresTopLevelSkillFile(t *testing.T) {
	for name, entries := range map[string][]zipEntry{
		"none":   {{name: "README.md", body: "x"}},
		"nested": {{name: "demo/SKILL.md", body: "x"}},
	} {
		t.Run(name, func(t *testing.T) {
			data := streamedZip(t, entries...)
			dir := t.TempDir()
			if err := Unpack(data, wholeIntegrity(data), dir); !errors.Is(err, ErrNoSkillFile) {
				t.Fatalf("err = %v, want ErrNoSkillFile", err)
			}
			assertEmpty(t, dir)
		})
	}
}

func TestUnpack_SizeAndCountCaps(t *testing.T) {
	t.Run("one entry too large", func(t *testing.T) {
		// Zeros deflate to almost nothing: the cap must come from the declared and inflated
		// size, not the download size.
		data := streamedZip(t, zipEntry{name: "SKILL.md", body: "x"}, zipEntry{name: "big.bin", body: strings.Repeat("\x00", MaxExtractBytes+1)})
		if len(data) > 1<<20 {
			t.Fatalf("test archive unexpectedly large: %d bytes", len(data))
		}
		if _, err := Verify(data, wholeIntegrity(data)); !errors.Is(err, ErrUnsafeArchive) {
			t.Fatalf("err = %v, want ErrUnsafeArchive", err)
		}
	})
	t.Run("total too large", func(t *testing.T) {
		half := strings.Repeat("\x00", MaxExtractBytes/2+1)
		data := streamedZip(t, zipEntry{name: "SKILL.md", body: "x"}, zipEntry{name: "a.bin", body: half}, zipEntry{name: "b.bin", body: half})
		if _, err := Verify(data, wholeIntegrity(data)); !errors.Is(err, ErrUnsafeArchive) {
			t.Fatalf("err = %v, want ErrUnsafeArchive", err)
		}
	})
	t.Run("too many entries", func(t *testing.T) {
		entries := []zipEntry{{name: "SKILL.md", body: "x"}}
		for i := 0; i < MaxFiles; i++ {
			entries = append(entries, zipEntry{name: fmt.Sprintf("f/%d.md", i), body: "x"})
		}
		data := streamedZip(t, entries...)
		if _, err := Verify(data, wholeIntegrity(data)); !errors.Is(err, ErrUnsafeArchive) {
			t.Fatalf("err = %v, want ErrUnsafeArchive", err)
		}
	})
	t.Run("download too large", func(t *testing.T) {
		if _, err := Verify(make([]byte, MaxArchiveBytes+1), wholeIntegrity(nil)); !errors.Is(err, ErrUnsafeArchive) {
			t.Fatalf("err = %v, want ErrUnsafeArchive", err)
		}
	})
	t.Run("not a zip", func(t *testing.T) {
		data := []byte("<html>not found</html>")
		if _, err := Verify(data, wholeIntegrity(data)); !errors.Is(err, ErrUnsafeArchive) {
			t.Fatalf("err = %v, want ErrUnsafeArchive", err)
		}
	})
}

func TestUnpack_WritesFoldersAndFiles(t *testing.T) {
	data := streamedZip(t,
		zipEntry{name: "SKILL.md", body: "---\nname: demo\n---\n"},
		zipEntry{name: "refs/", mode: fs.ModeDir | 0o755},
		zipEntry{name: "refs/guide.md", body: "guide"},
		zipEntry{name: "scripts/tool.py", body: "print(1)"},
	)
	dir := t.TempDir()
	if err := Unpack(data, wholeIntegrity(data), dir); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"refs/guide.md": "guide", "scripts/tool.py": "print(1)"} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", path, got, err)
		}
	}
}
