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

			decoded := piston.DecodeNBT(piston.EncodeNBT()).(Piston)
			if decoded.Facing != test.face {
				t.Fatalf("decoded facing = %v, want runtime state %v", decoded.Facing, test.face)
			}
		})
	}
}

func TestPistonLifecycle(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	pos := cube.Pos{0, 0, 0}
	piston := Piston{Facing: cube.FaceEast, Sticky: true}
	blockPos := pos.Side(cube.FaceEast)
	sourcePos := pos.Side(cube.FaceWest)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, piston, nil)
		tx.SetBlock(blockPos, Stone{}, nil)
		tx.SetBlock(sourcePos, RedstoneBlock{}, nil)
		piston.RedstonePowerActionUpdate(pos, tx, world.RedstoneUpdate{})
		got, ok := tx.Block(pos).(Piston)
		if !ok || !got.Moving || !got.Extending || !got.Powered {
			t.Fatalf("powered piston = %#v, want extending and powered", tx.Block(pos))
		}
		if head, ok := tx.Block(blockPos).(PistonHead); !ok || head.Facing != cube.FaceEast || !head.Sticky {
			t.Fatalf("piston head = %#v, want east sticky head", tx.Block(blockPos))
		}
		if moving, ok := tx.Block(blockPos.Side(cube.FaceEast)).(MovingBlock); !ok || moving.PistonPos != pos {
			t.Fatalf("moving block = %#v, want block moved east", tx.Block(blockPos.Side(cube.FaceEast)))
		}
	})
	w.AdvanceTick()
	w.AdvanceTick()
	w.AdvanceTick()

	<-w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(blockPos).(PistonHead); !ok {
			t.Fatalf("extended piston head missing: %#v", tx.Block(blockPos))
		}
		tx.SetBlock(sourcePos, Air{}, nil)
		piston, ok := tx.Block(pos).(Piston)
		if !ok {
			t.Fatalf("piston = %#v", tx.Block(pos))
		}
		piston.RedstonePowerActionUpdate(pos, tx, world.RedstoneUpdate{})
		piston = tx.Block(pos).(Piston)
		if !piston.Moving || piston.Extending || piston.Powered {
			t.Fatalf("retracting piston = %#v", piston)
		}
	})
	w.AdvanceTick()
	w.AdvanceTick()
	w.AdvanceTick()

	<-w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(blockPos).(Stone); !ok {
			t.Fatalf("sticky piston did not retract block: %#v", tx.Block(blockPos))
		}
		if _, ok := tx.Block(blockPos.Side(cube.FaceEast)).(Air); !ok {
			t.Fatalf("moving block remained after retraction: %#v", tx.Block(blockPos.Side(cube.FaceEast)))
		}
	})
}

func TestPistonRedstoneDirections(t *testing.T) {
	for _, facing := range cube.Faces() {
		t.Run(facing.String(), func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()

			pos := cube.Pos{0, 0, 0}
			piston := Piston{Facing: facing}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos.Side(facing.Opposite()), RedstoneBlock{}, nil)
				if !piston.shouldExtend(pos, tx) {
					t.Fatal("piston did not accept power from behind")
				}
				tx.SetBlock(pos.Side(facing.Opposite()), Air{}, nil)
				tx.SetBlock(pos.Side(facing), RedstoneBlock{}, nil)
				if piston.shouldExtend(pos, tx) {
					t.Fatal("piston accepted power from its facing side")
				}
			})
		})
	}
}

func TestPistonNBT(t *testing.T) {
	piston := Piston{
		Facing: cube.FaceNorth, Sticky: true, Moving: true, Powered: true,
		Extending: true, Progress: 0.5, LastProgress: 0,
		State: pistonStateExtending, NewState: pistonStateExtending,
		Attached: []cube.Pos{{1, 2, 3}},
	}
	decoded := piston.DecodeNBT(piston.EncodeNBT()).(Piston)
	if decoded.Facing != piston.Facing || decoded.Sticky != piston.Sticky || decoded.Powered != piston.Powered ||
		decoded.Extending != piston.Extending || decoded.State != piston.State || len(decoded.Attached) != 1 || decoded.Attached[0] != (cube.Pos{1, 2, 3}) {
		t.Fatalf("decoded piston = %#v, want %#v", decoded, piston)
	}
	legacy := (Piston{Facing: cube.FaceWest, Sticky: true}).DecodeNBT(map[string]any{}).(Piston)
	if legacy.Facing != cube.FaceWest || !legacy.Sticky {
		t.Fatalf("NBT without facing state changed runtime properties: %#v", legacy)
	}

	moving := MovingBlock{Moving: Chest{}, PistonPos: cube.Pos{4, 5, 6}, MovingEntity: map[string]any{"id": "Chest"}}
	decodedMoving := moving.DecodeNBT(moving.EncodeNBT()).(MovingBlock)
	if _, ok := decodedMoving.Moving.(Chest); !ok || decodedMoving.PistonPos != moving.PistonPos || decodedMoving.MovingEntity["id"] != "Chest" {
		t.Fatalf("decoded moving block = %#v, want %#v", decodedMoving, moving)
	}
}

func TestPistonRestoresMovingBlockWithRegistry(t *testing.T) {
	registry, custom := containerTestBlockRegistry()
	w := world.Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()

	chest := NewChest()
	if err := chest.inventory.SetItem(0, item.NewStack(custom, 2)); err != nil {
		t.Fatal(err)
	}
	movingNBT := world.DecodeNBT(MovingBlock{}, MovingBlock{
		Moving:       custom,
		MovingEntity: chest.EncodeNBT(),
		PistonPos:    cube.Pos{},
	}.EncodeNBT(), registry).(MovingBlock)
	if movingNBT.Moving != custom {
		t.Fatalf("moving block = %#v, want %#v", movingNBT.Moving, custom)
	}

	<-w.Exec(func(tx *world.Tx) {
		movingPos := cube.Pos{2, 0, 0}
		tx.SetBlock(movingPos, MovingBlock{Moving: Chest{}, MovingEntity: movingNBT.MovingEntity}, nil)
		piston := Piston{Facing: cube.FaceEast, Extending: true, Attached: []cube.Pos{{1, 0, 0}}}
		piston.finishMove(cube.Pos{}, tx)
		decoded := tx.Block(movingPos).(Chest)
		got, err := decoded.inventory.Item(0)
		if err != nil {
			t.Fatal(err)
		}
		if got.Count() != 2 || got.Item() != custom {
			t.Fatalf("moved chest item = %#v x%d, want %#v x2", got.Item(), got.Count(), custom)
		}
	})
}
