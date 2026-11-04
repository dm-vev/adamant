package mcdb

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb/leveldat"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/opt"
	"github.com/df-mc/goleveldb/leveldb/util"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/google/uuid"
)

// store is where the writes of the DB go, read back through it: the
// database itself, at once (directStore), or a commit, all together.
type store interface {
	get(key []byte) ([]byte, error)
	put(key, value []byte)
	delete(key []byte)
	// keys calls f with each key the store holds with the prefix passed.
	keys(prefix []byte, f func(key []byte)) error
}

// ldbGetter reads the database itself.
type ldbGetter struct{ db *DB }

func (g ldbGetter) get(key []byte) ([]byte, error) { return g.db.ldb.Get(key, nil) }

// directStore collects writes in a batch, written at once by flush, and
// reads the database.
type directStore struct {
	ldbGetter
	batch *leveldb.Batch
}

func (db *DB) direct() *directStore {
	return &directStore{ldbGetter: ldbGetter{db}, batch: new(leveldb.Batch)}
}

func (s *directStore) put(key, value []byte) { s.batch.Put(key, value) }
func (s *directStore) delete(key []byte)     { s.batch.Delete(key) }
func (s *directStore) keys(prefix []byte, f func(key []byte)) error {
	return s.db.prefixKeys(prefix, f)
}

// flush writes the batch.
func (s *directStore) flush() error { return s.db.writeDirect(s.batch) }

// prefixKeys calls f with each key of the database with the prefix passed.
func (db *DB) prefixKeys(prefix []byte, f func(key []byte)) error {
	it := db.ldb.NewIterator(util.BytesPrefix(prefix), nil)
	defer it.Release()
	for it.Next() {
		f(slices.Clone(it.Key()))
	}
	return it.Error()
}

// writeDirect writes a batch to the database at once, outside any commit.
func (db *DB) writeDirect(b *leveldb.Batch) error {
	if h := db.directWrite; h != nil {
		h()
	}
	return db.ldb.Write(b, nil)
}

// putDirect writes one key to the database at once, outside any commit.
func (db *DB) putDirect(key, value []byte) error {
	b := new(leveldb.Batch)
	b.Put(key, value)
	return db.writeDirect(b)
}

// keyCheckpoint holds the record of the last checkpoint committed
// (checkpointRecord).
const keyCheckpoint = "adamant:checkpoint"

// commitBatchLimit is the size of the largest commit written as one
// leveldb.Batch. A larger one is written through a transaction, which
// goleveldb warns is costly for small writes.
var commitBatchLimit = 32 << 20

// commitHook, if not nil, is called by a commit written through a
// transaction once it wrote half of its writes, for tests that crash it
// there.
var commitHook func()

// transactionChunk is the size of the batches a commit writes its
// transaction in.
var transactionChunk = 4 << 20

// checkpointRecord is the record of a checkpoint the DB holds under
// keyCheckpoint, written in the same commit as the checkpoint: its number,
// the time it was committed, the current tick of each dimension it saved
// and of the settings, and the level.dat of its settings, header included,
// from which Open restores a level.dat older than it.
type checkpointRecord struct {
	ID          int64            `nbt:"id"`
	Time        int64            `nbt:"time"`
	CurrentTick int64            `nbt:"currentTick"`
	Dimensions  map[string]int64 `nbt:"dimensions"`
	LevelDat    []byte           `nbt:"levelDat"`
}

// commitOp is a write of a commit: a put of value, or a delete.
type commitOp struct {
	key, value []byte
	del        bool
}

// Commit holds the writes of one checkpoint to a DB and writes them all
// together: as one leveldb.Batch written with Sync, or, if they take more
// than 32 MiB, through a transaction, whose tables and manifest goleveldb
// syncs. A crash leaves the database with all of them or none. The writes
// made through a Commit are read back through it until it was committed.
// It implements world.Commit: the worlds of a checkpoint write their
// columns, data, maps, dragon fight and position tracking through it, and
// SaveSettings gives it the settings of the checkpoint. Players.Save puts
// the records of players in it too while it is open.
type Commit struct {
	db *DB

	mu    sync.Mutex
	ops   []commitOp
	index map[string]int
	size  int
	// players holds the keys of the records of players saved into the
	// commit by Players.Save, which a failed commit writes again at once.
	players  map[string]bool
	settings *world.Settings
	ticks    map[world.Dimension]int64
	fight    map[string]any
	done     bool
	// ended is set with done, for writes to refuse a commit ended.
	ended atomic.Bool
}

// errCommitEnded is the error of a write into a commit committed or
// discarded.
var errCommitEnded = errors.New("commit: committed or discarded already")

var _ world.Commit = (*Commit)(nil)

// BeginCommit opens a commit of the DB. Until it is committed or
// discarded, the records of players saved go into it. One commit is open
// at a time: BeginCommit discards one still open.
func (db *DB) BeginCommit() world.Commit {
	c := &Commit{db: db, index: map[string]int{}, players: map[string]bool{}, ticks: map[world.Dimension]int64{}}
	db.cmu.Lock()
	if old := db.open; old != nil {
		db.restorePlayers(old)
		old.end()
	}
	db.open = c
	db.cmu.Unlock()
	return c
}

func (c *Commit) get(key []byte) ([]byte, error) {
	c.mu.Lock()
	i, ok := c.index[string(key)]
	var op commitOp
	if ok {
		op = c.ops[i]
	}
	c.mu.Unlock()
	if !ok {
		return c.db.ldb.Get(key, nil)
	}
	if op.del {
		return nil, leveldb.ErrNotFound
	}
	return slices.Clone(op.value), nil
}

func (c *Commit) put(key, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.add(commitOp{key: slices.Clone(key), value: slices.Clone(value)})
}

func (c *Commit) delete(key []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.add(commitOp{key: slices.Clone(key), del: true})
}

// add appends op. The caller holds mu.
func (c *Commit) add(op commitOp) {
	c.index[string(op.key)] = len(c.ops)
	c.ops = append(c.ops, op)
	c.size += len(op.key) + len(op.value)
}

func (c *Commit) keys(prefix []byte, f func(key []byte)) error {
	held := map[string]bool{}
	err := c.db.prefixKeys(prefix, func(key []byte) {
		held[string(key)] = true
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	for key, i := range c.index {
		if strings.HasPrefix(key, string(prefix)) {
			held[key] = !c.ops[i].del
		}
	}
	c.mu.Unlock()
	keys := make([]string, 0, len(held))
	for key, ok := range held {
		if ok {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		f([]byte(key))
	}
	return nil
}

// Size returns the bytes of the writes held.
func (c *Commit) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.size
}

// Settings returns the settings of the DB.
func (c *Commit) Settings() *world.Settings { return c.db.Settings() }

// SaveSettings sets the settings the commit saves: the DB takes them once
// it committed, and level.dat is written with them by WriteSettings.
func (c *Commit) SaveSettings(s *world.Settings) {
	s = s.Snapshot()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings = s
}

// SaveTick records the current tick of a dimension the commit saves, in the
// record of the checkpoint.
func (c *Commit) SaveTick(dim world.Dimension, tick int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ticks[dim] = tick
}

// LoadColumn reads a column as the commit leaves it.
func (c *Commit) LoadColumn(pos world.ChunkPos, dim world.Dimension) (*chunk.Column, error) {
	col, err := c.db.column(c, dbKey{pos: pos, dim: dim})
	if err != nil {
		return nil, fmt.Errorf("load column %v (%v): %w", pos, dim, err)
	}
	return col, nil
}

// StoreColumn stores a column in the commit.
func (c *Commit) StoreColumn(pos world.ChunkPos, dim world.Dimension, col *chunk.Column) error {
	if c.ended.Load() {
		return fmt.Errorf("store column %v (%v): %w", pos, dim, errCommitEnded)
	}
	if err := c.db.storeColumn(c, dbKey{pos: pos, dim: dim}, col); err != nil {
		return fmt.Errorf("store column %v (%v): %w", pos, dim, err)
	}
	return nil
}

// LoadPlayerSpawnPosition reads the spawn point of a player from the DB:
// spawn points are written at once, outside commits.
func (c *Commit) LoadPlayerSpawnPosition(id uuid.UUID) (cube.Pos, bool, error) {
	return c.db.LoadPlayerSpawnPosition(id)
}

// SavePlayerSpawnPosition saves the spawn point of a player to the DB at
// once, as the DB does.
func (c *Commit) SavePlayerSpawnPosition(id uuid.UUID, pos cube.Pos) error {
	return c.db.SavePlayerSpawnPosition(id, pos)
}

// LoadWorldData reads namespaced world data as the commit leaves it.
func (c *Commit) LoadWorldData(namespace string) ([]byte, error) {
	return c.get(worldDataKey(namespace))
}

// SaveWorldData stores namespaced world data in the commit.
func (c *Commit) SaveWorldData(namespace string, data []byte) error {
	if c.ended.Load() {
		return errCommitEnded
	}
	c.put(worldDataKey(namespace), data)
	return nil
}

// LoadMap reads a map as the commit leaves it.
func (c *Commit) LoadMap(id int64) (world.MapData, error) { return c.db.loadMap(c, id) }

// StoreMap stores a map in the commit.
func (c *Commit) StoreMap(data world.MapData) error {
	if c.ended.Load() {
		return errCommitEnded
	}
	return c.db.storeMap(c, data)
}

// AllocateMapID reserves a map ID in the DB at once, as the DB does.
func (c *Commit) AllocateMapID() (int64, error) { return c.db.AllocateMapID() }

// JavaMapID returns the Java ID of a map, reserving one in the DB at once,
// as the DB does.
func (c *Commit) JavaMapID(nativeID int64) (int32, error) { return c.db.JavaMapID(nativeID) }

// NativeMapID returns the native ID of a map from the DB.
func (c *Commit) NativeMapID(javaID int32) (int64, error) { return c.db.NativeMapID(javaID) }

// LoadDragonFightData reads the dragon fight of the DB.
func (c *Commit) LoadDragonFightData() (world.DragonFightData, bool, error) {
	return c.db.LoadDragonFightData()
}

// SaveDragonFightData stores the dragon fight in the commit.
func (c *Commit) SaveDragonFightData(d world.DragonFightData) error {
	root, err := c.db.encodeDragonFight(c, d)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.fight = root
	c.mu.Unlock()
	return nil
}

// LoadPositionTrackingData reads the position tracking data as the commit
// leaves it.
func (c *Commit) LoadPositionTrackingData() (world.PositionTrackingData, error) {
	return c.db.loadPositionTracking(c)
}

// SavePositionTrackingData stores the position tracking data in the commit.
func (c *Commit) SavePositionTrackingData(data world.PositionTrackingData) error {
	return c.db.savePositionTracking(c, data)
}

// Close discards the commit.
func (c *Commit) Close() error {
	c.Discard()
	return nil
}

// Discard drops the writes of the commit, which then holds no more. The
// records of players saved into it are written at once.
func (c *Commit) Discard() {
	c.db.cmu.Lock()
	defer c.db.cmu.Unlock()
	if !c.done {
		c.db.restorePlayers(c)
	}
	c.end()
}

// end ends the commit. The caller holds cmu.
func (c *Commit) end() {
	c.done = true
	c.ended.Store(true)
	if c.db.open == c {
		c.db.open = nil
	}
}

// Commit writes the writes of the commit to the DB, all together, with the
// record of checkpoint id (keyCheckpoint), and syncs them to disk. The DB
// then takes the settings saved; WriteSettings writes them to level.dat.
// Saves of players wait for it. If it fails, nothing of it was written, and
// the records of players saved into it are written again at once.
func (c *Commit) Commit(id uint64) error {
	db := c.db
	db.cmu.Lock()
	defer db.cmu.Unlock()
	if c.done {
		return errCommitEnded
	}
	defer c.end()

	db.lmu.Lock()
	ldat := *db.ldat
	c.mu.Lock()
	settings := c.settings
	c.mu.Unlock()
	if settings != nil {
		ldat.PutSettings(settings)
	}
	ldat.LastPlayed = time.Now().Unix()
	db.lmu.Unlock()
	record, err := c.record(id, &ldat)
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	c.put([]byte(keyCheckpoint), record)

	c.mu.Lock()
	ops, size := c.ops, c.size
	c.mu.Unlock()
	if size <= commitBatchLimit {
		err = db.ldb.Write(batchOf(ops), &opt.WriteOptions{Sync: true})
	} else {
		err = db.commitTransaction(ops)
	}
	if err != nil {
		db.restorePlayers(c)
		return fmt.Errorf("commit: %w", err)
	}
	db.lmu.Lock()
	*db.ldat = ldat
	db.lmu.Unlock()
	if c.fight != nil {
		db.lmu.Lock()
		db.endFight = c.fight
		db.lmu.Unlock()
	}
	return nil
}

// record encodes the record of checkpoint id, with the level.dat ldat.
func (c *Commit) record(id uint64, ldat *leveldat.Data) ([]byte, error) {
	var l leveldat.LevelDat
	if err := l.Marshal(*ldat); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := l.Write(&buf); err != nil {
		return nil, err
	}
	r := checkpointRecord{ID: int64(id), Time: ldat.LastPlayed, CurrentTick: ldat.CurrentTick, Dimensions: map[string]int64{}, LevelDat: buf.Bytes()}
	c.mu.Lock()
	for dim, tick := range c.ticks {
		if n, ok := world.DimensionID(dim); ok {
			r.Dimensions[fmt.Sprint(n)] = tick
		}
	}
	c.mu.Unlock()
	return nbt.MarshalEncoding(r, nbt.LittleEndian)
}

// batchOf returns the batch of the writes passed, in order.
func batchOf(ops []commitOp) *leveldb.Batch {
	b := leveldb.MakeBatch(len(ops))
	for _, op := range ops {
		if op.del {
			b.Delete(op.key)
		} else {
			b.Put(op.key, op.value)
		}
	}
	return b
}

// commitTransaction writes the writes passed through a transaction, in
// batches of up to transactionChunk bytes, and commits it.
func (db *DB) commitTransaction(ops []commitOp) error {
	tr, err := db.ldb.OpenTransaction()
	if err != nil {
		return err
	}
	written, half := 0, len(ops)/2
	for start := 0; start < len(ops); {
		end, size := start, 0
		for end < len(ops) && (end == start || size < transactionChunk) {
			size += len(ops[end].key) + len(ops[end].value)
			end++
		}
		if err := tr.Write(batchOf(ops[start:end]), nil); err != nil {
			tr.Discard()
			return err
		}
		if written < half && end >= half && commitHook != nil {
			commitHook()
		}
		written, start = end, end
	}
	return tr.Commit()
}

// restorePlayers writes at once the records of players saved into c, which
// failed to commit. The caller holds cmu.
func (db *DB) restorePlayers(c *Commit) {
	c.mu.Lock()
	b := new(leveldb.Batch)
	for key := range c.players {
		if op := c.ops[c.index[key]]; !op.del {
			b.Put(op.key, op.value)
		}
	}
	c.mu.Unlock()
	if b.Len() == 0 {
		return
	}
	if err := db.writeDirect(b); err != nil {
		db.conf.Log.Error("save players of a failed commit: " + err.Error())
	}
}

// lastCheckpoint reads the record of the last checkpoint committed. ok is
// false if the DB holds none.
func (db *DB) lastCheckpoint() (r checkpointRecord, ok bool, err error) {
	b, err := db.ldb.Get([]byte(keyCheckpoint), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return r, false, nil
	} else if err != nil {
		return r, false, err
	}
	if err := nbt.UnmarshalEncoding(b, &r, nbt.LittleEndian); err != nil {
		return r, false, fmt.Errorf("decode %s: %w", keyCheckpoint, err)
	}
	return r, true, nil
}

// reconcile takes the settings of the last checkpoint the DB committed if
// level.dat is older than it, as a crash between the commit and the write
// of level.dat leaves it: level.dat is written after each commit. found
// reports whether a level.dat was read; if not, the checkpoint's settings
// are taken whenever there is one.
func (db *DB) reconcile(found bool) (bool, error) {
	r, ok, err := db.lastCheckpoint()
	if err != nil || !ok {
		return found, err
	}
	if found && r.CurrentTick <= db.ldat.CurrentTick {
		return found, nil
	}
	l, err := leveldat.Read(bytes.NewReader(r.LevelDat))
	if err != nil {
		return found, fmt.Errorf("read the level.dat of %s: %w", keyCheckpoint, err)
	}
	var data leveldat.Data
	if err := l.Unmarshal(&data); err != nil {
		return found, fmt.Errorf("decode the level.dat of %s: %w", keyCheckpoint, err)
	}
	if found {
		db.conf.Log.Warn("level.dat is older than the last checkpoint: its settings are taken from the checkpoint.", "checkpoint", r.ID, "level.dat tick", db.ldat.CurrentTick, "checkpoint tick", r.CurrentTick)
	} else {
		db.conf.Log.Warn("level.dat is missing: the settings are taken from the last checkpoint.", "checkpoint", r.ID)
	}
	*db.ldat = data
	return true, nil
}
