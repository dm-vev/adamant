package bedrock

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
)

// Compression compresses the bodies of batches.
type Compression interface {
	// Algorithm returns the algorithm, as NetworkSettings names it.
	Algorithm() uint16
	// Compress appends src compressed to dst.
	Compress(dst, src []byte) ([]byte, error)
	// Decompress appends src decompressed to dst. It fails once that takes
	// more than limit bytes.
	Decompress(dst, src []byte, limit int) ([]byte, error)
}

// ErrTooLarge is returned for a batch that decompresses past its limit.
var ErrTooLarge = errors.New("bedrock: batch too large")

// None leaves batches as they are.
type None struct{}

// Algorithm returns AlgorithmNone.
func (None) Algorithm() uint16 { return AlgorithmNone }

// Compress appends src to dst.
func (None) Compress(dst, src []byte) ([]byte, error) { return append(dst, src...), nil }

// Decompress appends src to dst.
func (None) Decompress(dst, src []byte, limit int) ([]byte, error) {
	if len(src) > limit {
		return nil, ErrTooLarge
	}
	return append(dst, src...), nil
}

// Flate compresses batches with raw DEFLATE (RFC 1951). A batch shorter
// than FixedBelow goes in one block of the fixed Huffman codes, which takes
// a fraction of the time compress/flate spends building code tables for
// the small batches of every tick, at about a tenth more bytes; a longer
// one goes through compress/flate at Level. The zero value uses level 1,
// which keeps most of the gain of level 6 at a fraction of its cost, and
// the fixed codes below 16 KiB.
type Flate struct {
	Level int
	// FixedBelow is the length below which batches take the fixed codes:
	// zero means 16 KiB, a negative value none.
	FixedBelow int
}

// defaultFixedBelow is the FixedBelow of the zero Flate.
const defaultFixedBelow = 16 << 10

// Algorithm returns AlgorithmFlate.
func (Flate) Algorithm() uint16 { return AlgorithmFlate }

type sliceWriter struct {
	b []byte
}

func (w *sliceWriter) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}

type flateWriter struct {
	w   *flate.Writer
	dst sliceWriter
}

type flateReader struct {
	r  io.ReadCloser
	br bytes.Reader
}

// flateWriters pools a writer per level; flateReaders pools readers.
var (
	flateWriters [flate.BestCompression + 1]sync.Pool
	flateReaders sync.Pool
)

func (f Flate) level() int {
	if f.Level <= 0 || f.Level > flate.BestCompression {
		return flate.BestSpeed
	}
	return f.Level
}

func (f Flate) fixedBelow() int {
	if f.FixedBelow == 0 {
		return defaultFixedBelow
	}
	return f.FixedBelow
}

// Compress appends src compressed to dst.
func (f Flate) Compress(dst, src []byte) ([]byte, error) {
	if len(src) < f.fixedBelow() {
		return fixedDeflate(dst, src), nil
	}
	level := f.level()
	if cap(dst)-len(dst) < 64 && len(src) > 0 {
		dst = slices.Grow(dst, min(max(len(src)/2, 256), 64<<10))
	}
	fw, _ := flateWriters[level].Get().(*flateWriter)
	if fw == nil {
		fw = &flateWriter{dst: sliceWriter{b: dst}}
		w, err := flate.NewWriter(&fw.dst, level)
		if err != nil {
			return nil, err
		}
		fw.w = w
	} else {
		fw.dst.b = dst
		fw.w.Reset(&fw.dst)
	}
	_, werr := fw.w.Write(src)
	cerr := fw.w.Close()
	out := fw.dst.b
	fw.dst.b = nil
	flateWriters[level].Put(fw)
	if werr != nil {
		return nil, werr
	}
	if cerr != nil {
		return nil, cerr
	}
	return out, nil
}

// Decompress appends src decompressed to dst.
func (Flate) Decompress(dst, src []byte, limit int) ([]byte, error) {
	fr, _ := flateReaders.Get().(*flateReader)
	if fr == nil {
		fr = &flateReader{}
		fr.br.Reset(src)
		fr.r = flate.NewReader(&fr.br)
	} else {
		fr.br.Reset(src)
		if err := fr.r.(flate.Resetter).Reset(&fr.br, nil); err != nil {
			fr.br.Reset(nil)
			flateReaders.Put(fr)
			return nil, err
		}
	}
	defer func() {
		fr.br.Reset(nil)
		flateReaders.Put(fr)
	}()

	start := len(dst)
	if limit >= 0 && cap(dst)-start < 64 {
		est := len(src) * 2
		if len(src) >= 2048 {
			est = len(src) * 3
		}
		est = min(max(est, 256), 64<<10)
		if limit < est {
			est = limit + 1
		}
		dst = slices.Grow(dst, est)
	}
	for {
		if len(dst) == cap(dst) {
			cur := len(dst) - start
			if cur > limit {
				return nil, ErrTooLarge
			}
			grow := max(cur, 512)
			if rem := limit - cur + 1; rem > 0 && grow > rem {
				grow = rem
			}
			dst = slices.Grow(dst, grow)
		}
		n, err := fr.r.Read(dst[len(dst):cap(dst)])
		dst = dst[:len(dst)+n]
		if len(dst)-start > limit {
			return nil, ErrTooLarge
		}
		switch {
		case err == io.EOF:
			return dst, nil
		case err != nil:
			return nil, fmt.Errorf("bedrock: inflate batch: %w", err)
		}
	}
}
