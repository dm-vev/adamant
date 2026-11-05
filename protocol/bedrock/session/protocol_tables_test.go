package session

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// TestTablesOf2193 checks the tables of the clients of 2193, now that the
// game holds the blocks of 2225: the states of redstone wire, fire and
// chorus plants hash without their connections, and are read back as those
// connected to no side; the three blocks 2193 lacks hash as their
// fallbacks, read back as those; every other state hashes as the game's;
// the items take 2193's runtime IDs; the entities and biomes are those of
// 2193. Their payloads of columns are kept in the cache of the key of 2193,
// and those of 2225, the model, in the column's own.
func TestTablesOf2193(t *testing.T) {
	br := world.DefaultBlockRegistry
	old, now := tablesOf(oldProtocol(), br), tablesOf(versions.Model(), br)
	game, h := now.hashes(), old.hashes()
	if game != hashesOf(br) || h == game || old.slot.Load() == nil || now.slot.Load() != nil {
		t.Fatal("the tables of 2193 are the game's, or 2193's keep the column's own cache")
	}
	changed := map[string]int{}
	scratch := []byte(nil)
	for i := range br.BlockCount() {
		rid := uint32(i)
		name, props, _ := br.RuntimeIDToState(rid)
		want, wantName := map[string]any{}, name
		for k, v := range props {
			if !strings.HasPrefix(k, "minecraft:connection_") && !strings.HasPrefix(k, "redstone_") || k == "redstone_signal" {
				want[k] = v
			}
		}
		switch name {
		case "minecraft:redstone_wire", "minecraft:fire", "minecraft:chorus_plant":
		case "minecraft:disc_press":
			wantName, want = "minecraft:stonecutter_block", map[string]any{"minecraft:cardinal_direction": "north"}
		case "minecraft:ice_crystal":
			wantName, want = "minecraft:blue_ice", map[string]any{}
		case "minecraft:icicle":
			wantName, want = "minecraft:pointed_dripstone", map[string]any{"dripstone_thickness": "tip", "hanging": uint8(1)}
		default:
			// The upper half of an open door is sent closed, in every
			// protocol, and is read back as the closed one.
			wantRID, _ := game.hashToRuntimeID(game.hash[rid])
			if h.hash[rid] != game.hash[rid] {
				t.Fatalf("%s %v hashes otherwise in 2193", name, props)
			}
			if back, ok := h.hashToRuntimeID(h.hash[rid]); !ok || back != wantRID {
				t.Fatalf("%s %v: hash %d is runtime ID %d, %v, want %d", name, props, h.hash[rid], back, ok, wantRID)
			}
			continue
		}
		changed[name]++
		var hash uint32
		if hash, scratch = networkBlockHash(wantName, want, scratch); hash != h.hash[rid] {
			t.Errorf("%s %v hashes as no state of 2193", name, props)
		}
		// A hash of 2193 is read back as the state of the game it names:
		// connected to no side, or the fallback itself.
		backRID, _ := h.hashToRuntimeID(h.hash[rid])
		backName, backProps, _ := br.RuntimeIDToState(backRID)
		if backName != wantName && backName != name {
			t.Errorf("%s %v is read back as %s %v", name, props, backName, backProps)
		}
		for k, v := range backProps {
			if strings.HasPrefix(k, "minecraft:connection_") && v != uint8(0) || strings.HasPrefix(k, "redstone_") && k != "redstone_signal" && v != "none" {
				t.Errorf("%s %v is read back connected, as %v", name, props, backProps)
			}
		}
	}
	if fmt.Sprint(changed) != "map[minecraft:chorus_plant:64 minecraft:disc_press:4 minecraft:fire:512 minecraft:ice_crystal:6 minecraft:icicle:20 minecraft:redstone_wire:1296]" {
		t.Errorf("2193 names otherwise %v", changed)
	}
	for _, it := range []world.Item{item.Apple{}, item.Bundle{}, block.Stone{}} {
		a, _, okA := old.itemRuntimeID(it)
		b, _, okB := now.itemRuntimeID(it)
		_, isBlock := it.(world.Block)
		if !okA || !okB || isBlock && a != b || !isBlock && b != a+1 {
			t.Errorf("%T is %d in 2193, %d in 2225", it, a, b)
		}
	}
	ids := []string{"minecraft:zombie", "minecraft:pig", "adamant:custom"}
	if got := now.entities(ids); !slices.Equal(got, ids) {
		t.Errorf("entities of 2225 %v", got)
	}
	if got := now.entities([]string{"minecraft:not_an_entity", "minecraft:pig"}); !slices.Equal(got, []string{"minecraft:pig"}) {
		t.Errorf("a vanilla entity 2225 does not know is told: %v", got)
	}
	a, _ := old.biomes()
	b, _ := now.biomes()
	if len(a) == 0 || len(a) != len(b) {
		t.Errorf("%d biomes of 2193, %d of 2225", len(a), len(b))
	}
}

// TestChunksOf2193 encodes a sub-chunk of redstone wire connected to the
// north for the clients of each protocol: each hashes it as its protocol
// names it, and keeps the payload in its cache, the model's the column's
// own.
func TestChunksOf2193(t *testing.T) {
	br := world.DefaultBlockRegistry
	wire := br.BlockRuntimeID(block.RedstoneWire{Power: 3, North: block.SideWireConnection()})
	c := chunk.New(br, cube.Range{0, 255})
	c.SetBlock(0, 0, 0, 0, wire)
	col := &world.Column{Chunk: c}
	old, now := tablesOf(oldProtocol(), br), tablesOf(versions.Model(), br)
	a, b := old.networkSubChunkPayload(col, 0), now.networkSubChunkPayload(col, 0)
	if string(a) == string(b) {
		t.Fatal("the redstone wire of 2193 and 2225 encode alike")
	}
	if cached, _ := col.Cache(nil).CachedNetworkSubChunkPayload(0); string(cached) != string(b) {
		t.Fatal("the column's own cache does not hold 2225's sub-chunk")
	}
	if cached, _ := col.Cache(old.slot.Load()).CachedNetworkSubChunkPayload(0); string(cached) != string(a) {
		t.Fatal("the cache of 2193's key does not hold its sub-chunk")
	}
	if again := old.networkSubChunkPayload(col, 0); &again[0] != &a[0] {
		t.Fatal("2193 encoded its sub-chunk again")
	}
}

// TestDiscPressOf2193 checks the disc press of 1.26.60 reaches the clients
// of 2193, which lack it, as a stonecutter: the hash of its block and the
// runtime ID of its item are a stonecutter's, while 2225's clients know its
// own; and the registry enums of commands leave it out for 2193 alone.
func TestDiscPressOf2193(t *testing.T) {
	br := world.DefaultBlockRegistry
	old, now := tablesOf(oldProtocol(), br), tablesOf(versions.Model(), br)
	press, cutter := block.DiscPress{Facing: cube.South}, block.Stonecutter{Facing: cube.North}
	if a, b := old.blockID(press), old.blockID(cutter); a != b {
		t.Errorf("2193 hashes a disc press as %d, a stonecutter as %d", a, b)
	}
	if now.blockID(press) == now.blockID(cutter) {
		t.Error("2225 hashes a disc press as a stonecutter")
	}
	ridA, _, okA := old.itemRuntimeID(press)
	ridB, _, okB := old.itemRuntimeID(cutter)
	if !okA || !okB || ridA != ridB {
		t.Errorf("2193 knows the disc press item as %d (%v), the stonecutter as %d (%v)", ridA, okA, ridB, okB)
	}
	if rid, _, ok := now.itemRuntimeID(press); !ok || rid == ridB {
		t.Errorf("2225 knows the disc press item as %d (%v)", rid, ok)
	}
	for _, tc := range []struct {
		tables *wireTables
		want   bool
	}{{old, false}, {now, true}} {
		pk := &mc.AvailableCommandsPacket{
			EnumValues: []string{"stone", "disc_press", "minecraft:disc_press", "apple"},
			Enums: []mc.CommandEnum{
				{Type: "Block", ValueIndices: []uint32{0, 1, 2}},
				{Type: "Item", ValueIndices: []uint32{3, 1, 2}},
			},
			Constraints: []mc.CommandEnumConstraint{{EnumValueIndex: 2, EnumIndex: 0}, {EnumValueIndex: 3, EnumIndex: 1}},
		}
		tc.tables.dropUnknownEnumOptions(pk)
		listed := slices.Contains(pk.EnumValues, "disc_press")
		if listed != tc.want {
			t.Errorf("protocol %d lists the disc press: %v, want %v", tc.tables.proto.Number, listed, tc.want)
		}
		if !tc.want {
			if fmt.Sprint(pk.EnumValues, pk.Enums, pk.Constraints) != "[stone apple] [{Block [0]} {Item [1]}] [{1 1 []}]" {
				t.Errorf("2193's enums are %v %v %v", pk.EnumValues, pk.Enums, pk.Constraints)
			}
		}
	}
}
