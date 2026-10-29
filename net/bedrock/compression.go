package bedrock

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"io"
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

// Compress appends src compressed to dst.
func (f Flate) Compress(dst, src []byte) ([]byte, error) {
	fixedBelow := f.FixedBelow
	if fixedBelow == 0 {
		fixedBelow = defaultFixedBelow
	}
	if len(src) < fixedBelow {
		return fixedDeflate(dst, src), nil
	}
	level := f.level()
	buf := bytes.NewBuffer(dst)
	w, _ := flateWriters[level].Get().(*flate.Writer)
	if w == nil {
		var err error
		if w, err = flate.NewWriter(buf, level); err != nil {
			return nil, err
		}
	} else {
		w.Reset(buf)
	}
	defer flateWriters[level].Put(w)
	if _, err := w.Write(src); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decompress appends src decompressed to dst.
func (Flate) Decompress(dst, src []byte, limit int) ([]byte, error) {
	r, _ := flateReaders.Get().(io.ReadCloser)
	if r == nil {
		r = flate.NewReader(bytes.NewReader(src))
	} else if err := r.(flate.Resetter).Reset(bytes.NewReader(src), nil); err != nil {
		return nil, err
	}
	defer flateReaders.Put(r)
	start := len(dst)
	for {
		if len(dst) == cap(dst) {
			dst = append(dst, 0)[:len(dst)]
		}
		n, err := r.Read(dst[len(dst):cap(dst)])
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
