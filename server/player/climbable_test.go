package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPlayerClimbsOpenTrapdoorAboveLadder ports LivingEntity.trapdoorUsableAsLadder:
// a player in a trapdoor can climb it if it is open and the ladder below it
// faces the same way, and in no other case.
func TestPlayerClimbsOpenTrapdoorAboveLadder(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		open   bool
		facing cube.Direction
		ladder cube.Direction
		want   bool
	}{
		{name: "open, same facing", open: true, facing: cube.North, ladder: cube.North, want: true},
		{name: "open, same facing east", open: true, facing: cube.East, ladder: cube.East, want: true},
		{name: "open, other facing", open: true, facing: cube.North, ladder: cube.South},
		{name: "closed, same facing", facing: cube.North, ladder: cube.North},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 64, 0}, block.Ladder{Facing: test.ladder}, nil)
				tx.SetBlock(cube.Pos{0, 65, 0}, block.WoodTrapdoor{Wood: block.OakWood(), Facing: test.facing, Open: test.open}, nil)
				pos := mgl64.Vec3{.5, 65, .5}
				handle := world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos, GameMode: world.GameModeSurvival})
				p := tx.AddEntity(handle).(*Player)
				if got := p.onClimbable(); got != test.want {
					t.Fatalf("onClimbable() = %v, want %v", got, test.want)
				}
			})
		})
	}
}
