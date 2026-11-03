package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
)

// customHashRegistry returns a finalized registry with states of custom
// blocks beside the vanilla ones: one of properties of every type a state
// holds (bool, uint8, int32 and string), and a door of its own, open and
// closed, both halves.
func customHashRegistry(t testing.TB) world.BlockRegistry {
	t.Helper()
	br := world.NewBlockRegistry()
	for _, v := range []any{uint8(0), uint8(1)} {
		br.RegisterBlockState(world.BlockState{Name: "test:hashed", Properties: map[string]any{
			"bits": v, "facing": "north", "level": int32(3), "lit": true,
		}})
	}
	for _, top := range []bool{false, true} {
		for _, open := range []bool{false, true} {
			br.RegisterBlockState(world.BlockState{Name: "test:door", Properties: map[string]any{
				"door_hinge_bit": false, "upper_block_bit": top, "open_bit": open,
			}})
		}
	}
	br.Finalize()
	return br
}

// TestBlockHashesOfRegistries checks the session hashes every runtime ID of
// the default registry and of a registry with custom states by its state,
// the state BDS sends for an open upper door half aside, and maps the hash
// of every state back to its runtime ID. Until the world left the hashes to
// the session, it checked them against the world's, both ways.
func TestBlockHashesOfRegistries(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for name, br := range map[string]world.BlockRegistry{
		"default": world.DefaultBlockRegistry,
		"custom":  customHashRegistry(t),
	} {
		t.Run(name, func(t *testing.T) {
			h := hashesOf(br)
			if h != hashesOf(br) {
				t.Fatal("the hashes of a registry are built again")
			}
			if len(h.hash) != br.BlockCount() || len(h.rid) != br.BlockCount() {
				t.Fatalf("%d hashes, %d mapped back, %d runtime IDs", len(h.hash), len(h.rid), br.BlockCount())
			}
			doors := 0
			for rid := range uint32(br.BlockCount()) {
				name, props, _ := br.RuntimeIDToState(rid)
				own, _ := networkBlockHash(name, props, nil)
				want := own
				if client, ok := clientDoorProperties(props); ok {
					want, _ = networkBlockHash(name, client, nil)
					doors++
				}
				if got, ok := h.runtimeIDToHash(rid); !ok || got != want {
					t.Fatalf("runtime ID %d (%s %v) hashes to (%d, %t), want %d", rid, name, props, got, ok, want)
				}
				if back, ok := h.hashToRuntimeID(own); !ok || back != rid {
					t.Fatalf("the hash of %s %v maps back to (%d, %t), want %d", name, props, back, ok, rid)
				}
			}
			if doors == 0 {
				t.Error("no open upper door half")
			}
			if _, ok := h.runtimeIDToHash(uint32(br.BlockCount())); ok {
				t.Error("a runtime ID past the registry has a hash")
			}
		})
	}
}

// TestUpperDoorHalfSentClosed checks the network state of door halves as BDS
// sends them: the upper half never carries open_bit, while the lower half
// does and the server keeps the open state on both.
func TestUpperDoorHalfSentClosed(t *testing.T) {
	br := world.DefaultBlockRegistry
	br.Finalize()
	hash := func(b world.Block) uint32 {
		h, _ := hashesOf(br).runtimeIDToHash(br.BlockRuntimeID(b))
		return h
	}
	for _, door := range []world.Block{
		block.WoodDoor{Wood: block.OakWood(), Top: true, Open: true},
		block.IronDoor{Top: true, Open: true},
		block.CopperDoor{Top: true, Open: true},
	} {
		var closedTop, openBottom, closedBottom world.Block
		switch d := door.(type) {
		case block.WoodDoor:
			closedTop, openBottom, closedBottom = block.WoodDoor{Wood: d.Wood, Top: true}, block.WoodDoor{Wood: d.Wood, Open: true}, block.WoodDoor{Wood: d.Wood}
		case block.IronDoor:
			closedTop, openBottom, closedBottom = block.IronDoor{Top: true}, block.IronDoor{Open: true}, block.IronDoor{}
		case block.CopperDoor:
			closedTop, openBottom, closedBottom = block.CopperDoor{Top: true}, block.CopperDoor{Open: true}, block.CopperDoor{}
		}
		if hash(door) != hash(closedTop) {
			t.Errorf("%T: open upper half is sent open", door)
		}
		if hash(openBottom) == hash(closedBottom) {
			t.Errorf("%T: open lower half is sent closed", door)
		}
		if br.BlockRuntimeID(door) == br.BlockRuntimeID(closedTop) {
			t.Errorf("%T: server lost the open state of the upper half", door)
		}
	}
}

// TestCustomDoorUpperHalfSentClosed checks a door of a custom state is sent
// as BDS sends doors: its open upper half as the closed one, which the hash
// of the open upper half does not map back to.
func TestCustomDoorUpperHalfSentClosed(t *testing.T) {
	br := customHashRegistry(t)
	h := hashesOf(br)
	rid := func(top, open bool) uint32 {
		rid, ok := br.StateToRuntimeID("test:door", map[string]any{"door_hinge_bit": false, "upper_block_bit": top, "open_bit": open})
		if !ok {
			t.Fatal("no state of the custom door")
		}
		return rid
	}
	openTop, closedTop := rid(true, true), rid(true, false)
	if h.hash[openTop] != h.hash[closedTop] {
		t.Error("the open upper half of a custom door is sent open")
	}
	if h.hash[rid(false, true)] == h.hash[rid(false, false)] {
		t.Error("the open lower half of a custom door is sent closed")
	}
	if back, _ := h.hashToRuntimeID(h.hash[closedTop]); back != closedTop {
		t.Errorf("the hash of the closed upper half maps back to %d, want %d", back, closedTop)
	}
}

// TestBlockHashCollisionPanics checks two states of one hash fail the build
// of the hashes with the message the registry gave.
func TestBlockHashCollisionPanics(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if want := "network block hash collision for (minecraft:unknown map[]) and (minecraft:unknown map[])"; msg != want {
			t.Fatalf("panic %q, want %q", r, want)
		}
	}()
	buildBlockHashes(collidingRegistry{world.DefaultBlockRegistry})
}

// collidingRegistry is a registry of two runtime IDs of the same state,
// which the hash of minecraft:unknown gives without hashing.
type collidingRegistry struct{ world.BlockRegistry }

func (collidingRegistry) BlockCount() int { return 2 }
func (collidingRegistry) RuntimeIDToState(uint32) (string, map[string]any, bool) {
	return "minecraft:unknown", map[string]any{}, true
}
