package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestHopperMinecartPullAndCooldown(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		b := hopperMinecartConf.New()
		above := cube.Pos{0, 2, 0}
		source := block.NewBarrel()
		_ = b.inv.SetItem(0, item.NewStack(item.Diamond{}, 2))
		_ = source.Inventory(tx, above).SetItem(0, item.NewStack(item.Coal{}, 2))
		tx.SetBlock(above, source, nil)

		if !b.pull(above, tx) {
			t.Fatal("hopper minecart did not pull")
		}
		diamonds, _ := b.inv.Item(0)
		coal, _ := tx.Block(above).(block.Barrel).Inventory(tx, above).Item(0)
		if diamonds.Count() != 2 || coal.Count() != 1 || inventoryCount(b.inv.Items()) != 3 {
			t.Fatalf("unexpected transfer counts: minecart=%v above=%v", b.inv.Items(), coal)
		}

		b.transferCooldown = 2
		handle := world.EntitySpawnOpts{Position: cube.Pos{4, 4, 4}.Vec3Centre()}.New(HopperMinecartType, hopperMinecartConf)
		tx.AddEntity(handle)
		entity, _ := handle.Entity(tx)
		other := entity.(*MinecartContainer).Behaviour().(*HopperMinecartBehaviour)
		other.transferCooldown = 2
		other.Tick(entity.(*MinecartContainer).Ent, tx)
		if other.transferCooldown != 1 {
			t.Fatalf("cooldown = %d, want 1", other.transferCooldown)
		}
	})
}

func TestFullHopperMinecartDoesNotLoseItemsOnFailedPull(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		b := hopperMinecartConf.New()
		pos := cube.Pos{0, 0, 0}
		source := block.NewBarrel()
		for slot := 0; slot < b.inv.Size(); slot++ {
			_ = b.inv.SetItem(slot, item.NewStack(item.Coal{}, 64))
		}
		_ = source.Inventory(tx, pos).SetItem(0, item.NewStack(item.Diamond{}, 2))
		tx.SetBlock(pos, source, nil)

		if b.pull(pos, tx) {
			t.Fatal("pull succeeded into a full cart")
		}
		stack, _ := tx.Block(pos).(block.Barrel).Inventory(tx, pos).Item(0)
		if stack.Count() != 2 || inventoryCount(b.inv.Items()) != 5*64 {
			t.Fatalf("items changed after failed pull: source=%v", stack)
		}
	})
}

func TestHopperMinecartCollectsStackWithoutDuplication(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 1, 0}.Vec3Centre()
		minecartHandle := world.EntitySpawnOpts{Position: pos}.New(HopperMinecartType, hopperMinecartConf)
		itemHandle := NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(item.Diamond{}, 3))
		tx.AddEntity(minecartHandle)
		tx.AddEntity(itemHandle)
		minecartEntity, _ := minecartHandle.Entity(tx)
		minecart := minecartEntity.(*MinecartContainer)
		behaviour := minecart.Behaviour().(*HopperMinecartBehaviour)

		if !behaviour.collectItem(minecart.Ent, tx) {
			t.Fatal("hopper minecart did not collect nearby item")
		}
		remaining := 0
		for entity := range tx.Entities() {
			if entity.H().Type() == ItemType {
				remaining += entity.(*Ent).Behaviour().(*ItemBehaviour).Item().Count()
			}
		}
		if got := inventoryCount(behaviour.inv.Items()); got != 3 || remaining != 0 {
			t.Fatalf("collected=%d remaining=%d, want 3 and 0", got, remaining)
		}
	})
}

func TestHopperMinecartNBTRoundTrip(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	b := hopperMinecartConf.New()
	b.transferCooldown = 7
	b.enabled = false
	_ = b.inv.SetItem(4, item.NewStack(item.Diamond{}, 3))
	data := &world.EntityData{Data: b}
	nbt := HopperMinecartType.EncodeNBT(data)

	decoded := new(world.EntityData)
	HopperMinecartType.DecodeNBT(nbt, decoded)
	got := decoded.Data.(*HopperMinecartBehaviour)
	stack, _ := got.inv.Item(4)
	if nbt["TransferCooldown"] != int32(7) || got.transferCooldown != 7 || got.enabled || stack.Count() != 3 || got.ContainerType() != minecartContainerHopper || got.ContainerSize() != 5 {
		t.Fatalf("round trip failed: nbt=%v cooldown=%d enabled=%v stack=%v type=%d size=%d", nbt, got.transferCooldown, got.enabled, stack, got.ContainerType(), got.ContainerSize())
	}
}

func TestHopperMinecartActivatorRail(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 1, 0}
		handle := world.EntitySpawnOpts{Position: pos.Vec3Middle()}.New(HopperMinecartType, hopperMinecartConf)
		tx.AddEntity(handle)
		entity, _ := handle.Entity(tx)
		behaviour := entity.(*MinecartContainer).Behaviour().(*HopperMinecartBehaviour)

		tx.SetBlock(pos, block.ActivatorRail{Powered: true}, nil)
		behaviour.Activate(entity.(*MinecartContainer).Ent, tx, true)
		if behaviour.enabled {
			t.Fatal("powered activator rail did not disable hopper minecart")
		}

		tx.SetBlock(pos, block.ActivatorRail{}, nil)
		behaviour.Activate(entity.(*MinecartContainer).Ent, tx, false)
		if !behaviour.enabled {
			t.Fatal("unpowered activator rail did not enable hopper minecart")
		}
	})
}

func inventoryCount(stacks []item.Stack) (count int) {
	for _, stack := range stacks {
		count += stack.Count()
	}
	return
}
