package clawhub

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Limits on a downloaded archive and on what it may unpack to.
const (
	MaxArchiveBytes = 25 << 20
	MaxExtractBytes = 25 << 20
	MaxFiles        = 2000
	maxNameBytes    = 500
)

// skillCardFile is a file ClawHub adds to downloads after publishing. The feed's hash does
// not cover it, so it is dropped unless the whole archive matches the hash.
const skillCardFile = "skill-card.md"

var (
	ErrNoIntegrity       = errors.New("the ClawHub entry has no sha256 integrity value, so the download cannot be checked")
	ErrIntegrityMismatch = errors.New("the downloaded archive does not match the checksum in ClawHub's feed")
	ErrUnsafeArchive     = errors.New("the archive is unsafe to unpack")
	ErrNoSkillFile       = errors.New("the archive has no SKILL.md at its top level")
)

func unsafeArchive(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsafeArchive, fmt.Sprintf(format, args...))
}

// File is an archive entry whose compressed bytes the integrity hash covered.
type File struct {
	Name  string // slash-separated, checked by validName
	Dir   bool
	mode  fs.FileMode
	raw   []byte // compressed bytes exactly as hashed
	meth  uint16
	crc   uint32
	usize uint64
}

// Verify checks a downloaded skill archive against the feed's "sha256:<hex>" integrity and
// returns the entries the hash covers, in archive order, before anything is unpacked.
//
// ClawHub's integrity is the SHA-256 of the zip it built at publish time (convex/
// skillVersionArtifactHash.ts). The download streams the same entries with data descriptors
// and adds the generated skill-card.md, so it is re-packed here the way the publish-time zip
// was laid out (fflate zipSync: sizes in each local header, no descriptors) from the very
// compressed bytes that were downloaded, without skill-card.md, and that re-pack is hashed.
// An archive whose own bytes match the hash is accepted as is.
func Verify(data []byte, integrity string) ([]File, error) {
	want, ok := ParseIntegrity(integrity)
	if !ok {
		return nil, ErrNoIntegrity
	}
	if len(data) > MaxArchiveBytes {
		return nil, unsafeArchive("larger than %d MB", MaxArchiveBytes>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if errors.Is(err, zip.ErrInsecurePath) {
		return nil, unsafeArchive("an entry has a path outside the skill folder")
	}
	if err != nil {
		return nil, fmt.Errorf("%w: not a zip archive (%v)", ErrUnsafeArchive, err)
	}
	if len(zr.File) > MaxFiles {
		return nil, unsafeArchive("more than %d entries", MaxFiles)
	}
	var all []File
	var total uint64
	for _, f := range zr.File {
		if f.CompressedSize64 > uint64(len(data)) || f.UncompressedSize64 > MaxExtractBytes {
			return nil, unsafeArchive("%q is too large", f.Name)
		}
		total += f.UncompressedSize64
		if total > MaxExtractBytes {
			return nil, unsafeArchive("unpacks to more than %d MB", MaxExtractBytes>>20)
		}
		r, err := f.OpenRaw()
		if err != nil {
			return nil, unsafeArchive("cannot read %q: %v", f.Name, err)
		}
		raw, err := io.ReadAll(r)
		if err != nil || uint64(len(raw)) != f.CompressedSize64 {
			return nil, unsafeArchive("cannot read %q", f.Name)
		}
		all = append(all, File{
			Name: f.Name, Dir: strings.HasSuffix(f.Name, "/"), mode: f.Mode(),
			raw: raw, meth: f.Method, crc: f.CRC32, usize: f.UncompressedSize64,
		})
	}

	whole := sha256.Sum256(data)
	if hex.EncodeToString(whole[:]) == want {
		return all, nil
	}
	var kept []*zip.File
	var files []File
	for i, f := range zr.File {
		if strings.EqualFold(f.Name, skillCardFile) {
			continue
		}
		kept = append(kept, f)
		files = append(files, all[i])
	}
	h := sha256.New()
	if err := writeCanonical(h, kept, files); err != nil {
		return nil, err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return nil, ErrIntegrityMismatch
	}
	return files, nil
}

// writeCanonical writes entries as fflate's zipSync lays out a zip: per entry a local header
// carrying the CRC and sizes, then the data; then the central directory and its end record.
func writeCanonical(w hash.Hash, headers []*zip.File, files []File) error {
	var central bytes.Buffer
	var offset uint64
	le := binary.LittleEndian
	for i, f := range headers {
		name := []byte(f.Name)
		if len(name) > 0xffff || len(f.Extra) > 0xffff || len(f.Comment) > 0xffff ||
			f.CompressedSize64 >= 0xffffffff || f.UncompressedSize64 >= 0xffffffff || offset >= 0xffffffff {
			return unsafeArchive("entry %q needs zip64", f.Name)
		}
		flags := f.Flags &^ 0x8 // the data-descriptor bit; the sizes now sit in the header
		var local [30]byte
		le.PutUint32(local[0:], 0x04034b50)
		le.PutUint16(local[4:], f.ReaderVersion)
		le.PutUint16(local[6:], flags)
		le.PutUint16(local[8:], f.Method)
		le.PutUint16(local[10:], f.ModifiedTime)
		le.PutUint16(local[12:], f.ModifiedDate)
		le.PutUint32(local[14:], f.CRC32)
		le.PutUint32(local[18:], uint32(f.CompressedSize64))
		le.PutUint32(local[22:], uint32(f.UncompressedSize64))
		le.PutUint16(local[26:], uint16(len(name)))
		le.PutUint16(local[28:], uint16(len(f.Extra)))
		w.Write(local[:])
		w.Write(name)
		w.Write(f.Extra)
		w.Write(files[i].raw)

		var cen [46]byte
		le.PutUint32(cen[0:], 0x02014b50)
		le.PutUint16(cen[4:], f.CreatorVersion)
		le.PutUint16(cen[6:], f.ReaderVersion)
		le.PutUint16(cen[8:], flags)
		le.PutUint16(cen[10:], f.Method)
		le.PutUint16(cen[12:], f.ModifiedTime)
		le.PutUint16(cen[14:], f.ModifiedDate)
		le.PutUint32(cen[16:], f.CRC32)
		le.PutUint32(cen[20:], uint32(f.CompressedSize64))
		le.PutUint32(cen[24:], uint32(f.UncompressedSize64))
		le.PutUint16(cen[28:], uint16(len(name)))
		le.PutUint16(cen[30:], uint16(len(f.Extra)))
		le.PutUint16(cen[32:], uint16(len(f.Comment)))
		// disk number start and internal attributes stay 0, as fflate writes them
		le.PutUint32(cen[38:], f.ExternalAttrs)
		le.PutUint32(cen[42:], uint32(offset))
		central.Write(cen[:])
		central.Write(name)
		central.Write(f.Extra)
		central.WriteString(f.Comment)

		offset += uint64(len(local) + len(name) + len(f.Extra) + len(files[i].raw))
	}
	var end [22]byte
	le.PutUint32(end[0:], 0x06054b50)
	le.PutUint16(end[8:], uint16(len(headers)))
	le.PutUint16(end[10:], uint16(len(headers)))
	le.PutUint32(end[12:], uint32(central.Len()))
	le.PutUint32(end[16:], uint32(offset))
	w.Write(central.Bytes())
	w.Write(end[:])
	return nil
}

// validName checks an entry name before it becomes a path: relative, slash-separated, no
// "." or ".." segments, no drive letters or other colons, no backslashes or control
// characters, and nothing Windows would map onto another name.
func validName(name string) (clean string, dir bool, err error) {
	if name == "" || len(name) > maxNameBytes {
		return "", false, unsafeArchive("entry name %q is empty or too long", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '\\' || r == ':' {
			return "", false, unsafeArchive("entry name %q has a character not allowed in a path", name)
		}
	}
	if strings.HasPrefix(name, "/") {
		return "", false, unsafeArchive("entry %q has an absolute path", name)
	}
	clean, dir = strings.CutSuffix(name, "/")
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false, unsafeArchive("entry %q leaves the skill folder or has an empty path segment", name)
		}
		if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
			return "", false, unsafeArchive("entry %q has a name Windows would change", name)
		}
	}
	if !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", false, unsafeArchive("entry %q is not a plain relative path here", name)
	}
	return clean, dir, nil
}

// Check validates what Verify returned before anything is written: safe names, only plain
// files and folders, no two names that differ only in case, a file count and size within the
// limits, and a SKILL.md at the top.
func Check(files []File) error {
	if len(files) > MaxFiles {
		return unsafeArchive("more than %d entries", MaxFiles)
	}
	seen := map[string]bool{}
	dirs := map[string]bool{}
	var total uint64
	hasSkill := false
	for i := range files {
		clean, dir, err := validName(files[i].Name)
		if err != nil {
			return err
		}
		mode := files[i].mode
		if mode&fs.ModeSymlink != 0 {
			return unsafeArchive("entry %q is a symbolic link", files[i].Name)
		}
		if mode.Type()&^fs.ModeDir != 0 || mode.IsDir() != dir {
			return unsafeArchive("entry %q is not a plain file or folder", files[i].Name)
		}
		if files[i].meth != zip.Store && files[i].meth != zip.Deflate {
			return unsafeArchive("entry %q uses an unsupported compression method", files[i].Name)
		}
		key := strings.ToLower(clean)
		if seen[key] {
			return unsafeArchive("entry %q appears twice", files[i].Name)
		}
		seen[key] = true
		files[i].Name, files[i].Dir = clean, dir
		if dir {
			dirs[key] = true
			continue
		}
		total += files[i].usize
		if total > MaxExtractBytes {
			return unsafeArchive("unpacks to more than %d MB", MaxExtractBytes>>20)
		}
		if clean == "SKILL.md" {
			hasSkill = true
		}
	}
	// No entry may sit inside something another entry makes a file.
	for i := range files {
		parts := strings.Split(strings.ToLower(files[i].Name), "/")
		for j := 1; j < len(parts); j++ {
			prefix := strings.Join(parts[:j], "/")
			if seen[prefix] && !dirs[prefix] {
				return unsafeArchive("%q is inside a file", files[i].Name)
			}
		}
	}
	if !hasSkill {
		return ErrNoSkillFile
	}
	return nil
}

// Unpack verifies a downloaded archive against integrity, checks every entry, and writes the
// covered files into dir, a new empty folder. It writes nothing unless both checks pass.
func Unpack(data []byte, integrity, dir string) error {
	files, err := Verify(data, integrity)
	if err != nil {
		return err
	}
	if err := Check(files); err != nil {
		return err
	}
	return extract(files, dir)
}

// extract writes entries that Verify and Check passed into dir.
func extract(files []File, dir string) error {
	for _, f := range files {
		if err := writeEntry(dir, f); err != nil {
			return err
		}
	}
	return nil
}

// writeEntry inflates the hashed bytes of one entry and checks its size and CRC as it goes.
func writeEntry(dir string, f File) error {
	target := filepath.Join(dir, filepath.FromSlash(f.Name))
	if f.Dir {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	var r io.Reader = bytes.NewReader(f.raw)
	if f.meth == zip.Deflate {
		fr := flate.NewReader(r)
		defer fr.Close()
		r = fr
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	sum := crc32.NewIEEE()
	n, err := io.Copy(io.MultiWriter(out, sum), io.LimitReader(r, int64(f.usize)+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("%w: cannot unpack %q: %v", ErrUnsafeArchive, f.Name, err)
	}
	if uint64(n) != f.usize || sum.Sum32() != f.crc {
		return unsafeArchive("%q does not unpack to its recorded size and checksum", f.Name)
	}
	return nil
}
