package loot_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
)

func generate(t *testing.T, table string, seed int64, ctx loot.Context) []item.Stack {
	t.Helper()
	random := java26.NewXoroshiroRandom(seed)
	ctx.Random = &random
	stacks, err := loot.Generate(table, seed, ctx)
	if err != nil {
		t.Fatalf("%s seed %d: %v", table, seed, err)
	}
	return stacks
}

func TestSetStewEffectGivesOneListedEffect(t *testing.T) {
	stews := 0
	for seed := range int64(200) {
		for _, s := range generate(t, "minecraft:chests/ancient_city_ice_box", seed, loot.Context{}) {
			stew, ok := s.Item().(item.SuspiciousStew)
			if !ok {
				continue
			}
			stews++
			effects := stew.Type.Effects()
			if len(effects) != 1 || s.Count() < 2 || s.Count() > 6 {
				t.Fatalf("seed %d: %d stews with effects %v", seed, s.Count(), effects)
			}
			// Night vision lasts 7 to 10 seconds, blindness 5 to 7.
			lo, hi := 7*time.Second, 10*time.Second
			switch effects[0].Type() {
			case effect.NightVision:
			case effect.Blindness:
				lo, hi = 5*time.Second, 7*time.Second
			default:
				t.Fatalf("seed %d: stew of %v", seed, effects[0].Type())
			}
			if d := effects[0].Duration(); d < lo || d > hi {
				t.Fatalf("seed %d: %v lasts %v", seed, effects[0].Type(), d)
			}
		}
	}
	if stews == 0 {
		t.Fatal("no ice box held suspicious stew")
	}
}

func TestTreasureMapWithoutStructureIsDiscarded(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		for seed := range int64(50) {
			// The first pool always holds the buried treasure map, which is
			// discarded with no treasure to map; plain empty maps are other
			// loot.
			for _, s := range generate(t, "minecraft:chests/shipwreck_map", seed, loot.Context{Tx: tx}) {
				if m, ok := s.Item().(item.FilledMap); ok {
					t.Fatalf("seed %d: shipwreck kept %#v without a treasure to map", seed, m)
				}
			}
		}
	})
}

func TestSweetBerryHarvestDependsOnBushAge(t *testing.T) {
	for _, c := range []struct {
		growth, lo, hi int
	}{{2, 1, 2}, {3, 2, 3}} {
		seen := map[int]bool{}
		for seed := range int64(100) {
			n := 0
			for _, s := range generate(t, "minecraft:harvest/sweet_berry_bush", seed, loot.Context{BlockState: block.SweetBerries{Growth: c.growth}}) {
				n += s.Count()
			}
			if n < c.lo || n > c.hi {
				t.Fatalf("age %d harvest gave %d berries, want %d to %d", c.growth, n, c.lo, c.hi)
			}
			seen[n] = true
		}
		if !seen[c.lo] || !seen[c.hi] {
			t.Fatalf("age %d harvests %v never spanned %d to %d", c.growth, seen, c.lo, c.hi)
		}
	}
}
