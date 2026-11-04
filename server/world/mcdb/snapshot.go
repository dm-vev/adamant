package mcdb

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/world/mcdb/leveldat"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/opt"
	"github.com/df-mc/goleveldb/leveldb/util"
	"github.com/dm-vev/adamant/net/nbt"
)

// CheckpointRecord describes the last checkpoint a world database holds, as
// its record adamant:checkpoint says.
type CheckpointRecord struct {
	// ID is the number of the checkpoint, 0 if the database records none.
	ID uint64
	// Time is when the checkpoint was committed, to the second.
	Time time.Time
	// CurrentTick is the current tick of the world the checkpoint saved.
	CurrentTick int64
}

// recordOf returns the CheckpointRecord of r.
func recordOf(r checkpointRecord) CheckpointRecord {
	return CheckpointRecord{ID: uint64(r.ID), Time: time.Unix(r.Time, 0).UTC(), CurrentTick: r.CurrentTick}
}

// LastCheckpoint returns the record of the last checkpoint the DB
// committed. Its ID is 0 if the DB holds none.
func (db *DB) LastCheckpoint() (CheckpointRecord, error) {
	r, ok, err := db.lastCheckpoint()
	if err != nil || !ok {
		return CheckpointRecord{}, err
	}
	return recordOf(r), nil
}

// Verification is how much of a copy of a world is read to verify it.
type Verification uint8

const (
	// VerifyRecord opens the database of the copy read-only, replaying its
	// journals with every checksum checked, and reads and decodes the
	// record of its checkpoint.
	VerifyRecord Verification = iota
	// VerifyFull also reads every key of the database, checking the
	// checksum of each block of its tables. It reads the whole database.
	VerifyFull
	// VerifyOff reads nothing of the copy once it is made.
	VerifyOff
)

// SnapshotOptions holds the optional parameters of DB.Snapshot.
type SnapshotOptions struct {
	// Retries is how many times a copy that raced the database is made
	// again before Snapshot fails. If zero, 8.
	Retries int
	// Verify is how the copy is verified. A copy that fails verification
	// is made again, as one that raced the database. With VerifyOff the
	// result holds no checkpoint.
	Verify Verification
}

// SnapshotResult describes a copy made by DB.Snapshot.
type SnapshotResult struct {
	// Checkpoint is the record of the last checkpoint the copy holds, read
	// from it when it was verified.
	Checkpoint CheckpointRecord
	// Files is the number of files of the copy, and Bytes their size.
	Files int
	Bytes int64
	// Linked is the number of tables of the database hard-linked into the
	// copy rather than copied. Copied is the number of bytes copied.
	Linked int
	Copied int64
	// Raced holds why each copy made before the last was made again: the
	// database changed under it in a way the copy cannot hold.
	Raced []string
	// Took is how long Snapshot took, all attempts included.
	Took time.Duration
}

// Snapshot copies the world in the folder of the DB to dst, which must not
// exist and whose parent must, as it is now, while the DB keeps being read
// and written: level.dat and level.dat_old, then the files of the database,
// then the other files and folders of the world. The tables of the database
// are hard-linked into the copy if dst is on the same file system, and
// copied otherwise. Snapshot takes no lock that the readers or writers of
// the DB take: it reads the files of the database as LevelDB keeps them,
// which are never changed once written but appended to, and keeps the
// tables of the current version from being deleted while it copies them.
//
// The copy holds the database as it was at one instant, every write before
// it and none after, as a crash at that instant would leave it: every
// checkpoint committed is in it whole or not at all. Snapshot checks that it
// does, by the sequence numbers of the writes, and makes the copy again,
// up to SnapshotOptions.Retries times, if the database was changed in a way
// that the copy cannot hold, such as a journal deleted by a flush before it
// was copied. The copy is then verified as SnapshotOptions.Verify says. On
// error, or if ctx is done, Snapshot deletes dst.
func (db *DB) Snapshot(ctx context.Context, dst string, o SnapshotOptions) (SnapshotResult, error) {
	start := time.Now()
	retries := o.Retries
	if retries <= 0 {
		retries = 8
	}
	if _, err := os.Lstat(dst); err == nil {
		return SnapshotResult{}, fmt.Errorf("snapshot: %v exists", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return SnapshotResult{}, fmt.Errorf("snapshot: %w", err)
	}
	var raced []string
	for attempt := 0; ; attempt++ {
		res, err := db.snapshot(ctx, dst, o)
		if err == nil {
			res.Raced, res.Took = raced, time.Since(start)
			return res, nil
		}
		_ = os.RemoveAll(dst)
		var race *raceError
		switch {
		case ctx.Err() != nil:
			return SnapshotResult{}, fmt.Errorf("snapshot: %w", context.Cause(ctx))
		case !errors.As(err, &race):
			return SnapshotResult{}, fmt.Errorf("snapshot: %w", err)
		case attempt >= retries:
			return SnapshotResult{}, fmt.Errorf("snapshot: %w (copied %d times)", err, attempt+1)
		}
		raced = append(raced, race.Error())
		// The database changed under the copy: a flush, a compaction or a
		// new manifest. Let it settle before copying again.
		t := time.NewTimer(10*time.Millisecond + rand.N(90*time.Millisecond))
		select {
		case <-ctx.Done():
			t.Stop()
			return SnapshotResult{}, fmt.Errorf("snapshot: %w", context.Cause(ctx))
		case <-t.C:
		}
	}
}

// raceError is an error of a copy that raced the database, which is made
// again: a file that vanished, sequence numbers missing, or a failed
// verification.
type raceError struct{ err error }

func (e *raceError) Error() string { return e.err.Error() }
func (e *raceError) Unwrap() error { return e.err }

// raced returns a raceError of the error formatted.
func raced(format string, a ...any) error { return &raceError{err: fmt.Errorf(format, a...)} }

// snapshotHook, if not nil, is called by a copy of Snapshot at each of its
// steps, for tests: "pinned" once the tables of the current version are
// pinned, "manifest" once the manifest is copied, "journals" once the
// journals are, and "tables" once the tables are linked or copied.
var snapshotHook func(step string)

// linkFile hard-links a table into a copy. Tests replace it to fail as
// across file systems.
var linkFile = os.Link

// snapshotCopy is one copy of Snapshot.
type snapshotCopy struct {
	ctx      context.Context
	db       *DB
	src, dst string
	res      SnapshotResult
	// copyTables is set once a hard link failed: the remaining tables are
	// copied.
	copyTables bool
	dirs       []string
}

func (db *DB) snapshot(ctx context.Context, dst string, o SnapshotOptions) (SnapshotResult, error) {
	c := &snapshotCopy{ctx: ctx, db: db, src: db.dir, dst: dst}
	if err := c.mkdir(dst); err != nil {
		return SnapshotResult{}, err
	}
	name, err := c.levelDat()
	if err != nil {
		return SnapshotResult{}, err
	}
	if err := c.database(); err != nil {
		return SnapshotResult{}, err
	}
	if err := c.worldFiles(name); err != nil {
		return SnapshotResult{}, err
	}
	for _, dir := range slices.Backward(c.dirs) {
		if err := syncDir(dir); err != nil {
			return SnapshotResult{}, fmt.Errorf("sync %v: %w", dir, err)
		}
	}
	if o.Verify != VerifyOff {
		r, err := VerifyBackup(dst, o.Verify == VerifyFull)
		if err != nil {
			return SnapshotResult{}, &raceError{err: fmt.Errorf("verify the copy: %w", err)}
		}
		c.res.Checkpoint = r
	}
	return c.res, nil
}

// mkdir makes the directory dir of the copy.
func (c *snapshotCopy) mkdir(dir string) error {
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}
	c.dirs = append(c.dirs, dir)
	return nil
}

// levelDat copies level.dat and level.dat_old. Being copied before the
// database, level.dat is never newer than the copy of the database, and
// Open restores one older than its last checkpoint from it. It returns the
// name of the world, from the level.dat copied, or "" if none could be read.
func (c *snapshotCopy) levelDat() (string, error) {
	src, dst := filepath.Join(c.src, "level.dat"), filepath.Join(c.dst, "level.dat")
	copied := false
	for i := 0; ; i++ {
		_, err := c.copyFile(src, dst)
		if err == nil {
			copied = true
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		// level.dat is missing between the two renames that replace it
		// (leveldat.WriteFile), or the world has never written one.
		if _, err := os.Lstat(src + "_new"); errors.Is(err, fs.ErrNotExist) {
			break
		} else if err != nil {
			return "", err
		} else if i == 100 {
			return "", raced("level.dat stayed missing while being written")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := c.copyFile(src+"_old", dst+"_old"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if !copied {
		return "", nil
	}
	var data leveldat.Data
	if err := decodeLevelDat(dst, "level.dat", &data); err != nil {
		return "", nil
	}
	return data.LevelName, nil
}

// database copies the folder db of the world. With the tables of the
// current version pinned by an iterator, so that LevelDB deletes none of
// them meanwhile, it copies the manifest named by CURRENT up to its last
// whole record, then the journals the manifest needs, oldest first, each up
// to its last whole record, checking that their writes follow the
// manifest's and each other's without a gap, and last links or copies the
// tables the manifest copied names.
//
// LevelDB writes a table once and only deletes it, and appends to the
// manifest and the journals records that are each checksummed. The copy of
// the manifest holds the version of some instant; the journals the
// sequence of writes after it, all of each but the newest, which LevelDB
// was still writing; a write is in it whole or not at all. The copy is made
// again if the journal the manifest names was flushed into a table it does
// not name yet and deleted, if sequence numbers are missing, as a
// transaction committed after the manifest was copied leaves them, or if a
// table the manifest names was deleted before it was linked.
func (c *snapshotCopy) database() error {
	srcDB, dstDB := filepath.Join(c.src, "db"), filepath.Join(c.dst, "db")
	if err := c.mkdir(dstDB); err != nil {
		return err
	}
	it := c.db.ldb.NewIterator(nil, &opt.ReadOptions{DontFillCache: true})
	defer it.Release()
	c.step("pinned")

	current, err := os.ReadFile(filepath.Join(srcDB, "CURRENT"))
	if err != nil {
		return fmt.Errorf("read CURRENT: %w", err)
	}
	manifest := strings.TrimSuffix(string(current), "\n")
	if _, ok := manifestNum(manifest); !ok || len(current) == len(manifest) {
		return fmt.Errorf("CURRENT names no manifest: %q", current)
	}
	var m manifestState
	if err := c.copyLog(filepath.Join(srcDB, manifest), filepath.Join(dstDB, manifest), -1, m.apply); errors.Is(err, fs.ErrNotExist) {
		return raced("the manifest %v was replaced before it was copied", manifest)
	} else if err != nil {
		return fmt.Errorf("copy %v: %w", manifest, err)
	}
	if !m.hasJournal || !m.hasSeq {
		return fmt.Errorf("%v names no journal or sequence number", manifest)
	}
	c.step("manifest")

	journals, err := listJournals(srcDB, &m)
	if err != nil {
		return err
	}
	if !slices.Contains(journals, m.journal) {
		// Flushed into a table that the manifest copied does not name:
		// LevelDB deletes a journal once a newer manifest holds its writes.
		return raced("the journal %06d.log that %v names was flushed before it was copied", m.journal, manifest)
	}
	seq := sequence{next: m.seq + 1}
	for _, num := range journals {
		name := fmt.Sprintf("%06d.log", num)
		err := c.copyLog(filepath.Join(srcDB, name), filepath.Join(dstDB, name), batchHeaderLen, seq.follow)
		if errors.Is(err, fs.ErrNotExist) {
			return raced("the journal %v was deleted before it was copied", name)
		} else if err != nil {
			return fmt.Errorf("copy %v: %w", name, err)
		}
	}
	c.step("journals")
	if err := c.writeFile(filepath.Join(dstDB, "CURRENT"), current); err != nil {
		return err
	}

	for _, t := range m.sortedTables() {
		if err := c.table(srcDB, dstDB, t); err != nil {
			return err
		}
	}
	c.step("tables")
	return nil
}

// step calls snapshotHook with the step passed.
func (c *snapshotCopy) step(name string) {
	if h := snapshotHook; h != nil {
		h(name)
	}
}

// table links the table t of the folder src into dst, or copies it if it
// cannot be linked.
func (c *snapshotCopy) table(src, dst string, t manifestTable) error {
	name, err := tableName(src, t.num)
	if err != nil {
		return err
	}
	from, to := filepath.Join(src, name), filepath.Join(dst, name)
	if !c.copyTables {
		err := linkFile(from, to)
		if err == nil {
			fi, err := os.Lstat(to)
			if err != nil {
				return err
			}
			if fi.Size() != t.size {
				return raced("the table %v is %d bytes, the manifest says %d", name, fi.Size(), t.size)
			}
			c.res.Linked++
			c.res.Files++
			c.res.Bytes += t.size
			return nil
		}
		if _, serr := os.Lstat(from); errors.Is(serr, fs.ErrNotExist) {
			return raced("the table %v was deleted before it was linked", name)
		}
		// Another file system, or one without hard links: the tables are
		// copied, while they stay pinned.
		c.copyTables = true
		if !c.db.copiesTables.Swap(true) {
			c.db.conf.Log.Info("Snapshots copy the tables of the world: they cannot be hard-linked into the copy.", "dst", c.dst, "error", err)
		}
	}
	n, err := c.copyFile(from, to)
	if errors.Is(err, fs.ErrNotExist) {
		return raced("the table %v was deleted before it was copied", name)
	} else if err != nil {
		return err
	}
	if n != t.size {
		return raced("the table %v is %d bytes, the manifest says %d", name, n, t.size)
	}
	return nil
}

// tableName returns the name of the table num in the folder dir: LevelDB
// names tables NNNNNN.ldb, and once named them NNNNNN.sst.
func tableName(dir string, num int64) (string, error) {
	name := fmt.Sprintf("%06d.ldb", num)
	if _, err := os.Lstat(filepath.Join(dir, name)); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return name, err
	}
	old := fmt.Sprintf("%06d.sst", num)
	if _, err := os.Lstat(filepath.Join(dir, old)); err == nil {
		return old, nil
	}
	return name, nil
}

// worldFiles copies the files and folders of the world beside its
// database: levelname.txt, written with name if not "", world_icon.jpeg,
// the pack lists, behavior_packs and resource_packs, and any other. LOCK and
// LOG are LevelDB's own and stay out.
func (c *snapshotCopy) worldFiles(name string) error {
	entries, err := os.ReadDir(c.src)
	if err != nil {
		return err
	}
	dst, _ := filepath.Abs(c.dst)
	for _, e := range entries {
		switch e.Name() {
		case "db", "level.dat", "level.dat_old", "level.dat_new":
			continue
		case "levelname.txt":
			if name != "" {
				// mcdb writes it in place: a copy could catch it half
				// written. It holds the name in level.dat.
				if err := c.writeFile(filepath.Join(c.dst, e.Name()), []byte(name)); err != nil {
					return err
				}
				continue
			}
		}
		from := filepath.Join(c.src, e.Name())
		if abs, _ := filepath.Abs(from); abs == dst || strings.HasPrefix(dst, abs+string(filepath.Separator)) {
			// The copy itself, if made inside the world.
			continue
		}
		if err := c.copyTree(from, filepath.Join(c.dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// copyTree copies the file or folder src to dst. A symbolic link is copied
// as the file it links to; one to a folder is left out.
func (c *snapshotCopy) copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return c.mkdir(to)
		case d.Type()&fs.ModeSymlink != 0:
			fi, err := os.Stat(path)
			if err != nil || !fi.Mode().IsRegular() {
				return nil
			}
		case !d.Type().IsRegular():
			return nil
		}
		_, err = c.copyFile(path, to)
		return err
	})
}

// copyChunk is how much a copy of a file copies between two checks that the
// context of the snapshot is not done.
const copyChunk = 8 << 20

// copyFile copies the file src to dst, which must not exist, and syncs it.
// It returns the bytes copied.
func (c *snapshotCopy) copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return 0, err
	}
	var n int64
	for {
		if err := c.ctx.Err(); err != nil {
			_ = out.Close()
			return n, err
		}
		k, err := io.CopyN(out, in, copyChunk)
		n += k
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			_ = out.Close()
			return n, err
		}
	}
	return n, c.closeFile(out, n)
}

// writeFile writes data to the file dst of the copy, which must not exist,
// and syncs it.
func (c *snapshotCopy) writeFile(dst string, data []byte) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if _, err := out.Write(data); err != nil {
		_ = out.Close()
		return err
	}
	return c.closeFile(out, int64(len(data)))
}

// closeFile syncs and closes the file f of the copy, of n bytes written.
func (c *snapshotCopy) closeFile(f *os.File, n int64) error {
	err := f.Sync()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	c.res.Files++
	c.res.Bytes += n
	c.res.Copied += n
	return nil
}

// copyLog copies the log src, a journal or a manifest of LevelDB, as long
// as it is when opened, to dst, which must not exist, up to the end of its
// last whole record: a record LevelDB was appending while it was copied is
// left out. f is called with the first keep bytes of each record copied,
// all of them if keep is negative. A chunk whose checksum does not match is
// an error: what LevelDB wrote is never changed.
func (c *snapshotCopy) copyLog(src, dst string, keep int, f func(rec []byte) error) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	n, err := scanLog(c.ctx, in, fi.Size(), out, keep, f)
	if err == nil && n < fi.Size() {
		err = out.Truncate(n)
	}
	if err != nil {
		_ = out.Close()
		return err
	}
	return c.closeFile(out, n)
}

// The format of the logs of LevelDB, its journals and its manifests: blocks
// of logBlockSize bytes, each a sequence of chunks of a header and a payload.
// A record is one full chunk, or a first chunk, middle chunks and a last
// chunk, in consecutive blocks.
const (
	logBlockSize  = 32 << 10
	logHeaderSize = 7

	chunkFull   = 1
	chunkFirst  = 2
	chunkMiddle = 3
	chunkLast   = 4

	// batchHeaderLen is the length of the header of a batch of writes, the
	// record of a journal: its sequence number and its number of writes.
	batchHeaderLen = 12
)

// scanLog reads the log r of size bytes, writing it to w if not nil, and
// calls f with the first keep bytes of each record, all of them if keep is
// negative. It returns the length of the log up to the end of its last
// whole record. The end of the log is as LevelDB leaves it while appending:
// a record whose chunks are not all there, or a chunk not all there, ends
// it.
func scanLog(ctx context.Context, r io.Reader, size int64, w io.Writer, keep int, f func(rec []byte) error) (int64, error) {
	buf := make([]byte, logBlockSize)
	var (
		off, whole int64
		in         bool
		rec        []byte
	)
	for off < size {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n := int(min(logBlockSize, size-off))
		if _, err := io.ReadFull(r, buf[:n]); err != nil {
			return 0, err
		}
		if w != nil {
			if _, err := w.Write(buf[:n]); err != nil {
				return 0, err
			}
		}
		b := buf[:n]
		for i := 0; len(b)-i >= logHeaderSize; {
			sum := binary.LittleEndian.Uint32(b[i:])
			length := int(binary.LittleEndian.Uint16(b[i+4:]))
			typ := b[i+6]
			end := i + logHeaderSize + length
			if end > len(b) {
				if n < logBlockSize {
					// The chunk LevelDB is writing.
					return whole, nil
				}
				return 0, fmt.Errorf("chunk at %d overflows its block", off+int64(i))
			}
			if util.NewCRC(b[i+6:end]).Value() != sum {
				return 0, fmt.Errorf("checksum mismatch of the chunk at %d", off+int64(i))
			}
			payload := b[i+logHeaderSize : end]
			switch {
			case (typ == chunkFull || typ == chunkFirst) && !in:
				rec, in = rec[:0], true
			case (typ == chunkMiddle || typ == chunkLast) && in:
			default:
				return 0, fmt.Errorf("chunk of type %d at %d out of order", typ, off+int64(i))
			}
			if keep < 0 {
				rec = append(rec, payload...)
			} else if len(rec) < keep {
				rec = append(rec, payload[:min(len(payload), keep-len(rec))]...)
			}
			i = end
			if typ == chunkFull || typ == chunkLast {
				in = false
				if err := f(rec); err != nil {
					return 0, fmt.Errorf("record ending at %d: %w", off+int64(i), err)
				}
				whole = off + int64(i)
			}
		}
		off += int64(n)
	}
	return whole, nil
}

// manifestNum parses the name of a manifest, MANIFEST-NNNNNN.
func manifestNum(name string) (int64, bool) {
	num, ok := strings.CutPrefix(name, "MANIFEST-")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(num, 10, 64)
	return n, err == nil && n >= 0
}

// manifestState is the state of a database its manifest records: the
// tables of its version, the journal its writes after them start in, and
// the sequence number of the last write in its tables.
type manifestState struct {
	tables      map[tableKey]int64
	journal     int64
	prevJournal int64
	seq         uint64
	hasJournal  bool
	hasSeq      bool
}

// tableKey is a table of a manifestState: its level and number.
type tableKey struct {
	level uint64
	num   int64
}

// manifestTable is a table of a manifestState with its size.
type manifestTable struct {
	num  int64
	size int64
}

// The fields of the records of a manifest, which goleveldb calls session
// records.
const (
	recComparer       = 1
	recJournalNum     = 2
	recNextFileNum    = 3
	recSeqNum         = 4
	recCompPtr        = 5
	recDelTable       = 6
	recAddTable       = 7
	recPrevJournalNum = 9
)

// errBadRecord is the error of a record of a manifest that cannot be
// decoded.
var errBadRecord = errors.New("manifest record cannot be decoded")

// recordReader decodes the fields of a record of a manifest. Its first
// error stays.
type recordReader struct {
	r   *bytes.Reader
	err error
}

func (d *recordReader) uvarint() uint64 {
	if d.err != nil {
		return 0
	}
	x, err := binary.ReadUvarint(d.r)
	if err != nil {
		d.err = errBadRecord
	}
	return x
}

func (d *recordReader) num() int64 {
	x := int64(d.uvarint())
	if x < 0 {
		d.err = errBadRecord
	}
	return x
}

func (d *recordReader) skipBytes() {
	if n := d.uvarint(); d.err == nil && n > uint64(d.r.Len()) {
		d.err = errBadRecord
	} else if d.err == nil {
		_, _ = d.r.Seek(int64(n), io.SeekCurrent)
	}
}

// apply applies a record of the manifest: its deleted tables, then its
// added tables, as LevelDB does.
func (m *manifestState) apply(rec []byte) error {
	d := &recordReader{r: bytes.NewReader(rec)}
	var deleted []tableKey
	var added []tableKey
	var sizes []int64
	for d.err == nil && d.r.Len() > 0 {
		switch d.uvarint() {
		case recComparer:
			d.skipBytes()
		case recJournalNum:
			m.journal, m.hasJournal = d.num(), true
		case recPrevJournalNum:
			m.prevJournal = d.num()
		case recNextFileNum:
			d.num()
		case recSeqNum:
			m.seq, m.hasSeq = d.uvarint(), true
		case recCompPtr:
			d.uvarint()
			d.skipBytes()
		case recDelTable:
			deleted = append(deleted, tableKey{level: d.uvarint(), num: d.num()})
		case recAddTable:
			added = append(added, tableKey{level: d.uvarint(), num: d.num()})
			sizes = append(sizes, d.num())
			d.skipBytes()
			d.skipBytes()
		default:
			d.err = errBadRecord
		}
	}
	if d.err != nil {
		return d.err
	}
	if m.tables == nil {
		m.tables = map[tableKey]int64{}
	}
	for _, k := range deleted {
		delete(m.tables, k)
	}
	for i, k := range added {
		m.tables[k] = sizes[i]
	}
	return nil
}

// sortedTables returns the tables of m, by number.
func (m *manifestState) sortedTables() []manifestTable {
	tables := make([]manifestTable, 0, len(m.tables))
	for k, size := range m.tables {
		tables = append(tables, manifestTable{num: k.num, size: size})
	}
	slices.SortFunc(tables, func(a, b manifestTable) int { return cmp.Compare(a.num, b.num) })
	return tables
}

// listJournals returns the numbers of the journals in the folder dir that a
// database recorded by m replays when opened, in order: those from its
// journal on.
func listJournals(dir string, m *manifestState) ([]int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var nums []int64
	for _, e := range entries {
		num, ok := strings.CutSuffix(e.Name(), ".log")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(num, 10, 64)
		if err != nil {
			continue
		}
		if n >= m.journal || (m.prevJournal != 0 && n == m.prevJournal) {
			nums = append(nums, n)
		}
	}
	slices.Sort(nums)
	return nums, nil
}

// sequence checks that the batches of writes of the journals follow each
// other: LevelDB numbers each write of a batch, one after the other, from
// the number after the last write of its tables.
type sequence struct{ next uint64 }

// follow checks the batch whose header is rec.
func (s *sequence) follow(rec []byte) error {
	if len(rec) < batchHeaderLen {
		return errors.New("batch header too short")
	}
	seq, n := binary.LittleEndian.Uint64(rec), binary.LittleEndian.Uint32(rec[8:])
	if seq != s.next {
		return raced("the writes from %d to %d are missing", s.next, seq-1)
	}
	s.next = seq + uint64(n)
	return nil
}

// VerifyBackup verifies the copy of a world in the folder dir, as Snapshot
// makes one, and returns the record of the last checkpoint it holds, whose
// ID is 0 if it holds none, as a world that was never checkpointed:
//
//  1. the manifest named by db/CURRENT is read whole; every table it names
//     is there, of the size it records; the journals it needs follow it
//     without a missing write, and end with a whole record;
//  2. the database is opened read-only, the journals replayed with every
//     checksum checked, and the record adamant:checkpoint is read and
//     decoded, with the level.dat it holds and the world's level.dat;
//  3. if full, every key of the database is read, checking the checksum of
//     every block of its tables.
func VerifyBackup(dir string, full bool) (CheckpointRecord, error) {
	dbDir := filepath.Join(dir, "db")
	if err := verifyFiles(dbDir); err != nil {
		return CheckpointRecord{}, err
	}
	ldb, err := leveldb.OpenFile(dbDir, &opt.Options{ReadOnly: true, Strict: opt.StrictAll})
	if err != nil {
		return CheckpointRecord{}, fmt.Errorf("open the database: %w", err)
	}
	defer ldb.Close()
	var rec CheckpointRecord
	b, err := ldb.Get([]byte(keyCheckpoint), nil)
	switch {
	case errors.Is(err, leveldb.ErrNotFound):
	case err != nil:
		return CheckpointRecord{}, fmt.Errorf("read %s: %w", keyCheckpoint, err)
	default:
		var r checkpointRecord
		if err := nbt.UnmarshalEncoding(b, &r, nbt.LittleEndian); err != nil {
			return CheckpointRecord{}, fmt.Errorf("decode %s: %w", keyCheckpoint, err)
		}
		if r.ID <= 0 {
			return CheckpointRecord{}, fmt.Errorf("%s has ID %d", keyCheckpoint, r.ID)
		}
		l, err := leveldat.Read(bytes.NewReader(r.LevelDat))
		if err == nil {
			err = l.Unmarshal(&leveldat.Data{})
		}
		if err != nil {
			return CheckpointRecord{}, fmt.Errorf("decode the level.dat of %s: %w", keyCheckpoint, err)
		}
		rec = recordOf(r)
	}
	if err := decodeLevelDat(filepath.Join(dir, "level.dat"), "level.dat", &leveldat.Data{}); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return CheckpointRecord{}, err
	}
	if full {
		it := ldb.NewIterator(nil, &opt.ReadOptions{DontFillCache: true, Strict: opt.StrictAll | opt.StrictOverride})
		for it.Next() {
		}
		it.Release()
		if err := it.Error(); err != nil {
			return CheckpointRecord{}, fmt.Errorf("read the database: %w", err)
		}
	}
	return rec, nil
}

// verifyFiles checks the files of the database in the folder dir: the
// manifest CURRENT names, read whole, the tables it names, of the sizes it
// records, and the journals after it, each whole and following it.
func verifyFiles(dir string) error {
	current, err := os.ReadFile(filepath.Join(dir, "CURRENT"))
	if err != nil {
		return err
	}
	manifest := strings.TrimSuffix(string(current), "\n")
	if _, ok := manifestNum(manifest); !ok || len(current) == len(manifest) {
		return fmt.Errorf("CURRENT names no manifest: %q", current)
	}
	var m manifestState
	if err := scanWhole(filepath.Join(dir, manifest), -1, m.apply); err != nil {
		return fmt.Errorf("read %v: %w", manifest, err)
	}
	if !m.hasJournal || !m.hasSeq {
		return fmt.Errorf("%v names no journal or sequence number", manifest)
	}
	for _, t := range m.sortedTables() {
		name, err := tableName(dir, t.num)
		if err != nil {
			return err
		}
		fi, err := os.Stat(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("the table %v that %v names is missing", name, manifest)
		} else if err != nil {
			return err
		}
		if fi.Size() != t.size {
			return fmt.Errorf("the table %v is %d bytes, %v says %d", name, fi.Size(), manifest, t.size)
		}
	}
	journals, err := listJournals(dir, &m)
	if err != nil {
		return err
	}
	if !slices.Contains(journals, m.journal) {
		return fmt.Errorf("the journal %06d.log that %v names is missing", m.journal, manifest)
	}
	seq := sequence{next: m.seq + 1}
	for _, num := range journals {
		name := fmt.Sprintf("%06d.log", num)
		if err := scanWhole(filepath.Join(dir, name), batchHeaderLen, seq.follow); err != nil {
			return fmt.Errorf("read %v: %w", name, err)
		}
	}
	return nil
}

// scanWhole reads the log at path with scanLog and checks that it ends with
// a whole record.
func scanWhole(path string, keep int, f func(rec []byte) error) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	n, err := scanLog(context.Background(), in, fi.Size(), nil, keep, f)
	if err != nil {
		return err
	}
	// Nothing follows the last record but, at most, the padding of its
	// block.
	if rest := fi.Size() - n; rest != 0 && (rest >= logHeaderSize || n%logBlockSize+rest != logBlockSize) {
		return fmt.Errorf("%d bytes after its last whole record", rest)
	}
	return nil
}

// syncDir syncs the directory at dir, so that the files made in it reach
// the disk.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		// A directory cannot be opened for syncing on Windows, where NTFS
		// journals the files made itself.
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}
