// Fork-only (endgame302/skillshare): the package's only disk writes, for its caches.

package mcpreg

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// maxIndexBytes caps a decompressed cache file.
const maxIndexBytes = 512 << 20

// writeFileAtomic replaces path in one step (a temp file in the same folder, then a rename), so
// a reader sees the old or the new file. A cache that cannot be written only costs a fetch.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		os.Remove(tmp.Name())
	}
	return werr
}

func writeGzipJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeGzip(path, data)
}

// writeGzip compresses data and writes it with writeFileAtomic.
func writeGzip(path string, data []byte) error {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return writeFileAtomic(path, buf.Bytes())
}

func readGzipJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(zr, maxIndexBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxIndexBytes {
		return fmt.Errorf("%s is larger than %d bytes", path, maxIndexBytes)
	}
	return json.Unmarshal(data, v)
}
