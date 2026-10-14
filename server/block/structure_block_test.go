package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// structureRow builds the 3x1x2 structure the Bedrock Dedicated Server probe
// loaded: stone, glass and gold along X, lapis behind the stone, and air.
func structureRow(tx *world.Tx, sb cube.Pos) {
	tx.SetBlock(sb.Add(cube.Pos{1, 0, 0}), Stone{}, nil)
	tx.SetBlock(sb.Add(cube.Pos{2, 0, 0}), Glass{}, nil)
	tx.SetBlock(sb.Add(cube.Pos{3, 0, 0}), Gold{}, nil)
	tx.SetBlock(sb.Add(cube.Pos{1, 0, 1}), Lapis{}, nil)
}

// TestStructureBlocksLoadInTheirBoxLikeBedrock checks that a structure block
// keeps a rotated or mirrored structure in its box at the offset, with the
// blocks where Bedrock Dedicated Server 1.26.52 places them.
func TestStructureBlocksLoadInTheirBoxLikeBedrock(t *testing.T) {
	type at struct {
		x, z int
		b    world.Block
	}
	cases := []struct {
		name             string
		rotation, mirror uint8
		want             []at
	}{
		{"rotated 90", 1, 0, []at{{0, 0, Lapis{}}, {1, 0, Stone{}}, {1, 1, Glass{}}, {1, 2, Gold{}}}},
		{"rotated 180", 2, 0, []at{{0, 1, Gold{}}, {1, 1, Glass{}}, {2, 1, Stone{}}, {2, 0, Lapis{}}}},
		{"rotated 270", 3, 0, []at{{0, 0, Gold{}}, {0, 1, Glass{}}, {0, 2, Stone{}}, {1, 2, Lapis{}}}},
		{"mirrored along X", 0, 1, []at{{0, 0, Lapis{}}, {0, 1, Stone{}}, {1, 1, Glass{}}, {2, 1, Gold{}}}},
		{"mirrored along Z", 0, 2, []at{{0, 0, Gold{}}, {1, 0, Glass{}}, {2, 0, Stone{}}, {2, 1, Lapis{}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			defer w.Close()
			<-w.Exec(func(tx *world.Tx) {
				sb := cube.Pos{9, 70, 10}
				structureRow(tx, sb)
				save := NewStructureBlock(StructureBlockSave)
				save.Name, save.Offset, save.Size = "row", cube.Pos{1, 0, 0}, [3]int{3, 1, 2}
				if !save.Save(sb, tx, false) {
					t.Fatal("structure block did not save the row")
				}
				load := save
				load.Mode, load.Offset, load.Rotation, load.Mirror = StructureBlockLoad, cube.Pos{1, 0, 5}, c.rotation, c.mirror
				if !load.Load(sb, tx) {
					t.Fatal("structure block did not load the row")
				}
				origin := sb.Add(load.Offset)
				for _, want := range c.want {
					if got := tx.Block(origin.Add(cube.Pos{want.x, 0, want.z})); got != want.b {
						t.Errorf("block %d, %d into the box is %T, want %T", want.x, want.z, got, want.b)
					}
				}
			})
		})
	}
}

// TestPoweredStructureBlocksSaveAndLoad checks StructureBlock.trigger: a
// powered structure block in save mode saves its box, one in load mode places
// the structure, and one in corner mode forgets the structure kept in memory.
func TestPoweredStructureBlocksSaveAndLoad(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	saver, loader, corner := cube.Pos{0, 70, 0}, cube.Pos{0, 70, 20}, cube.Pos{0, 70, 40}
	power := func(pos cube.Pos) {
		<-w.Exec(func(tx *world.Tx) { tx.SetBlock(pos.Side(cube.FaceDown), RedstoneBlock{}, nil) })
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		structureRow(tx, saver)
		save := NewStructureBlock(StructureBlockSave)
		save.Name, save.Offset, save.Size = "row", cube.Pos{1, 0, 0}, [3]int{3, 1, 2}
		tx.SetBlock(saver, save, nil)
		load := NewStructureBlock(StructureBlockLoad)
		load.Name, load.Offset = "row", cube.Pos{1, 0, 0}
		tx.SetBlock(loader, load, nil)
		c := NewStructureBlock(StructureBlockCorner)
		c.Name = "row"
		tx.SetBlock(corner, c, nil)
	})
	power(saver)
	<-w.Exec(func(tx *world.Tx) {
		if _, ok, _ := tx.World().StructureTemplate("row"); !ok {
			t.Fatal("powering the structure block in save mode did not save the row")
		}
	})
	power(loader)
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(loader.Add(cube.Pos{3, 0, 0})).(Gold); !ok {
			t.Fatalf("powering the structure block in load mode placed %#v where the gold goes", tx.Block(loader.Add(cube.Pos{3, 0, 0})))
		}
		if got := tx.Block(loader).(StructureBlock).Size; got != [3]int{3, 1, 2} {
			t.Fatalf("loading structure block has size %v, want the size of the row", got)
		}
	})
	power(corner)
	<-w.Exec(func(tx *world.Tx) {
		if _, ok, _ := tx.World().StructureTemplate("row"); ok {
			t.Fatal("powering a corner structure block kept the structure in memory")
		}
	})
}

// TestStructureBlockNBTMatchesBedrock checks the structure block survives
// the block entity data Bedrock Dedicated Server writes.
func TestStructureBlockNBTMatchesBedrock(t *testing.T) {
	bds := map[string]any{"BlockEntityVersion": int32(0), "animationMode": uint8(1), "animationSeconds": float32(2.5), "data": int32(2), "dataField": "marker", "id": "StructureBlock",
		"ignoreEntities": uint8(1), "includePlayers": uint8(1), "integrity": float32(50), "isPowered": uint8(0), "lastTouchedPlayerId": int64(-4294967295), "mirror": uint8(2), "redstoneSaveMode": int32(1),
		"removeBlocks": uint8(1), "rotation": uint8(3), "seed": int64(7), "showBoundingBox": uint8(0), "structureName": "probe:test", "x": int32(1), "xStructureOffset": int32(1), "xStructureSize": int32(3),
		"y": int32(120), "yStructureOffset": int32(0), "yStructureSize": int32(3), "z": int32(0), "zStructureOffset": int32(6), "zStructureSize": int32(3)}
	b := StructureBlock{}.DecodeNBT(bds).(StructureBlock)
	want := StructureBlock{Mode: StructureBlockLoad, Name: "probe:test", DataField: "marker", Offset: cube.Pos{1, 0, 6}, Size: [3]int{3, 3, 3}, IgnoreEntities: true, IncludePlayers: true,
		RemoveBlocks: true, Rotation: 3, Mirror: 2, Integrity: 50, Seed: 7, RedstoneSaveMode: 1, AnimationMode: 1, AnimationSeconds: 2.5, LastTouchedPlayerID: -4294967295}
	if b != want {
		t.Fatalf("decoded %+v, want %+v", b, want)
	}
	encoded := b.EncodeNBT()
	for key, value := range bds {
		if key == "x" || key == "y" || key == "z" || key == "BlockEntityVersion" {
			continue
		}
		if encoded[key] != value {
			t.Errorf("encoded %s=%v (%T), Bedrock writes %v (%T)", key, encoded[key], encoded[key], value, value)
		}
	}
}
