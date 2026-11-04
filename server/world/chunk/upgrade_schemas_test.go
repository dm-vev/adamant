package chunk_test

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/dm-vev/adamant/net/nbt"
)

// TestOldConnectionStatesAreMissing decodes palette entries of redstone
// wire, fire and chorus plants saved without the connections 1.26.60 added,
// and with them. Schema 0361 upgrades the first to the states connected to
// no side, which the chunk records as missing their connections, to be
// derived from their neighbours; the second are complete.
func TestOldConnectionStatesAreMissing(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	r := world.Overworld.Range()
	v := chunk.CurrentBlockVersion
	for _, tc := range []struct {
		entry   map[string]any
		missing string
	}{
		{map[string]any{"name": "minecraft:redstone_wire", "states": map[string]any{"redstone_signal": int32(7)}, "version": v},
			"[redstone_east redstone_north redstone_south redstone_west]"},
		{map[string]any{"name": "minecraft:fire", "states": map[string]any{"age": int32(3)}, "version": v},
			"[minecraft:connection_east minecraft:connection_north minecraft:connection_south minecraft:connection_up minecraft:connection_west]"},
		{map[string]any{"name": "minecraft:chorus_plant", "states": map[string]any{}, "version": v},
			"[minecraft:connection_down minecraft:connection_east minecraft:connection_north minecraft:connection_south minecraft:connection_up minecraft:connection_west]"},
		{map[string]any{"name": "minecraft:redstone_wire", "states": map[string]any{"redstone_signal": int32(7), "redstone_north": "side",
			"redstone_east": "none", "redstone_south": "side", "redstone_west": "none"}, "version": v}, "[]"},
		{map[string]any{"name": "minecraft:stone", "states": map[string]any{}, "version": v}, "[]"},
	} {
		var b bytes.Buffer
		b.Write([]byte{chunk.SubChunkVersion, 1, 0, 1 << 1})
		// Every block but the first of each word is the entry, the block at
		// (0, 1, 0) among them.
		for range 128 {
			b.Write([]byte{0xfe, 0xff, 0xff, 0xff})
		}
		b.Write([]byte{2, 0, 0, 0})
		enc := nbt.NewEncoderWithEncoding(&b, nbt.LittleEndian)
		if err := enc.Encode(map[string]any{"name": "minecraft:air", "states": map[string]any{}, "version": v}); err != nil {
			t.Fatal(err)
		}
		if err := enc.Encode(tc.entry); err != nil {
			t.Fatal(err)
		}
		data := chunk.SerialisedData{SubChunks: make([][]byte, (r.Height()>>4)+1)}
		data.SubChunks[(0-r.Min())>>4] = b.Bytes()
		c, err := chunk.DiskDecode(world.DefaultBlockRegistry, data, r)
		if err != nil {
			t.Fatal(err)
		}
		name, props, _ := world.DefaultBlockRegistry.RuntimeIDToState(c.Block(0, 1, 0, 0))
		var missing []string
		for _, s := range c.MissingBlockStates() {
			if s.Position == [3]int{0, 1, 0} {
				missing = s.Properties
			}
		}
		slices.Sort(missing)
		if fmt.Sprint(missing) != tc.missing {
			t.Errorf("%v is %s %v, missing %v, want %s", tc.entry["states"], name, props, missing, tc.missing)
		}
	}
}
