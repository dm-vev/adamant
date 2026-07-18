package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type pistonTestUser struct {
	pos mgl64.Vec3
	rot cube.Rotation
}

func (*pistonTestUser) Close() error                        { return nil }
func (*pistonTestUser) H() *world.EntityHandle              { return nil }
func (u *pistonTestUser) Position() mgl64.Vec3              { return u.pos }
func (u *pistonTestUser) Rotation() cube.Rotation           { return u.rot }
func (*pistonTestUser) HeldItems() (item.Stack, item.Stack) { return item.Stack{}, item.Stack{} }
func (*pistonTestUser) SetHeldItems(item.Stack, item.Stack) {}
func (*pistonTestUser) UsingItem() bool                     { return false }
func (*pistonTestUser) ReleaseItem()                        {}
func (*pistonTestUser) UseItem()                            {}

func TestPistonFacing(t *testing.T) {
	tests := []struct {
		name string
		face cube.Face
		pos  mgl64.Vec3
		rot  cube.Rotation
		want uint8
	}{
		{"down", cube.FaceDown, mgl64.Vec3{0.5, -1, 0.5}, cube.Rotation{}, 0},
		{"up", cube.FaceUp, mgl64.Vec3{0.5, 3, 0.5}, cube.Rotation{}, 1},
		{"north", cube.FaceNorth, mgl64.Vec3{0.5, 1, -3}, cube.Rotation{0, 0}, 2},
		{"south", cube.FaceSouth, mgl64.Vec3{0.5, 1, 3}, cube.Rotation{180, 0}, 3},
		{"west", cube.FaceWest, mgl64.Vec3{-3, 1, 0.5}, cube.Rotation{-90, 0}, 4},
		{"east", cube.FaceEast, mgl64.Vec3{3, 1, 0.5}, cube.Rotation{90, 0}, 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()
			pos := cube.Pos{0, 0, 0}
			ctx := &item.UseContext{}
			w.Exec(func(tx *world.Tx) {
				Piston{}.UseOnBlock(pos, cube.FaceUp, mgl64.Vec3{}, tx, &pistonTestUser{pos: test.pos, rot: test.rot}, ctx)
				placed := tx.Block(pos).(Piston)
				if placed.Facing != test.face {
					t.Fatalf("placed facing = %v, want %v", placed.Facing, test.face)
				}
			})

			piston := Piston{Facing: test.face}
			_, properties := piston.EncodeBlock()
			if got := properties["facing_direction"]; got != int32(test.want) {
				t.Fatalf("facing_direction = %v (%T), want %d (int32)", got, got, test.want)
			}
			if got := piston.EncodeNBT()["facing"]; got != test.want {
				t.Fatalf("NBT facing = %v (%T), want %d (uint8)", got, got, test.want)
			}

			decoded := piston.DecodeNBT(map[string]any{"facing": uint8(5 - test.want)}).(Piston)
			if decoded.Facing != test.face {
				t.Fatalf("decoded facing = %v, want runtime state %v", decoded.Facing, test.face)
			}
		})
	}
}
