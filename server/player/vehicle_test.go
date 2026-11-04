package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// vehicleRecord is what a test vehicle was asked to do, and how it answers.
type vehicleRecord struct {
	controls, dashes, jumps bool

	strafe, forward  float64
	steered, jumping bool
	dashedBy         world.Entity
	rideJumps        int
	dismounted       []world.Entity
}

// vehicleType opens test vehicles that record into rec: vehicles that take
// every input of a rider if full is set, none otherwise.
type vehicleType struct {
	rec  *vehicleRecord
	full bool
}

func (t vehicleType) Open(_ *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	base := inertVehicle{handle: handle, data: data}
	if t.full {
		return &fullVehicle{inertVehicle: base, rec: t.rec}
	}
	return &base
}
func (vehicleType) EncodeEntity() string                        { return "dragonfly:test_vehicle" }
func (vehicleType) BBox(world.Entity) cube.BBox                 { return cube.Box(-.5, 0, -.5, .5, 1, .5) }
func (vehicleType) DecodeNBT(map[string]any, *world.EntityData) {}
func (vehicleType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

// vehicleConfig configures nothing of a test vehicle.
type vehicleConfig struct{}

func (vehicleConfig) Apply(*world.EntityData) {}

// inertVehicle is a vehicle that takes no input of its riders.
type inertVehicle struct {
	handle *world.EntityHandle
	data   *world.EntityData
}

func (v *inertVehicle) Close() error            { return nil }
func (v *inertVehicle) H() *world.EntityHandle  { return v.handle }
func (v *inertVehicle) Position() mgl64.Vec3    { return v.data.Pos }
func (v *inertVehicle) Rotation() cube.Rotation { return v.data.Rot }

// fullVehicle is a vehicle that takes every input of its riders.
type fullVehicle struct {
	inertVehicle
	rec *vehicleRecord
}

func (v *fullVehicle) SetVehicleInput(strafe, forward float64) {
	v.rec.strafe, v.rec.forward, v.rec.steered = strafe, forward, true
}
func (v *fullVehicle) SetVehicleJumping(jumping bool)    { v.rec.jumping = jumping }
func (v *fullVehicle) ControlsVehicle(world.Entity) bool { return v.rec.controls }
func (v *fullVehicle) Dash(_ *world.Tx, rider world.Entity) bool {
	v.rec.dashedBy = rider
	return v.rec.dashes
}
func (v *fullVehicle) RideJump() bool {
	v.rec.rideJumps++
	return v.rec.jumps
}
func (v *fullVehicle) Dismount(_ *world.Tx, passenger world.Entity) {
	v.rec.dismounted = append(v.rec.dismounted, passenger)
}

// ride makes p ride a new test vehicle of t in tx and returns it.
func ride(tx *world.Tx, p *Player, t vehicleType) world.Entity {
	e := tx.AddEntity(world.EntitySpawnOpts{Position: p.Position(), ID: uuid.New()}.New(t, vehicleConfig{}))
	p.SetRiding(e.H())
	return e
}

func TestPlayerSteersNothingWhenNotRiding(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		if p.SteerVehicle(1, 1, true) || p.DashVehicle() || p.RideJump() || p.DismountVehicle() {
			t.Fatal("a player riding nothing steered, dashed, jumped or dismounted")
		}
	})
}

func TestPlayerSteersVehicle(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		rec := &vehicleRecord{controls: true, dashes: true, jumps: true}
		v := ride(tx, p, vehicleType{rec: rec, full: true})
		if !p.SteerVehicle(0.25, -1, true) {
			t.Fatal("SteerVehicle reported the player rides nothing")
		}
		if !rec.steered || rec.strafe != 0.25 || rec.forward != -1 || !rec.jumping {
			t.Fatalf("the vehicle got %+v, want strafe 0.25, forward -1 and a jump", rec)
		}
		if !p.DashVehicle() || rec.dashedBy != world.Entity(p) {
			t.Fatal("the vehicle did not dash for the player")
		}
		if !p.RideJump() || rec.rideJumps != 1 {
			t.Fatal("the vehicle did not jump")
		}
		if !p.DismountVehicle() || len(rec.dismounted) != 1 || rec.dismounted[0] != world.Entity(p) {
			t.Fatalf("DismountVehicle let off %v, want the player", rec.dismounted)
		}
		p.LeaveVehicle(v)
		if len(rec.dismounted) != 2 {
			t.Fatal("LeaveVehicle did not dismount the player")
		}

		rec.dashes, rec.jumps = false, false
		if p.DashVehicle() || p.RideJump() {
			t.Fatal("DashVehicle or RideJump reported what the vehicle refused")
		}
	})
}

func TestPlayerSteersOnlyWhatItControls(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		rec := &vehicleRecord{}
		ride(tx, p, vehicleType{rec: rec, full: true})
		if !p.SteerVehicle(1, 1, true) {
			t.Fatal("SteerVehicle reported the player rides nothing")
		}
		if rec.steered || rec.jumping {
			t.Fatalf("a passenger that does not control the vehicle steered it: %+v", rec)
		}
	})
}

func TestPlayerRidesInertVehicle(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		v := ride(tx, p, vehicleType{})
		if !p.SteerVehicle(1, 1, false) {
			t.Fatal("SteerVehicle reported the player rides nothing")
		}
		if p.DashVehicle() || p.RideJump() || p.DismountVehicle() {
			t.Fatal("a vehicle without the inputs dashed, jumped or let the player off")
		}
		p.LeaveVehicle(v)

		// A vehicle gone from the world is still ridden: the client does
		// not move the player.
		tx.RemoveEntity(v)
		if !p.SteerVehicle(1, 1, false) || p.DismountVehicle() {
			t.Fatal("the player stopped riding a vehicle gone from the world")
		}
	})
}
