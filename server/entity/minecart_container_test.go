package entity

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/inventory"
	"github.com/dm-vev/adamant/server/logging"
	"github.com/dm-vev/adamant/server/world"
)

func TestHopperMinecartPullAndCooldown(t *testing.T) {
	t.Parallel()
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
		handle := world.EntitySpawnOpts{Position: cube.Pos{4, 4, 4}.Vec3Centre()}.New(entityHopperMinecartType, hopperMinecartConf)
		tx.AddEntity(handle)
		entity, _ := handle.Entity(tx)
		other := entity.(*minecartContainer).Behaviour().(*hopperMinecartBehaviour)
		other.transferCooldown = 2
		other.Tick(entity.(*minecartContainer).Ent, tx)
		if other.transferCooldown != 1 {
			t.Fatalf("cooldown = %d, want 1", other.transferCooldown)
		}
	})
}

func TestFullHopperMinecartDoesNotLoseItemsOnFailedPull(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 1, 0}.Vec3Centre()
		minecartHandle := world.EntitySpawnOpts{Position: pos}.New(entityHopperMinecartType, hopperMinecartConf)
		itemHandle := NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(item.Diamond{}, 3))
		tx.AddEntity(minecartHandle)
		tx.AddEntity(itemHandle)
		minecartEntity, _ := minecartHandle.Entity(tx)
		minecart := minecartEntity.(*minecartContainer)
		behaviour := minecart.Behaviour().(*hopperMinecartBehaviour)

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
	t.Parallel()
	world.DefaultBlockRegistry.Finalize()
	b := hopperMinecartConf.New()
	b.transferCooldown = 7
	b.enabled = false
	_ = b.inv.SetItem(4, item.NewStack(item.Diamond{}, 3))
	data := &world.EntityData{Data: b}
	nbt := entityHopperMinecartType.EncodeNBT(data)

	decoded := new(world.EntityData)
	entityHopperMinecartType.DecodeNBT(nbt, decoded)
	got := decoded.Data.(*hopperMinecartBehaviour)
	stack, _ := got.inv.Item(4)
	if nbt["TransferCooldown"] != int32(7) || got.transferCooldown != 7 || got.enabled || stack.Count() != 3 || got.ContainerKind() != ContainerMinecartHopper || got.ContainerSize() != 5 {
		t.Fatalf("round trip failed: nbt=%v cooldown=%d enabled=%v stack=%v kind=%d size=%d", nbt, got.transferCooldown, got.enabled, stack, got.ContainerKind(), got.ContainerSize())
	}
}

func TestHopperMinecartActivatorRail(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 1, 0}
		handle := world.EntitySpawnOpts{Position: pos.Vec3Middle()}.New(entityHopperMinecartType, hopperMinecartConf)
		tx.AddEntity(handle)
		entity, _ := handle.Entity(tx)
		behaviour := entity.(*minecartContainer).Behaviour().(*hopperMinecartBehaviour)

		tx.SetBlock(pos, block.ActivatorRail{Powered: true}, nil)
		behaviour.Activate(entity.(*minecartContainer).Ent, tx, true)
		if behaviour.enabled {
			t.Fatal("powered activator rail did not disable hopper minecart")
		}

		tx.SetBlock(pos, block.ActivatorRail{}, nil)
		behaviour.Activate(entity.(*minecartContainer).Ent, tx, false)
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

// TestVehicleContainerKinds checks the kind of container of chest boats and
// container minecarts, which clients are told the container type of their
// actor data by, and that a minecart configured with a kind of no minecart
// is a chest minecart.
func TestVehicleContainerKinds(t *testing.T) {
	for _, c := range []struct {
		name string
		got  ContainerKind
		want ContainerKind
	}{
		{"chest boat", chestBoatConf.New().ContainerKind(), ContainerChestBoat},
		{"chest minecart", chestMinecartConf.New().ContainerKind(), ContainerMinecartChest},
		{"hopper minecart", hopperMinecartConf.New().ContainerKind(), ContainerMinecartHopper},
		{"minecart of the zero kind", minecartContainerBehaviourConfig{Size: 1}.New().ContainerKind(), ContainerMinecartChest},
	} {
		if c.got != c.want {
			t.Errorf("%s: ContainerKind() = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// TestMinecartLootOverfillWarns fills a container minecart with more loot than
// it has room for: the rest is dropped and a warning is logged as the world
// component, not to the standard logger.
func TestMinecartLootOverfillWarns(t *testing.T) {
	var out bytes.Buffer
	set := logging.New(logging.Options{Format: logging.JSON, Output: &out, Level: slog.LevelDebug})
	w := world.Config{Synchronous: true, Log: set.Logger(logging.Server)}.New()
	defer w.Close()

	inv := inventory.New(1, nil)
	stacks := []item.Stack{item.NewStack(block.Stone{}, 1), item.NewStack(block.Dirt{}, 1)}
	random := vanilla.NewLegacyRandom(1)
	fillMinecartInventory(w.Log(), inv, stacks, &random)

	var rec struct{ Level, Msg, Component string }
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &rec); err != nil {
		t.Fatalf("log output %q is not one JSON line: %v", out.String(), err)
	}
	if rec.Level != "WARN" || rec.Component != logging.World || rec.Msg != "loot table tried to over-fill a minecart container" {
		t.Fatalf("logged %+v, want a warning of the world component", rec)
	}
}
