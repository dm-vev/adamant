package leveldat

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/dm-vev/adamant/net/nbt"
)

// LevelDat implements the encoding and decoding of level.dat files. An empty
// LevelDat is a valid value and may be used to Marshal and Write to a writer or
// file afterward.
type LevelDat struct {
	hdr  header
	data []byte
}

// header holds the header for a level.dat file.
type header struct {
	StorageVersion int32
	FileLength     int32
}

// ReadFile reads a level.dat at a path and returns it.
func ReadFile(name string) (*LevelDat, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("level.dat: open file: %w", err)
	}
	defer f.Close()
	return Read(bufio.NewReader(f))
}

// Read reads a level.dat from r and returns it.
func Read(r io.Reader) (*LevelDat, error) {
	var ldat LevelDat
	if err := binary.Read(r, binary.LittleEndian, &ldat.hdr); err != nil {
		return nil, fmt.Errorf("level.dat: read header: %w", err)
	}
	if ldat.hdr.FileLength < 0 {
		return nil, fmt.Errorf("level.dat: read data: negative length %v", ldat.hdr.FileLength)
	}
	// The length is read from the file, so a corrupt one could be anything:
	// the buffer grows as data is read rather than being allocated upfront.
	var buf bytes.Buffer
	if n, err := buf.ReadFrom(io.LimitReader(r, int64(ldat.hdr.FileLength))); err != nil {
		return nil, fmt.Errorf("level.dat: read data: %w", err)
	} else if n != int64(ldat.hdr.FileLength) {
		return nil, fmt.Errorf("level.dat: read data: %w", io.ErrUnexpectedEOF)
	}
	ldat.data = buf.Bytes()
	return &ldat, nil
}

// Unmarshal decodes the level.dat properties from ld into dst. Unmarshal
// returns an error if dst was unable to store all properties found in the
// level.dat.
func (ld *LevelDat) Unmarshal(dst any) error {
	if err := nbt.UnmarshalEncoding(ld.data, dst, nbt.LittleEndian); err != nil {
		return fmt.Errorf("level.dat: decode nbt: %w", err)
	}
	return nil
}

// Ver returns the version of the level.dat decoded, or 0 if ld is the empty
// value.
func (ld *LevelDat) Ver() int {
	return int(ld.hdr.StorageVersion)
}

// Marshal encodes src and stores it in the level.dat. src should be either a
// struct or a map of fields. Marshal updates the storage version to the latest.
func (ld *LevelDat) Marshal(src any) error {
	var err error
	ld.data, err = nbt.MarshalEncoding(src, nbt.LittleEndian)
	if err != nil {
		return fmt.Errorf("level.dat: encode nbt: %w", err)
	}
	ld.hdr = header{
		StorageVersion: Version,
		FileLength:     int32(len(ld.data)),
	}
	return nil
}

// Write writes ld to w.
func (ld *LevelDat) Write(w io.Writer) error {
	if err := binary.Write(w, binary.LittleEndian, ld.hdr); err != nil {
		return fmt.Errorf("level.dat: write header: %w", err)
	}
	if _, err := w.Write(ld.data); err != nil {
		return fmt.Errorf("level.dat: write data: %w", err)
	}
	return nil
}

// WriteFile writes ld to a file at name, keeping the file it replaces as
// name+"_old", as Bedrock Dedicated Server does: ld is written to
// name+"_new" and synced to disk, the file at name is renamed to name+"_old"
// and name+"_new" to name, and the directory is synced. A crash at any point
// leaves a complete file at name or name+"_old", and ReadFile callers may
// fall back to the latter. If WriteFile fails, the file at name is left as
// it was.
func (ld *LevelDat) WriteFile(name string) error {
	var buf bytes.Buffer
	if err := ld.Write(&buf); err != nil {
		return err
	}
	return osFS.writeFileAtomic(name, buf.Bytes())
}
