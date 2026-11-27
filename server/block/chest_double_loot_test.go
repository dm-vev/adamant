package block_test

import (
	"slices"
	"testing"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/inventory"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type doubleChestTestViewer struct {
	world.NopViewer
}

func (doubleChestTestViewer) ViewSlotChange(int, item.Stack) {}

type chestTestUser struct {
	tx  *world.Tx
	pos mgl64.Vec3
	rot cube.Rotation
}

func (*chestTestUser) Close() error                        { return nil }
func (*chestTestUser) H() *world.EntityHandle              { return nil }
func (u *chestTestUser) Position() mgl64.Vec3              { return u.pos }
func (u *chestTestUser) Rotation() cube.Rotation           { return u.rot }
func (*chestTestUser) HeldItems() (item.Stack, item.Stack) { return item.Stack{}, item.Stack{} }
func (*chestTestUser) SetHeldItems(item.Stack, item.Stack) {}
func (*chestTestUser) UsingItem() bool                     { return false }
func (*chestTestUser) ReleaseItem()                        {}
func (*chestTestUser) UseItem()                            {}
func (u *chestTestUser) PlaceBlock(pos cube.Pos, b world.Block, ctx *item.UseContext) {
	u.tx.SetBlock(pos, b, nil)
	ctx.SubtractFromCount(1)
}

type chestDropEntityType struct{}
type chestDropEntityConfig struct{}
type chestDropEntity struct {
	handle *world.EntityHandle
	data   *world.EntityData
}

func (chestDropEntityConfig) Apply(*world.EntityData) {}
func (chestDropEntityType) Open(_ *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return chestDropEntity{handle: handle, data: data}
}
func (chestDropEntityType) EncodeEntity() string                 { return "test:chest_drop" }
func (chestDropEntityType) BBox(world.Entity) cube.BBox          { return cube.BBox{} }
func (chestDropEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (chestDropEntityType) EncodeNBT(*world.EntityData) map[string]any { return nil }
func (e chestDropEntity) H() *world.EntityHandle                 { return e.handle }
func (e chestDropEntity) Position() mgl64.Vec3                   { return e.data.Pos }
func (e chestDropEntity) Rotation() cube.Rotation                { return e.data.Rot }
func (chestDropEntity) Close() error                             { return nil }

func countHalfItems(inv *inventory.Inventory, start, end int) int {
	n := 0
	for slot := start; slot < end; slot++ {
		if s, err := inv.Item(slot); err == nil && !s.Empty() {
			n++
		}
	}
	return n
}

func equalSlots(a, b []item.Stack) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Comparable(b[i]) || a[i].Count() != b[i].Count() {
			return false
		}
	}
	return true
}

func TestDoubleChestAddViewerThenInventoryGeneratesBothHalvesLoot(t *testing.T) {
	t.Parallel()
	variants := []struct {
		name  string
		make  func(cube.Direction) world.NBTer
		nbtID string
	}{
		{"chest", func(f cube.Direction) world.NBTer { return block.Chest{Facing: f} }, "Chest"},
		{"trapped_chest", func(f cube.Direction) world.NBTer { return block.TrappedChest{Chest: block.Chest{Facing: f}} }, "Chest"},
		{"copper_chest", func(f cube.Direction) world.NBTer { return block.CopperChest{Chest: block.Chest{Facing: f}} }, "Chest"},
	}
	for _, v := range variants {
		for _, openDirect := range []bool{false, true} {
			mode := "AddViewer+Inventory"
			if openDirect {
				mode = "Inventory-only"
			}
			t.Run(v.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
				t.Cleanup(func() { _ = w.Close() })

				leftPos := cube.Pos{0, 65, 0}
				rightPos := cube.Pos{0, 65, 1}
				facing := cube.East // RotateRight().Face() == FaceSouth (+Z) -> rightPos is right half

				<-w.Exec(func(tx *world.Tx) {
					left := world.DecodeNBT(v.make(facing), map[string]any{
						"id":            v.nbtID,
						"pairx":         int32(rightPos[0]),
						"pairz":         int32(rightPos[2]),
						"LootTable":     "chests/bastion_other",
						"LootTableSeed": int64(1001),
					}, world.DefaultBlockRegistry).(world.Block)
					right := world.DecodeNBT(v.make(facing), map[string]any{
						"id":            v.nbtID,
						"pairx":         int32(leftPos[0]),
						"pairz":         int32(leftPos[2]),
						"LootTable":     "chests/bastion_other",
						"LootTableSeed": int64(2002),
					}, world.DefaultBlockRegistry).(world.Block)
					tx.SetBlock(leftPos, left, nil)
					tx.SetBlock(rightPos, right, nil)
				})

				var firstSnap []item.Stack
				<-w.Exec(func(tx *world.Tx) {
					c := tx.Block(leftPos).(block.Container)
					if !openDirect {
						c.AddViewer(doubleChestTestViewer{}, tx, leftPos)
						c = tx.Block(leftPos).(block.Container)
					}
					inv := c.Inventory(tx, leftPos)
					if inv.Size() != 54 {
						t.Fatalf("double chest size = %d, want 54", inv.Size())
					}
					leftCount := countHalfItems(inv, 0, 27)
					rightCount := countHalfItems(inv, 27, 54)
					if leftCount == 0 || rightCount == 0 {
						t.Fatalf("double chest loot missing: leftHalf=%d rightHalf=%d", leftCount, rightCount)
					}
					firstSnap = slices.Clone(inv.Slots())
				})

				<-w.Exec(func(tx *world.Tx) {
					c := tx.Block(rightPos).(block.Container)
					if !openDirect {
						c.AddViewer(doubleChestTestViewer{}, tx, rightPos)
						c = tx.Block(rightPos).(block.Container)
					}
					inv := c.Inventory(tx, rightPos)
					if inv.Size() != 54 {
						t.Fatalf("reopened chest size = %d, want 54", inv.Size())
					}
					if !equalSlots(inv.Slots(), firstSnap) {
						t.Fatal("reopening double chest from opposite half altered loot slots")
					}
				})
			})
		}
	}
}

func TestDoubleChestPlaceNextToUnopenedLootChestPopulatesOnOpen(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })

	lootPos := cube.Pos{0, 65, 0}
	placedPos := cube.Pos{0, 65, 1}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 64, 1}, block.Stone{}, nil)
		c := world.DecodeNBT(block.Chest{Facing: cube.East}, map[string]any{
			"id":            "Chest",
			"LootTable":     "chests/bastion_other",
			"LootTableSeed": int64(4242),
		}, world.DefaultBlockRegistry).(world.Block)
		tx.SetBlock(lootPos, c, nil)
	})
	<-w.Exec(func(tx *world.Tx) {
		user := &chestTestUser{tx: tx, rot: cube.Rotation{90, 0}} // facing West -> opposite is East
		if user.Rotation().Direction().Opposite() != cube.East {
			t.Fatalf("unexpected placed facing %v, want East", user.Rotation().Direction().Opposite())
		}
		if !(block.Chest{}).UseOnBlock(cube.Pos{0, 64, 1}, cube.FaceUp, mgl64.Vec3{}, tx, user, &item.UseContext{}) {
			t.Fatal("UseOnBlock failed to place chest")
		}
		placed, ok := tx.Block(placedPos).(block.Chest)
		if !ok || !placed.Paired() {
			t.Fatalf("placed chest paired=%v, want true", ok && placed.Paired())
		}
		if nb := tx.Block(lootPos).(block.Chest).EncodeNBT(); nb["LootTableSeed"] != int64(4242) {
			t.Fatalf("placing adjacent chest prematurely consumed loot table: %v", nb)
		}
	})
	<-w.Exec(func(tx *world.Tx) {
		c := tx.Block(placedPos).(block.Container)
		c.AddViewer(doubleChestTestViewer{}, tx, placedPos)
		c = tx.Block(placedPos).(block.Container)
		inv := c.Inventory(tx, placedPos)
		if inv.Size() != 54 {
			t.Fatalf("paired size = %d, want 54", inv.Size())
		}
		if countHalfItems(inv, 0, 27) != 0 || countHalfItems(inv, 27, 54) == 0 {
			t.Fatalf("unexpected item distribution: placed(0..27)=%d loot(27..54)=%d", countHalfItems(inv, 0, 27), countHalfItems(inv, 27, 54))
		}
		if nb := tx.Block(lootPos).(block.Chest).EncodeNBT(); nb["LootTable"] != nil {
			t.Fatalf("loot half still has unconsumed loot table: %v", nb["LootTable"])
		}
	})
}

func TestBreakingHalfOfUnopenedDoubleChestLeavesOtherHalfLootIntact(t *testing.T) {
	t.Parallel()
	var dropped []item.Stack
	entities := world.EntityRegistryConfig{
		Item: func(opts world.EntitySpawnOpts, st any) *world.EntityHandle {
			if s, ok := st.(item.Stack); ok {
				dropped = append(dropped, s)
			}
			return opts.New(chestDropEntityType{}, chestDropEntityConfig{})
		},
	}.New([]world.EntityType{chestDropEntityType{}})
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entities}.New()
	t.Cleanup(func() { _ = w.Close() })

	leftPos := cube.Pos{0, 65, 0}
	rightPos := cube.Pos{0, 65, 1}
	<-w.Exec(func(tx *world.Tx) {
		left := world.DecodeNBT(block.Chest{Facing: cube.East}, map[string]any{
			"id":            "Chest",
			"pairx":         int32(rightPos[0]),
			"pairz":         int32(rightPos[2]),
			"LootTable":     "chests/bastion_other",
			"LootTableSeed": int64(5555),
		}, world.DefaultBlockRegistry).(world.Block)
		right := world.DecodeNBT(block.Chest{Facing: cube.East}, map[string]any{
			"id":            "Chest",
			"pairx":         int32(leftPos[0]),
			"pairz":         int32(leftPos[2]),
			"LootTable":     "chests/bastion_other",
			"LootTableSeed": int64(6666),
		}, world.DefaultBlockRegistry).(world.Block)
		tx.SetBlock(leftPos, left, nil)
		tx.SetBlock(rightPos, right, nil)
	})

	<-w.Exec(func(tx *world.Tx) {
		left := tx.Block(leftPos).(block.Chest)
		tx.SetBlock(leftPos, nil, nil)
		left.BreakInfo().BreakHandler(leftPos, tx, nil)

		if len(dropped) == 0 {
			t.Fatal("breaking unopened loot half dropped no items")
		}
		rem := tx.Block(rightPos).(block.Chest)
		if rem.Paired() {
			t.Fatal("remaining chest should be unpaired after breaking partner")
		}
		inv := rem.Inventory(tx, rightPos)
		if inv.Size() != 27 || countHalfItems(inv, 0, 27) == 0 {
			t.Fatalf("remaining single chest failed to generate loot: size=%d items=%d", inv.Size(), countHalfItems(inv, 0, 27))
		}
	})
}
