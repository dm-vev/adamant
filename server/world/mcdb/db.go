package mcdb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb/leveldat"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/util"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/google/uuid"
)

// DB implements a world provider for the Minecraft world format, which
// is based on a leveldb database.
type DB struct {
	conf Config
	ldb  *leveldb.DB
	dir  string
	ldat *leveldat.Data
	lmu  sync.Mutex
	// emu serialises column stores, which read the stored entity lists
	// and records of other chunks to decide what to delete.
	emu      sync.Mutex
	set      *world.Settings
	endFight map[string]any
}

// Open creates a new provider reading and writing from/to files under the path
// passed using default options. If a world is present at the path, Open will
// parse its data and initialise the world with it. If the data cannot be
// parsed, an error is returned.
func Open(dir string) (*DB, error) {
	var conf Config
	return conf.Open(dir)
}

// SetBlockRegistry updates the block registry used for chunk and scheduled update encoding/decoding.
// It should be called before any columns are loaded or stored.
func (db *DB) SetBlockRegistry(br world.BlockRegistry) {
	if br == nil {
		br = world.DefaultBlockRegistry
	}
	br.Finalize()
	db.conf.Blocks = br
}

// Settings returns the world.Settings of the world loaded by the DB.
func (db *DB) Settings() *world.Settings {
	return db.set
}

// SaveSettings saves the world.Settings passed to the level.dat.
func (db *DB) SaveSettings(s *world.Settings) {
	s = s.Snapshot()
	db.lmu.Lock()
	defer db.lmu.Unlock()
	db.ldat.PutSettings(s)
}

// LoadWorldData reads an opaque Adamant-owned namespaced payload.
func (db *DB) LoadWorldData(namespace string) ([]byte, error) {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	return db.ldb.Get([]byte("adamant:data:"+namespace), nil)
}

// SaveWorldData writes an opaque Adamant-owned namespaced payload.
func (db *DB) SaveWorldData(namespace string, data []byte) error {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	return db.ldb.Put([]byte("adamant:data:"+namespace), data, nil)
}

type positionTrackLastID struct {
	ID      string `nbt:"id"`
	Version byte   `nbt:"version"`
}

type positionTrackEntry struct {
	Dimension int32   `nbt:"dim"`
	ID        string  `nbt:"id"`
	Position  []int32 `nbt:"pos"`
	Status    byte    `nbt:"status"`
	Version   byte    `nbt:"version"`
}

// LoadPositionTrackingData loads Bedrock's position tracking database.
func (db *DB) LoadPositionTrackingData() (world.PositionTrackingData, error) {
	db.lmu.Lock()
	defer db.lmu.Unlock()

	b, err := db.ldb.Get([]byte(keyPositionTrackLast), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		iter := db.ldb.NewIterator(util.BytesPrefix([]byte(keyPositionTrackEntry)), nil)
		hasEntries := iter.Next()
		iterErr := iter.Error()
		iter.Release()
		if iterErr != nil {
			return world.PositionTrackingData{}, fmt.Errorf("iterate position tracking entries: %w", iterErr)
		}
		if hasEntries {
			return world.PositionTrackingData{}, errors.New("position tracking entries exist without a last ID")
		}
		return world.PositionTrackingData{}, nil
	} else if err != nil {
		return world.PositionTrackingData{}, fmt.Errorf("read position tracking last ID: %w", err)
	}
	var last positionTrackLastID
	if err := nbt.UnmarshalEncoding(b, &last, nbt.LittleEndian); err != nil {
		return world.PositionTrackingData{}, fmt.Errorf("decode position tracking last ID: %w", err)
	}
	next, err := parsePositionTrackID(last.ID)
	if err != nil {
		return world.PositionTrackingData{}, fmt.Errorf("decode position tracking last ID: %w", err)
	}
	data := world.PositionTrackingData{Next: next}
	iter := db.ldb.NewIterator(util.BytesPrefix([]byte(keyPositionTrackEntry)), nil)
	defer iter.Release()
	for iter.Next() {
		n, err := strconv.ParseUint(strings.TrimPrefix(string(iter.Key()), keyPositionTrackEntry), 16, 31)
		if err != nil {
			return world.PositionTrackingData{}, fmt.Errorf("decode position tracking entry key %q: %w", iter.Key(), err)
		}
		handle := int32(n)
		if handle == 0 || handle > next {
			return world.PositionTrackingData{}, fmt.Errorf("position tracking entry handle %d exceeds last ID %d", handle, next)
		}
		var entry positionTrackEntry
		if err := nbt.UnmarshalEncoding(iter.Value(), &entry, nbt.LittleEndian); err != nil {
			return world.PositionTrackingData{}, fmt.Errorf("decode position tracking entry %d: %w", handle, err)
		}
		if len(entry.Position) != 3 {
			return world.PositionTrackingData{}, fmt.Errorf("decode position tracking entry %d: position length is %d", handle, len(entry.Position))
		}
		entryHandle, err := parsePositionTrackID(entry.ID)
		if err != nil || entryHandle != handle {
			return world.PositionTrackingData{}, fmt.Errorf("decode position tracking entry %d: invalid ID %q", handle, entry.ID)
		}
		if entry.Status > 1 {
			return world.PositionTrackingData{}, fmt.Errorf("decode position tracking entry %d: invalid status %d", handle, entry.Status)
		}
		data.Entries = append(data.Entries, world.PositionTrackingEntry{
			Handle: handle, Position: cube.Pos{int(entry.Position[0]), int(entry.Position[1]), int(entry.Position[2])},
			Dimension: int(entry.Dimension), Active: entry.Status == 0,
		})
	}
	if err := iter.Error(); err != nil {
		return world.PositionTrackingData{}, fmt.Errorf("iterate position tracking entries: %w", err)
	}
	return data, nil
}

// SavePositionTrackingData saves Bedrock's position tracking database.
func (db *DB) SavePositionTrackingData(data world.PositionTrackingData) error {
	db.lmu.Lock()
	defer db.lmu.Unlock()

	batch := new(leveldb.Batch)
	iter := db.ldb.NewIterator(util.BytesPrefix([]byte(keyPositionTrackEntry)), nil)
	for iter.Next() {
		batch.Delete(slices.Clone(iter.Key()))
	}
	if err := iter.Error(); err != nil {
		iter.Release()
		return fmt.Errorf("iterate position tracking entries: %w", err)
	}
	iter.Release()

	last, err := nbt.MarshalEncoding(positionTrackLastID{ID: positionTrackID(data.Next), Version: 1}, nbt.LittleEndian)
	if err != nil {
		return fmt.Errorf("encode position tracking last ID: %w", err)
	}
	batch.Put([]byte(keyPositionTrackLast), last)
	for _, tracked := range data.Entries {
		if tracked.Handle <= 0 {
			continue
		}
		status := byte(1)
		if tracked.Active {
			status = 0
		}
		entry, err := nbt.MarshalEncoding(positionTrackEntry{
			Dimension: int32(tracked.Dimension), ID: positionTrackID(tracked.Handle),
			Position: []int32{int32(tracked.Position[0]), int32(tracked.Position[1]), int32(tracked.Position[2])},
			Status:   status, Version: 1,
		}, nbt.LittleEndian)
		if err != nil {
			return fmt.Errorf("encode position tracking entry %d: %w", tracked.Handle, err)
		}
		batch.Put(positionTrackKey(tracked.Handle), entry)
	}
	if err := db.ldb.Write(batch, nil); err != nil {
		return fmt.Errorf("write position tracking data: %w", err)
	}
	return nil
}

func positionTrackKey(handle int32) []byte {
	return []byte(keyPositionTrackEntry + fmt.Sprintf("%08x", uint32(handle)))
}

func positionTrackID(handle int32) string {
	return fmt.Sprintf("0x%08x", uint32(handle))
}

func parsePositionTrackID(id string) (int32, error) {
	n, err := strconv.ParseUint(strings.TrimPrefix(id, "0x"), 16, 31)
	return int32(n), err
}

// playerData holds the fields that indicate where player data is stored for a player with a specific UUID.
type playerData struct {
	UUID         string `nbt:"MsaId"`
	ServerID     string `nbt:"ServerId"`
	SelfSignedID string `nbt:"SelfSignedId"`
}

// LoadPlayerSpawnPosition loads the players spawn position stored in the level.dat from their UUID.
func (db *DB) LoadPlayerSpawnPosition(id uuid.UUID) (pos cube.Pos, exists bool, err error) {
	serverData, _, exists, err := db.loadPlayerData(id)
	if !exists || err != nil {
		return cube.Pos{}, exists, err
	}
	x, y, z := serverData["SpawnX"], serverData["SpawnY"], serverData["SpawnZ"]
	if x == nil || y == nil || z == nil {
		return cube.Pos{}, true, fmt.Errorf("error reading spawn fields from server data for player %v", id)
	}
	return cube.Pos{int(x.(int32)), int(y.(int32)), int(z.(int32))}, true, nil
}

// loadPlayerData loads the data stored in a LevelDB database for a specific UUID.
func (db *DB) loadPlayerData(id uuid.UUID) (serverData map[string]interface{}, key string, exists bool, err error) {
	data, err := db.ldb.Get([]byte("player_"+id.String()), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, "", false, nil
	} else if err != nil {
		return nil, "", true, fmt.Errorf("error reading player data for uuid %v: %w", id, err)
	}

	var d playerData
	if err := nbt.UnmarshalEncoding(data, &d, nbt.LittleEndian); err != nil {
		return nil, "", true, fmt.Errorf("error decoding player data for uuid %v: %w", id, err)
	}
	if d.UUID != id.String() || d.ServerID == "" {
		return nil, d.ServerID, true, fmt.Errorf("invalid player data for uuid %v: %v", id, d)
	}
	serverDB, err := db.ldb.Get([]byte(d.ServerID), nil)
	if err != nil {
		return nil, d.ServerID, true, fmt.Errorf("error reading server data for player %v (%v): %w", id, d.ServerID, err)
	}

	if err := nbt.UnmarshalEncoding(serverDB, &serverData, nbt.LittleEndian); err != nil {
		return nil, d.ServerID, true, fmt.Errorf("error decoding server data for player %v", id)
	}
	return serverData, d.ServerID, true, nil
}

// SavePlayerSpawnPosition saves the player spawn position passed to the levelDB database.
func (db *DB) SavePlayerSpawnPosition(id uuid.UUID, pos cube.Pos) error {
	_, err := db.ldb.Get([]byte("player_"+id.String()), nil)
	d := make(map[string]interface{})
	k := "player_server_" + id.String()

	if errors.Is(err, leveldb.ErrNotFound) {
		data, err := nbt.MarshalEncoding(playerData{UUID: id.String(), ServerID: k}, nbt.LittleEndian)
		if err != nil {
			return fmt.Errorf("marshal player data (uuid=%v): %w", id, err)
		}
		if err := db.ldb.Put([]byte("player_"+id.String()), data, nil); err != nil {
			return fmt.Errorf("write player data (uuid=%v): %w", id, err)
		}
	} else if d, k, _, err = db.loadPlayerData(id); err != nil {
		return err
	}
	d["SpawnX"], d["SpawnY"], d["SpawnZ"] = int32(pos.X()), int32(pos.Y()), int32(pos.Z())

	data, err := nbt.MarshalEncoding(d, nbt.LittleEndian)
	if err != nil {
		return fmt.Errorf("marshal server data for player %v: %w", id, err)
	}
	if err = db.ldb.Put([]byte(k), data, nil); err != nil {
		return fmt.Errorf("write server data for player %v: %w", id, err)
	}
	return nil
}

// LoadColumn reads a world.Column from the DB at a position and dimension in
// the DB. If no column at that position exists, errors.Is(err,
// leveldb.ErrNotFound) equals true.
func (db *DB) LoadColumn(pos world.ChunkPos, dim world.Dimension) (*chunk.Column, error) {
	k := dbKey{pos: pos, dim: dim}
	col, err := db.column(k)
	if err != nil {
		return nil, fmt.Errorf("load column %v (%v): %w", pos, dim, err)
	}
	return col, nil
}

const chunkVersion = 42

func (db *DB) column(k dbKey) (*chunk.Column, error) {
	var cdata chunk.SerialisedData
	col := new(chunk.Column)

	ver, err := db.version(k)
	if err != nil {
		return nil, fmt.Errorf("read version: %w", err)
	}
	if ver != chunkVersion {
		db.conf.Log.Debug("column: unsupported chunk version, trying to load anyway", "X", k.pos[0], "Z", k.pos[1], "dimension", fmt.Sprint(k.dim), "ver", ver)
	}
	cdata.Biomes, err = db.biomes(k)
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		// Some chunks still use 2D chunk data and might not have this field, in
		// which case we can just move on.
		return nil, fmt.Errorf("read biomes: %w", err)
	}
	cdata.SubChunks, err = db.subChunks(k)
	if err != nil {
		return nil, fmt.Errorf("read sub chunks: %w", err)
	}
	col.Chunk, err = chunk.DiskDecode(db.conf.Blocks, cdata, k.dim.Range())
	if err != nil {
		return nil, fmt.Errorf("decode chunk data: %w", err)
	}
	col.Entities, err = db.entities(k)
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		// Not all chunks need to have entities, so an ErrNotFound is fine here.
		return nil, fmt.Errorf("read entities: %w", err)
	}
	col.BlockEntities, err = db.blockEntities(k)
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		// Same as with entities, an ErrNotFound is fine here.
		return nil, fmt.Errorf("read block entities: %w", err)
	}
	col.ScheduledTicks, err = db.scheduledUpdates(k)
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		return nil, fmt.Errorf("read scheduled updates: %w", err)
	}
	col.PostProcessing, err = db.postProcessing(k)
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		return nil, fmt.Errorf("read Adamant post-processing markers: %w", err)
	}
	if errors.Is(err, leveldb.ErrNotFound) {
		col.PostProcessing = nil
	}
	inhabitedTime, err := db.ldb.Get(k.Sum([]byte(keyAdamantInhabitedTime)...), nil)
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		return nil, fmt.Errorf("read Adamant inhabited time: %w", err)
	}
	if err == nil {
		if len(inhabitedTime) != 8 {
			return nil, fmt.Errorf("read Adamant inhabited time: expected 8 bytes, got %d", len(inhabitedTime))
		}
		col.InhabitedTime = binary.LittleEndian.Uint64(inhabitedTime)
	}

	return col, nil
}

func (db *DB) postProcessing(k dbKey) ([][]uint16, error) {
	data, err := db.ldb.Get(k.Sum([]byte(keyAdamantPostProcessing)...), nil)
	if err != nil {
		return nil, err
	}
	sections := dimensionSectionCount(k.dim)
	if len(data) < 3 {
		return nil, errors.New("truncated header")
	}
	if data[0] != postProcessingVersion {
		return nil, fmt.Errorf("unsupported version %d", data[0])
	}
	rows := int(binary.LittleEndian.Uint16(data[1:3]))
	if rows > sections {
		return nil, fmt.Errorf("section row count %d exceeds dimension section count %d", rows, sections)
	}
	if rows == 0 {
		if len(data) != 3 {
			return nil, errors.New("unexpected data after empty section rows")
		}
		return nil, nil
	}
	if len(data)-3 < rows*4 {
		return nil, errors.New("truncated section counts")
	}
	out := make([][]uint16, rows)
	offset := 3
	for section := range out {
		count := binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4
		if uint64(count) > uint64((len(data)-offset)/2) {
			return nil, fmt.Errorf("truncated markers in section %d", section)
		}
		if count != 0 {
			out[section] = make([]uint16, int(count))
			for i := range out[section] {
				value := binary.LittleEndian.Uint16(data[offset : offset+2])
				offset += 2
				if value&0xf000 != 0 {
					return nil, fmt.Errorf("invalid packed marker 0x%04x in section %d", value, section)
				}
				out[section][i] = value
			}
		}
	}
	if offset != len(data) {
		return nil, errors.New("unexpected trailing marker data")
	}
	return out, nil
}

const postProcessingVersion = 1

func dimensionSectionCount(dim world.Dimension) int {
	r := dim.Range()
	return (r[1] - r[0] + 1) / 16
}

func encodePostProcessing(markers [][]uint16, dim world.Dimension) ([]byte, error) {
	if len(markers) == 0 {
		return nil, nil
	}
	if len(markers) > dimensionSectionCount(dim) || len(markers) > int(^uint16(0)) {
		return nil, fmt.Errorf("section row count %d exceeds supported dimension range", len(markers))
	}
	size := 3 + 4*len(markers)
	maxInt := int(^uint(0) >> 1)
	for section, row := range markers {
		if uint64(len(row)) > uint64(^uint32(0)) {
			return nil, fmt.Errorf("marker count %d in section %d exceeds supported range", len(row), section)
		}
		if len(row) > (maxInt-size)/2 {
			return nil, errors.New("post-processing payload is too large")
		}
		size += 2 * len(row)
		for _, marker := range row {
			if marker&0xf000 != 0 {
				return nil, fmt.Errorf("invalid packed marker 0x%04x in section %d", marker, section)
			}
		}
	}
	data := make([]byte, size)
	data[0] = postProcessingVersion
	binary.LittleEndian.PutUint16(data[1:3], uint16(len(markers)))
	offset := 3
	for _, row := range markers {
		binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(len(row)))
		offset += 4
		for _, marker := range row {
			binary.LittleEndian.PutUint16(data[offset:offset+2], marker)
			offset += 2
		}
	}
	return data, nil
}

func (db *DB) version(k dbKey) (byte, error) {
	p, err := db.ldb.Get(k.Sum(keyVersion), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		// Although the version at `keyVersion` may not be found, there is
		// another `keyVersionOld` where the version may be found.
		if p, err = db.ldb.Get(k.Sum(keyVersionOld), nil); err != nil {
			return 0, err
		}
	}
	if err != nil {
		return 0, err
	}
	if n := len(p); n != 1 {
		return 0, fmt.Errorf("expected 1 version byte, got %v", n)
	}
	return p[0], nil
}

func (db *DB) biomes(k dbKey) ([]byte, error) {
	biomes, err := db.ldb.Get(k.Sum(key3DData), nil)
	if err != nil {
		return nil, err
	}
	// The first 512 bytes is a heightmap (16*16 int16s), the biomes follow. We
	// calculate a heightmap on startup so the heightmap can be discarded.
	if n := len(biomes); n <= 512 {
		return nil, fmt.Errorf("expected at least 513 bytes for 3D data, got %v", n)
	}
	return biomes[512:], nil
}

func (db *DB) subChunks(k dbKey) ([][]byte, error) {
	r := k.dim.Range()
	sub := make([][]byte, (r.Height()>>4)+1)

	var err error
	for i := range sub {
		y := uint8(i + (r[0] >> 4))
		sub[i], err = db.ldb.Get(k.Sum(keySubChunkData, y), nil)
		if errors.Is(err, leveldb.ErrNotFound) {
			// No sub chunk present at this Y level. We skip this one and move
			// to the next, which might still be present.
			continue
		} else if err != nil {
			return nil, fmt.Errorf("sub chunk %v: %w", int8(i), err)
		}
	}
	return sub, nil
}

func (db *DB) entities(k dbKey) ([]chunk.Entity, error) {
	// https://learn.microsoft.com/en-us/minecraft/creator/documents/actorstorage
	ids, err := db.ldb.Get(append([]byte(keyEntityIdentifiers), index(k.pos, k.dim)...), nil)
	if err != nil {
		// Key not found, try old method of loading entities.
		return db.entitiesOld(k)
	}
	entities := make([]chunk.Entity, 0, len(ids)/8)
	for i := 0; i < len(ids); i += 8 {
		id := int64(binary.LittleEndian.Uint64(ids[i : i+8]))
		data, err := db.ldb.Get(entityIndex(id), nil)
		if errors.Is(err, leveldb.ErrNotFound) {
			// A world saved before ownership checks may list an entity whose
			// record another chunk's save removed. The entity is gone, so the
			// rest of the chunk loads and the next save drops the reference.
			db.conf.Log.Debug("skip missing entity record", "ID", id)
			continue
		}
		if err != nil {
			db.conf.Log.Error("read entity: "+err.Error(), "ID", id)
			return nil, err
		}
		ent := chunk.Entity{ID: id, Data: make(map[string]any)}
		if err = nbt.UnmarshalEncoding(data, &ent.Data, nbt.LittleEndian); err != nil {
			db.conf.Log.Error("decode entity nbt: "+err.Error(), "ID", id)
		}
		entities = append(entities, ent)
	}
	return entities, nil
}

func (db *DB) entitiesOld(k dbKey) ([]chunk.Entity, error) {
	data, err := db.ldb.Get(k.Sum(keyEntitiesOld), nil)
	if err != nil {
		return nil, err
	}
	var entities []chunk.Entity

	buf := bytes.NewBuffer(data)
	dec, ok := nbt.NewDecoderWithEncoding(buf, nbt.LittleEndian), false

	for buf.Len() != 0 {
		ent := chunk.Entity{Data: make(map[string]any)}
		if err := dec.Decode(&ent.Data); err != nil {
			return nil, fmt.Errorf("decode entity nbt: %w", err)
		}
		ent.ID, ok = ent.Data["UniqueID"].(int64)
		if !ok {
			db.conf.Log.Error("missing unique ID field, generating random", "data", fmt.Sprint(ent.Data))
			ent.ID = rand.Int64()
		}
		entities = append(entities, ent)
	}
	return entities, nil
}

func (db *DB) blockEntities(k dbKey) ([]chunk.BlockEntity, error) {
	var blockEntities []chunk.BlockEntity

	data, err := db.ldb.Get(k.Sum(keyBlockEntities), nil)
	if err != nil {
		return blockEntities, err
	}

	buf := bytes.NewBuffer(data)
	dec := nbt.NewDecoderWithEncoding(buf, nbt.LittleEndian)

	for buf.Len() != 0 {
		be := chunk.BlockEntity{Data: make(map[string]any)}
		if err := dec.Decode(&be.Data); err != nil {
			return blockEntities, fmt.Errorf("decode nbt: %w", err)
		}
		be.Pos = blockPosFromNBT(be.Data)
		blockEntities = append(blockEntities, be)
	}
	return blockEntities, nil
}

func (db *DB) scheduledUpdates(k dbKey) ([]chunk.SavedScheduledTick, error) {
	data, err := db.ldb.Get(k.Sum(keyPendingScheduledTicks), nil)
	if err != nil {
		return nil, err
	}
	var saved scheduledUpdatesData
	if err := nbt.UnmarshalEncoding(data, &saved, nbt.LittleEndian); err != nil {
		return nil, fmt.Errorf("read nbt: %w", err)
	}

	updates := make([]chunk.SavedScheduledTick, 0, len(saved.TickList))
	bpe := chunk.BlockPaletteEncoding{Blocks: db.conf.Blocks}
	for _, entry := range saved.TickList {
		pos, err := scheduledTickPosition(entry)
		if err != nil {
			return nil, err
		}
		trigger, ok := entry["time"].(int64)
		if !ok {
			return nil, fmt.Errorf("read scheduled tick at %v: invalid time field", pos)
		}
		state, ok := entry["blockState"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("read scheduled tick at %v: invalid blockState field", pos)
		}
		block, err := bpe.DecodeBlockState(state)
		if err != nil {
			return nil, fmt.Errorf("read scheduled tick at %v: decode block state: %w", pos, err)
		}
		tick := chunk.ScheduledTick{Pos: pos}
		switch state["name"] {
		case "minecraft:water", "minecraft:flowing_water", "minecraft:lava", "minecraft:flowing_lava":
			tick.Kind = chunk.FluidTick
		}
		if java, ok := entry["adamant:java"].(map[string]any); ok && len(java) != 0 {
			var typeOK, kindOK, priorityOK bool
			tick.Type, typeOK = java["type"].(string)
			kind, validKind := java["kind"].(uint8)
			tick.Kind = chunk.ScheduledTickKind(kind)
			kindOK = validKind && (tick.Kind == chunk.BlockTick || tick.Kind == chunk.FluidTick)
			tick.Priority, priorityOK = java["priority"].(int32)
			if !typeOK || tick.Type == "" || !kindOK || !priorityOK {
				return nil, fmt.Errorf("read scheduled tick at %v: invalid Java tick metadata", pos)
			}
		}
		updates = append(updates, chunk.SavedScheduledTick{
			ScheduledTick: tick,
			Delay:         int32(trigger - int64(saved.CurrentTick)),
			NativeBlock:   block,
		})
	}
	return updates, nil
}

// StoreColumn stores a world.Column at a position and dimension in the DB. An
// error is returned if storing was unsuccessful.
func (db *DB) StoreColumn(pos world.ChunkPos, dim world.Dimension, col *chunk.Column) error {
	k := dbKey{pos: pos, dim: dim}
	if err := db.storeColumn(k, col); err != nil {
		return fmt.Errorf("store column %v (%v): %w", pos, dim, err)
	}
	return nil
}

func (db *DB) storeColumn(k dbKey, col *chunk.Column) error {
	postProcessing, err := encodePostProcessing(col.PostProcessing, k.dim)
	if err != nil {
		return fmt.Errorf("encode Adamant post-processing markers: %w", err)
	}
	data := chunk.Encode(col.Chunk, chunk.DiskEncoding)
	n := 9 + len(data.SubChunks) + len(col.Entities)
	batch := leveldb.MakeBatch(n)

	db.storeVersion(batch, k, chunkVersion)
	db.storeBiomes(batch, k, data.Biomes)
	db.storeSubChunks(batch, k, data.SubChunks, col.Chunk.Range())
	db.storeFinalisation(batch, k, finalisationPopulated)
	db.emu.Lock()
	defer db.emu.Unlock()
	db.storeEntities(batch, k, col.Entities)
	db.storeBlockEntities(batch, k, col.BlockEntities)
	db.storeScheduledUpdates(batch, k, col.ScheduledTicks)
	if len(postProcessing) == 0 {
		batch.Delete(k.Sum([]byte(keyAdamantPostProcessing)...))
	} else {
		batch.Put(k.Sum([]byte(keyAdamantPostProcessing)...), postProcessing)
	}
	inhabitedTime := make([]byte, 8)
	binary.LittleEndian.PutUint64(inhabitedTime, col.InhabitedTime)
	batch.Put(k.Sum([]byte(keyAdamantInhabitedTime)...), inhabitedTime)

	return db.ldb.Write(batch, nil)
}

func (db *DB) storeVersion(batch *leveldb.Batch, k dbKey, ver uint8) {
	batch.Put(k.Sum(keyVersion), []byte{ver})
}

var emptyHeightmap = make([]byte, 512)

func (db *DB) storeBiomes(batch *leveldb.Batch, k dbKey, biomes []byte) {
	batch.Put(k.Sum(key3DData), append(emptyHeightmap, biomes...))
}

func (db *DB) storeSubChunks(batch *leveldb.Batch, k dbKey, subChunks [][]byte, r cube.Range) {
	for i, sub := range subChunks {
		batch.Put(k.Sum(keySubChunkData, byte(i+(r[0]>>4))), sub)
	}
}

func (db *DB) storeFinalisation(batch *leveldb.Batch, k dbKey, finalisation uint32) {
	p := make([]byte, 4)
	binary.LittleEndian.PutUint32(p, finalisation)
	batch.Put(k.Sum(keyFinalisation), p)
}

func (db *DB) storeEntities(batch *leveldb.Batch, k dbKey, entities []chunk.Entity) {
	idsKey := append([]byte(keyEntityIdentifiers), index(k.pos, k.dim)...)

	// load the ids of the previous entities
	var previousIDs []int64
	digpPrev, err := db.ldb.Get(idsKey, nil)
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		db.conf.Log.Error("store entities: read chunk entity IDs: " + err.Error())
	}
	if err == nil {
		for i := 0; i < len(digpPrev); i += 8 {
			previousIDs = append(previousIDs, int64(binary.LittleEndian.Uint64(digpPrev[i:])))
		}
	}

	owner := entityOwner(k)
	newIDs := make([]int64, 0, len(entities))
	for _, e := range entities {
		e.Data["UniqueID"] = e.ID
		e.Data[entityOwnerKey] = owner
		b, err := nbt.MarshalEncoding(e.Data, nbt.LittleEndian)
		if err != nil {
			db.conf.Log.Error("store entities: encode NBT: " + err.Error())
			continue
		}
		batch.Put(entityIndex(e.ID), b)
		newIDs = append(newIDs, e.ID)
	}

	// Remove entities that left the chunk, unless another chunk already
	// stored them after they moved there.
	for _, uniqueID := range previousIDs {
		if !slices.Contains(newIDs, uniqueID) && db.ownsEntity(k, uniqueID) {
			batch.Delete(entityIndex(uniqueID))
		}
	}
	if len(entities) == 0 {
		batch.Delete(idsKey)
	} else {
		// Save the index of entities in the chunk.
		ids := make([]byte, 0, 8*len(newIDs))
		for _, uniqueID := range newIDs {
			ids = binary.LittleEndian.AppendUint64(ids, uint64(uniqueID))
		}
		batch.Put(idsKey, ids)
	}

	// Remove old entity data for this chunk.
	batch.Delete(k.Sum(keyEntitiesOld))
}

// entityOwnerKey is the entity record field naming the chunk whose entity
// list holds the entity.
const entityOwnerKey = "adamant:owner"

// entityOwner returns the owner field of entities stored in the chunk k.
func entityOwner(k dbKey) map[string]any {
	dim, _ := world.DimensionID(k.dim)
	return map[string]any{"x": k.pos[0], "z": k.pos[1], "dim": int32(dim)}
}

// ownsEntity reports whether the stored record of the entity belongs to the
// chunk k. Records without an owner field, written before it existed, belong
// to the chunk holding their position.
func (db *DB) ownsEntity(k dbKey, id int64) bool {
	data, err := db.ldb.Get(entityIndex(id), nil)
	if err != nil {
		return false
	}
	var record map[string]any
	if err := nbt.UnmarshalEncoding(data, &record, nbt.LittleEndian); err != nil {
		return true
	}
	if owner, ok := record[entityOwnerKey].(map[string]any); ok {
		want := entityOwner(k)
		return owner["x"] == want["x"] && owner["z"] == want["z"] && owner["dim"] == want["dim"]
	}
	pos, ok := record["Pos"].([]any)
	if !ok || len(pos) != 3 {
		return true
	}
	x, xOK := pos[0].(float32)
	z, zOK := pos[2].(float32)
	if !xOK || !zOK {
		return true
	}
	return world.ChunkPos{int32(math.Floor(float64(x))) >> 4, int32(math.Floor(float64(z))) >> 4} == k.pos
}

func entityIndex(id int64) []byte {
	return binary.LittleEndian.AppendUint64([]byte(keyEntity), uint64(id))
}

func (db *DB) storeBlockEntities(batch *leveldb.Batch, k dbKey, blockEntities []chunk.BlockEntity) {
	if len(blockEntities) == 0 {
		batch.Delete(k.Sum(keyBlockEntities))
		return
	}

	buf := bytes.NewBuffer(nil)
	enc := nbt.NewEncoderWithEncoding(buf, nbt.LittleEndian)
	for _, b := range blockEntities {
		data := b.Data
		if data == nil {
			data = map[string]any{}
		}
		data["x"], data["y"], data["z"] = int32(b.Pos[0]), int32(b.Pos[1]), int32(b.Pos[2])
		if err := enc.Encode(data); err != nil {
			db.conf.Log.Error("store block entities: encode nbt: " + err.Error())
		}
	}
	batch.Put(k.Sum(keyBlockEntities), buf.Bytes())
}

func (db *DB) storeScheduledUpdates(batch *leveldb.Batch, k dbKey, updates []chunk.SavedScheduledTick) {
	if len(updates) == 0 {
		batch.Delete(k.Sum(keyPendingScheduledTicks))
		return
	}
	entries := make([]scheduledTickNBT, len(updates))
	palette := chunk.BlockPaletteEncoding{Blocks: db.conf.Blocks}
	for i, tick := range updates {
		if tick.Kind != chunk.BlockTick && tick.Kind != chunk.FluidTick {
			panic(fmt.Sprintf("store scheduled updates: invalid tick kind %d", tick.Kind))
		}
		var java map[string]any
		if tick.Type != "" {
			java = map[string]any{
				"type": tick.Type, "kind": uint8(tick.Kind), "priority": tick.Priority,
			}
		}
		entries[i] = scheduledTickNBT{
			BlockState: palette.EncodeBlockState(tick.NativeBlock),
			Time:       int64(tick.Delay),
			X:          int32(tick.Pos[0]), Y: int32(tick.Pos[1]), Z: int32(tick.Pos[2]),
			Java: java,
		}
	}
	// BDS stores currentTick as an int and time as a long. Rebasing both to
	// zero retains paused delays; Java-only queue identity stays outside states.
	data, err := nbt.MarshalEncoding(scheduledUpdatesNBT{TickList: entries}, nbt.LittleEndian)
	if err != nil {
		db.conf.Log.Error("store scheduled updates: encode nbt: " + err.Error())
		return
	}
	batch.Put(k.Sum(keyPendingScheduledTicks), data)
}

type scheduledTickNBT struct {
	BlockState any            `nbt:"blockState"`
	Time       int64          `nbt:"time"`
	X          int32          `nbt:"x"`
	Y          int32          `nbt:"y"`
	Z          int32          `nbt:"z"`
	Java       map[string]any `nbt:"adamant:java"`
}

type scheduledUpdatesNBT struct {
	CurrentTick int32              `nbt:"currentTick"`
	TickList    []scheduledTickNBT `nbt:"tickList"`
}

type scheduledUpdatesData struct {
	CurrentTick int32            `nbt:"currentTick"`
	TickList    []map[string]any `nbt:"tickList"`
}

func scheduledTickPosition(data map[string]any) (cube.Pos, error) {
	x, xOK := data["x"].(int32)
	y, yOK := data["y"].(int32)
	z, zOK := data["z"].(int32)
	if !xOK || !yOK || !zOK {
		return cube.Pos{}, fmt.Errorf("read scheduled tick: invalid position fields")
	}
	return cube.Pos{int(x), int(y), int(z)}, nil
}

// NewColumnIterator returns a ColumnIterator that may be used to iterate over all
// position/chunk pairs in a database.
// An IteratorRange r may be passed to specify limits in terms of what chunks
// should be read. r may be set to nil to read all chunks from the DB.
func (db *DB) NewColumnIterator(r *IteratorRange) *ColumnIterator {
	if r == nil {
		r = &IteratorRange{}
	}
	return newColumnIterator(db, r)
}

// Close closes the provider, saving any file that might need to be saved, such as the level.dat.
func (db *DB) Close() error {
	db.lmu.Lock()
	defer db.lmu.Unlock()

	db.ldat.LastPlayed = time.Now().Unix()

	var ldat leveldat.LevelDat
	if err := ldat.Marshal(*db.ldat); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := ldat.WriteFile(filepath.Join(db.dir, "level.dat")); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.WriteFile(filepath.Join(db.dir, "levelname.txt"), []byte(db.ldat.LevelName), 0644); err != nil {
		return fmt.Errorf("close: write levelname.txt: %w", err)
	}
	return db.ldb.Close()
}

// dbKey holds a position and dimension.
type dbKey struct {
	pos world.ChunkPos
	dim world.Dimension
}

// Sum converts k to its []byte representation and appends p.
func (k dbKey) Sum(p ...byte) []byte {
	return append(index(k.pos, k.dim), p...)
}

// index returns a byte buffer holding the written index of the chunk position passed. If the dimension passed
// is not world.Overworld, the length of the index returned is 12. It is 8 otherwise.
func index(position world.ChunkPos, d world.Dimension) []byte {
	dim, _ := world.DimensionID(d)
	x, z := uint32(position[0]), uint32(position[1])
	b := make([]byte, 12)

	binary.LittleEndian.PutUint32(b, x)
	binary.LittleEndian.PutUint32(b[4:], z)
	if dim == 0 {
		return b[:8]
	}
	binary.LittleEndian.PutUint32(b[8:], uint32(dim))
	return b
}

// blockPosFromNBT returns a position from the X, Y and Z components stored in the NBT data map passed. The
// map is assumed to have an 'x', 'y' and 'z' key.
func blockPosFromNBT(data map[string]any) cube.Pos {
	x, _ := data["x"].(int32)
	y, _ := data["y"].(int32)
	z, _ := data["z"].(int32)
	return cube.Pos{int(x), int(y), int(z)}
}
