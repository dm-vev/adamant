package npc_test

import (
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/sdk/npc"
	"github.com/df-mc/dragonfly/sdk/plugin"
	"github.com/df-mc/dragonfly/sdk/sdktest"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestHumanNPCSpawnLookAtInteractAndAutoCleanup(t *testing.T) {
	app := sdktest.NewApp(t)
	w := sdktest.NewWorld(t)
	visitor := sdktest.SpawnPlayer(t, w, "Visitor", mgl64.Vec3{4.5, 64, 0.5})

	var attacked, interacted atomic.Int32
	var guide *npc.NPC

	app.Use(plugin.Func("hub-npcs", func(ctx *plugin.Context) {
		<-w.Exec(func(tx *world.Tx) {
			guide = npc.Spawn(ctx, tx, "Guide",
				npc.Position(mgl64.Vec3{0.5, 64, 0.5}),
				npc.ScoreTag("Click me!"),
				npc.HeldItems(item.NewStack(item.Compass{}, 1), item.Stack{}),
				npc.Armour(item.NewStack(item.Helmet{Tier: item.ArmourTierGold{}}, 1), item.Stack{}, item.Stack{}, item.Stack{}),
				npc.LookAtPlayers(10),
				npc.OnInteract(func(*player.Player, *world.Tx) { interacted.Add(1) }),
				npc.OnAttack(func(*player.Player, *world.Tx) { attacked.Add(1) }),
			)
		})
	}, nil))

	if guide == nil || guide.Handle() == nil {
		t.Fatal("npc.Spawn returned nil")
	}

	// Advance tick so LookAtPlayers rotates the NPC toward Visitor (+X direction -> yaw -90).
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		p, ok := guide.Player(tx)
		if !ok {
			t.Fatal("NPC missing from world")
		}
		if p.Rotation().Yaw() == 0 {
			t.Fatalf("expected LookAtPlayers to update NPC yaw, got %v", p.Rotation().Yaw())
		}
		main, _ := p.HeldItems()
		if main.Empty() || p.Armour().Helmet().Empty() {
			t.Fatal("expected NPC equipment set")
		}
	})

	// Test OnAttack (protected from damage) and OnInteract + per-player visibility.
	visitor.Run(func(vp *player.Player, tx *world.Tx) {
		np, ok := guide.Player(tx)
		if !ok {
			t.Fatal("NPC missing during visitor interaction")
		}
		vp.Teleport(mgl64.Vec3{1.5, 64, 0.5})
		vp.AttackEntity(np)
		vp.UseItemOnEntity(np)
		if np.Health() != np.MaxHealth() {
			t.Fatalf("NPC took damage despite protection: %v/%v", np.Health(), np.MaxHealth())
		}
		guide.HideFrom(vp, tx)
		guide.ShowTo(vp, tx)
	})

	if attacked.Load() != 1 || interacted.Load() != 1 {
		t.Fatalf("callbacks: attacked=%d interacted=%d, want 1/1", attacked.Load(), interacted.Load())
	}

	// Disabling plugin must clean up the NPC entity automatically.
	app.Disable()
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := guide.Player(tx); ok {
			t.Fatal("expected NPC removed from world on plugin disable")
		}
	})
}
