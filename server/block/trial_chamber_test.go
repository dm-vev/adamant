package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// trialTestWorld returns a world with a stone floor at y 64 and a survival
// player standing at pos.
func trialTestWorld(t *testing.T, pos mgl64.Vec3) (*world.World, *world.EntityHandle) {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	h := world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "TrialTest", Position: pos, GameMode: world.GameModeSurvival})
	<-w.Exec(func(tx *world.Tx) {
		for x := -8; x <= 8; x++ {
			for z := -8; z <= 8; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
		tx.AddEntity(h)
	})
	return w, h
}

func countEntities(tx *world.Tx, name string) (n int) {
	for e := range tx.Entities() {
		if e.H().Type().EncodeEntity() == name {
			n++
		}
	}
	return n
}

// TestTrialSpawnerRunsTemplateTrial places a spawner as a generated trial
// chamber does, with its configurations named, and runs its trial.
func TestTrialSpawnerRunsTemplateTrial(t *testing.T) {
	w, h := trialTestWorld(t, mgl64.Vec3{3.5, 65, 0.5})
	pos := cube.Pos{0, 65, 0}
	<-w.Exec(func(tx *world.Tx) {
		w.SetDifficulty(world.DifficultyNormal)
		template := map[string]any{"id": "TrialSpawner", "normal_config": "minecraft:trial_chamber/melee/spider/normal", "ominous_config": "minecraft:trial_chamber/melee/spider/ominous"}
		tx.SetBlock(pos, block.TrialSpawner{}.DecodeNBT(template).(block.TrialSpawner), nil)
		tick := int64(1000)
		run := func(ticks int) {
			for range ticks {
				tick++
				tx.Block(pos).(block.TrialSpawner).Tick(tick, pos, tx)
			}
		}
		run(1)
		s := tx.Block(pos).(block.TrialSpawner)
		if s.State != block.TrialSpawnerWaitingForPlayers {
			t.Fatalf("spawner with a configured mob is %v, want waiting for players", s.State)
		}
		if spawnData, _ := s.UpdateNBT()["spawn_data"].(map[string]any); spawnData["TypeId"] != "minecraft:spider" {
			t.Fatalf("clients are sent spawn data %v, want a spider", s.UpdateNBT())
		}
		run(20)
		if s = tx.Block(pos).(block.TrialSpawner); s.State != block.TrialSpawnerActive {
			t.Fatalf("spawner near a player is %v, want active", s.State)
		}
		players := s.EncodeNBT()["registered_players"].([]any)
		p, _ := h.Entity(tx)
		if len(players) != 1 || players[0].(map[string]any)["uuid"] != p.H().UniqueID() {
			t.Fatalf("registered_players = %v, want the player's unique ID", players)
		}
		// The spider configuration keeps three spiders out of six at a time.
		for i := 0; i < 40 && countEntities(tx, "minecraft:spider") < 3; i++ {
			run(20)
		}
		if n := countEntities(tx, "minecraft:spider"); n != 3 {
			t.Fatalf("%d spiders at once, want 3", n)
		}
		for spawned := 0; spawned < 12; spawned++ {
			for e := range tx.Entities() {
				if e.H().Type().EncodeEntity() == "minecraft:spider" {
					tx.RemoveEntity(e)
				}
			}
			run(40)
			if s = tx.Block(pos).(block.TrialSpawner); s.State != block.TrialSpawnerActive {
				break
			}
		}
		if s.State != block.TrialSpawnerWaitingForReward {
			t.Fatalf("spawner whose six spiders died is %v, want waiting for reward", s.State)
		}
		before := countEntities(tx, "minecraft:item")
		run(100)
		if countEntities(tx, "minecraft:item") == before {
			t.Fatal("spawner ejected no reward")
		}
		if s = tx.Block(pos).(block.TrialSpawner); s.State != block.TrialSpawnerCooldown {
			t.Fatalf("spawner that rewarded its only player is %v, want cooldown", s.State)
		}
	})
}

// TestVaultRewardsEachPlayerOnce unlocks a vault placed from a trial chamber
// template.
func TestVaultRewardsEachPlayerOnce(t *testing.T) {
	w, h := trialTestWorld(t, mgl64.Vec3{2.5, 65, 0.5})
	pos := cube.Pos{0, 65, 0}
	<-w.Exec(func(tx *world.Tx) {
		template := map[string]any{"id": "Vault", "config": map[string]any{
			"key_item": map[string]any{"count": int32(1), "id": "minecraft:trial_key"}, "loot_table": "minecraft:chests/trial_chambers/reward",
		}}
		tx.SetBlock(pos, block.Vault{}.DecodeNBT(template).(block.Vault), nil)
		tick := tx.CurrentTick()
		run := func(ticks int) {
			for range ticks {
				tick++
				tx.Block(pos).(block.Vault).Tick(tick, pos, tx)
			}
		}
		run(1)
		v := tx.Block(pos).(block.Vault)
		p, _ := h.Entity(tx)
		if v.State != block.VaultActive {
			t.Fatalf("vault near a player is %v, want active", v.State)
		}
		update := v.UpdateNBT()
		if ids, _ := update["connected_players"].([]int64); len(ids) != 1 || ids[0] != p.H().UniqueID() {
			t.Fatalf("connected_players = %#v, want the player", update["connected_players"])
		}
		if display := update["display_item"].(map[string]any); display["Name"] == "" {
			t.Fatal("active vault displays nothing")
		}

		pl := p.(*player.Player)
		pl.SetHeldItems(item.NewStack(item.TrialKey{}, 2), item.Stack{})
		ctx := &item.UseContext{}
		if !v.Activate(pos, cube.FaceEast, tx, pl, ctx) || ctx.CountSub != 1 {
			t.Fatalf("inserting a trial key used %d keys, want 1", ctx.CountSub)
		}
		if v = tx.Block(pos).(block.Vault); v.State != block.VaultUnlocking {
			t.Fatalf("vault unlocked with its key is %v, want unlocking", v.State)
		}
		before := countEntities(tx, "minecraft:item")
		run(300)
		if countEntities(tx, "minecraft:item") == before {
			t.Fatal("vault ejected no reward")
		}
		v = tx.Block(pos).(block.Vault)
		if v.State != block.VaultInactive {
			t.Fatalf("vault that rewarded its only player is %v, want inactive", v.State)
		}
		data := v.EncodeNBT()["data"].(map[string]any)
		if ids, _ := data["rewarded_players"].([]int64); len(ids) != 1 || ids[0] != p.H().UniqueID() {
			t.Fatalf("rewarded_players = %#v, want the player", data["rewarded_players"])
		}
		// A player rewarded once never connects to the vault again.
		ctx = &item.UseContext{}
		v.Activate(pos, cube.FaceEast, tx, pl, ctx)
		if ctx.CountSub != 0 {
			t.Fatal("inactive vault took a key")
		}
	})
}

// TestTrialChamberContainersHoldLoot fills a dispenser and a decorated pot
// placed from trial chamber templates, which name their loot tables.
func TestTrialChamberContainersHoldLoot(t *testing.T) {
	w, h := trialTestWorld(t, mgl64.Vec3{2.5, 65, 0.5})
	<-w.Exec(func(tx *world.Tx) {
		dispenserPos, potPos := cube.Pos{0, 65, 0}, cube.Pos{0, 65, 2}
		dispenser := block.Dispenser{}.DecodeNBT(map[string]any{"id": "Dispenser", "LootTable": "minecraft:dispensers/trial_chambers/corridor", "LootTableSeed": int64(7)}).(block.Dispenser)
		if saved := dispenser.EncodeNBT(); saved["LootTable"] != "loot_tables/dispensers/trial_chambers/corridor.json" || saved["LootTableSeed"] != int64(7) {
			t.Fatalf("dispenser saved as %v", saved)
		}
		tx.SetBlock(dispenserPos, dispenser, nil)
		items := 0
		for _, s := range tx.Block(dispenserPos).(block.Dispenser).Inventory(tx, dispenserPos).Slots() {
			items += s.Count()
		}
		if items == 0 {
			t.Fatal("trial chamber dispenser holds nothing")
		}
		if saved := tx.Block(dispenserPos).(block.Dispenser).EncodeNBT(); saved["LootTable"] != nil {
			t.Fatalf("opened dispenser keeps its loot table: %v", saved)
		}

		pot := block.DecoratedPot{}.DecodeNBT(map[string]any{"id": "DecoratedPot", "LootTable": "loot_tables/pots/trial_chambers/corridor.json", "LootTableSeed": int64(9)}).(block.DecoratedPot)
		tx.SetBlock(potPos, pot, nil)
		p, _ := h.Entity(tx)
		pot.Activate(potPos, cube.FaceUp, tx, p.(*player.Player), &item.UseContext{})
		if tx.Block(potPos).(block.DecoratedPot).Item.Empty() {
			t.Fatal("trial chamber pot holds nothing")
		}
	})
}
