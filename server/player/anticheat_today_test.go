package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The tests of this file pin what the actions of a client do before the
// anti-cheat: each says which commit of the anti-cheat changes it.

// TestAntiCheatTodayBreakBlockIsInstant pins that the instant break a
// client asks for (UseItemActionBreakBlock) breaks stone at once in
// survival. "Break blocks in the time they take" refuses it.
func TestAntiCheatTodayBreakBlockIsInstant(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, clienttest.New(), func(tx *world.Tx, p *Player) {
		pos := cube.Pos{0, 100, 2}
		tx.SetBlock(pos, block.Stone{}, nil)
		p.BreakBlock(pos)
		if _, air := tx.Block(pos).(block.Air); !air {
			t.Fatalf("stone after BreakBlock in survival: %T", tx.Block(pos))
		}
	})
}

// TestAntiCheatTodayFinishBreakingIsInstant pins that FinishBreaking right
// after StartBreaking breaks obsidian by hand at once. "Break blocks in the
// time they take" refuses it.
func TestAntiCheatTodayFinishBreakingIsInstant(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, clienttest.New(), func(tx *world.Tx, p *Player) {
		pos := cube.Pos{0, 100, 2}
		tx.SetBlock(pos, block.Obsidian{}, nil)
		p.StartBreaking(pos, cube.FaceNorth)
		p.FinishBreaking()
		if _, air := tx.Block(pos).(block.Air); !air {
			t.Fatalf("obsidian after StartBreaking and FinishBreaking: %T", tx.Block(pos))
		}
	})
}

// TestAntiCheatTodayMenuStaysOpenFarAway pins that a chest menu stays open
// once the player is 20 blocks away. "Close menus out of reach" closes it.
func TestAntiCheatTodayMenuStaysOpenFarAway(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, clienttest.New(), func(tx *world.Tx, p *Player) {
		chest := cube.Pos{1, 100, 0}
		tx.SetBlock(chest, block.NewChest(), nil)
		p.OpenBlockContainer(chest, tx)
		if p.Menu() == nil {
			t.Fatal("the chest did not open")
		}
		p.Teleport(mgl64.Vec3{21.5, 100, 0.5})
		p.Tick(tx, 1)
		if p.Menu() == nil {
			t.Fatal("the chest closed 20 blocks away")
		}
	})
}

// TestAntiCheatTodayAttackReachesFar pins that an attack lands on a cow
// whose box is 7.3 blocks from the eyes of the player, as the reach is 8 to
// the position of the target. "Measure the reach to entities as Java does"
// refuses it: Java's reach is 6 to the box.
func TestAntiCheatTodayAttackReachesFar(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, clienttest.New(), func(tx *world.Tx, p *Player) {
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{8.25, 100, 0.5}}))
		if !p.AttackEntity(cow) {
			t.Fatal("the attack 7.3 blocks to the box missed")
		}
	})
}

// TestAntiCheatTodayUpBlipResetsFallDistance pins that any move up resets
// the fall distance, as Java's movedUpwards does. "Log players who rise
// without cause" keeps it so by default: Ascend logs, and only its cancel
// action keeps the distance.
func TestAntiCheatTodayUpBlipResetsFallDistance(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, clienttest.New(), func(tx *world.Tx, p *Player) {
		for range 4 {
			p.Move(mgl64.Vec3{0, -0.5, 0}, 0, 0)
		}
		if p.FallDistance() < 1.9 {
			t.Fatalf("fall distance after falling 2 blocks: %v", p.FallDistance())
		}
		p.Move(mgl64.Vec3{0, 0.1, 0}, 0, 0)
		if d := p.FallDistance(); d != 0 {
			t.Fatalf("fall distance after a move up: %v, want 0", d)
		}
	})
}

// TestAntiCheatTodayMoveIsUnchecked pins that Player.Move takes the player
// 11 blocks in one move: only the Bedrock adapter refuses such a move of
// its client (TestMoveTooFarIsRefused). "Let the client move the player
// through the game" moves the refusal into Player.ClientMove; Move stays
// the unchecked API of plugins.
func TestAntiCheatTodayMoveIsUnchecked(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, clienttest.New(), func(tx *world.Tx, p *Player) {
		start := p.Position()
		p.Move(mgl64.Vec3{11, 0, 0}, 0, 0)
		if got := p.Position(); !got.ApproxEqual(start.Add(mgl64.Vec3{11, 0, 0})) {
			t.Fatalf("position after an 11-block Move: %v", got)
		}
	})
}
