package mcdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/dm-vev/adamant/server/world"
)

const mapKeyPrefix = "map_"
const mapNextIDKey = "adamant:map_next_id"
const mapNextCanonicalIDKey = "adamant:map_next_canonical_id"
const mapCanonicalKeyPrefix = "adamant:map_canonical_"
const mapNativeCanonicalKeyPrefix = "adamant:map_native_canonical_"
const legacyMapNextCanonicalIDKey = "adamant:map_next_java_id"
const legacyMapCanonicalKeyPrefix = "adamant:map_java_"
const legacyMapNativeCanonicalKeyPrefix = "adamant:map_native_java_"

// LoadMap decodes a Bedrock map record. Its signed long mapId is shared by the
// map record, map item component, and MapInfo packets.
func (db *DB) LoadMap(id int64) (world.MapData, error) {
	s := db.direct()
	data, err := db.loadMap(s, id)
	if err == nil && s.batch.Len() != 0 {
		err = s.flush()
	}
	return data, err
}

// loadMap reads a map from s, persisting its Java ID in s if it has none.
func (db *DB) loadMap(s store, id int64) (world.MapData, error) {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	b, err := s.get(mapRecordKey(id))
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
	data.CanonicalID, err = db.canonicalMapIDLocked(s, id)
	if err != nil {
		return world.MapData{}, err
	}
	data.HasCanonicalID = true
	return data, nil
}

// StoreMap updates native map fields while retaining unknown root and nested
// decoration tags from the existing Bedrock record.
func (db *DB) StoreMap(data world.MapData) error {
	s := db.direct()
	if err := db.storeMap(s, data); err != nil {
		return err
	}
	if err := s.flush(); err != nil {
		return fmt.Errorf("store map %d: %w", data.ID, err)
	}
	return nil
}

// storeMap stores a map in s.
func (db *DB) storeMap(s store, data world.MapData) error {
	if !data.HasCanonicalID {
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
	b, err := s.get(key)
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
	if err := db.persistCanonicalMapMapping(s, data.ID, data.CanonicalID); err != nil {
		return fmt.Errorf("store map %d Java ID %d: %w", data.ID, data.CanonicalID, err)
	}
	s.put(key, encoded)
	s.put(canonicalMapKey(data.CanonicalID), int64Bytes(data.ID))
	s.put(nativeCanonicalMapKey(data.ID), int32Bytes(data.CanonicalID))
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
	if err := db.putDirect([]byte(mapNextIDKey), next); err != nil {
		return 0, fmt.Errorf("allocate map ID: persist reservation: %w", err)
	}
	return candidate, nil
}

// JavaMapID returns the stable Java map ID for a native Bedrock map ID,
// allocating the next non-negative Java ID when no mapping exists.
func (db *DB) CanonicalMapID(nativeID int64) (int32, error) {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	s := db.direct()
	id, err := db.canonicalMapIDLocked(s, nativeID)
	if err == nil && s.batch.Len() != 0 {
		err = s.flush()
	}
	return id, err
}

func (db *DB) canonicalMapIDLocked(s store, nativeID int64) (int32, error) {
	b, err := storeGetWithFallback(s, nativeCanonicalMapKey(nativeID), legacyNativeCanonicalMapKey(nativeID))
	if err == nil {
		if len(b) != 4 {
			return 0, fmt.Errorf("invalid canonical map ID mapping for native ID %d: length %d", nativeID, len(b))
		}
		canonicalID := int32(binary.LittleEndian.Uint32(b))
		if err := db.persistCanonicalMapMapping(s, nativeID, canonicalID); err != nil {
			return 0, err
		}
		return canonicalID, nil
	}
	if !errors.Is(err, leveldb.ErrNotFound) {
		return 0, fmt.Errorf("read canonical map ID mapping: %w", err)
	}
	counter, err := storeGetWithFallback(s, []byte(mapNextCanonicalIDKey), []byte(legacyMapNextCanonicalIDKey))
	var next int64
	switch {
	case err == nil:
		if len(counter) != 8 {
			return 0, fmt.Errorf("invalid canonical map ID counter length %d", len(counter))
		}
		next = int64(binary.LittleEndian.Uint64(counter))
	case errors.Is(err, leveldb.ErrNotFound):
		next = 0
	default:
		return 0, fmt.Errorf("read canonical map ID counter: %w", err)
	}
	if next < 0 || next > int64(^uint32(0)>>1) {
		return 0, fmt.Errorf("canonical map ID space exhausted")
	}
	canonicalID := int32(next)
	if err := db.persistCanonicalMapMapping(s, nativeID, canonicalID); err != nil {
		return 0, err
	}
	return canonicalID, nil
}

// NativeMapID resolves a canonical map ID to its distinct native signed map ID.
func (db *DB) NativeMapID(canonicalID int32) (int64, error) {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	b, err := db.ldb.Get(canonicalMapKey(canonicalID), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		b, err = db.ldb.Get(legacyCanonicalMapKey(canonicalID), nil)
	}
	if err != nil {
		return 0, fmt.Errorf("load native map ID for canonical ID %d: %w", canonicalID, err)
	}
	if len(b) != 8 {
		return 0, fmt.Errorf("invalid native map ID mapping for canonical ID %d: length %d", canonicalID, len(b))
	}
	return int64(binary.LittleEndian.Uint64(b)), nil
}

func (db *DB) persistCanonicalMapMapping(s store, nativeID int64, canonicalID int32) error {
	forward, err := storeGetWithFallback(s, canonicalMapKey(canonicalID), legacyCanonicalMapKey(canonicalID))
	if err == nil && (len(forward) != 8 || int64(binary.LittleEndian.Uint64(forward)) != nativeID) {
		return fmt.Errorf("canonical map ID %d already maps to another native ID", canonicalID)
	}
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		return err
	}
	reverse, err := storeGetWithFallback(s, nativeCanonicalMapKey(nativeID), legacyNativeCanonicalMapKey(nativeID))
	if err == nil && (len(reverse) != 4 || int32(binary.LittleEndian.Uint32(reverse)) != canonicalID) {
		return fmt.Errorf("native map ID %d already maps to another canonical ID", nativeID)
	}
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		return err
	}
	next := int64(0)
	counter, err := storeGetWithFallback(s, []byte(mapNextCanonicalIDKey), []byte(legacyMapNextCanonicalIDKey))
	if err == nil {
		if len(counter) != 8 {
			return fmt.Errorf("invalid canonical map ID counter length %d", len(counter))
		}
		next = int64(binary.LittleEndian.Uint64(counter))
	} else if !errors.Is(err, leveldb.ErrNotFound) {
		return err
	}
	if canonicalID >= 0 && int64(canonicalID) >= next {
		next = int64(canonicalID) + 1
	} else if _, hasNewF := dbGetExists(s, canonicalMapKey(canonicalID)); hasNewF {
		if _, hasNewR := dbGetExists(s, nativeCanonicalMapKey(nativeID)); hasNewR {
			if _, hasNewC := dbGetExists(s, []byte(mapNextCanonicalIDKey)); hasNewC {
				// Both new directions and counter are already stored: reading writes nothing.
				return nil
			}
		}
	}
	s.put(canonicalMapKey(canonicalID), int64Bytes(nativeID))
	s.put(nativeCanonicalMapKey(nativeID), int32Bytes(canonicalID))
	s.put([]byte(mapNextCanonicalIDKey), int64Bytes(next))
	return nil
}

func storeGetWithFallback(s store, primary, fallback []byte) ([]byte, error) {
	b, err := s.get(primary)
	if errors.Is(err, leveldb.ErrNotFound) {
		return s.get(fallback)
	}
	return b, err
}

func dbGetExists(s store, key []byte) ([]byte, bool) {
	b, err := s.get(key)
	return b, err == nil
}

func canonicalMapKey(id int32) []byte {
	return []byte(mapCanonicalKeyPrefix + strconv.FormatInt(int64(id), 10))
}

func nativeCanonicalMapKey(id int64) []byte {
	return []byte(mapNativeCanonicalKeyPrefix + strconv.FormatInt(id, 10))
}

func legacyCanonicalMapKey(id int32) []byte {
	return []byte(legacyMapCanonicalKeyPrefix + strconv.FormatInt(int64(id), 10))
}

func legacyNativeCanonicalMapKey(id int64) []byte {
	return []byte(legacyMapNativeCanonicalKeyPrefix + strconv.FormatInt(id, 10))
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
			if !ok {
				return world.MapData{}, fmt.Errorf("invalid native decoration rot tag: got %T", details["rot"])
			}
			typ, ok := details["type"].(int32)
			if !ok {
				return world.MapData{}, fmt.Errorf("invalid native decoration type tag: got %T", details["type"])
			}
			dx, ok := details["x"].(int32)
			if !ok {
				return world.MapData{}, fmt.Errorf("invalid native decoration x tag: got %T", details["x"])
			}
			dy, ok := details["y"].(int32)
			if !ok {
				return world.MapData{}, fmt.Errorf("invalid native decoration y tag: got %T", details["y"])
			}
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
			if !ok {
				return world.MapData{}, fmt.Errorf("invalid native decoration blockX tag: got %T", key["blockX"])
			}
			blockY, ok := key["blockY"].(int32)
			if !ok {
				return world.MapData{}, fmt.Errorf("invalid native decoration blockY tag: got %T", key["blockY"])
			}
			blockZ, ok := key["blockZ"].(int32)
			if !ok {
				return world.MapData{}, fmt.Errorf("invalid native decoration blockZ tag: got %T", key["blockZ"])
			}
			keyType, ok := key["type"].(int32)
			if !ok {
				return world.MapData{}, fmt.Errorf("invalid native decoration key type tag: got %T", key["type"])
			}
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

func mapByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}
