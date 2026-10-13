package block_test

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

func TestBarrelDeferredLootSurvivesRestartAndIsConsumedOnce(t *testing.T) {
	const table = "minecraft:barrels/abandoned_camp_barrel"
	const seed = int64(263777)
	pos := cube.Pos{257, 70, -257}
	dir := t.TempDir()
	open := func() *world.World {
		t.Helper()
		db, err := mcdb.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	}
	w := open()
	t.Cleanup(func() {
		if w != nil {
			_ = w.Close()
		}
	})
	transaction := func(f func(*world.Tx)) {
		t.Helper()
		job := w.Do(f)
		<-job.Done()
		if err := job.Err(); err != nil {
			t.Fatal(err)
		}
	}
	restart := func() {
		t.Helper()
		w.Save()
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		w = nil
		w = open()
	}
	transaction(func(tx *world.Tx) {
		barrel := world.DecodeNBT(block.NewBarrel(), map[string]any{"id": "Barrel", "LootTable": table, "LootTableSeed": seed}, world.DefaultBlockRegistry).(block.Barrel)
		tx.SetBlock(pos, barrel, nil)
	})
	restart()
	transaction(func(tx *world.Tx) {
		barrel := tx.Block(pos).(block.Barrel)
		pending := barrel.EncodeNBT()
		if pending["LootTable"] != table || pending["LootTableSeed"] != seed {
			t.Fatalf("unopened barrel lost pending loot: %v", pending)
		}
		inv := barrel.Inventory(tx, pos)
		before := inv.Slots()
		items := 0
		for _, stack := range before {
			items += stack.Count()
		}
		if items == 0 {
			t.Fatal("real abandoned-camp table produced no items")
		}
		barrel = tx.Block(pos).(block.Barrel)
		if _, pending := barrel.EncodeNBT()["LootTable"]; pending {
			t.Fatal("access retained pending loot")
		}
		if !reflect.DeepEqual(before, barrel.Inventory(tx, pos).Slots()) {
			t.Fatal("second access regenerated loot")
		}
		inv.Clear()
		if !tx.Block(pos).(block.Barrel).Inventory(tx, pos).Empty() {
			t.Fatal("emptying barrel regenerated its loot")
		}
		t.Logf("actual World unpacked %d items; table cleared; second access and emptying did not regenerate", items)
	})
	restart()
	transaction(func(tx *world.Tx) {
		barrel := tx.Block(pos).(block.Barrel)
		if _, pending := barrel.EncodeNBT()["LootTable"]; pending {
			t.Fatal("restart restored consumed loot table")
		}
		if !barrel.Inventory(tx, pos).Empty() {
			t.Fatal("restart regenerated consumed loot")
		}
		t.Log("MCDB reopen retained the empty, consumed barrel")
	})
}

func TestBarrelLootErrorPreservesPendingTableAndInventory(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{257, 70, -257}
	job := w.Do(func(tx *world.Tx) {
		barrel := world.DecodeNBT(block.NewBarrel(), map[string]any{"id": "Barrel", "LootTable": "minecraft:barrels/not_a_registered_table", "LootTableSeed": int64(263778)}, world.DefaultBlockRegistry).(block.Barrel)
		tx.SetBlock(pos, barrel, nil)
		before := barrel.EncodeNBT()
		if err := barrel.GenerateLoot(tx, pos); err == nil {
			t.Fatal("unknown loot table succeeded")
		}
		if after := tx.Block(pos).(block.Barrel).EncodeNBT(); !reflect.DeepEqual(after, before) {
			t.Fatalf("failed loot access mutated pending actor: before=%v after=%v", before, after)
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}
