package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// placerTestUser is a user with a player's eye height.
type placerTestUser struct{ pistonTestUser }

func (placerTestUser) EyeHeight() float64 { return 1.62 }

// TestBlocksFaceTheirPlacerLikeBDS places directional blocks from player
// positions and rotations recorded on BDS 1.26.52 and checks they take the
// states BDS gave them. A player in one of the nine block columns around the
// block turns it up or down by eye height; elsewhere it faces horizontally.
func TestBlocksFaceTheirPlacerLikeBDS(t *testing.T) {
	tests := []struct {
		block  world.Block
		feet   mgl64.Vec3 // player feet relative to the placed block
		rot    cube.Rotation
		face   cube.Face
		key    string
		want   any
		reason string
	}{
		{Dispenser{}, mgl64.Vec3{1.16, 1.178, -0.812}, cube.Rotation{20, 50}, cube.FaceNorth, "facing_direction", int32(1), "eyes over two blocks above, a column away"},
		{Dispenser{}, mgl64.Vec3{2.312, 0.678, 1.16}, cube.Rotation{110, 50}, cube.FaceUp, "facing_direction", int32(5), "two columns away faces back east"},
		{Dispenser{}, mgl64.Vec3{1.526, -1.12, 2.819}, cube.Rotation{160, 0}, cube.FaceSouth, "facing_direction", int32(3), "two columns away faces back south"},
		{Dispenser{}, mgl64.Vec3{-1.312, -2.918, 1.16}, cube.Rotation{250, -50}, cube.FaceDown, "facing_direction", int32(4), "below but two columns away faces back west"},
		{Observer{}, mgl64.Vec3{1.16, 1.178, -0.812}, cube.Rotation{20, 50}, cube.FaceNorth, "minecraft:facing_direction", "down", "observes away from a player above"},
		{Observer{}, mgl64.Vec3{3.319, -1.12, 1.026}, cube.Rotation{110, 0}, cube.FaceSouth, "minecraft:facing_direction", "west", "observes the way the player looks"},
		{Observer{}, mgl64.Vec3{-1.312, -2.918, 1.16}, cube.Rotation{250, -50}, cube.FaceDown, "minecraft:facing_direction", "east", "two columns away observes east"},
		{Piston{}, mgl64.Vec3{1.16, 1.178, -0.812}, cube.Rotation{20, 50}, cube.FaceNorth, "facing_direction", int32(1), "pushes up at a player above"},
		{Piston{}, mgl64.Vec3{3.319, -1.12, 1.026}, cube.Rotation{110, 0}, cube.FaceSouth, "facing_direction", int32(4), "pushes east, encoded 4"},
		{Piston{}, mgl64.Vec3{-1.312, -3.418, 0.34}, cube.Rotation{290, -50}, cube.FaceNorth, "facing_direction", int32(5), "pushes west, encoded 5"},
		{Crafter{}, mgl64.Vec3{1.16, 1.178, -0.812}, cube.Rotation{20, 50}, cube.FaceNorth, "orientation", "up_north", "faces up, top back at the player"},
		{Crafter{}, mgl64.Vec3{1.16, -3.418, 1.812}, cube.Rotation{160, -50}, cube.FaceSouth, "orientation", "down_south", "faces down, top back at the player"},
		{Crafter{}, mgl64.Vec3{3.319, -1.12, -0.026}, cube.Rotation{70, 0}, cube.FaceNorth, "orientation", "east_up", "faces back east"},
	}
	for _, test := range tests {
		name, _ := test.block.EncodeBlock()
		t.Run(name+" "+test.reason, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			defer w.Close()
			placed := cube.Pos{0, 64, 0}
			target := placed.Side(test.face.Opposite())
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(target, Stone{}, nil)
				user := &placerTestUser{pistonTestUser{pos: placed.Vec3().Add(test.feet), rot: test.rot}}
				test.block.(item.UsableOnBlock).UseOnBlock(target, test.face, mgl64.Vec3{}, tx, user, &item.UseContext{})
				_, props := tx.Block(placed).EncodeBlock()
				if got := props[test.key]; got != test.want {
					t.Fatalf("%s = %v, want %v", test.key, got, test.want)
				}
			})
		})
	}
}
