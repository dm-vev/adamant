package entity

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMinecartMovesOnStraightRails(t *testing.T) {
	for _, test := range []struct {
		name string
		dir  block.RailDirection
		vel  mgl64.Vec3
		axis int
	}{
		{"east-west", block.RailEastWest, mgl64.Vec3{0.1, 0, 0}, 0},
		{"north-south", block.RailNorthSouth, mgl64.Vec3{0, 0, 0.1}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()

			<-w.Exec(func(tx *world.Tx) {
				pos := cube.Pos{0, 1, 0}
				placeMinecartRailLine(tx, pos, test.dir, block.Rail{Direction: test.dir})
				handle := world.EntitySpawnOpts{Position: pos.Vec3Middle(), Velocity: test.vel}.New(MinecartType, minecartConf)
				tx.AddEntity(handle)
				entity, _ := handle.Entity(tx)
				cart := entity.(*Minecart)
				before := cart.Position()

				for range 8 {
					cart.Behaviour().(*MinecartBehaviour).Tick(cart.Ent, tx).Send()
				}
				if got := cart.Position()[test.axis]; got <= before[test.axis]+0.5 {
					t.Fatalf("position on axis %d = %v, want > %v", test.axis, got, before[test.axis]+0.5)
				}
			})
		})
	}
}

func TestMinecartAcceleratesOnPoweredRail(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 1, 0}
		placeMinecartRailLine(tx, pos, block.RailEastWest, block.PoweredRail{Direction: block.RailEastWest, Powered: true})
		tx.SetBlock(pos.Side(cube.FaceDown), block.RedstoneBlock{}, nil)
		handle := world.EntitySpawnOpts{Position: pos.Vec3Middle(), Velocity: mgl64.Vec3{0.1, 0, 0}}.New(MinecartType, minecartConf)
		tx.AddEntity(handle)
		entity, _ := handle.Entity(tx)
		cart := entity.(*Minecart)

		cart.Behaviour().(*MinecartBehaviour).Tick(cart.Ent, tx).Send()
		if got := cart.Velocity()[0]; got <= 0.1 {
			t.Fatalf("powered velocity = %v, want > 0.1", got)
		}
	})
}

func TestRiderInputStartsMinecart(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 1, 0}
		placeMinecartRailLine(tx, pos, block.RailNorthSouth, block.Rail{Direction: block.RailNorthSouth})
		cartHandle := world.EntitySpawnOpts{Position: pos.Vec3Middle()}.New(MinecartType, minecartConf)
		riderHandle := NewCow(world.EntitySpawnOpts{Position: pos.Vec3Middle()})
		tx.AddEntity(cartHandle)
		tx.AddEntity(riderHandle)
		entity, _ := cartHandle.Entity(tx)
		cart := entity.(*Minecart)
		behaviour := cart.Behaviour().(*MinecartBehaviour)
		behaviour.passenger = riderHandle
		behaviour.SetVehicleInput(0, 1)
		before := cart.Position()[2]

		for range 2 {
			behaviour.Tick(cart.Ent, tx).Send()
		}
		if got := cart.Position()[2]; got <= before+0.1 {
			t.Fatalf("position = %v, want > %v", got, before+0.1)
		}
	})
}

func TestMinecartPassengerSeatHeight(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	pos := mgl64.Vec3{0, 4, 0}
	cartHandle := world.EntitySpawnOpts{Position: pos}.New(MinecartType, minecartConf)
	passengerHandle := world.EntitySpawnOpts{Position: pos}.New(seatTestRiderType{}, seatTestRiderConfig{})
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(cartHandle)
		tx.AddEntity(passengerHandle)
		cartEntity, _ := cartHandle.Entity(tx)
		passenger, _ := passengerHandle.Entity(tx)
		rider := passenger.(*seatTestRider)
		cart := cartEntity.(*Minecart)
		behaviour := cart.Behaviour().(*MinecartBehaviour)
		behaviour.passenger = passengerHandle
		behaviour.updatePassenger(cart.Ent, tx)

		rim := cart.Position()[1] + MinecartType.BBox(cart).Max()[1]
		eyeY := passenger.Position()[1] + rider.EyeHeight()
		if passenger.Position()[1] <= rim || eyeY <= rim {
			t.Fatalf("passenger base/eye = %v/%v, want above minecart rim %v", passenger.Position()[1], eyeY, rim)
		}
	})
}

func placeMinecartRailLine(tx *world.Tx, pos cube.Pos, direction block.RailDirection, centre world.Block) {
	for offset := -1; offset <= 1; offset++ {
		railPos := pos.Add(cube.Pos{offset, 0, 0})
		if direction == block.RailNorthSouth {
			railPos = pos.Add(cube.Pos{0, 0, offset})
		}
		tx.SetBlock(railPos.Side(cube.FaceDown), block.Cobblestone{}, nil)
		tx.SetBlock(railPos, block.Rail{Direction: direction}, nil)
	}
	tx.SetBlock(pos, centre, nil)
}

// TestRiddenMinecartReachesMaxSpeedOnPoweredRails checks a ridden minecart
// reaches 0.4 blocks a tick on powered rails. Its velocity used to be replaced
// by its movement, scaled by 0.75 for the passenger every tick, so it stayed
// under 0.18 blocks a tick.
func TestRiddenMinecartReachesMaxSpeedOnPoweredRails(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: DefaultRegistry, RandomTickSpeed: -1}.New()
	defer w.Close()
	var cart *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := range 80 {
			tx.SetBlock(cube.Pos{x, 0, 0}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{x, 1, 0}, block.PoweredRail{Direction: block.RailEastWest}, nil)
			if x%8 == 0 {
				// A powered rail passes power on to eight rails.
				tx.SetBlock(cube.Pos{x, 1, 1}, block.RedstoneBlock{}, nil)
			}
		}
	})
	for range 20 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		cart = world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 1, 0.5}, Velocity: mgl64.Vec3{0.05}}.New(MinecartType, minecartConf)
		tx.AddEntity(cart)
		rider := NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 1, 0.5}})
		tx.AddEntity(rider)
		e, _ := cart.Entity(tx)
		e.(*Minecart).Behaviour().(*MinecartBehaviour).passenger = rider
	})
	var before, after float64
	for i := range 30 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := cart.Entity(tx)
			if i == 28 {
				before = e.Position()[0]
			}
			after = e.Position()[0]
		})
	}
	if step := after - before; step < 0.39 {
		t.Fatalf("ridden minecart moves %.3f blocks a tick on powered rails, want 0.4", step)
	}
}

// TestMinecartFollowsSlope checks a minecart rides down and up a slope on the
// track. It used to move at its height on the slope, which ran it into the
// block under the next rail uphill and left it inside the ground at the foot
// of a slope, which it fell through.
func TestMinecartFollowsSlope(t *testing.T) {
	railY := func(x int) int {
		switch {
		case x >= 20:
			return 11
		case x >= 10:
			return 1 + x - 10
		}
		return 1
	}
	for _, test := range []struct {
		name       string
		start, vel mgl64.Vec3
		end        float64
	}{
		{"down", mgl64.Vec3{21.5, 11, 0.5}, mgl64.Vec3{-0.2}, 1.5},
		{"up", mgl64.Vec3{4.5, 1, 0.5}, mgl64.Vec3{1.2}, 11.5},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true, Entities: DefaultRegistry, RandomTickSpeed: -1}.New()
			defer w.Close()
			var cart *world.EntityHandle
			<-w.Exec(func(tx *world.Tx) {
				for x := -8; x < 40; x++ {
					dir := block.RailEastWest
					if x >= 10 && x < 20 {
						dir = block.RailAscendingEast
					}
					for y := 0; y < railY(x); y++ {
						tx.SetBlock(cube.Pos{x, y, 0}, block.Stone{}, nil)
					}
					tx.SetBlock(cube.Pos{x, railY(x), 0}, block.Rail{Direction: dir}, nil)
				}
				cart = world.EntitySpawnOpts{Position: test.start, Velocity: test.vel}.New(MinecartType, minecartConf)
				tx.AddEntity(cart)
				rider := NewCow(world.EntitySpawnOpts{Position: test.start})
				tx.AddEntity(rider)
				e, _ := cart.Entity(tx)
				e.(*Minecart).Behaviour().(*MinecartBehaviour).passenger = rider
			})
			var y float64
			for i := range 60 {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					e, _ := cart.Entity(tx)
					pos := e.Position()
					y = pos[1]
					if floor := float64(railY(int(math.Floor(pos[0])))); y < floor {
						t.Fatalf("tick %d: minecart at %v is below the rail at y=%v", i, pos, floor)
					}
				})
			}
			if math.Abs(y-test.end) > 1e-6 {
				t.Fatalf("minecart ends at y=%v, want %v", y, test.end)
			}
		})
	}
}
