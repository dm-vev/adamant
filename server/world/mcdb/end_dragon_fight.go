package mcdb

import (
	"errors"
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/dm-vev/adamant/net/nbt"
)

const dragonFightSupplementKey = "adamant:DragonFightState"

// LoadDragonFightData loads the Bedrock End fight record. Unknown fields in the
// native record are retained so a subsequent save does not discard game data.
func (db *DB) LoadDragonFightData() (world.DragonFightData, bool, error) {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	b, err := db.ldb.Get([]byte("TheEnd"), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		db.endFight = nil
		return world.DragonFightData{NeedsStateScanning: true}, false, nil
	}
	if err != nil {
		return world.DragonFightData{}, false, fmt.Errorf("load End dragon fight: %w", err)
	}
	root := map[string]any{}
	if err := nbt.UnmarshalEncoding(b, &root, nbt.LittleEndian); err != nil {
		return world.DragonFightData{}, false, fmt.Errorf("decode End data: %w", err)
	}
	dataRoot, ok := root["data"].(map[string]any)
	if !ok {
		return world.DragonFightData{}, false, fmt.Errorf("decode End data: missing data compound")
	}
	dragon, ok := dataRoot["DragonFight"].(map[string]any)
	if !ok {
		db.endFight = cloneCompound(root)
		return world.DragonFightData{NeedsStateScanning: true}, false, nil
	}
	db.endFight = cloneCompound(root)
	d := world.DragonFightData{
		DragonKilled: boolValue(dragon["DragonKilled"]), DragonSpawned: boolValue(dragon["DragonSpawned"]),
		PreviouslyKilled: boolValue(dragon["PreviouslyKilled"]), DragonUniqueID: int64Value(dragon["DragonUUID"]),
		IsRespawning: boolValue(dragon["IsRespawning"]), Version: uint8(int64Value(dragon["DragonFightVersion"])),
		NeedsStateScanning: true,
	}
	if portal, ok := dragon["ExitPortalLocation"].([]int32); ok && len(portal) == 3 {
		pos := cube.Pos{int(portal[0]), int(portal[1]), int(portal[2])}
		d.ExitPortal = &pos
	}
	if gateways, ok := dragon["Gateways"].([]int32); ok {
		d.Gateways = append([]int32(nil), gateways...)
	}
	if supplemental, ok := dragon[dragonFightSupplementKey].(map[string]any); ok {
		d.NeedsStateScanning = boolValue(supplemental["NeedsStateScanning"])
		d.RespawnStage, _ = supplemental["RespawnStage"].(string)
		d.RespawnTime = int32(int64Value(supplemental["RespawnTime"]))
		if ids, ok := supplemental["RespawnCrystalIDs"].([]int64); ok {
			d.RespawnCrystalIDs = append([]int64(nil), ids...)
		}
	}
	return d, true, nil
}

// SaveDragonFightData saves native fields while preserving unrelated native NBT.
func (db *DB) SaveDragonFightData(d world.DragonFightData) error {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	root := cloneCompound(db.endFight)
	if root == nil { root = map[string]any{} }
	dataRoot, ok := root["data"].(map[string]any)
	if !ok { dataRoot = map[string]any{}; root["data"] = dataRoot }
	dragon, ok := dataRoot["DragonFight"].(map[string]any)
	if !ok { dragon = map[string]any{}; dataRoot["DragonFight"] = dragon }
	dragon["DragonFightVersion"] = d.Version
	dragon["DragonKilled"] = byteValue(d.DragonKilled)
	dragon["DragonSpawned"] = byteValue(d.DragonSpawned)
	dragon["PreviouslyKilled"] = byteValue(d.PreviouslyKilled)
	dragon["DragonUUID"] = d.DragonUniqueID
	if d.ExitPortal == nil { delete(dragon, "ExitPortalLocation") } else { dragon["ExitPortalLocation"] = []int32{int32(d.ExitPortal.X()), int32(d.ExitPortal.Y()), int32(d.ExitPortal.Z())} }
	dragon["Gateways"] = append([]int32(nil), d.Gateways...)
	dragon["IsRespawning"] = byteValue(d.IsRespawning)
	dragon[dragonFightSupplementKey] = map[string]any{
		"NeedsStateScanning": byteValue(d.NeedsStateScanning), "RespawnStage": d.RespawnStage,
		"RespawnTime": d.RespawnTime, "RespawnCrystalIDs": append([]int64(nil), d.RespawnCrystalIDs...),
	}
	encoded, err := nbt.MarshalEncoding(root, nbt.LittleEndian)
	if err != nil { return fmt.Errorf("encode End dragon fight: %w", err) }
	if err := db.ldb.Put([]byte("TheEnd"), encoded, nil); err != nil { return fmt.Errorf("save End dragon fight: %w", err) }
	db.endFight = cloneCompound(root)
	return nil
}

func boolValue(v any) bool {
	switch x := v.(type) {
	case byte: return x != 0
	case int8: return x != 0
	case bool: return x
	}
	return false
}

func byteValue(v bool) byte { if v { return 1 }; return 0 }

func int64Value(v any) int64 {
	switch x := v.(type) {
	case int64: return x
	case int32: return int64(x)
	case int16: return int64(x)
	case int8: return int64(x)
	case byte: return int64(x)
	}
	return 0
}

func cloneCompound(src map[string]any) map[string]any {
	if src == nil { return nil }
	dst := make(map[string]any, len(src))
	for key, value := range src { dst[key] = cloneNBTValue(value) }
	return dst
}

func cloneNBTValue(value any) any {
	switch v := value.(type) {
	case map[string]any: return cloneCompound(v)
	case []any:
		out := make([]any, len(v)); for i := range v { out[i] = cloneNBTValue(v[i]) }; return out
	case []int32: return append([]int32(nil), v...)
	case []int64: return append([]int64(nil), v...)
	case []byte: return append([]byte(nil), v...)
	case []int8: return append([]int8(nil), v...)
	case []int16: return append([]int16(nil), v...)
	case []float32: return append([]float32(nil), v...)
	case []float64: return append([]float64(nil), v...)
	case []string: return append([]string(nil), v...)
	}
	return value
}
