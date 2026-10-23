package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func creakingTestWorld(t *testing.T) (*world.World, *world.EntityHandle, *world.EntityHandle) {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { w.Close() })
	w.SetTime(13000)
	var mob, owner *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -16; x <= 16; x++ {
			for z := -16; z <= 16; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
		pos := cube.Pos{0, 65, 0}
		for _, face := range []cube.Face{cube.FaceDown, cube.FaceUp} {
			tx.SetBlock(pos.Side(face), block.Log{Wood: block.PaleOakWood(), Axis: cube.Y}, nil)
		}
		mob = entity.DefaultRegistry.Config().SpawnMobFromNBT("minecraft:creaking", world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 65, .5}}, map[string]any{"home_pos": []int32{0, 65, 0}})
		tx.AddEntity(mob)
		heart := (block.CreakingHeart{State: block.CreakingHeartAwake}).DecodeNBT(map[string]any{"SpawnedCreakingID": mob.UniqueID()}).(block.CreakingHeart)
		tx.SetBlock(pos, heart, nil)
		owner = world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "CreakingTest", Position: mgl64.Vec3{10, 65, 8}, GameMode: world.GameModeCreative})
		tx.AddEntity(owner)
	})
	return w, mob, owner
}

func TestCreakingDamageRemembersResponsiblePlayer(t *testing.T) {
	w, mob, owner := creakingTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		e, _ := mob.Entity(tx)
		c := e.(*entity.Creaking)
		if _, accepted := c.Hurt(1, entity.SuffocationDamageSource{}); accepted {
			t.Fatal("unattributed environmental damage swayed unhurt Creaking")
		}
		p, _ := owner.Entity(tx)
		if damage, accepted := c.Hurt(0, entity.AttackDamageSource{Attacker: p}); !accepted || damage != 0 || c.Health() != 1 {
			t.Fatalf("zero-damage player hit = %v/%v, health=%v", damage, accepted, c.Health())
		}
	})
	for range 8 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		e, _ := mob.Entity(tx)
		if _, accepted := e.(*entity.Creaking).Hurt(1, entity.SuffocationDamageSource{}); !accepted {
			t.Fatal("Creaking lost recent player attribution")
		}
	})
	for range 93 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		e, ok := mob.Entity(tx)
		if !ok {
			t.Fatal("protector vanished during memory test")
		}
		if _, accepted := e.(*entity.Creaking).Hurt(1, entity.SuffocationDamageSource{}); accepted {
			t.Fatal("player attribution survived 101-tick memory expiry")
		}
	})
}

func TestCreakingTeardownDoesNotAdvanceGameplayRandom(t *testing.T) {
	w, mob, _ := creakingTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		*tx.RegionRandom() = vanilla.NewLegacyRandom(17)
		expected := *tx.RegionRandom()
		e, _ := mob.Entity(tx)
		e.(*entity.Creaking).RemoveProtector(nil)
		if got, want := tx.RegionRandom().NextLong(), expected.NextLong(); got != want {
			t.Fatalf("client-side particle sampling consumed gameplay RNG: got %d want %d", got, want)
		}
		if _, present := tx.EntityByUUID(mob.UUID()); present {
			t.Fatal("immediate teardown retained entity")
		}
	})
}
