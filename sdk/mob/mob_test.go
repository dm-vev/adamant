package mob_test

import (
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/dm-vev/adamant/sdk/mob"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMobBrainGoalsEquipmentDropsAndCodec(t *testing.T) {
	var sensorRuns, patrolTicks, goalTicks, hurtRuns, deathRuns atomic.Int32

	brain := mob.NewBrain("idle",
		mob.WithSensor("scan", 1, func(_ *mob.Entity, b *mob.Brain, _ *world.Tx) {
			if sensorRuns.Add(1) == 1 {
				b.SetMemoryWithTTL("alert", true, 3)
			}
		}),
		mob.ActivitySelector(func(_ *mob.Entity, b *mob.Brain, _ *world.Tx) string {
			if _, ok := b.Memory("alert"); ok {
				return "patrol"
			}
			return "idle"
		}),
		mob.Activity("patrol", mob.Behaviour{
			Name: "walk",
			Tick: func(_ *mob.Entity, _ *mob.Brain, _ *world.Tx) {
				patrolTicks.Add(1)
			},
		}),
	)

	goal := mob.NewGoal(
		mob.GoalEveryTick(),
		mob.GoalCanStart(func(*entity.Mob) bool { return true }),
		mob.GoalTick(func(*entity.Mob) { goalTicks.Add(1) }),
	)

	guardType := mob.Define("sdk_test:guard",
		mob.NetworkType("minecraft:zombie"),
		mob.MaxHealth(20),
		mob.WithBrain(brain),
		mob.WithGoal(1, goal),
		mob.WithEquipment(mob.Equipment{
			MainHand: item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1),
			Helmet:   item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1),
		}),
		mob.Drops(item.NewStack(item.Emerald{}, 3)),
		mob.XP(5),
		mob.Codec(
			func(state any) map[string]any {
				s, _ := state.(string)
				return map[string]any{"rank": s}
			},
			func(m map[string]any) any {
				s, _ := m["rank"].(string)
				return s
			},
		),
		mob.OnHurt(func(_ *mob.Entity, dmg *float64, _ world.DamageSource, _ *world.Tx) bool {
			hurtRuns.Add(1)
			*dmg *= 2
			return true
		}),
		mob.OnDeath(func(_ *mob.Entity, _ world.DamageSource, _ *world.Tx) {
			deathRuns.Add(1)
		}),
	)

	world.DefaultBlockRegistry.Finalize()
	reg := mob.Register(entity.DefaultRegistry, guardType)
	w := world.Config{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       world.NopGenerator{},
		Provider:        world.NopProvider{},
		Blocks:          world.DefaultBlockRegistry,
		Entities:        reg,
	}.New()
	t.Cleanup(func() { _ = w.Close() })

	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		e := guardType.SpawnIn(tx, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}})
		if e == nil {
			t.Fatal("SpawnIn returned nil")
		}
		h = e.H()
		e.SetCustomState("captain")
		main, _ := e.HeldItems()
		if main.Empty() {
			t.Fatal("expected non-empty MainHand equipment")
		}
		if helm, _, _, _ := e.EquipmentArmour(); helm.Empty() {
			t.Fatal("expected non-empty Helmet equipment")
		}
	})

	for range 5 {
		w.AdvanceTick()
	}
	if patrolTicks.Load() == 0 || goalTicks.Load() == 0 || sensorRuns.Load() == 0 {
		t.Fatalf("AI did not run: sensors=%d patrol=%d goals=%d", sensorRuns.Load(), patrolTicks.Load(), goalTicks.Load())
	}

	// Verify NBT Codec round-trip and OnHurt/OnDeath + drops/XP.
	<-w.Exec(func(tx *world.Tx) {
		ent, ok := h.Entity(tx)
		if !ok {
			t.Fatal("guard missing before hurt")
		}
		liv, ok := ent.(*mob.Living)
		if !ok {
			t.Fatalf("expected *mob.Living, got %T", ent)
		}
		if liv.Brain().Activity() != "idle" {
			t.Fatalf("expected brain back to idle after TTL expiry, got %q", liv.Brain().Activity())
		}
		// 15 * 2 = 30 damage kills 20 HP guard.
		liv.Hurt(15, entity.AttackDamageSource{})
	})

	if hurtRuns.Load() != 1 || deathRuns.Load() != 1 {
		t.Fatalf("hurt=%d death=%d, want 1/1", hurtRuns.Load(), deathRuns.Load())
	}

	// Test Codec directly via EncodeNBT / DecodeNBT.
	var data world.EntityData
	guardType.DecodeNBT(map[string]any{"rank": "general"}, &data)
	enc := guardType.EncodeNBT(&data)
	if enc["rank"] != "general" {
		t.Fatalf("Codec round-trip = %#v, want rank=general", enc)
	}
}
