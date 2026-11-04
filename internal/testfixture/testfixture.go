// Package testfixture reads test fixtures that may be stored zstd-compressed.
//
// Large fixtures, such as the BDS captures in server/block/testdata, are kept in the repository as "x.json.zst"
// next to where "x.json" used to be. Tests keep naming the uncompressed path: Read and Open use "x.json" when it
// exists and fall back to "x.json.zst" otherwise, so a fixture may be decompressed locally for inspection without
// changing any test.
package testfixture

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// Ext is the extension a compressed fixture carries after its own.
const Ext = ".zst"

// Threshold is the size from which Write stores a fixture compressed. Smaller fixtures gain too little from
// compression to be worth the binary file in review.
const Threshold = 100 << 10

// decoder decodes whole fixtures. DecodeAll is safe for concurrent use.
var decoder = sync.OnceValues(func() (*zstd.Decoder, error) {
	return zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
})

// Read returns the contents of the fixture at path. If path does not exist but path+Ext does, Read returns the
// decompressed contents of path+Ext. If neither exists, the error is that of reading path.
func Read(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if !errors.Is(err, fs.ErrNotExist) {
		return data, err
	}
	compressed, zerr := os.ReadFile(path + Ext)
	if zerr != nil {
		if errors.Is(zerr, fs.ErrNotExist) {
			return nil, err
		}
		return nil, zerr
	}
	dec, zerr := decoder()
	if zerr != nil {
		return nil, zerr
	}
	data, zerr = dec.DecodeAll(compressed, nil)
	if zerr != nil {
		return nil, &fs.PathError{Op: "decompress", Path: path + Ext, Err: zerr}
	}
	return data, nil
}

// Open opens the fixture at path for streaming, falling back to path+Ext as Read does. The caller must close the
// returned reader.
func Open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if !errors.Is(err, fs.ErrNotExist) {
		return f, err
	}
	zf, zerr := os.Open(path + Ext)
	if zerr != nil {
		if errors.Is(zerr, fs.ErrNotExist) {
			return nil, err
		}
		return nil, zerr
	}
	dec, zerr := zstd.NewReader(zf, zstd.WithDecoderConcurrency(1))
	if zerr != nil {
		_ = zf.Close()
		return nil, &fs.PathError{Op: "decompress", Path: path + Ext, Err: zerr}
	}
	return &stream{dec: dec, f: zf}, nil
}

// stream decompresses a fixture file and closes both the decoder and the file.
type stream struct {
	dec *zstd.Decoder
	f   *os.File
}

// Read reads decompressed data.
func (s *stream) Read(p []byte) (int, error) { return s.dec.Read(p) }

// Close releases the decoder and closes the file.
func (s *stream) Close() error {
	s.dec.Close()
	return s.f.Close()
}

// Write records a fixture under path. Data of at least Threshold bytes is written compressed to path+Ext and
// smaller data to path itself; the other of the two files is removed, so that Read finds what was just written.
func Write(path string, data []byte) error {
	if len(data) < Threshold {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
		return removeIfExists(path + Ext)
	}
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return err
	}
	compressed := enc.EncodeAll(data, make([]byte, 0, len(data)/16))
	if err := enc.Close(); err != nil {
		return err
	}
	if err := os.WriteFile(path+Ext, compressed, 0o644); err != nil {
		return err
	}
	return removeIfExists(path)
}

// removeIfExists removes the file at path, if there is one.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
