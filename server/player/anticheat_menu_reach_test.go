package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMenuStaysOpenWhereTheClientOpensIt checks that Menu does not close a
// menu the client of Bedrock opened from where it can: the Bedrock client
// opens a block within 5.7 blocks in survival, and 12 blocks in creative, so
// that the menu stays open past the first tick, in every game mode. The
// reach of a menu is, for blocks, that of Java and Buffer or that of
// BlockReach, whichever is more: Java's 5 blocks of creative are less than
// what BlockReach lets a creative player click.
func TestMenuStaysOpenWhereTheClientOpensIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  world.GameMode
		dx    float64
		stays bool
	}{
		{"survival, 5.7 blocks", world.GameModeSurvival, 5.7, true},
		{"survival, past BlockReach", world.GameModeSurvival, 13, false},
		{"creative, 12 blocks", world.GameModeCreative, 12, true},
		{"creative, past BlockReach", world.GameModeCreative, 20, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withBreaker(t, anticheat.Default(), func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(int)) {
				p.SetGameMode(tc.mode)
				chest := cube.Pos{0, 100, 0}
				tx.SetBlock(chest, block.NewChest(), nil)
				// The client stands at the distance, level with the chest.
				p.data.Pos = mgl64.Vec3{0.5 + tc.dx, 100, 0.5}
				p.OpenBlockContainer(chest, tx)
				tick(5)
				if got := p.Menu() != nil; got != tc.stays {
					t.Fatalf("menu open after 5 ticks: %v, want %v (violations %+v)", got, tc.stays, h.violations)
				}
			})
		})
	}
}

// TestMenuStaysOpenForPostures checks that the eyes of a player that
// sneaks, which are lower, or crawls or swims, which are lower still, keep
// a menu open that the player stands within reach of.
func TestMenuStaysOpenForPostures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stand func(p *Player)
	}{
		{"sneaking", func(p *Player) { p.StartSneaking() }},
		{"swimming", func(p *Player) { p.StartSwimming() }},
		{"crawling", func(p *Player) { p.StartCrawling() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withBreaker(t, anticheat.Default(), func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(int)) {
				chest := cube.Pos{0, 100, 0}
				tx.SetBlock(chest, block.NewChest(), nil)
				p.data.Pos = mgl64.Vec3{5.5, 100, 0.5}
				p.OpenBlockContainer(chest, tx)
				tc.stand(p)
				tick(5)
				if p.Menu() == nil {
					t.Fatalf("menu closed %s 5 blocks from the chest (violations %+v)", tc.name, h.violations)
				}
			})
		})
	}
}

// TestMenuOfEntityStaysOpenWithinEntityReach checks that the menu of an
// entity stays open while the entity is within the reach of an entity: Java's
// 3 blocks of the box and Buffer, and that EntityReach lets the client
// click.
func TestMenuOfEntityStaysOpenWithinEntityReach(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  world.GameMode
		dx    float64
		stays bool
	}{
		{"survival, 5 blocks", world.GameModeSurvival, 5, true},
		{"survival, 15 blocks", world.GameModeSurvival, 15, false},
		{"creative, 8 blocks", world.GameModeCreative, 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withBreaker(t, anticheat.Default(), func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(int)) {
				p.SetGameMode(tc.mode)
				cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5 + tc.dx, 100, 0.5}}))
				p.OpenEntityContainer(cow, inventory.New(27, nil), entity.ContainerChestBoat, tx)
				tick(5)
				if got := p.Menu() != nil; got != tc.stays {
					t.Fatalf("menu open after 5 ticks: %v, want %v (violations %+v)", got, tc.stays, h.violations)
				}
			})
		})
	}
}
