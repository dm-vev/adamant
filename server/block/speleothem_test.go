package block

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type sneakingTestUser struct {
	pistonTestUser
	sneaking bool
}

func (u *sneakingTestUser) Sneaking() bool { return u.sneaking }

func placeDripstone(t *testing.T, w *world.World, clicked cube.Pos, face cube.Face, user item.User) {
	t.Helper()
	<-w.Exec(func(tx *world.Tx) {
		PointedDripstone{}.UseOnBlock(clicked, face, mgl64.Vec3{}, tx, user, &item.UseContext{})
	})
	w.AdvanceTick()
}

func dripstoneColumn(w *world.World, from, to int) (column []world.Block) {
	<-w.Exec(func(tx *world.Tx) {
		for y := from; y >= to; y-- {
			column = append(column, tx.Block(cube.Pos{0, y, 0}))
		}
	})
	return column
}

func TestPointedDripstonePlacementShapesColumn(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) { tx.SetBlock(cube.Pos{0, 10, 0}, Stone{}, nil) })
	lookingUp := &pistonTestUser{rot: cube.Rotation{0, -45}}
	for y := 10; y > 6; y-- {
		placeDripstone(t, w, cube.Pos{0, y, 0}, cube.FaceDown, lookingUp)
	}
	// Thickness changes propagate up the column one neighbour update per tick.
	for range 4 {
		w.AdvanceTick()
	}
	want := []world.Block{
		PointedDripstone{Hanging: true, Thickness: SpeleothemBase},
		PointedDripstone{Hanging: true, Thickness: SpeleothemMiddle},
		PointedDripstone{Hanging: true, Thickness: SpeleothemFrustum},
		PointedDripstone{Hanging: true, Thickness: SpeleothemTip},
	}
	for i, got := range dripstoneColumn(w, 9, 6) {
		if got != want[i] {
			t.Fatalf("segment %d = %#v, want %#v", i, got, want[i])
		}
	}
}

func TestPointedDripstonePlacementFallsBackToSupportedDirection(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) { tx.SetBlock(cube.Pos{0, 0, 0}, Stone{}, nil) })
	// Looking up asks for a stalactite, but only the floor supports one.
	placeDripstone(t, w, cube.Pos{0, 0, 0}, cube.FaceUp, &pistonTestUser{rot: cube.Rotation{0, -45}})
	if got := dripstoneColumn(w, 1, 1)[0]; got != (PointedDripstone{Thickness: SpeleothemTip}) {
		t.Fatalf("placed %#v, want a stalagmite tip", got)
	}
}

func TestPointedDripstoneOpposingTipsMergeUnlessSneaking(t *testing.T) {
	for _, sneaking := range []bool{false, true} {
		w := newMultifaceTestWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{0, 3, 0}, Stone{}, nil)
			tx.SetBlock(cube.Pos{0, 2, 0}, PointedDripstone{Hanging: true}, nil)
			tx.SetBlock(cube.Pos{0, 0, 0}, Stone{}, nil)
		})
		placeDripstone(t, w, cube.Pos{0, 0, 0}, cube.FaceUp, &sneakingTestUser{pistonTestUser{rot: cube.Rotation{0, 45}}, sneaking})
		want := SpeleothemTipMerge
		if sneaking {
			want = SpeleothemTip
		}
		column := dripstoneColumn(w, 2, 1)
		if column[0] != (PointedDripstone{Hanging: true, Thickness: want}) || column[1] != (PointedDripstone{Thickness: want}) {
			t.Fatalf("sneaking=%v: column %#v, want both %v", sneaking, column, want)
		}
	}
}

// dripstoneCave builds a dripstone block at y=10 with fluid above at y=11 and
// a stalactite tip at y=9.
func dripstoneCave(tx *world.Tx, above world.Block) {
	tx.SetBlock(cube.Pos{0, 10, 0}, Dripstone{}, nil)
	tx.SetBlock(cube.Pos{0, 11, 0}, above, nil)
	tx.SetBlock(cube.Pos{0, 9, 0}, PointedDripstone{Hanging: true}, nil)
}

func TestPointedDripstoneDripsIntoCauldron(t *testing.T) {
	cases := []struct {
		name   string
		above  world.Block
		chance float32
		want   Cauldron
	}{
		{"water raises a third", Water{Depth: 8, Still: true}, 0.17, Cauldron{FillLevel: 2, Liquid: CauldronWater}},
		{"water beyond its chance", Water{Depth: 8, Still: true}, 0.18, Cauldron{}},
		{"lava fills", Lava{Depth: 8, Still: true}, 0.05, Cauldron{FillLevel: 6, Liquid: CauldronLava}},
		{"lava beyond its chance", Lava{Depth: 8, Still: true}, 0.06, Cauldron{}},
		{"nothing above", Air{}, 0.01, Cauldron{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newMultifaceTestWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				dripstoneCave(tx, c.above)
				tx.SetBlock(cube.Pos{0, 4, 0}, Cauldron{}, nil)
				dripstoneTransferFluid(PointedDripstone{Hanging: true}, cube.Pos{0, 9, 0}, tx, c.chance)
			})
			// The drop lands 50 ticks plus the five blocks it falls later.
			for range 54 {
				w.AdvanceTick()
			}
			<-w.Exec(func(tx *world.Tx) {
				if got := tx.Block(cube.Pos{0, 4, 0}); got != (Cauldron{}) {
					t.Fatalf("cauldron filled after 54 ticks: %#v", got)
				}
			})
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) {
				if got := tx.Block(cube.Pos{0, 4, 0}); got != c.want {
					t.Fatalf("cauldron = %#v, want %#v", got, c.want)
				}
			})
		})
	}
}

func TestPointedDripstoneDripBlockedBySolidBlock(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		dripstoneCave(tx, Water{Depth: 8, Still: true})
		tx.SetBlock(cube.Pos{0, 6, 0}, Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 4, 0}, Cauldron{}, nil)
		dripstoneTransferFluid(PointedDripstone{Hanging: true}, cube.Pos{0, 9, 0}, tx, 0.01)
	})
	for range 60 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if got := tx.Block(cube.Pos{0, 4, 0}); got != (Cauldron{}) {
			t.Fatalf("cauldron below a solid block filled: %#v", got)
		}
	})
}

func TestPointedDripstoneDriesMudAboveIntoClay(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		dripstoneCave(tx, Mud{})
		dripstoneTransferFluid(PointedDripstone{Hanging: true}, cube.Pos{0, 9, 0}, tx, 0.17)
		if got := tx.Block(cube.Pos{0, 11, 0}); got != (Clay{}) {
			t.Fatalf("mud above the stalactite became %#v, want clay", got)
		}
	})
}

func TestStalactiteGrowsOnlyUnderWaterSource(t *testing.T) {
	for _, water := range []bool{true, false} {
		w := newMultifaceTestWorld(t)
		var above world.Block = Air{}
		if water {
			above = Water{Depth: 8, Still: true}
		}
		r := rand.New(rand.NewPCG(1, 2))
		<-w.Exec(func(tx *world.Tx) {
			dripstoneCave(tx, above)
			tx.SetBlock(cube.Pos{0, 0, 0}, Stone{}, nil)
			for range 20 {
				speleothemGrowIfPossible(PointedDripstone{Hanging: true}, cube.Pos{0, 9, 0}, tx, r)
			}
			stalactite := tx.Block(cube.Pos{0, 8, 0}) == PointedDripstone{Hanging: true}
			stalagmite := tx.Block(cube.Pos{0, 1, 0}) == PointedDripstone{}
			if grew := stalactite || stalagmite; grew != water {
				t.Fatalf("water=%v: stalactite grew %v, stalagmite grew %v", water, stalactite, stalagmite)
			}
		})
	}
}
