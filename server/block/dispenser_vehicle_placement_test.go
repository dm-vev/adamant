package block

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// dispenseVehicle dispenses the stack from a dispenser at (0, 5, 0) that faces
// the face given, in a world that set builds around it, and returns the
// vehicle that appeared, or nil if the stack was ejected as an item.
func dispenseVehicle(t *testing.T, facing cube.Face, stack item.Stack, set func(tx *world.Tx, front cube.Pos)) (spawned *dispenserTestEntity) {
	t.Helper()
	entities := world.EntityRegistryConfig{
		Item: func(opts world.EntitySpawnOpts, _ any) *world.EntityHandle {
			return opts.New(dispenserTestEntityType{}, dispenserTestEntityConfig{variant: -1})
		},
		Boat: func(opts world.EntitySpawnOpts, v int) *world.EntityHandle {
			return opts.New(dispenserTestEntityType{}, dispenserTestEntityConfig{variant: v})
		},
		ChestBoat: func(opts world.EntitySpawnOpts, v int) *world.EntityHandle {
			return opts.New(dispenserTestEntityType{}, dispenserTestEntityConfig{variant: v})
		},
		Minecart: func(opts world.EntitySpawnOpts) *world.EntityHandle {
			return opts.New(dispenserTestEntityType{}, dispenserTestEntityConfig{variant: 0})
		},
		MinecartChest: func(opts world.EntitySpawnOpts) *world.EntityHandle {
			return opts.New(dispenserTestEntityType{}, dispenserTestEntityConfig{variant: 1})
		},
	}.New([]world.EntityType{dispenserTestEntityType{}})
	d := NewDispenser()
	d.Facing, d.Triggered = facing, true
	_ = d.inventory.SetItem(0, stack)
	w := world.Config{Synchronous: true, Entities: entities}.New()
	defer func() { _ = w.Close() }()
	pos := cube.Pos{0, 5, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		set(tx, pos.Side(facing))
		d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		for e := range tx.Entities() {
			if te := e.(*dispenserTestEntity); te.data.Data.(int) >= 0 {
				cp := *te
				cp.data = &world.EntityData{Pos: te.Position(), Rot: te.Rotation(), Vel: te.data.Vel, Data: te.data.Data}
				spawned = &cp
			}
		}
	})
	return spawned
}

// TestDispenserPlacesBoatsAsJavaDoes pins BoatDispenseItemBehavior for every
// facing of a dispenser at (0, 5, 0): the boat is put 1.25 blocks out of the
// centre of the dispenser (0.5625 plus half the width of 1.375), 1.125 blocks
// up or down for a vertical dispenser, and a block higher over water than over
// the air above it; it faces the way the dispenser does, and starts at rest.
func TestDispenserPlacesBoatsAsJavaDoes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		facing cube.Face
		// water is whether the water is in front, not below the front.
		water   bool
		pos     mgl64.Vec3
		yaw     float64
		ejected bool
	}{
		{"east on water", cube.FaceEast, true, mgl64.Vec3{1.75, 6.5, 0.5}, 90, false},
		{"east above water", cube.FaceEast, false, mgl64.Vec3{1.75, 5.5, 0.5}, 90, false},
		{"west on water", cube.FaceWest, true, mgl64.Vec3{-0.75, 6.5, 0.5}, 270, false},
		{"west above water", cube.FaceWest, false, mgl64.Vec3{-0.75, 5.5, 0.5}, 270, false},
		{"north on water", cube.FaceNorth, true, mgl64.Vec3{0.5, 6.5, -0.75}, 0, false},
		{"north above water", cube.FaceNorth, false, mgl64.Vec3{0.5, 5.5, -0.75}, 0, false},
		{"south on water", cube.FaceSouth, true, mgl64.Vec3{0.5, 6.5, 1.75}, 180, false},
		{"south above water", cube.FaceSouth, false, mgl64.Vec3{0.5, 5.5, 1.75}, 180, false},
		{"up on water", cube.FaceUp, true, mgl64.Vec3{0.5, 7.625, 0.5}, 90, false},
		{"down on water", cube.FaceDown, true, mgl64.Vec3{0.5, 5.375, 0.5}, 90, false},
		{"down above water", cube.FaceDown, false, mgl64.Vec3{0.5, 4.375, 0.5}, 90, false},
		// The block below the one above the dispenser is the dispenser.
		{"up above water", cube.FaceUp, false, mgl64.Vec3{}, 0, true},
	} {
		for _, stack := range []item.Stack{item.NewStack(item.Boat{Variant: item.BoatVariantCherry}, 1), item.NewStack(item.ChestBoat{Variant: item.BoatVariantBamboo}, 1)} {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				boat := dispenseVehicle(t, c.facing, stack, func(tx *world.Tx, front cube.Pos) {
					if c.ejected {
						return
					}
					at := front
					if !c.water {
						at = front.Side(cube.FaceDown)
					}
					tx.SetLiquid(at, Water{Depth: 8, Still: true})
				})
				if c.ejected {
					if boat != nil {
						t.Fatalf("boat at %v, want it ejected", boat.Position())
					}
					return
				}
				if boat == nil {
					t.Fatal("no boat spawned")
				}
				if boat.Position() != c.pos {
					t.Errorf("boat at %v, want %v", boat.Position(), c.pos)
				}
				if rot := boat.Rotation(); math.Abs(rot.Yaw()-c.yaw) > 1e-9 || rot.Pitch() != 0 {
					t.Errorf("boat rotated %v, want yaw %v", rot, c.yaw)
				}
				if boat.data.Vel != (mgl64.Vec3{}) {
					t.Errorf("boat moves at %v, want it at rest", boat.data.Vel)
				}
			})
		}
	}
}

// TestDispenserPlacesMinecartsOnRails pins where a dispenser at (0, 5, 0)
// requests a minecart, for each facing: the centre of the rail cell, 1/8 along
// the facing, as Bedrock Edition's MinecartItem::dispense does for flat and
// ascending rails alike (net/bedrock testdata bedrock_1_26_51_dispenser_cart_*),
// in the rail in front of the dispenser or in the rail below the air in front.
// Java's MinecartDispenseItemBehavior puts a cart 0.1 blocks above a flat rail
// and 0.6 above a sloped one instead (and -0.9 or -0.4 from the air above),
// but it is the Bedrock clients that see the cart, and BDS was measured.
func TestDispenserPlacesMinecartsOnRails(t *testing.T) {
	t.Parallel()
	flat, slope := world.Block(Rail{Direction: RailEastWest}), world.Block(PoweredRail{Direction: RailAscendingEast})
	for _, c := range []struct {
		name   string
		facing cube.Face
		// above is whether the rail is below the air in front of the dispenser.
		above   bool
		pos     mgl64.Vec3
		ejected bool
	}{
		{"east", cube.FaceEast, false, mgl64.Vec3{1.625, 5.5, 0.5}, false},
		{"east above", cube.FaceEast, true, mgl64.Vec3{1.625, 4.5, 0.5}, false},
		{"west", cube.FaceWest, false, mgl64.Vec3{-0.625, 5.5, 0.5}, false},
		{"west above", cube.FaceWest, true, mgl64.Vec3{-0.625, 4.5, 0.5}, false},
		{"north", cube.FaceNorth, false, mgl64.Vec3{0.5, 5.5, -0.625}, false},
		{"north above", cube.FaceNorth, true, mgl64.Vec3{0.5, 4.5, -0.625}, false},
		{"south", cube.FaceSouth, false, mgl64.Vec3{0.5, 5.5, 1.625}, false},
		{"south above", cube.FaceSouth, true, mgl64.Vec3{0.5, 4.5, 1.625}, false},
		{"up", cube.FaceUp, false, mgl64.Vec3{0.5, 6.5, 0.5}, false},
		{"down", cube.FaceDown, false, mgl64.Vec3{0.5, 4.5, 0.5}, false},
		{"down above", cube.FaceDown, true, mgl64.Vec3{0.5, 3.5, 0.5}, false},
		// The block below the one above the dispenser is the dispenser.
		{"up above", cube.FaceUp, true, mgl64.Vec3{}, true},
	} {
		for shape, rail := range map[string]world.Block{"flat": flat, "ascending": slope} {
			t.Run(c.name+" "+shape, func(t *testing.T) {
				t.Parallel()
				cart := dispenseVehicle(t, c.facing, item.NewStack(item.Minecart{}, 1), func(tx *world.Tx, front cube.Pos) {
					if c.ejected {
						return
					}
					at := front
					if c.above {
						at = front.Side(cube.FaceDown)
					}
					tx.SetBlock(at, rail, nil)
				})
				if c.ejected {
					if cart != nil {
						t.Fatalf("cart at %v, want it ejected", cart.Position())
					}
					return
				}
				if cart == nil {
					t.Fatal("no cart spawned")
				}
				if cart.Position() != c.pos || cart.data.Vel != (mgl64.Vec3{}) {
					t.Errorf("cart at %v moving %v, want %v at rest", cart.Position(), cart.data.Vel, c.pos)
				}
			})
		}
	}
}
