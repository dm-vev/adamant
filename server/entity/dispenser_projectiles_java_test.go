package entity_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestDispenserShotsFlyAsJavaShoots checks ProjectileDispenseBehavior for the
// arrow, the bottle o' enchanting and the potions: they spawn 0.7 blocks out
// of the dispenser and 0.1 above its centre and leave with the direction
// scaled by the power, moved by at most 0.0172275 times the uncertainty on each
// axis, the uncertainty and power halved and multiplied by 1.25 for the
// thrown items.
func TestDispenserShotsFlyAsJavaShoots(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name               string
		item               world.Item
		typ                world.EntityType
		uncertainty, power float64
	}{
		{"arrow", item.Arrow{}, entity.ArrowType, 6, 1.1},
		{"bottle o' enchanting", item.BottleOfEnchanting{}, entity.BottleOfEnchantingType, 3, 1.375},
		{"splash potion", item.SplashPotion{Type: potion.Harming()}, entity.SplashPotionType, 3, 1.375},
		{"lingering potion", item.LingeringPotion{Type: potion.Poison()}, entity.LingeringPotionType, 3, 1.375},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			spread := 0.0172275 * c.uncertainty * c.power
			var sum, sumSq float64
			const shots = 60
			for range shots {
				<-w.Exec(func(tx *world.Tx) {
					pos := loadDispenser(tx, item.NewStack(c.item, 1))
					tx.Block(pos).(block.Dispenser).ScheduledTick(pos, tx, nil)
					shot := entitiesOfType(tx, c.typ)
					if len(shot) != 1 {
						t.Fatalf("%d shots spawned, want 1", len(shot))
					}
					e := shot[0].(*entity.Ent)
					if want := (mgl64.Vec3{1.2, 64.6, 0.5}); e.Position().Sub(want).Len() > 1e-9 {
						t.Fatalf("spawned at %v, want %v", e.Position(), want)
					}
					v := e.Velocity()
					if dx := v[0] - c.power; math.Abs(dx) > spread+1e-9 || math.Abs(v[1]) > spread+1e-9 || math.Abs(v[2]) > spread+1e-9 {
						t.Fatalf("velocity %v is further than %v from %v", v, spread, mgl64.Vec3{c.power})
					}
					sum += v[1]
					sumSq += v[1] * v[1]
					tx.RemoveEntity(e)
				})
			}
			if sumSq == 0 || math.Sqrt(sumSq/shots) < spread/10 {
				t.Fatalf("shots do not spread: sum %v squares %v", sum, sumSq)
			}
		})
	}
}
