package mcdb

import (
	"encoding/base64"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/dm-vev/adamant/net/nbt"
)

// Captured from BDS 1.26.52.3, pending-tick key fdffffff0000000033.
// Two active sensors and flowing water remain due after 3, 7 and 3 ticks.
const nativePendingTicks = "CgAAAwsAY3VycmVudFRpY2uCZAAACQgAdGlja0xpc3QKAwAAAAoKAGJsb2NrU3RhdGUIBABuYW1lIQBtaW5lY3JhZnQ6Y2FsaWJyYXRlZF9zY3Vsa19zZW5zb3IKBgBzdGF0ZXMIHABtaW5lY3JhZnQ6Y2FyZGluYWxfZGlyZWN0aW9uBQBzb3V0aAMSAHNjdWxrX3NlbnNvcl9waGFzZQEAAAAAAwcAdmVyc2lvbiE8FQEABAQAdGltZYVkAAAAAAAAAwEAeNH///8DAQB5PwAAAAMBAHoKAAAAAAoKAGJsb2NrU3RhdGUIBABuYW1lFgBtaW5lY3JhZnQ6c2N1bGtfc2Vuc29yCgYAc3RhdGVzAxIAc2N1bGtfc2Vuc29yX3BoYXNlAQAAAAADBwB2ZXJzaW9uITwVAQAEBAB0aW1liWQAAAAAAAADAQB40P///wMBAHkyAAAAAwEAegoAAAAACgoAYmxvY2tTdGF0ZQgEAG5hbWUPAG1pbmVjcmFmdDp3YXRlcgoGAHN0YXRlcwMMAGxpcXVpZF9kZXB0aAEAAAAAAwcAdmVyc2lvbiE8FQEABAQAdGltZYVkAAAAAAAAAwEAeNH///8DAQB5MgAAAAMBAHoLAAAAAAA="

func loadNativePendingTicks(t *testing.T, db *DB) []chunk.SavedScheduledTick {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(nativePendingTicks)
	if err != nil {
		t.Fatal(err)
	}
	key := dbKey{pos: world.ChunkPos{-3, 0}, dim: world.Overworld}
	if err := db.ldb.Put(key.Sum(keyPendingScheduledTicks), data, nil); err != nil {
		t.Fatal(err)
	}
	ticks, err := db.scheduledUpdates(key)
	if err != nil {
		t.Fatal(err)
	}
	return ticks
}

func TestNativePendingTicksRestorePausedDelays(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ticks := loadNativePendingTicks(t, db)
	want := []struct {
		pos      cube.Pos
		kind     chunk.ScheduledTickKind
		delay    int32
		name     string
		property string
	}{
		{cube.Pos{-47, 63, 10}, chunk.BlockTick, 3, "minecraft:calibrated_sculk_sensor", "sculk_sensor_phase"},
		{cube.Pos{-48, 50, 10}, chunk.BlockTick, 7, "minecraft:sculk_sensor", "sculk_sensor_phase"},
		{cube.Pos{-47, 50, 11}, chunk.FluidTick, 3, "minecraft:water", "liquid_depth"},
	}
	if len(ticks) != len(want) {
		t.Fatalf("native pending ticks = %d, want %d", len(ticks), len(want))
	}
	for i, expected := range want {
		tick := ticks[i]
		if tick.Pos != expected.pos || tick.Kind != expected.kind || tick.Delay != expected.delay {
			t.Fatalf("native tick %d: got %+v, want position=%v kind=%v paused delay=%d", i, tick, expected.pos, expected.kind, expected.delay)
		}
		name, properties, ok := db.conf.Blocks.RuntimeIDToState(tick.NativeBlock)
		if !ok || name != expected.name || properties[expected.property] != int32(1) {
			t.Fatalf("native tick %d lost its active/flowing state: %s %v", i, name, properties)
		}
	}
}

func TestJavaTickMetadataRetainsNativePendingFormat(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ticks := loadNativePendingTicks(t, db)
	types := []string{"minecraft:calibrated_sculk_sensor", "minecraft:sculk_sensor", "minecraft:flowing_water"}
	priorities := []int32{-2, 0, 3}
	for i := range ticks {
		ticks[i].Type = types[i]
		ticks[i].Priority = priorities[i]
	}
	pos := world.ChunkPos{-3, 0}
	column := &chunk.Column{Chunk: chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()), ScheduledTicks: ticks}
	if err := db.StoreColumn(pos, world.Overworld, column); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	loaded, err := db.LoadColumn(pos, world.Overworld)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ScheduledTicks) != 3 {
		t.Fatalf("restored tick count = %d, want 3", len(loaded.ScheduledTicks))
	}
	for i, tick := range loaded.ScheduledTicks {
		if tick.Type != types[i] || tick.Priority != priorities[i] || tick.Kind != ticks[i].Kind {
			t.Fatalf("Java registry identity/priority lost at tick %d: %+v", i, tick)
		}
	}
	data, err := db.ldb.Get((dbKey{pos: pos, dim: world.Overworld}).Sum(keyPendingScheduledTicks), nil)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := nbt.UnmarshalEncoding(data, &stored, nbt.LittleEndian); err != nil {
		t.Fatal(err)
	}
	current, currentOK := stored["currentTick"].(int32)
	entries, entriesOK := stored["tickList"].([]any)
	if !currentOK || !entriesOK || len(entries) != 3 {
		t.Fatalf("BDS cannot consume pending tick root: %#v", stored)
	}
	for i, value := range entries {
		entry := value.(map[string]any)
		trigger, ok := entry["time"].(int64)
		if !ok || trigger-int64(current) != int64(ticks[i].Delay) {
			t.Fatalf("BDS paused delay changed for tick %d: current=%v time=%#v", i, current, entry["time"])
		}
		state, ok := entry["blockState"].(map[string]any)
		if !ok || state["version"] != int32(18168865) {
			t.Fatalf("BDS state payload missing for tick %d: %#v", i, entry["blockState"])
		}
		if i == 2 {
			properties := state["states"].(map[string]any)
			if state["name"] != "minecraft:water" || properties["liquid_depth"] != int32(1) {
				t.Fatalf("native flowing-water state replaced by Java identity: %#v", state)
			}
		}
	}
}
