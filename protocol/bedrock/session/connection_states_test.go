package session

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/internal/testfixture"
	"github.com/dm-vev/adamant/net/nbt"
)

// connectionCapture is what cmd/vanilla-probe -capture wrote against BDS
// 1.26.61.1 as a client of 2225, running the commands of
// testdata/connection_states.commands.
type connectionCapture struct {
	Protocol                    int32            `json:"protocol"`
	Origin                      [3]int32         `json:"origin"`
	EditorLevelMigrationVersion int8             `json:"editor_level_migration_version"`
	ClientBiomeUpdate           map[string]int   `json:"client_biome_update"`
	Commands                    []string         `json:"commands"`
	Updates                     []capturedUpdate `json:"updates"`
	SubChunks                   []struct {
		Position [3]int32  `json:"position"`
		Result   uint8     `json:"result"`
		Palettes [][]int32 `json:"palettes"`
	} `json:"sub_chunks"`
}

type capturedUpdate struct {
	Position [3]int32 `json:"position"`
	Layer    uint32   `json:"layer"`
	Hash     uint32   `json:"hash"`
}

// paletteStates returns the states of the palette of 2225, Geyser's, by
// their network hashes, as a string of the name and the sorted properties.
func paletteStates(t *testing.T) map[uint32]string {
	t.Helper()
	b, err := testfixture.Read("../internal/bedrockdata/testdata/v2225/palette.nbt")
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := nbt.UnmarshalEncoding(b, &root, nbt.BigEndian); err != nil {
		t.Fatal(err)
	}
	out := map[uint32]string{}
	var scratch []byte
	for _, s := range root["blocks"].([]any) {
		block := s.(map[string]any)
		name, _ := block["name"].(string)
		props, _ := block["states"].(map[string]any)
		var hash uint32
		hash, scratch = networkBlockHash(name, props, scratch)
		keys := make([]string, 0, len(props))
		for k, v := range props {
			keys = append(keys, fmt.Sprintf("%s=%v", strings.TrimPrefix(k, "minecraft:"), v))
		}
		sort.Strings(keys)
		out[hash] = strings.TrimPrefix(name, "minecraft:") + " " + strings.Join(keys, " ")
	}
	return out
}

// TestConnectionStatesOfBDS reads what BDS 1.26.61.1 sent a client of 2225
// for redstone wire, fire and chorus plants placed by /setblock, the open
// question R1 of the design: it sends the states of their connections as
// it computes them from their neighbours, not the defaults of pmmp's
// schema 0361, which the adapter sends today (TestTablesOf2225). A client
// of 2225 draws those blocks from the states it is sent, so the game needs
// the connection states of the three blocks to show them right to it.
// The capture also tells the values of the fields 2225 added that BDS
// fills: EditorLevelMigrationVersion 0 in StartGame and ClientBiomeUpdate
// false in every LevelChunk, as the step up from 2193 fills them.
func TestConnectionStatesOfBDS(t *testing.T) {
	b, err := os.ReadFile("testdata/bedrock_1_26_61_connection_states.json")
	if err != nil {
		t.Fatal(err)
	}
	var c connectionCapture
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if c.Protocol != 2225 || c.EditorLevelMigrationVersion != 0 || c.ClientBiomeUpdate["true"] != 0 || c.ClientBiomeUpdate["false"] == 0 {
		t.Fatalf("capture of protocol %d, editor migration %d, biome updates %v", c.Protocol, c.EditorLevelMigrationVersion, c.ClientBiomeUpdate)
	}
	states := paletteStates(t)
	// BDS holds a new player at a height it does not tell: the blocks are
	// found by the first wire, which the commands place at ~2 ~ ~.
	origin, found := [3]int32{}, false
	for _, u := range c.Updates {
		if strings.HasPrefix(states[u.Hash], "redstone_wire") {
			origin, found = [3]int32{u.Position[0] - 2, u.Position[1], u.Position[2]}, true
			break
		}
	}
	if !found {
		t.Fatal("BDS sent no redstone wire")
	}
	c.Origin = origin
	at := func(dx, dy, dz int32) [3]int32 { return [3]int32{c.Origin[0] + dx, c.Origin[1] + dy, c.Origin[2] + dz} }
	last := map[[3]int32]string{}
	for _, u := range c.Updates {
		if u.Layer == 0 {
			last[u.Position] = states[u.Hash]
		}
	}
	for pos, want := range map[[3]int32]string{
		// The line of wire along x, its corner at x 4 turning to z 1, and
		// the step up onto the block at x 5.
		at(2, 0, 0): "redstone_wire redstone_east=side redstone_north=none redstone_signal=0 redstone_south=none redstone_west=side",
		at(3, 0, 0): "redstone_wire redstone_east=side redstone_north=none redstone_signal=0 redstone_south=none redstone_west=side",
		at(4, 0, 0): "redstone_wire redstone_east=up redstone_north=none redstone_signal=0 redstone_south=side redstone_west=side",
		at(4, 0, 1): "redstone_wire redstone_east=none redstone_north=side redstone_signal=0 redstone_south=side redstone_west=none",
		// The wire on the block BDS leaves unconnected: the one below
		// climbs to it.
		at(5, 1, 0): "redstone_wire redstone_east=none redstone_north=none redstone_signal=0 redstone_south=none redstone_west=none",
		// The dot alone.
		at(2, 0, 6): "redstone_wire redstone_east=none redstone_north=none redstone_signal=0 redstone_south=none redstone_west=none",
	} {
		if got := last[pos]; got != want {
			t.Errorf("at %v BDS last sent %q, want %q", pos, got, want)
		}
	}
	// What else BDS sent of the three blocks, in the order it sent it.
	var sent []string
	for _, u := range c.Updates {
		if s := states[u.Hash]; strings.HasPrefix(s, "redstone_wire") || strings.HasPrefix(s, "fire") || strings.HasPrefix(s, "chorus_plant") {
			sent = append(sent, fmt.Sprintf("%v %s", [3]int32{u.Position[0] - c.Origin[0], u.Position[1] - c.Origin[1], u.Position[2] - c.Origin[2]}, s))
		}
	}
	var palettes []string
	for _, s := range c.SubChunks {
		for _, p := range s.Palettes {
			for _, h := range p {
				if st := states[uint32(h)]; strings.HasPrefix(st, "redstone_wire") || strings.HasPrefix(st, "fire") || strings.HasPrefix(st, "chorus_plant") {
					palettes = append(palettes, st)
				}
			}
		}
	}
	slices.Sort(palettes)
	palettes = slices.Compact(palettes)
	// Of fire and chorus plants BDS sent no update: the sub-chunks it sent
	// after hold their states, connected as their neighbours are.
	for _, want := range []string{
		"fire age=0 connection_east=0 connection_north=0 connection_south=0 connection_up=0 connection_west=1",
		"chorus_plant connection_down=1 connection_east=0 connection_north=0 connection_south=0 connection_up=1 connection_west=0",
		"chorus_plant connection_down=1 connection_east=1 connection_north=0 connection_south=0 connection_up=0 connection_west=0",
		"chorus_plant connection_down=0 connection_east=0 connection_north=0 connection_south=0 connection_up=0 connection_west=1",
		"redstone_wire redstone_east=up redstone_north=none redstone_signal=0 redstone_south=side redstone_west=side",
	} {
		if !slices.Contains(palettes, want) {
			t.Errorf("no sub-chunk holds %s", want)
		}
	}
	t.Logf("updates of the three blocks, from the first wire:\n%s\nin the palettes of the sub-chunks:\n%s", strings.Join(sent, "\n"), strings.Join(palettes, "\n"))
}
