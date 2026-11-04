package leveldat

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// encode returns the level.dat bytes of d, written as WriteFile wrote them
// before it was atomic: header and data straight to the file, buffered.
func encode(t *testing.T, d Data) (*LevelDat, []byte) {
	t.Helper()
	var ldat LevelDat
	if err := ldat.Marshal(d); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := ldat.Write(w); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	return &ldat, buf.Bytes()
}

func testData(seed int64) Data {
	var d Data
	d.FillDefault()
	d.RandomSeed = seed
	return d
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertMissing(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%v exists (stat error %v), want it missing", filepath.Base(name), err)
	}
}

// TestWriteFileKeepsPrevious checks that WriteFile writes the bytes Write
// writes, and keeps the file it replaces as level.dat_old as BDS does.
func TestWriteFileKeepsPrevious(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "level.dat")
	first, firstBytes := encode(t, testData(1))
	second, secondBytes := encode(t, testData(2))

	if err := first.WriteFile(name); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, name); !bytes.Equal(got, firstBytes) {
		t.Fatalf("level.dat after first write differs from Write: %d bytes, want %d", len(got), len(firstBytes))
	}
	assertMissing(t, name+"_old")
	assertMissing(t, name+"_new")

	if err := second.WriteFile(name); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, name); !bytes.Equal(got, secondBytes) {
		t.Fatal("level.dat after second write differs from Write")
	}
	if got := readFile(t, name+"_old"); !bytes.Equal(got, firstBytes) {
		t.Fatal("level.dat_old does not hold the level.dat replaced")
	}
	assertMissing(t, name+"_new")

	// A level.dat_new left behind by a crash is overwritten.
	if err := os.WriteFile(name+"_new", bytes.Repeat([]byte{0xff}, 2*len(firstBytes)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := first.WriteFile(name); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, name); !bytes.Equal(got, firstBytes) {
		t.Fatal("level.dat after write over a stale level.dat_new differs from Write")
	}
	if got := readFile(t, name+"_old"); !bytes.Equal(got, secondBytes) {
		t.Fatal("level.dat_old does not hold the level.dat replaced")
	}
}

// TestWriteFileWithoutLevelDatKeepsOld checks that a level.dat_old is left
// alone if there is no level.dat to replace it with.
func TestWriteFileWithoutLevelDatKeepsOld(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "level.dat")
	_, oldBytes := encode(t, testData(1))
	second, secondBytes := encode(t, testData(2))
	if err := os.WriteFile(name+"_old", oldBytes, 0644); err != nil {
		t.Fatal(err)
	}
	if err := second.WriteFile(name); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, name); !bytes.Equal(got, secondBytes) {
		t.Fatal("level.dat differs from Write")
	}
	if got := readFile(t, name+"_old"); !bytes.Equal(got, oldBytes) {
		t.Fatal("level.dat_old changed")
	}
}

// faultyFile is a file that fails the operation op.
type faultyFile struct {
	file
	op string
}

func (f faultyFile) Write(b []byte) (int, error) {
	if f.op == "write" {
		// Half the data reaches the file, as when a disk fills up.
		n, _ := f.file.Write(b[:len(b)/2])
		return n, errors.New("injected write failure")
	}
	return f.file.Write(b)
}

func (f faultyFile) Sync() error {
	if f.op == "sync" {
		return errors.New("injected sync failure")
	}
	return f.file.Sync()
}

func (f faultyFile) Close() error {
	err := f.file.Close()
	if f.op == "close" {
		return errors.New("injected close failure")
	}
	return err
}

// faultyFS returns osFS failing the operation op, and the operations it did.
func faultyFS(op string) (fileSystem, *[]string) {
	var ops []string
	fsys := osFS
	fsys.create = func(name string) (file, error) {
		ops = append(ops, "create "+filepath.Base(name))
		if op == "create" {
			return nil, errors.New("injected create failure")
		}
		f, err := osFS.create(name)
		if err != nil {
			return nil, err
		}
		return faultyFile{file: f, op: op}, nil
	}
	fsys.rename = func(oldpath, newpath string) error {
		step := "rename " + filepath.Base(oldpath) + " " + filepath.Base(newpath)
		ops = append(ops, step)
		if op == step || op == "rename all" && strings.HasSuffix(step, " level.dat") {
			return errors.New("injected rename failure")
		}
		return osFS.rename(oldpath, newpath)
	}
	fsys.syncDir = func(dir string) error {
		ops = append(ops, "sync dir")
		if op == "sync dir" {
			return errors.New("injected sync dir failure")
		}
		return osFS.syncDir(dir)
	}
	return fsys, &ops
}

// TestWriteFileFailureKeepsLevelDat makes each step of an atomic write fail,
// and checks that the error is returned and that level.dat keeps the bytes
// it had, with no level.dat_new left behind.
func TestWriteFileFailureKeepsLevelDat(t *testing.T) {
	_, firstBytes := encode(t, testData(1))
	_, oldBytes := encode(t, testData(3))
	second, _ := encode(t, testData(2))
	var secondBuf bytes.Buffer
	if err := second.Write(&secondBuf); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{
		"create",
		"write",
		"sync",
		"close",
		"rename level.dat level.dat_old",
		"rename level.dat_new level.dat",
	} {
		t.Run(op, func(t *testing.T) {
			dir := t.TempDir()
			name := filepath.Join(dir, "level.dat")
			if err := os.WriteFile(name, firstBytes, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name+"_old", oldBytes, 0644); err != nil {
				t.Fatal(err)
			}
			fsys, ops := faultyFS(op)
			err := fsys.writeFileAtomic(name, secondBuf.Bytes())
			if err == nil || !strings.Contains(err.Error(), "injected") {
				t.Fatalf("writeFileAtomic error = %v, want the injected failure (ops %q)", err, *ops)
			}
			if got := readFile(t, name); !bytes.Equal(got, firstBytes) {
				t.Fatalf("level.dat changed after a failed write (ops %q)", *ops)
			}
			assertMissing(t, name+"_new")
			if op != "rename level.dat_new level.dat" {
				if got := readFile(t, name+"_old"); !bytes.Equal(got, oldBytes) {
					t.Fatalf("level.dat_old changed after a failed write (ops %q)", *ops)
				}
			}
		})
	}

	t.Run("restore", func(t *testing.T) {
		// The new file cannot be renamed into place, nor the previous one
		// back: the previous one is still there as level.dat_old, which Open
		// reads.
		dir := t.TempDir()
		name := filepath.Join(dir, "level.dat")
		if err := os.WriteFile(name, firstBytes, 0644); err != nil {
			t.Fatal(err)
		}
		fsys, ops := faultyFS("rename all")
		err := fsys.writeFileAtomic(name, secondBuf.Bytes())
		if err == nil || !strings.Contains(err.Error(), "restore previous file") {
			t.Fatalf("writeFileAtomic error = %v, want a failed restore (ops %q)", err, *ops)
		}
		if got := readFile(t, name+"_old"); !bytes.Equal(got, firstBytes) {
			t.Fatalf("level.dat_old does not hold the previous level.dat (ops %q)", *ops)
		}
	})

	t.Run("sync dir", func(t *testing.T) {
		// The renames are done when the directory fails to sync: the error
		// is returned, and level.dat holds the new bytes.
		dir := t.TempDir()
		name := filepath.Join(dir, "level.dat")
		if err := os.WriteFile(name, firstBytes, 0644); err != nil {
			t.Fatal(err)
		}
		fsys, ops := faultyFS("sync dir")
		if err := fsys.writeFileAtomic(name, secondBuf.Bytes()); err == nil || !strings.Contains(err.Error(), "injected") {
			t.Fatalf("writeFileAtomic error = %v, want the injected failure", err)
		}
		want := []string{"create level.dat_new", "rename level.dat level.dat_old", "rename level.dat_new level.dat", "sync dir"}
		if strings.Join(*ops, ",") != strings.Join(want, ",") {
			t.Fatalf("ops = %q, want %q", *ops, want)
		}
		if got := readFile(t, name); !bytes.Equal(got, secondBuf.Bytes()) {
			t.Fatal("level.dat does not hold the new bytes")
		}
		if got := readFile(t, name+"_old"); !bytes.Equal(got, firstBytes) {
			t.Fatal("level.dat_old does not hold the previous level.dat")
		}
	})
}

// TestReadCorruptHeader checks that Read returns an error rather than
// panicking or allocating what a corrupt header asks for.
func TestReadCorruptHeader(t *testing.T) {
	for _, length := range []int32{-1, -1 << 31, 1<<31 - 1, 100} {
		var buf bytes.Buffer
		_ = binary.Write(&buf, binary.LittleEndian, header{StorageVersion: Version, FileLength: length})
		buf.WriteString("short")
		if _, err := Read(&buf); err == nil {
			t.Errorf("Read with length %v of 5 bytes succeeded", length)
		}
	}
	for _, b := range [][]byte{nil, {1, 2, 3}} {
		if _, err := Read(bytes.NewReader(b)); err == nil {
			t.Errorf("Read of %d bytes succeeded", len(b))
		}
	}
}
