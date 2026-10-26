package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestProjectilesBreakPotsPerGamerules(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		mob := tx.AddEntity(NewSkeleton(world.EntitySpawnOpts{Position: mgl64.Vec3{4, 64, 0}}))
		cases := []struct {
			name             string
			owner            world.Entity
			breakRule, grief bool
			broken           bool
		}{
			{"unowned snowball", nil, true, true, true},
			{"projectiles_can_break_blocks off", nil, false, true, false},
			{"mob-owned with mob_griefing off", mob, true, false, false},
			{"mob-owned with mob_griefing on", mob, true, true, true},
		}
		for _, c := range cases {
			_ = tx.World().SetGameRule("projectiles_can_break_blocks", c.breakRule)
			_ = tx.World().SetGameRule("mob_griefing", c.grief)
			tx.SetBlock(pos, block.DecoratedPot{}, nil)
			opts := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 66, 0.5}}
			var snowball world.Entity
			if c.owner != nil {
				snowball = tx.AddEntity(NewSnowball(opts, c.owner))
			} else {
				snowball = tx.AddEntity(opts.New(SnowballType, snowballConf))
			}
			tx.Block(pos).(block.DecoratedPot).ProjectileHit(pos, tx, snowball, cube.FaceUp)
			_, still := tx.Block(pos).(block.DecoratedPot)
			if still == c.broken {
				t.Errorf("%s: pot broken=%v, want %v", c.name, !still, c.broken)
			}
			_ = snowball.(interface{ CloseIn(*world.Tx) error }).CloseIn(tx)
		}
	})
}

// TestSnowballHurtsBlazes follows Snowball.onHitEntity: a blaze hit by a
// snowball takes 3 damage, other mobs none.
func TestSnowballHurtsBlazes(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		blaze := tx.AddEntity(NewBlaze(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 70, 0.5}}))
		cow := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 70, 0.5}}))
		if got := snowballConf.targetDamage(blaze); got != 3 {
			t.Fatalf("snowball deals %v to a blaze, want 3", got)
		}
		if got := snowballConf.targetDamage(cow); got != 0 {
			t.Fatalf("snowball deals %v to a cow, want 0", got)
		}
	})
}
