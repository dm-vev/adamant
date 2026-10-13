package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestKillCommandKillsMobAndPlayer(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		mob := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 1}})).(entity.Living)
		p := tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "KillTarget", GameMode: world.GameModeSurvival})).(*player.Player)
		if out := runTestCommand(newKillCommand(), "@e", tx); out.ErrorCount() != 0 {
			t.Fatalf("kill errors: %v", out.Errors())
		}
		if !mob.Dead() || !p.Dead() {
			t.Fatalf("kill left entities alive: mob=%v player=%v", mob.Health(), p.Health())
		}
	})
}

func TestDamageCommandAppliesTypedDamage(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		target := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 1}})).(entity.Living)
		before := target.Health()
		if out := runTestCommand(newDamageCommand(), "@e 3 fall", tx); out.ErrorCount() != 0 {
			t.Fatalf("damage errors: %v", out.Errors())
		}
		if got := before - target.Health(); got != 3 {
			t.Fatalf("fall damage dealt %v, want 3", got)
		}
		if out := runTestCommand(newDamageCommand(), "@e 1 not_a_damage_type", tx); out.ErrorCount() == 0 {
			t.Fatal("unknown damage type was accepted")
		}
	})
}
