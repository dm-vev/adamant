package world

import (
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

// nativeOnlyCase is a native state without a Java block state, and the
// Java block state game logic reads it as.
type nativeOnlyCase struct {
	name       string
	properties map[string]any
	// water puts water in the second layer.
	water bool
	// java is the name of the Java block it reads as, "" for none.
	java string
}

var nativeOnlyCases = []nativeOnlyCase{
	{name: "minecraft:torch", properties: map[string]any{"torch_facing_direction": "unknown"}, java: "minecraft:torch"},
	{name: "minecraft:unlit_redstone_torch", properties: map[string]any{"torch_facing_direction": "unknown"}, java: "minecraft:redstone_torch"},
	{name: "minecraft:skeleton_skull", properties: map[string]any{"facing_direction": int32(0)}, java: "minecraft:skeleton_skull"},
	{name: "minecraft:oak_hanging_sign", properties: map[string]any{"attached_bit": uint8(0), "facing_direction": int32(0), "ground_sign_direction": int32(0), "hanging": uint8(0)}, java: "minecraft:oak_hanging_sign"},
	{name: "minecraft:jigsaw", properties: map[string]any{"facing_direction": int32(2), "rotation": int32(1)}, java: "minecraft:jigsaw"},
	{name: "minecraft:structure_block", properties: map[string]any{"structure_block_type": "export"}, java: "minecraft:structure_block"},
	{name: "minecraft:portal", properties: map[string]any{"portal_axis": "unknown"}, java: "minecraft:nether_portal"},
	{name: "minecraft:leaf_litter", properties: map[string]any{"growth": int32(5), "minecraft:cardinal_direction": "east"}, java: "minecraft:leaf_litter"},
	{name: "minecraft:white_carpet", water: true, java: "minecraft:white_carpet"},
	{name: "minecraft:skeleton_skull", properties: map[string]any{"facing_direction": int32(0)}, water: true, java: "minecraft:skeleton_skull"},
	{name: "minecraft:underwater_torch", properties: map[string]any{"torch_facing_direction": "unknown"}},
}

// place sets the native state of tc at pos, checking it has no Java block
// state.
func (tc nativeOnlyCase) place(t *testing.T, tx *Tx, pos cube.Pos) {
	t.Helper()
	reg := tx.World().conf.Blocks
	rid, ok := reg.StateToRuntimeID(tc.name, tc.properties)
	if !ok {
		t.Fatalf("no native state %s%v", tc.name, tc.properties)
	}
	state := NativeState{Block: rid, Liquid: reg.AirRuntimeID()}
	if tc.water {
		if state.Liquid, ok = reg.StateToRuntimeID("minecraft:water", waterSourceProperties); !ok {
			t.Fatal("no native water")
		}
	}
	tx.SetBlock(pos, reg.BlockByRuntimeIDOrAir(rid), nil)
	SetNativeState(tx.chunk(chunkPosFromBlockPos(pos)).Chunk, uint8(pos[0]&15), int16(pos[1]), uint8(pos[2]&15), state)
	if _, err := tx.BlockStateID(pos); err == nil {
		t.Fatalf("%s%v (water %v) has a Java block state", tc.name, tc.properties, tc.water)
	}
}

// Game logic reads a native state without a Java block state as the Java
// block of the same name, without the water Java cannot waterlog it with,
// and a block of no Java block at all by its collision shape. Placing,
// survival and the heightmaps read neighbours so, instead of panicking on
// them as they did: a block placed beside one, a plant ticked above one, or
// the spawning of mobs over one panicked.
func TestGameLogicReadsBlocksWithoutJavaState(t *testing.T) {
	w := Config{Synchronous: true, RandomTickSpeed: -1, Log: slog.New(slog.DiscardHandler)}.New()
	defer func() { _ = w.Close() }()
	wallTorch, err := gamedata.BlockStateID("minecraft:wall_torch", map[string]any{"facing": "north"})
	if err != nil {
		t.Fatal(err)
	}
	torch, err := gamedata.BlockStateID("minecraft:torch", nil)
	if err != nil {
		t.Fatal(err)
	}
	stone := w.conf.Blocks.BlockByRuntimeIDOrAir(ResolveStateID(w.conf.Blocks, javaDefault("minecraft:stone")).Block)
	done := false
	<-w.Exec(func(tx *Tx) {
		for i, tc := range nativeOnlyCases {
			pos := cube.Pos{i * 3, 65, 0}
			tx.SetBlock(pos.Side(cube.FaceDown), stone, nil)
			tc.place(t, tx, pos)

			id, ok := tx.JavaBlockState(pos)
			if name, _ := gamedata.BlockStateName(id); ok != (tc.java != "") || ok && name != tc.java {
				t.Errorf("%s: reads as %q (%v), want %q", tc.name, name, ok, tc.java)
			}
			// What the neighbours read: the Java state, or a stand-in by the
			// collision shape of a block of no Java block.
			collides := len(tx.Block(pos).Model().BBox(pos, tx)) != 0
			if !ok {
				id = javaAir()
				if collides {
					id = javaBedrock()
				}
			}
			shape := javaGenerationShape(id)
			// A wall torch placed against its north face, and a torch on top.
			wall, _ := tx.JavaPlacement(wallTorch, pos.Side(cube.FaceNorth))
			standing, _ := tx.JavaPlacement(torch, pos.Side(cube.FaceUp))
			if want := shape.Sturdy&(1<<uint(cube.FaceNorth)) != 0; javaIsAir(wall) == want {
				t.Errorf("%s: a wall torch against it is %s, want one surviving: %v", tc.name, javaName(wall), want)
			}
			if want := shape.Center&(1<<uint(cube.FaceUp)) != 0; javaIsAir(standing) == want {
				t.Errorf("%s: a torch on it is %s, want one surviving: %v", tc.name, javaName(standing), want)
			}
			// The heightmap counts the water a block holds, and else the block
			// as it reads.
			wantHeight := 65
			if in, _ := gamedata.BlockTagContainsID("minecraft:blocks_motion_in_heightmap", id); in || tc.water || !ok && collides {
				wantHeight = 66
			}
			if height := tx.JavaMotionBlockingHeight(pos[0], pos[2], false); height != wantHeight {
				t.Errorf("%s: motion-blocking height %d, want %d", tc.name, height, wantHeight)
			}
		}
		done = true
	})
	if !done {
		t.Fatal("the transaction panicked")
	}
}
