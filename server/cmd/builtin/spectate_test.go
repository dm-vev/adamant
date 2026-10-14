package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSpectateFollowsAnEntity checks SpectateCommand and ServerPlayer's
// camera: only spectators can spectate, the spectator follows the entity, and
// leaving spectator mode or the entity dying returns the camera.
func TestSpectateFollowsAnEntity(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	spectate := cmd.New("spectate", "", nil, spectateCommand{})
	var watcher, cow *world.EntityHandle
	resolve := func(tx *world.Tx) (*player.Player, *entity.Cow) {
		p, _ := watcher.Entity(tx)
		c, _ := cow.Entity(tx)
		return p.(*player.Player), c.(*entity.Cow)
	}
	<-w.Exec(func(tx *world.Tx) {
		watcher = tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "Watcher", Position: mgl64.Vec3{0, 64, 0}, GameMode: world.GameModeSurvival})).H()
		cow = tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{20.5, 64, 0.5}})).H()
		p, c := resolve(tx)
		if out := runTestCommand(spectate, "@e[type=cow] Watcher", tx); out.ErrorCount() == 0 {
			t.Fatal("a survival player started spectating")
		}
		p.SetGameMode(world.GameModeSpectator)
		if out := runTestCommand(spectate, "Watcher Watcher", tx); out.ErrorCount() == 0 {
			t.Fatal("a player spectated itself")
		}
		if out := runTestCommand(spectate, "@e[type=cow] Watcher", tx); out.ErrorCount() != 0 {
			t.Fatalf("spectating the cow failed: %v", out.Errors())
		}
		c.Teleport(mgl64.Vec3{30.5, 64, 0.5})
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		p, c := resolve(tx)
		if e, ok := p.Spectating(); !ok || e.H() != cow {
			t.Fatal("the player is not spectating the cow")
		}
		if d := p.Position().Sub(c.Position()).Len(); d > 1e-6 {
			t.Fatalf("spectator is %.2f blocks from the cow it watches, want it to follow", d)
		}
		p.SetGameMode(world.GameModeCreative)
		if _, ok := p.Spectating(); ok {
			t.Fatal("leaving spectator mode kept spectating the cow")
		}
		p.SetGameMode(world.GameModeSpectator)
		if out := runTestCommand(spectate, "@e[type=cow] Watcher", tx); out.ErrorCount() != 0 {
			t.Fatalf("spectating the cow again failed: %v", out.Errors())
		}
		c.Hurt(1000, entity.VoidDamageSource{})
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		p, _ := watcher.Entity(tx)
		if _, ok := p.(*player.Player).Spectating(); ok {
			t.Fatal("the spectator kept watching a dead cow")
		}
	})
}
