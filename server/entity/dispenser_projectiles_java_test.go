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
// arrow, the egg, the snowball, the bottle o' enchanting, the potions, the
// charges and the firework: they spawn where their DispenseConfig puts them
// (0.7 blocks out of the dispenser and 0.1 above its centre by default) and
// leave with the direction scaled by the power, moved by at most 0.0172275
// times the uncertainty on each axis. The thrown items have half the default
// uncertainty and 1.25 times its power; the charges and the firework have
// configs of their own.
func TestDispenserShotsFlyAsJavaShoots(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name               string
		item               world.Item
		typ                world.EntityType
		uncertainty, power float64
		pos                mgl64.Vec3
	}{
		{"arrow", item.Arrow{}, entity.ArrowType, 6, 1.1, mgl64.Vec3{1.2, 64.6, 0.5}},
		{"egg", item.Egg{}, entity.EggType, 6, 1.1, mgl64.Vec3{1.2, 64.6, 0.5}},
		{"snowball", item.Snowball{}, entity.SnowballType, 6, 1.1, mgl64.Vec3{1.2, 64.6, 0.5}},
		{"bottle o' enchanting", item.BottleOfEnchanting{}, entity.BottleOfEnchantingType, 3, 1.375, mgl64.Vec3{1.2, 64.6, 0.5}},
		{"splash potion", item.SplashPotion{Type: potion.Harming()}, entity.SplashPotionType, 3, 1.375, mgl64.Vec3{1.2, 64.6, 0.5}},
		{"lingering potion", item.LingeringPotion{Type: potion.Poison()}, entity.LingeringPotionType, 3, 1.375, mgl64.Vec3{1.2, 64.6, 0.5}},
		{"fire charge", item.FireCharge{}, entity.SmallFireballType, 6.6666665, 1, mgl64.Vec3{1.5, 64.5, 0.5}},
		{"wind charge", item.WindCharge{}, entity.WindChargeType, 6.6666665, 1, mgl64.Vec3{1.5, 64.5, 0.5}},
		{"firework", item.Firework{}, entity.FireworkType, 1, 0.5, mgl64.Vec3{1.0000099999997474, 64.5, 0.5}},
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
					if e.Position().Sub(c.pos).Len() > 1e-9 {
						t.Fatalf("spawned at %v, want %v", e.Position(), c.pos)
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
