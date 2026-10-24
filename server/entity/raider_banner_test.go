package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// omenHolder is a player under Raid Omen at a village.
type omenHolder struct {
	world.Entity
	pos cube.Pos
}

func (o omenHolder) RaidOmenPosition() cube.Pos { return o.pos }
func (o omenHolder) RaidOmenLevel() int         { return 1 }

// TestRaidersTakeTheCaptainsBanner checks ObtainRaidLeaderBannerGoal and
// Raider.pickUpItem: a raider of a wave without a captain walks to an
// ominous banner, puts it on and leads the wave.
func TestRaidersTakeTheCaptainsBanner(t *testing.T) {
	w, p := naturalSpawnWorld(t, 6000)
	w.SetSpawnMobs(false)
	var v *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		head := cube.Pos{0, 64, -6}
		tx.SetBlock(head.Side(cube.FaceSouth), block.Bed{Facing: cube.North}, nil)
		tx.SetBlock(head, block.Bed{Facing: cube.North, Head: true}, nil)
		tx.ClaimPOI(head, uuid.New())
		pl, _ := p.Entity(tx)
		if !tx.TryApplyRaidOmen(omenHolder{Entity: pl, pos: head}) {
			t.Fatal("no raid started at a village")
		}
		v = tx.AddEntity(entity.NewVindicator(world.EntitySpawnOpts{Position: mgl64.Vec3{6.5, 64, 6.5}})).H()
		raid := tx.RaidAt(cube.Pos{0, 64, -6})
		e, _ := v.Entity(tx)
		if raid == nil || !tx.RegisterRaider(raid.ID(), e) {
			t.Fatal("vindicator did not join the raid")
		}
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{12.5, 64, 6.5}}, entity.OminousBannerStack()))
	})
	for range 400 {
		w.AdvanceTick()
		captain := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := v.Entity(tx)
			captain = e.(*entity.Vindicator).IsCaptain()
		})
		if captain {
			return
		}
	}
	t.Fatal("vindicator in a raid without a captain did not take the ominous banner six blocks away")
}
