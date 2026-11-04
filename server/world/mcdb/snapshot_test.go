package mcdb

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world/mcdb/leveldat"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/journal"
	"github.com/df-mc/goleveldb/leveldb/opt"
	"github.com/df-mc/goleveldb/leveldb/util"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/google/uuid"
)

// churnOptions make LevelDB rotate its journal, flush and compact all the
// time: a 64 KiB write buffer, 128 KiB tables and compactions from two
// tables at level 0.
func churnOptions() *opt.Options {
	return &opt.Options{WriteBuffer: 64 << 10, CompactionTableSize: 128 << 10, CompactionL0Trigger: 2}
}

// openChurn opens a DB in dir with churnOptions.
func openChurn(t testing.TB, dir string) *DB {
	t.Helper()
	db, err := Config{Log: slog.New(slog.DiscardHandler), LDBOptions: churnOptions()}.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// The writes of the writer of TestSnapshotUnderWrites: batch n sets
// stressCounter to n, writes stressPerBatch keys of a space of stressKeys,
// each to a value of its own, and the record of checkpoint n.
const (
	stressCounter  = "stress:counter"
	stressKeys     = 1000
	stressPerBatch = 10
)

func stressKey(i int) []byte { return fmt.Appendf(nil, "stress:%04d", i) }

// stressValue is the value batch n writes to key i: 64 to 575 bytes of
// noise that compresses poorly, so that tables fill.
func stressValue(n uint64, i int) []byte {
	r := rand.New(rand.NewPCG(n, uint64(i)))
	b := make([]byte, 64+r.IntN(512))
	for j := range b {
		b[j] = byte(r.Uint32())
	}
	return b
}

// stressLevelDat is the level.dat the records of checkpoints of the writer
// of TestSnapshotUnderWrites hold.
var stressLevelDat = sync.OnceValue(func() []byte {
	var d leveldat.Data
	d.FillDefault()
	var l leveldat.LevelDat
	if err := l.Marshal(d); err != nil {
		panic(err)
	}
	var buf bytes.Buffer
	if err := l.Write(&buf); err != nil {
		panic(err)
	}
	return buf.Bytes()
})

// stressBatch returns batch n.
func stressBatch(n uint64) *leveldb.Batch {
	b := new(leveldb.Batch)
	b.Put([]byte(stressCounter), binary.LittleEndian.AppendUint64(nil, n))
	for j := range stressPerBatch {
		i := int((n*stressPerBatch + uint64(j)) % stressKeys)
		b.Put(stressKey(i), stressValue(n, i))
	}
	rec, err := nbt.MarshalEncoding(checkpointRecord{ID: int64(n), Time: int64(n), CurrentTick: int64(n), Dimensions: map[string]int64{}, LevelDat: stressLevelDat()}, nbt.LittleEndian)
	if err != nil {
		panic(err)
	}
	b.Put([]byte(keyCheckpoint), rec)
	return b
}

// checkStress checks that the database of the copy in dir holds the batches
// of the writer up to its counter whole, and none after: every key the
// value the last of those batches wrote to it.
func checkStress(dir string, res SnapshotResult) error {
	ldb, err := leveldb.OpenFile(filepath.Join(dir, "db"), &opt.Options{ReadOnly: true, Strict: opt.StrictAll})
	if err != nil {
		return err
	}
	defer ldb.Close()
	b, err := ldb.Get([]byte(stressCounter), nil)
	if err != nil {
		return fmt.Errorf("read the counter: %w", err)
	}
	n := binary.LittleEndian.Uint64(b)
	if res.Checkpoint.ID != n {
		return fmt.Errorf("snapshot of batch %d says checkpoint %d", n, res.Checkpoint.ID)
	}
	last := make([]uint64, stressKeys)
	for m := uint64(1); m <= n; m++ {
		for j := range stressPerBatch {
			last[(m*stressPerBatch+uint64(j))%stressKeys] = m
		}
	}
	for i, m := range last {
		v, err := ldb.Get(stressKey(i), nil)
		switch {
		case m == 0 && errors.Is(err, leveldb.ErrNotFound):
		case m == 0:
			return fmt.Errorf("key %d is set at batch %d, which writes none of it", i, n)
		case err != nil:
			return fmt.Errorf("batch %d: key %d (of batch %d): %w", n, i, m, err)
		case !bytes.Equal(v, stressValue(m, i)):
			return fmt.Errorf("batch %d: key %d does not hold the value of batch %d", n, i, m)
		}
	}
	return nil
}

// TestSnapshotUnderWrites snapshots a database 30 times while a writer
// commits a batch about every 200 microseconds, every 25th through a
// transaction, with options that rotate, flush and compact all the time.
// Each step of a copy waits up to 2 ms, as on a busy disk, so that flushes
// and compactions fall between them. Every copy must hold the batches up to
// the one it holds whole, and none after; the copies that raced the
// database are made again, which the test logs.
func TestSnapshotUnderWrites(t *testing.T) {
	db := openChurn(t, t.TempDir())
	defer db.Close()
	snapshotHook = func(string) { time.Sleep(rand.N(2 * time.Millisecond)) }
	defer func() { snapshotHook = nil }()

	stop := make(chan struct{})
	var written atomic.Uint64
	writerErr := make(chan error, 1)
	go func() {
		for n := uint64(1); ; n++ {
			select {
			case <-stop:
				writerErr <- nil
				return
			default:
			}
			var err error
			if n%25 == 0 {
				var tr *leveldb.Transaction
				if tr, err = db.ldb.OpenTransaction(); err == nil {
					if err = tr.Write(stressBatch(n), nil); err == nil {
						err = tr.Commit()
					} else {
						tr.Discard()
					}
				}
			} else {
				err = db.ldb.Write(stressBatch(n), nil)
			}
			if err != nil {
				writerErr <- err
				return
			}
			written.Store(n)
			time.Sleep(200 * time.Microsecond)
		}
	}()
	for written.Load() < 50 {
		time.Sleep(time.Millisecond)
	}

	dst := t.TempDir()
	races := 0
	var reasons []string
	for i := range 30 {
		dir := filepath.Join(dst, fmt.Sprint(i))
		res, err := db.Snapshot(context.Background(), dir, SnapshotOptions{})
		if err != nil {
			close(stop)
			t.Fatalf("snapshot %d: %v", i, err)
		}
		if err := checkStress(dir, res); err != nil {
			close(stop)
			t.Fatalf("snapshot %d is not consistent: %v", i, err)
		}
		races += len(res.Raced)
		reasons = append(reasons, res.Raced...)
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	if err := <-writerErr; err != nil {
		t.Fatal(err)
	}
	t.Logf("30 snapshots consistent over %d batches; %d copies raced the database and were made again", written.Load(), races)
	for _, r := range reasons {
		t.Logf("raced: %v", r)
	}
}

// putAndFlush writes key and flushes the memdb into a table, which rotates
// the journal and deletes the one flushed.
func putAndFlush(t *testing.T, db *DB, key string) {
	t.Helper()
	if err := db.ldb.Put([]byte(key), []byte("value"), nil); err != nil {
		t.Fatal(err)
	}
	if err := db.ldb.CompactRange(util.Range{Start: []byte(key), Limit: []byte(key + "\x00")}); err != nil {
		t.Fatal(err)
	}
}

// snapshotOnce makes a snapshot of db with hook as the snapshotHook, and
// checks that the copy holds every key of keys.
func snapshotOnce(t *testing.T, db *DB, hook func(step string), keys ...string) SnapshotResult {
	t.Helper()
	snapshotHook = hook
	defer func() { snapshotHook = nil }()
	dir := filepath.Join(t.TempDir(), "copy")
	res, err := db.Snapshot(context.Background(), dir, SnapshotOptions{Verify: VerifyFull})
	if err != nil {
		t.Fatal(err)
	}
	ldb, err := leveldb.OpenFile(filepath.Join(dir, "db"), &opt.Options{ReadOnly: true, Strict: opt.StrictAll})
	if err != nil {
		t.Fatal(err)
	}
	defer ldb.Close()
	for _, key := range keys {
		if _, err := ldb.Get([]byte(key), nil); err != nil {
			t.Errorf("the copy lacks %q: %v", key, err)
		}
	}
	return res
}

// TestSnapshotRetriesRaces makes each race Snapshot detects happen once, at
// a step of its first copy, and checks that the copy is made again and
// holds every write made before it.
func TestSnapshotRetriesRaces(t *testing.T) {
	db := openChurn(t, t.TempDir())
	defer db.Close()
	putAndFlush(t, db, "base")

	t.Run("journal flushed", func(t *testing.T) {
		// The journal the manifest copied names is flushed into a table
		// the copy does not name, and deleted, before it is copied.
		once := sync.Once{}
		res := snapshotOnce(t, db, func(step string) {
			if step == "manifest" {
				once.Do(func() { putAndFlush(t, db, "flushed") })
			}
		}, "base", "flushed")
		if len(res.Raced) != 1 || !strings.Contains(res.Raced[0], "was flushed before it was copied") {
			t.Fatalf("raced %q, want one flushed journal", res.Raced)
		}
	})
	t.Run("transaction committed", func(t *testing.T) {
		// With the memdb empty, a transaction rotates no journal: its
		// writes go into tables that the manifest copied does not name,
		// and the writes after it into the journal copied, which is all
		// there. Only their sequence numbers tell.
		putAndFlush(t, db, "empty memdb")
		once := sync.Once{}
		res := snapshotOnce(t, db, func(step string) {
			if step != "manifest" {
				return
			}
			once.Do(func() {
				tr, err := db.ldb.OpenTransaction()
				if err != nil {
					t.Fatal(err)
				}
				if err := tr.Put([]byte("in transaction"), []byte("value"), nil); err != nil {
					t.Fatal(err)
				}
				if err := tr.Commit(); err != nil {
					t.Fatal(err)
				}
				if err := db.ldb.Put([]byte("after transaction"), []byte("value"), nil); err != nil {
					t.Fatal(err)
				}
			})
		}, "base", "in transaction", "after transaction")
		if len(res.Raced) != 1 || !strings.Contains(res.Raced[0], "are missing") {
			t.Fatalf("raced %q, want one gap of sequence numbers", res.Raced)
		}
	})
	t.Run("table deleted", func(t *testing.T) {
		// A table the manifest copied names, newer than the version
		// pinned, is gone when it is linked. It is moved away for the
		// first link and back after.
		var hidden string
		linkFile = func(from, to string) error {
			if hidden == "" && strings.HasSuffix(from, ".ldb") {
				hidden = from
				if err := os.Rename(from, from+".hidden"); err != nil {
					t.Fatal(err)
				}
			}
			return os.Link(from, to)
		}
		defer func() { linkFile = os.Link }()
		res := snapshotOnce(t, db, func(step string) {
			if step == "pinned" && hidden != "" {
				if err := os.Rename(hidden+".hidden", hidden); err != nil && !errors.Is(err, fs.ErrNotExist) {
					t.Fatal(err)
				}
			}
		}, "base", "flushed")
		if len(res.Raced) != 1 || !strings.Contains(res.Raced[0], "was deleted before it was linked") {
			t.Fatalf("raced %q, want one table deleted", res.Raced)
		}
	})
	t.Run("gives up", func(t *testing.T) {
		snapshotHook = func(step string) {
			if step == "manifest" {
				putAndFlush(t, db, "again")
			}
		}
		defer func() { snapshotHook = nil }()
		dir := filepath.Join(t.TempDir(), "copy")
		_, err := db.Snapshot(context.Background(), dir, SnapshotOptions{Retries: 2})
		if err == nil || !strings.Contains(err.Error(), "copied 3 times") {
			t.Fatalf("Snapshot racing every copy: %v, want it to give up after 3", err)
		}
		if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("the copy given up is left: %v", err)
		}
	})
}

// TestSnapshotTakesNoLock snapshots a database while every lock of the DB
// is held, as by writers in the middle of their writes, a SavePlayer among
// them stuck in its write; then saves players, world data and a commit at
// each step of a snapshot, which must not wait for it.
func TestSnapshotTakesNoLock(t *testing.T) {
	db := openChurn(t, t.TempDir())
	defer db.Close()
	putAndFlush(t, db, "base")

	id := uuid.New()
	entered, release := make(chan struct{}), make(chan struct{})
	db.directWrite = func() {
		close(entered)
		<-release
	}
	saved := make(chan error, 1)
	go func() { saved <- db.SavePlayer(id, []byte("record")) }()
	<-entered
	db.directWrite = nil
	db.lmu.Lock()
	db.wmu.Lock()
	db.emu.Lock()

	done := make(chan error, 1)
	go func() {
		_, err := db.Snapshot(context.Background(), filepath.Join(t.TempDir(), "copy"), SnapshotOptions{})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a snapshot waited for a lock of the DB held by a writer")
	}
	db.emu.Unlock()
	db.wmu.Unlock()
	db.lmu.Unlock()
	close(release)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}

	var steps []string
	n := 0
	res := snapshotOnce(t, db, func(step string) {
		steps = append(steps, step)
		n++
		finished := make(chan error, 1)
		go func() {
			err := db.SavePlayer(id, fmt.Appendf(nil, "record %d", n))
			if err == nil {
				err = db.SaveWorldData("test", []byte(step))
			}
			if err == nil {
				c := db.BeginCommit().(*Commit)
				if err = c.SaveWorldData("commit", []byte(step)); err == nil {
					err = c.Commit(uint64(n))
				}
			}
			finished <- err
		}()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("a save at the step %v of a snapshot waited for it", step)
		}
	}, "base")
	if !slices.Equal(steps, []string{"pinned", "manifest", "journals", "tables"}) {
		t.Fatalf("steps %v", steps)
	}
	// The commits saved before the journals were copied are in the copy,
	// and none after.
	if res.Checkpoint.ID != 2 {
		t.Fatalf("the copy holds checkpoint %d, want 2", res.Checkpoint.ID)
	}
}

// TestSnapshotCrossDevice snapshots a database whose tables cannot be
// hard-linked into the copy: linking fails as across file systems, and,
// with ADAMANT_SNAPSHOT_OTHER_FS set to a folder on another file system
// than the temporary folder, into it. The tables are copied, and the copy
// verifies.
func TestSnapshotCrossDevice(t *testing.T) {
	src := t.TempDir()
	db := openChurn(t, src)
	defer db.Close()
	for i := range 200 {
		if err := db.ldb.Write(stressBatch(uint64(i+1)), nil); err != nil {
			t.Fatal(err)
		}
	}
	check := func(t *testing.T, dir string) {
		res, err := db.Snapshot(context.Background(), dir, SnapshotOptions{Verify: VerifyFull})
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		if res.Linked != 0 {
			t.Fatalf("%d tables linked across file systems", res.Linked)
		}
		if err := checkStress(dir, res); err != nil {
			t.Fatal(err)
		}
		tables := 0
		entries, err := os.ReadDir(filepath.Join(dir, "db"))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".ldb") {
				continue
			}
			tables++
			a, errA := os.Stat(filepath.Join(src, "db", e.Name()))
			b, errB := os.Stat(filepath.Join(dir, "db", e.Name()))
			if errA == nil && errB == nil && os.SameFile(a, b) {
				t.Fatalf("%v is linked", e.Name())
			}
		}
		if tables == 0 {
			t.Fatal("no table in the copy")
		}
		if res.Copied < res.Bytes {
			t.Fatalf("copied %d bytes of %d", res.Copied, res.Bytes)
		}
	}
	t.Run("EXDEV", func(t *testing.T) {
		calls := 0
		linkFile = func(from, to string) error {
			calls++
			return &os.LinkError{Op: "link", Old: from, New: to, Err: syscall.EXDEV}
		}
		defer func() { linkFile = os.Link }()
		check(t, filepath.Join(t.TempDir(), "copy"))
		if calls != 1 {
			t.Fatalf("linking tried %d times, want once", calls)
		}
	})
	t.Run("other file system", func(t *testing.T) {
		other := os.Getenv("ADAMANT_SNAPSHOT_OTHER_FS")
		if other == "" {
			t.Skip("ADAMANT_SNAPSHOT_OTHER_FS names no folder on another file system")
		}
		dir, err := os.MkdirTemp(other, "snapshot-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		check(t, filepath.Join(dir, "copy"))
	})
	t.Run("same file system", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "copy")
		res, err := db.Snapshot(context.Background(), dir, SnapshotOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Linked == 0 {
			t.Fatal("no table linked on the same file system")
		}
	})
}

// TestSnapshotCopiesTheWorld checks the files of the world beside its
// database: level.dat and level.dat_old, levelname.txt from level.dat, the
// packs, and nothing of LevelDB's own.
func TestSnapshotCopiesTheWorld(t *testing.T) {
	src := t.TempDir()
	db := openChurn(t, src)
	db.Settings().Name = "Snapshot world"
	db.SaveSettings(db.Settings())
	if err := db.WriteSettings(); err != nil {
		t.Fatal(err)
	}
	if err := db.WriteSettings(); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for path, data := range map[string]string{
		"world_icon.jpeg":                    "icon",
		"world_behavior_packs.json":          "[]",
		"behavior_packs/pack/manifest.json":  "{}",
		"resource_packs/pack/textures/a.png": "png",
		"levelname.txt":                      "half writ",
	} {
		p := filepath.Join(src, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(t.TempDir(), "copy")
	if _, err := db.Snapshot(context.Background(), dir, SnapshotOptions{}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"world_icon.jpeg":                    "icon",
		"world_behavior_packs.json":          "[]",
		"behavior_packs/pack/manifest.json":  "{}",
		"resource_packs/pack/textures/a.png": "png",
		"levelname.txt":                      "Snapshot world",
	} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%v holds %q, want %q", path, got, want)
		}
	}
	for _, path := range []string{"level.dat", "level.dat_old", "db/CURRENT"} {
		if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
			t.Error(err)
		}
	}
	for _, path := range []string{"db/LOG", "level.dat_new"} {
		if _, err := os.Stat(filepath.Join(dir, path)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%v copied: %v", path, err)
		}
	}
	// The copy opens as a world.
	cp, err := Config{Log: slog.New(slog.DiscardHandler)}.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if name := cp.Settings().Name; name != "Snapshot world" {
		t.Errorf("the copy is named %q", name)
	}
	if err := cp.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestScanLogTrimsTornTail writes records of all sizes to a log as LevelDB
// does, and checks for every length of it that scanLog keeps exactly the
// records whole in it.
func TestScanLogTrimsTornTail(t *testing.T) {
	var buf bytes.Buffer
	w := journal.NewWriter(&buf)
	var ends []int64
	sizes := []int{0, 1, 100, logBlockSize - 2*logHeaderSize, 5, logBlockSize, 3 * logBlockSize, 7, 1000}
	for i, size := range sizes {
		rw, err := w.Next()
		if err != nil {
			t.Fatal(err)
		}
		rec := bytes.Repeat([]byte{byte(i)}, size)
		if _, err := rw.Write(rec); err != nil {
			t.Fatal(err)
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
		ends = append(ends, int64(buf.Len()))
	}
	data := buf.Bytes()
	// Every length near the end of a record or of a block, and some
	// between.
	var lengths []int64
	for n := int64(0); n <= int64(len(data)); n += 997 {
		lengths = append(lengths, n)
	}
	for _, e := range append(slices.Clone(ends), 0, logBlockSize, 2*logBlockSize, 3*logBlockSize, 4*logBlockSize, 5*logBlockSize) {
		for n := max(0, e-logHeaderSize-2); n <= min(int64(len(data)), e+logHeaderSize+2); n++ {
			lengths = append(lengths, n)
		}
	}
	for _, n := range lengths {
		var got []int
		whole, err := scanLog(context.Background(), bytes.NewReader(data[:n]), n, nil, -1, func(rec []byte) error {
			got = append(got, len(rec))
			return nil
		})
		if err != nil {
			t.Fatalf("at %d: %v", n, err)
		}
		k := 0
		for k < len(ends) && ends[k] <= n {
			k++
		}
		want := int64(0)
		if k > 0 {
			want = ends[k-1]
		}
		if whole != want || !slices.Equal(got, sizes[:k]) {
			t.Fatalf("at %d of %d: whole %d with records %v, want %d with %v", n, len(data), whole, got, want, sizes[:k])
		}
	}
	corrupt := slices.Clone(data)
	corrupt[ends[1]+logHeaderSize+10] ^= 1
	if _, err := scanLog(context.Background(), bytes.NewReader(corrupt), int64(len(corrupt)), nil, -1, func([]byte) error { return nil }); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("a flipped byte: %v", err)
	}
}

// verifiedWorld makes a world in dir with tables, a journal and checkpoint
// 7, closes it and returns the names of its tables and its manifest.
func verifiedWorld(t *testing.T, dir string) (tables []string, manifest string) {
	t.Helper()
	db := openChurn(t, dir)
	for i := range 300 {
		if err := db.ldb.Write(stressBatch(uint64(i+1)), nil); err != nil {
			t.Fatal(err)
		}
	}
	c := db.BeginCommit().(*Commit)
	if err := c.SaveWorldData("verify", []byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := c.Commit(7); err != nil {
		t.Fatal(err)
	}
	// Small writes after it leave the journal with batches: a journal
	// rotates only once full.
	for i := range 3 {
		if err := db.ldb.Put(fmt.Appendf(nil, "after %d", i), []byte("value"), nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// The tables the manifest names: the folder may hold one LevelDB has
	// yet to delete, which no version reads.
	current, err := os.ReadFile(filepath.Join(dir, "db", "CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	manifest = strings.TrimSuffix(string(current), "\n")
	var m manifestState
	if err := scanWhole(filepath.Join(dir, "db", manifest), -1, m.apply); err != nil {
		t.Fatal(err)
	}
	for _, tb := range m.sortedTables() {
		tables = append(tables, fmt.Sprintf("%06d.ldb", tb.num))
	}
	if len(tables) < 2 {
		t.Fatalf("%d tables", len(tables))
	}
	return tables, manifest
}

// TestVerifyDetectsCorruption corrupts copies of a world each in one way and
// checks that VerifyBackup tells.
func TestVerifyDetectsCorruption(t *testing.T) {
	base := filepath.Join(t.TempDir(), "world")
	tables, manifest := verifiedWorld(t, base)
	r, err := VerifyBackup(base, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != 7 {
		t.Fatalf("checkpoint %d, want 7", r.ID)
	}
	journals, _ := filepath.Glob(filepath.Join(base, "db", "*.log"))
	if len(journals) == 0 {
		t.Fatal("no journal")
	}
	slices.Sort(journals)
	// The journal the manifest names is the oldest left; the newest has
	// the last writes.
	oldest, newest := filepath.Base(journals[0]), filepath.Base(journals[len(journals)-1])

	for _, tc := range []struct {
		name    string
		full    bool
		corrupt func(dir string) error
		want    string
	}{
		{"flipped byte in a table", true, func(dir string) error {
			return flipByte(filepath.Join(dir, "db", tables[0]), 100)
		}, "checksum"},
		{"deleted table", false, func(dir string) error {
			return os.Remove(filepath.Join(dir, "db", tables[len(tables)-1]))
		}, "is missing"},
		{"truncated table", false, func(dir string) error {
			return os.Truncate(filepath.Join(dir, "db", tables[0]), 100)
		}, "bytes,"},
		{"truncated manifest", false, func(dir string) error {
			fi, err := os.Stat(filepath.Join(dir, "db", manifest))
			if err != nil {
				return err
			}
			return os.Truncate(filepath.Join(dir, "db", manifest), fi.Size()-3)
		}, "after its last whole record"},
		{"flipped byte in the manifest", false, func(dir string) error {
			return flipByte(filepath.Join(dir, "db", manifest), 20)
		}, "checksum"},
		{"deleted journal", false, func(dir string) error {
			return os.Remove(filepath.Join(dir, "db", oldest))
		}, "is missing"},
		{"flipped byte in a journal", false, func(dir string) error {
			return flipByte(filepath.Join(dir, "db", newest), 20)
		}, "checksum"},
		{"CURRENT", false, func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "db", "CURRENT"), []byte("MANIFEST-999999\n"), 0644)
		}, "MANIFEST-999999"},
		{"level.dat", false, func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "level.dat"), []byte("not a level.dat"), 0644)
		}, "level.dat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "world")
			if err := os.CopyFS(dir, os.DirFS(base)); err != nil {
				t.Fatal(err)
			}
			if err := tc.corrupt(dir); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyBackup(dir, tc.full); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("VerifyBackup: %v, want an error with %q", err, tc.want)
			}
		})
	}
	t.Run("no checkpoint", func(t *testing.T) {
		dir := t.TempDir()
		db := openChurn(t, dir)
		if err := db.ldb.Put([]byte("key"), []byte("value"), nil); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		r, err := VerifyBackup(dir, true)
		if err != nil || r.ID != 0 {
			t.Fatalf("a world never checkpointed: %+v, %v", r, err)
		}
	})
}

// flipByte flips a bit of the byte at off in the file at path.
func flipByte(path string, off int64) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if off >= int64(len(b)) {
		return fmt.Errorf("%v has %d bytes", path, len(b))
	}
	b[off] ^= 0x10
	return os.WriteFile(path, b, 0644)
}

// benchWorld returns the DB of a world of about ADAMANT_BENCH_MIB MiB of
// tables (64 by default) in the folder ADAMANT_BENCH_DIR, made there if it
// holds none of that size, or in a temporary folder if it is not set. Its
// values compress about as well as chunks do, to about half.
func benchWorld(b *testing.B) *DB {
	b.Helper()
	mib := 64
	if v := os.Getenv("ADAMANT_BENCH_MIB"); v != "" {
		if _, err := fmt.Sscan(v, &mib); err != nil {
			b.Fatal(err)
		}
	}
	dir := b.TempDir()
	if v := os.Getenv("ADAMANT_BENCH_DIR"); v != "" {
		dir = filepath.Join(v, fmt.Sprintf("snapshot-%dmib", mib))
	}
	marker := filepath.Join(dir, "bench-made")
	_, err := os.Stat(marker)
	made := err == nil
	db, err := Config{Log: slog.New(slog.DiscardHandler)}.Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	if made {
		return db
	}
	r := rand.New(rand.NewPCG(1, 2))
	value := make([]byte, 8<<10)
	for written, n := 0, 0; written < mib<<20; {
		batch := new(leveldb.Batch)
		for range 128 {
			for j := range value {
				value[j] = 'a' + byte(r.IntN(16))
			}
			batch.Put(fmt.Appendf(nil, "bench:%08d", n), value)
			n++
			written += len(value) / 2
		}
		if err := db.ldb.Write(batch, nil); err != nil {
			b.Fatal(err)
		}
	}
	if err := os.WriteFile(marker, nil, 0644); err != nil {
		b.Fatal(err)
	}
	return db
}

// BenchmarkSnapshot snapshots the world of benchWorld: with its tables
// linked, copied as across file systems, and linked then verified, by its
// record and in full. Besides the time of each, it reports, per GiB of the
// world, the wall time and the CPU time of the process, which on a busy
// machine leaves out the time spent waiting for a CPU.
func BenchmarkSnapshot(b *testing.B) {
	db := benchWorld(b)
	defer db.Close()
	dst := b.TempDir()
	if v := os.Getenv("ADAMANT_BENCH_DIR"); v != "" {
		var err error
		if dst, err = os.MkdirTemp(v, "snapshot-copy-"); err != nil {
			b.Fatal(err)
		}
		defer os.RemoveAll(dst)
	}
	for _, bc := range []struct {
		name   string
		copy   bool
		verify Verification
	}{
		{"link", false, VerifyOff},
		{"copy", true, VerifyOff},
		{"link+record", false, VerifyRecord},
		{"link+full", false, VerifyFull},
	} {
		b.Run(bc.name, func(b *testing.B) {
			if bc.copy {
				linkFile = func(from, to string) error {
					return &os.LinkError{Op: "link", Old: from, New: to, Err: syscall.EXDEV}
				}
				defer func() { linkFile = os.Link }()
			}
			var (
				wall, cpu time.Duration
				bytes     int64
				i         int
			)
			for b.Loop() {
				dir := filepath.Join(dst, fmt.Sprint(i))
				i++
				start, startCPU := time.Now(), processCPUTime()
				res, err := db.Snapshot(context.Background(), dir, SnapshotOptions{Verify: bc.verify})
				wall += time.Since(start)
				cpu += processCPUTime() - startCPU
				if err != nil {
					b.Fatal(err)
				}
				bytes = res.Bytes
				b.StopTimer()
				if err := os.RemoveAll(dir); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
			gib := float64(bytes) / (1 << 30)
			b.ReportMetric(float64(bytes)/(1<<20), "MiB")
			b.ReportMetric(wall.Seconds()*1000/float64(i)/gib, "wall-ms/GiB")
			b.ReportMetric(cpu.Seconds()*1000/float64(i)/gib, "cpu-ms/GiB")
		})
	}
}
