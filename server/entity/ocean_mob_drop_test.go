package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestSourceLootingChangesActualDrownedEquipmentDrop(t *testing.T) {
	cases := []struct {
		name                   string
		chance                 float32
		looting                int32
		seed                   int64
		vanishing, drops, wear bool
	}{
		{name: "ordinary below threshold", chance: .085, seed: 4096},
		{name: "source-only Looting III", chance: .085, looting: 3, seed: 4096, drops: true},
		{name: "zero chance before looting", looting: 3, seed: 5120},
		{name: "preserve uses original chance", chance: 1, looting: 3, seed: 4096, drops: true, wear: true},
		{name: "vanishing prevents preserved drop", chance: 2, looting: 3, seed: 4096, vanishing: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				attacker := tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: mgl64.Vec3{.5, 65, .5}, GameMode: world.GameModeCreative})).(*player.Player)
				weapon, err := item.StackFromJavaNBT(map[string]any{"id": "minecraft:diamond_sword", "count": int32(1), "components": map[string]any{"minecraft:enchantments": map[string]any{"minecraft:looting": c.looting}}})
				if err != nil {
					t.Fatal(err)
				}
				attacker.SetHeldItems(weapon, item.Stack{})
				gear := map[string]any{"id": "minecraft:iron_sword", "count": int32(1)}
				if c.vanishing {
					gear["components"] = map[string]any{"minecraft:enchantments": map[string]any{"minecraft:vanishing_curse": int32(1)}}
				}
				h, err := entity.NewFromJavaNBT(map[string]any{"id": "minecraft:drowned", "Health": float32(20), "equipment": map[string]any{"mainhand": gear}, "drop_chances": map[string]any{"mainhand": c.chance}}, world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 65, .5}, SpawnReason: "structure", SpawnFinalized: true})
				if err != nil {
					t.Fatal(err)
				}
				drowned := tx.AddEntity(h).(*entity.Drowned)
				// Source's first float is .09789288 (4096), between the
				// ordinary and Looting III thresholds, or .00616163 (5120).
				h.JavaRandom().SetSeed(c.seed)
				drowned.Hurt(100, entity.AttackDamageSource{Attacker: attacker})
				count, damage := 0, 0
				for e := range tx.Entities() {
					ent, ok := e.(*entity.Ent)
					if !ok {
						continue
					}
					drop, ok := ent.Behaviour().(*entity.ItemBehaviour)
					if !ok {
						continue
					}
					stack := drop.Item()
					if id, _ := stack.Item().EncodeItem(); id == "minecraft:iron_sword" {
						count += stack.Count()
						damage = stack.MaxDurability() - stack.Durability()
					}
				}
				want := 0
				if c.drops {
					want = 1
				}
				if count != want || c.wear && damage <= 0 {
					t.Fatalf("real drops=%d damage=%d, Source expects count=%d randomized wear=%t", count, damage, want, c.wear)
				}
			})
		})
	}
}
