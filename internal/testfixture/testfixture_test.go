package testfixture

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFallsBackToCompressed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.json")
	data := bytes.Repeat([]byte(`{"tick":1,"block":"minecraft:stone"},`), Threshold/8)
	if err := Write(path, data); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Write left %s uncompressed: %v", path, err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("Read returned %d bytes, want %d", len(got), len(data))
	}
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(r)
	if cerr := r.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("Open streamed %d bytes, want %d", len(got), len(data))
	}
}

func TestReadPrefersUncompressed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := os.WriteFile(path+Ext, []byte("not zstd"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("small")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + Ext); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Write left the stale %s: %v", path+Ext, err)
	}
	got, err := Read(path)
	if err != nil || string(got) != "small" {
		t.Fatalf("Read = %q, %v; want \"small\"", got, err)
	}
}

func TestReadMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if _, err := Read(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read of a missing fixture = %v, want fs.ErrNotExist", err)
	}
	if _, err := Open(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Open of a missing fixture = %v, want fs.ErrNotExist", err)
	}
}
