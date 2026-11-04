package session

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestTablesOf2225 checks the tables of the clients of 2225: the states of
// redstone wire, fire and chorus plants, and those alone, hash as 2225
// names them, with their connections none, and back to their runtime IDs;
// the items take 2225's runtime IDs; the entities and biomes are those of
// 2193. Their payloads of columns are of a class of their own.
func TestTablesOf2225(t *testing.T) {
	br := world.DefaultBlockRegistry
	old, now := tablesOf(versions.Default(), br), tablesOf(versions.Model(), br)
	game, h := old.hashes(), now.hashes()
	if game != hashesOf(br) || h == game || now.slot.Load() == nil || old.slot.Load() != nil {
		t.Fatal("the tables of 2225 are the game's, or of no class of their own")
	}
	changed := map[string]int{}
	scratch := []byte(nil)
	for i := range br.BlockCount() {
		rid := uint32(i)
		name, props, _ := br.RuntimeIDToState(rid)
		// The upper half of an open door is sent closed, in every protocol,
		// and is read back as the closed one.
		wantRID, _ := game.hashToRuntimeID(game.hash[rid])
		if back, ok := h.hashToRuntimeID(h.hash[rid]); !ok || back != wantRID {
			t.Fatalf("%s %v: hash %d is runtime ID %d, %v, want %d", name, props, h.hash[rid], back, ok, wantRID)
		}
		if h.hash[rid] == game.hash[rid] {
			continue
		}
		changed[name]++
		want := map[string]any{}
		for k, v := range props {
			want[k] = v
		}
		switch name {
		case "minecraft:redstone_wire":
			for _, side := range []string{"north", "east", "south", "west"} {
				want["redstone_"+side] = "none"
			}
		default:
			sides := []string{"north", "east", "south", "west", "up"}
			if name == "minecraft:chorus_plant" {
				sides = append(sides, "down")
			}
			for _, side := range sides {
				want["minecraft:connection_"+side] = uint8(0)
			}
		}
		var hash uint32
		if hash, scratch = networkBlockHash(name, want, scratch); hash != h.hash[rid] {
			t.Errorf("%s %v hashes as no state of 2225", name, props)
		}
	}
	if len(changed) != 3 || changed["minecraft:redstone_wire"] != 16 || changed["minecraft:fire"] != 16 || changed["minecraft:chorus_plant"] != 1 {
		t.Errorf("2225 names otherwise %v", changed)
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

// TestChunksOf2225 encodes a sub-chunk of redstone wire for the clients of
// each protocol: each hashes it as its protocol names it, and keeps the
// payload in the cache of its class.
func TestChunksOf2225(t *testing.T) {
	br := world.DefaultBlockRegistry
	wire, _ := chunk.StateToRuntimeID("minecraft:redstone_wire", map[string]any{"redstone_signal": int32(3)})
	c := chunk.New(br, cube.Range{0, 255})
	c.SetBlock(0, 0, 0, 0, wire)
	col := &world.Column{Chunk: c}
	old, now := tablesOf(versions.Default(), br), tablesOf(versions.Model(), br)
	a, b := old.networkSubChunkPayload(col, 0), now.networkSubChunkPayload(col, 0)
	if string(a) == string(b) {
		t.Fatal("the redstone wire of 2193 and 2225 encode alike")
	}
	if cached, _ := col.Cache(nil).CachedNetworkSubChunkPayload(0); string(cached) != string(a) {
		t.Fatal("the column's own cache does not hold 2193's sub-chunk")
	}
	if cached, _ := col.Cache(now.slot.Load()).CachedNetworkSubChunkPayload(0); string(cached) != string(b) {
		t.Fatal("the cache of 2225's class does not hold its sub-chunk")
	}
	if again := now.networkSubChunkPayload(col, 0); &again[0] != &b[0] {
		t.Fatal("2225 encoded its sub-chunk again")
	}
}
