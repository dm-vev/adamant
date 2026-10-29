package mcdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/dm-vev/adamant/net/nbt"
)

const mapKeyPrefix = "map_"
const mapNextIDKey = "adamant:map_next_id"
const mapNextJavaIDKey = "adamant:map_next_java_id"
const mapJavaKeyPrefix = "adamant:map_java_"
const mapNativeJavaKeyPrefix = "adamant:map_native_java_"


// LoadMap decodes a Bedrock map record. Its signed long mapId is shared by the
// map record, map item component, and MapInfo packets.
func (db *DB) LoadMap(id int64) (world.MapData, error) {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	b, err := db.ldb.Get(mapRecordKey(id), nil)
	if err != nil {
		return world.MapData{}, fmt.Errorf("load map %d: %w", id, err)
	}
	root, err := decodeMapRecord(b)
	if err != nil {
		return world.MapData{}, fmt.Errorf("load map %d: %w", id, err)
	}
	data, err := mapDataFromNBT(root, id)
	if err != nil {
		return world.MapData{}, err
	}
	data.JavaID, err = db.javaMapIDLocked(id)
	if err != nil {
		return world.MapData{}, err
	}
	data.HasJavaID = true
	return data, nil
}

// StoreMap updates native map fields while retaining unknown root and nested
// decoration tags from the existing Bedrock record.
func (db *DB) StoreMap(data world.MapData) error {
	if !data.HasJavaID {
		return fmt.Errorf("store map %d: Java map ID presence is required", data.ID)
	}
	if len(data.NativeColors) != 128*128*4 {
		return fmt.Errorf("store map %d: native RGBA colors have length %d, want 65536", data.ID, len(data.NativeColors))
	}
	db.lmu.Lock()
	defer db.lmu.Unlock()
	key := mapRecordKey(data.ID)
	root := cloneCompound(data.NativeNBT)
	if root == nil {
		root = map[string]any{}
	}
	b, err := db.ldb.Get(key, nil)
	if err == nil {
		root, err = decodeMapRecord(b)
		if err != nil {
			return fmt.Errorf("store map %d: %w", data.ID, err)
		}
	} else if !errors.Is(err, leveldb.ErrNotFound) {
		return fmt.Errorf("store map %d: read existing record: %w", data.ID, err)
	}
	if err := mapDataToNBT(root, data); err != nil {
		return fmt.Errorf("store map %d: %w", data.ID, err)
	}
	encoded, err := nbt.MarshalEncoding(root, nbt.LittleEndian)
	if err != nil {
		return fmt.Errorf("encode map %d: %w", data.ID, err)
	}
	if err := db.persistJavaMapMapping(data.ID, data.JavaID); err != nil {
		return fmt.Errorf("store map %d Java ID %d: %w", data.ID, data.JavaID, err)
	}
	batch := new(leveldb.Batch)
	batch.Put(key, encoded)
	batch.Put(javaMapKey(data.JavaID), int64Bytes(data.ID))
	batch.Put(nativeJavaMapKey(data.ID), int32Bytes(data.JavaID))
	if err := db.ldb.Write(batch, nil); err != nil {
		return fmt.Errorf("store map %d: %w", data.ID, err)
	}
	return nil
}

// AllocateMapID reserves a unique native signed map ID. Bedrock allocates map
// IDs downward from its signed-long map namespace; the persisted counter keeps
// reservations unique across concurrent callers and process restarts.
func (db *DB) AllocateMapID() (int64, error) {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	var candidate int64
	counter, err := db.ldb.Get([]byte(mapNextIDKey), nil)
	switch {
	case err == nil:
		if len(counter) != 8 {
			return 0, fmt.Errorf("allocate map ID: invalid native counter length %d", len(counter))
		}
		candidate = int64(binary.LittleEndian.Uint64(counter))
	case errors.Is(err, leveldb.ErrNotFound):
		candidate, err = db.firstMapID()
		if err != nil {
			return 0, err
		}
	case err != nil:
		return 0, fmt.Errorf("allocate map ID: read counter: %w", err)
	}
	for {
		if _, err := db.ldb.Get(mapRecordKey(candidate), nil); errors.Is(err, leveldb.ErrNotFound) {
			break
		} else if err != nil {
			return 0, fmt.Errorf("allocate map ID: check %d: %w", candidate, err)
		}
		if candidate == -1<<63 {
			return 0, fmt.Errorf("allocate map ID: signed 64-bit ID space exhausted")
		}
		candidate--
	}
	if candidate == -1<<63 {
		return 0, fmt.Errorf("allocate map ID: signed 64-bit ID space exhausted")
	}
	next := make([]byte, 8)
	binary.LittleEndian.PutUint64(next, uint64(candidate-1))
	if err := db.ldb.Put([]byte(mapNextIDKey), next, nil); err != nil {
		return 0, fmt.Errorf("allocate map ID: persist reservation: %w", err)
	}
	return candidate, nil
}
// JavaMapID returns the stable Java map ID for a native Bedrock map ID,
// allocating the next non-negative Java ID when no mapping exists.
func (db *DB) JavaMapID(nativeID int64) (int32, error) {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	return db.javaMapIDLocked(nativeID)
}

func (db *DB) javaMapIDLocked(nativeID int64) (int32, error) {
	b, err := db.ldb.Get(nativeJavaMapKey(nativeID), nil)
	if err == nil {
		if len(b) != 4 {
			return 0, fmt.Errorf("invalid Java map ID mapping for native ID %d: length %d", nativeID, len(b))
		}
		javaID := int32(binary.LittleEndian.Uint32(b))
		if err := db.persistJavaMapMapping(nativeID, javaID); err != nil {
			return 0, err
		}
		return javaID, nil
	}
	if !errors.Is(err, leveldb.ErrNotFound) {
		return 0, fmt.Errorf("read Java map ID mapping: %w", err)
	}
	counter, err := db.ldb.Get([]byte(mapNextJavaIDKey), nil)
	var next int64
	switch {
	case err == nil:
		if len(counter) != 8 {
			return 0, fmt.Errorf("invalid Java map ID counter length %d", len(counter))
		}
		next = int64(binary.LittleEndian.Uint64(counter))
	case errors.Is(err, leveldb.ErrNotFound):
		next = 0
	default:
		return 0, fmt.Errorf("read Java map ID counter: %w", err)
	}
	if next < 0 || next > int64(^uint32(0)>>1) {
		return 0, fmt.Errorf("Java map ID space exhausted")
	}
	javaID := int32(next)
	if err := db.persistJavaMapMapping(nativeID, javaID); err != nil {
		return 0, err
	}
	return javaID, nil
}

// NativeMapID resolves a Java map ID to its distinct native signed map ID.
func (db *DB) NativeMapID(javaID int32) (int64, error) {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	b, err := db.ldb.Get(javaMapKey(javaID), nil)
	if err != nil {
		return 0, fmt.Errorf("load native map ID for Java ID %d: %w", javaID, err)
	}
	if len(b) != 8 {
		return 0, fmt.Errorf("invalid native map ID mapping for Java ID %d: length %d", javaID, len(b))
	}
	return int64(binary.LittleEndian.Uint64(b)), nil
}

func (db *DB) persistJavaMapMapping(nativeID int64, javaID int32) error {
	forward, err := db.ldb.Get(javaMapKey(javaID), nil)
	if err == nil && (len(forward) != 8 || int64(binary.LittleEndian.Uint64(forward)) != nativeID) {
		return fmt.Errorf("Java map ID %d already maps to another native ID", javaID)
	}
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		return err
	}
	reverse, err := db.ldb.Get(nativeJavaMapKey(nativeID), nil)
	if err == nil && (len(reverse) != 4 || int32(binary.LittleEndian.Uint32(reverse)) != javaID) {
		return fmt.Errorf("native map ID %d already maps to another Java ID", nativeID)
	}
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		return err
	}
	next := int64(0)
	counter, err := db.ldb.Get([]byte(mapNextJavaIDKey), nil)
	if err == nil {
		if len(counter) != 8 {
			return fmt.Errorf("invalid Java map ID counter length %d", len(counter))
		}
		next = int64(binary.LittleEndian.Uint64(counter))
	} else if !errors.Is(err, leveldb.ErrNotFound) {
		return err
	}
	if javaID >= 0 && int64(javaID) >= next {
		next = int64(javaID) + 1
	}
	batch := new(leveldb.Batch)
	batch.Put(javaMapKey(javaID), int64Bytes(nativeID))
	batch.Put(nativeJavaMapKey(nativeID), int32Bytes(javaID))
	batch.Put([]byte(mapNextJavaIDKey), int64Bytes(next))
	return db.ldb.Write(batch, nil)
}

func javaMapKey(id int32) []byte {
	return []byte(mapJavaKeyPrefix + strconv.FormatInt(int64(id), 10))
}

func nativeJavaMapKey(id int64) []byte {
	return []byte(mapNativeJavaKeyPrefix + strconv.FormatInt(id, 10))
}

func int64Bytes(v int64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(v))
	return b
}

func int32Bytes(v int32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	return b
}

func (db *DB) firstMapID() (int64, error) {
	it := db.ldb.NewIterator(nil, nil)
	defer it.Release()
	var lowest int64
	found := false
	for it.Next() {
		key := string(it.Key())
		if !strings.HasPrefix(key, mapKeyPrefix) {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(key, mapKeyPrefix), 10, 64)
		if err == nil && (!found || id < lowest) {
			lowest, found = id, true
		}
	}
	if err := it.Error(); err != nil {
		return 0, fmt.Errorf("allocate map ID: scan native keys: %w", err)
	}
	if !found {
		return -21474836480, nil
	}
	if lowest == -1<<63 {
		return 0, fmt.Errorf("allocate map ID: signed 64-bit ID space exhausted")
	}
	return lowest - 1, nil
}

func mapRecordKey(id int64) []byte { return []byte(mapKeyPrefix + strconv.FormatInt(id, 10)) }

func decodeMapRecord(data []byte) (map[string]any, error) {
	root := map[string]any{}
	if err := nbt.UnmarshalEncoding(data, &root, nbt.LittleEndian); err != nil {
		return nil, fmt.Errorf("decode native NBT: %w", err)
	}
	return root, nil
}

func mapDataFromNBT(root map[string]any, id int64) (world.MapData, error) {
	colors, ok := root["colors"].([]byte)
	if !ok || len(colors) != 128*128*4 {
		return world.MapData{}, fmt.Errorf("invalid native colors tag: got %T length %d, want byte array of length 65536", root["colors"], len(colors))
	}
	mapID, ok := root["mapId"].(int64)
	if !ok {
		return world.MapData{}, fmt.Errorf("invalid native mapId tag: got %T, want long", root["mapId"])
	}
	if mapID != id {
		return world.MapData{}, fmt.Errorf("native mapId %d does not match key ID %d", mapID, id)
	}
	dimension, ok := root["dimension"].(byte)
	if !ok {
		return world.MapData{}, fmt.Errorf("invalid native dimension tag: got %T, want byte", root["dimension"])
	}
	x, ok := root["xCenter"].(int32)
	if !ok {
		return world.MapData{}, fmt.Errorf("invalid native xCenter tag: got %T, want int", root["xCenter"])
	}
	z, ok := root["zCenter"].(int32)
	if !ok {
		return world.MapData{}, fmt.Errorf("invalid native zCenter tag: got %T, want int", root["zCenter"])
	}
	parentID, ok := root["parentMapId"].(int64)
	if !ok {
		return world.MapData{}, fmt.Errorf("invalid native parentMapId tag: got %T, want long", root["parentMapId"])
	}
	scale, ok := root["scale"].(byte)
	if !ok {
		return world.MapData{}, fmt.Errorf("invalid native scale tag: got %T, want byte", root["scale"])
	}
	locked, ok := root["mapLocked"].(byte)
	if !ok {
		return world.MapData{}, fmt.Errorf("invalid native mapLocked tag: got %T, want byte", root["mapLocked"])
	}
	tracking := byte(0)
	if nativeTracking, exists := root["trackingPosition"]; exists {
		var ok bool
		tracking, ok = nativeTracking.(byte)
		if !ok {
			return world.MapData{}, fmt.Errorf("invalid native trackingPosition tag: got %T, want byte", nativeTracking)
		}
	}
	fullyExplored, ok := root["fullyExplored"].(byte)
	if !ok {
		return world.MapData{}, fmt.Errorf("invalid native fullyExplored tag: got %T, want byte", root["fullyExplored"])
	}
	unlimitedTracking, ok := root["unlimitedTracking"].(byte)
	if !ok {
		return world.MapData{}, fmt.Errorf("invalid native unlimitedTracking tag: got %T, want byte", root["unlimitedTracking"])
	}
	data := world.MapData{
		ID: mapID, ParentID: parentID, Dimension: int32(dimension), CenterX: x, CenterZ: z, Scale: scale,
		Locked: locked != 0, Tracking: tracking != 0, UnlimitedTracking: unlimitedTracking != 0, FullyExplored: fullyExplored != 0,
		NativeColors: append([]byte(nil), colors...), NativeNBT: cloneCompound(root),
	}
	if rawDecorations, exists := root["decorations"]; exists {
		decorations, ok := rawDecorations.([]any)
		if !ok {
			return world.MapData{}, fmt.Errorf("invalid native decorations tag: got %T, want list", rawDecorations)
		}
		data.Decorations = make([]world.MapDecoration, 0, len(decorations))
		for _, raw := range decorations {
			d, ok := raw.(map[string]any)
			if !ok {
				return world.MapData{}, fmt.Errorf("invalid native decoration element %T", raw)
			}
			details, ok := d["data"].(map[string]any)
			if !ok {
				return world.MapData{}, fmt.Errorf("invalid native decoration data compound: got %T", d["data"])
			}
			rot, ok := details["rot"].(int32)
			if !ok { return world.MapData{}, fmt.Errorf("invalid native decoration rot tag: got %T", details["rot"]) }
			typ, ok := details["type"].(int32)
			if !ok { return world.MapData{}, fmt.Errorf("invalid native decoration type tag: got %T", details["type"]) }
			dx, ok := details["x"].(int32)
			if !ok { return world.MapData{}, fmt.Errorf("invalid native decoration x tag: got %T", details["x"]) }
			dy, ok := details["y"].(int32)
			if !ok { return world.MapData{}, fmt.Errorf("invalid native decoration y tag: got %T", details["y"]) }
			if rot < 0 || rot > 255 || typ < 0 || typ > 255 {
				return world.MapData{}, fmt.Errorf("native decoration type/rotation outside byte range: %d/%d", typ, rot)
			}
			if dx < -128 || dx > 127 || dy < -128 || dy > 127 {
				return world.MapData{}, fmt.Errorf("native decoration position outside signed byte range: %d/%d", dx, dy)
			}
			key, ok := d["key"].(map[string]any)
			if !ok {
				return world.MapData{}, fmt.Errorf("invalid native decoration key compound: got %T", d["key"])
			}
			blockX, ok := key["blockX"].(int32)
			if !ok { return world.MapData{}, fmt.Errorf("invalid native decoration blockX tag: got %T", key["blockX"]) }
			blockY, ok := key["blockY"].(int32)
			if !ok { return world.MapData{}, fmt.Errorf("invalid native decoration blockY tag: got %T", key["blockY"]) }
			blockZ, ok := key["blockZ"].(int32)
			if !ok { return world.MapData{}, fmt.Errorf("invalid native decoration blockZ tag: got %T", key["blockZ"]) }
			keyType, ok := key["type"].(int32)
			if !ok { return world.MapData{}, fmt.Errorf("invalid native decoration key type tag: got %T", key["type"]) }
			data.Decorations = append(data.Decorations, world.MapDecoration{
				Type: byte(typ), Rotation: byte(rot), X: int8(dx), Z: int8(dy),
				BlockX: blockX, BlockY: blockY, BlockZ: blockZ, KeyType: keyType,
				NativeNBT: cloneCompound(d),
			})
		}
	}
	return data, nil
}

func mapDataToNBT(root map[string]any, data world.MapData) error {
	if data.Dimension < 0 || data.Dimension > 255 {
		return fmt.Errorf("native map dimension %d is outside byte range", data.Dimension)
	}
	if data.Scale > 4 {
		return fmt.Errorf("native map scale %d is outside supported range 0..4", data.Scale)
	}
	parentID := data.ParentID
	if parentID == 0 {
		parentID = -1
	}
	root["mapId"], root["parentMapId"] = data.ID, parentID
	root["dimension"] = byte(data.Dimension)
	root["xCenter"], root["zCenter"] = data.CenterX, data.CenterZ
	root["scale"] = data.Scale
	root["mapLocked"] = mapByte(data.Locked)
	if _, exists := root["trackingPosition"]; exists {
		root["trackingPosition"] = mapByte(data.Tracking)
	}
	root["unlimitedTracking"] = mapByte(data.UnlimitedTracking)
	if _, exists := root["fullyExplored"]; exists {
		if _, ok := root["fullyExplored"].(byte); !ok {
			return fmt.Errorf("invalid native fullyExplored tag: got %T, want byte", root["fullyExplored"])
		}
	}
	root["fullyExplored"] = mapByte(data.FullyExplored)
	root["colors"] = append([]byte(nil), data.NativeColors...)
	for _, key := range []string{"width", "height"} {
		if _, exists := root[key]; !exists {
			root[key] = int16(128)
		} else if _, ok := root[key].(int16); !ok {
			return fmt.Errorf("invalid native %s tag: got %T, want short", key, root[key])
		}
	}
	decorations := make([]any, len(data.Decorations))
	oldDecorations, _ := root["decorations"].([]any)
	for i, d := range data.Decorations {
		native := cloneCompound(d.NativeNBT)
		if native == nil && i < len(oldDecorations) {
			if previous, ok := oldDecorations[i].(map[string]any); ok {
				native = cloneCompound(previous)
			}
		}
		if native == nil {
			native = map[string]any{}
		}
		details, ok := native["data"].(map[string]any)
		if !ok {
			details = map[string]any{}
		} else {
			details = cloneCompound(details)
		}
		details["type"], details["rot"] = int32(d.Type), int32(d.Rotation)
		details["x"], details["y"] = int32(d.X), int32(d.Z)
		native["data"] = details
		key, ok := native["key"].(map[string]any)
		if !ok {
			key = map[string]any{}
		} else {
			key = cloneCompound(key)
		}
		key["blockX"], key["blockY"], key["blockZ"], key["type"] = d.BlockX, d.BlockY, d.BlockZ, d.KeyType
		native["key"] = key
		decorations[i] = native
	}
	root["decorations"] = decorations
	return nil
}


func mapByte(v bool) byte { if v { return 1 }; return 0 }
