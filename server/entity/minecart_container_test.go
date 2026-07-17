package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestHopperMinecartPushPullAndCooldown(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		b := hopperMinecartConf.New()
		below, above := cube.Pos{0, 0, 0}, cube.Pos{0, 2, 0}
		destination, source := block.NewBarrel(), block.NewBarrel()
		_ = b.inv.SetItem(0, item.NewStack(item.Diamond{}, 2))
		_ = source.Inventory(tx, above).SetItem(0, item.NewStack(item.Coal{}, 2))
		tx.SetBlock(below, destination, nil)
		tx.SetBlock(above, source, nil)

		if !b.push(below, tx) || !b.pull(above, tx) {
			t.Fatal("hopper minecart did not push and pull")
		}
		diamonds, _ := b.inv.Item(0)
		coal, _ := tx.Block(above).(block.Barrel).Inventory(tx, above).Item(0)
		if diamonds.Count() != 1 || coal.Count() != 1 || inventoryCount(b.inv.Items()) != 2 || inventoryCount(tx.Block(below).(block.Barrel).Inventory(tx, below).Items()) != 1 {
			t.Fatalf("unexpected transfer counts: minecart=%v above=%v below=%v", b.inv.Items(), coal, tx.Block(below).(block.Barrel).Inventory(tx, below).Items())
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

func TestHopperMinecartFailedPushDoesNotLoseItems(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		b := hopperMinecartConf.New()
		pos := cube.Pos{0, 0, 0}
		full := block.NewBarrel()
		for slot := 0; slot < full.Inventory(tx, pos).Size(); slot++ {
			_ = full.Inventory(tx, pos).SetItem(slot, item.NewStack(item.Coal{}, 64))
		}
		_ = b.inv.SetItem(0, item.NewStack(item.Diamond{}, 2))
		tx.SetBlock(pos, full, nil)

		if b.push(pos, tx) {
			t.Fatal("push succeeded into a full container")
		}
		stack, _ := b.inv.Item(0)
		if stack.Count() != 2 || inventoryCount(tx.Block(pos).(block.Barrel).Inventory(tx, pos).Items()) != 27*64 {
			t.Fatalf("items changed after failed push: source=%v", stack)
		}
	})
}

func TestHopperMinecartCollectsOneItemWithoutDuplication(t *testing.T) {
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
		if got := inventoryCount(behaviour.inv.Items()); got != 1 || remaining != 2 {
			t.Fatalf("collected=%d remaining=%d, want 1 and 2", got, remaining)
		}
	})
}

func TestHopperMinecartNBTRoundTrip(t *testing.T) {
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
		behaviour.updateEnabled(entity.(*MinecartContainer).Ent, tx)
		if behaviour.enabled {
			t.Fatal("powered activator rail did not disable hopper minecart")
		}

		tx.SetBlock(pos, block.ActivatorRail{}, nil)
		behaviour.updateEnabled(entity.(*MinecartContainer).Ent, tx)
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
