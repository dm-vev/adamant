package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestConduitPowersAndHunts follows the BDS conduit: a framed conduit in water
// powers players in water, picks a hostile mob in water as its target and
// attacks it from the next cycle on, keeps a vanished target until another
// mob shows up, and drops its target once the frame is no longer full.
func TestConduitPowersAndHunts(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 100, 0}
	var frame []cube.Pos
	for x := -2; x <= 2; x++ {
		for y := -2; y <= 2; y++ {
			for z := -2; z <= 2; z++ {
				ax, ay, az := abs(x), abs(y), abs(z)
				if (ax > 1 || ay > 1 || az > 1) && ((x == 0 && (ay == 2 || az == 2)) || (y == 0 && (ax == 2 || az == 2)) || (z == 0 && (ax == 2 || ay == 2))) {
					frame = append(frame, pos.Add(cube.Pos{x, y, z}))
				}
			}
		}
	}
	if len(frame) != 42 {
		t.Fatalf("frame has %d blocks, want 42", len(frame))
	}

	swimmer := &conduitTestState{id: "minecraft:player", health: 20}
	walker := &conduitTestState{id: "minecraft:player", health: 20}
	drowned := &conduitTestState{id: "minecraft:drowned", health: 20}
	pig := &conduitTestState{id: "minecraft:pig", health: 10}
	var drownedHandle *world.EntityHandle
	tick := func(tx *world.Tx, n int64) Conduit {
		tx.Block(pos).(Conduit).Tick(n*40, pos, tx)
		return tx.Block(pos).(Conduit)
	}
	target := func(c Conduit) int64 { return c.EncodeNBT()["Target"].(int64) }

	<-w.Exec(func(tx *world.Tx) {
		for x := -4; x <= 4; x++ {
			for y := -4; y <= 4; y++ {
				for z := -4; z <= 4; z++ {
					tx.SetBlock(pos.Add(cube.Pos{x, y, z}), Water{Still: true, Depth: 8}, nil)
				}
			}
		}
		for y := 90; y <= 110; y++ {
			tx.SetBlock(cube.Pos{10, y, 0}, Stone{}, nil)
		}
		tx.SetBlock(pos, Conduit{}, nil)
		tx.SetLiquid(pos, Water{Still: true, Depth: 8})
		for _, p := range frame {
			tx.SetBlock(p, Prismarine{Type: NormalPrismarine()}, nil)
		}
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 98, 0.5}}.New(conduitTestEntityType{swimmer.id}, swimmer))
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{10.5, 111, 0.5}}.New(conduitTestEntityType{walker.id}, walker))
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 97, 3.5}}.New(conduitTestEntityType{pig.id}, pig))
		drownedHandle = world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 97, -3.5}}.New(conduitTestEntityType{drowned.id}, drowned)
		tx.AddEntity(drownedHandle)

		c := tick(tx, 1)
		if !c.Active() {
			t.Fatal("framed conduit in water is not active")
		}
		if e, ok := swimmer.effect(); !ok || !e.Ambient() || e.Level() != 1 || e.Duration().Milliseconds() != 13000 {
			t.Fatalf("player in water got %v, %t; want 13s ambient Conduit Power I", e, ok)
		}
		if _, ok := walker.effect(); ok {
			t.Fatal("player out of water got Conduit Power")
		}
		if target(c) != drownedHandle.UniqueID() {
			t.Fatalf("target = %d, want drowned %d", target(c), drownedHandle.UniqueID())
		}
		if drowned.health != 20 || pig.health != 10 {
			t.Fatal("conduit attacked in the cycle it picked its target")
		}

		tick(tx, 2)
		if drowned.health != 16 || drowned.magic != 1 || pig.health != 10 {
			t.Fatalf("after attack drowned health %v (%d magic hits), pig %v; want 16, 1, 10", drowned.health, drowned.magic, pig.health)
		}

		drownedEntity, _ := drownedHandle.Entity(tx)
		tx.RemoveEntity(drownedEntity)
		if c := tick(tx, 3); target(c) != drownedHandle.UniqueID() {
			t.Fatalf("vanished target replaced by %d with no other mob around", target(c))
		}
		second := &conduitTestState{id: "minecraft:zombie", health: 20}
		secondHandle := world.EntitySpawnOpts{Position: mgl64.Vec3{-2.5, 97, 0.5}}.New(conduitTestEntityType{second.id}, second)
		tx.AddEntity(secondHandle)
		if c := tick(tx, 4); target(c) != secondHandle.UniqueID() || second.health != 20 {
			t.Fatalf("target = %d, health %v; want new zombie %d untouched", target(c), second.health, secondHandle.UniqueID())
		}

		tx.SetBlock(frame[0], Water{Still: true, Depth: 8}, nil)
		c = tick(tx, 5)
		if !c.Active() || target(c) != -1 || second.health != 20 {
			t.Fatalf("41 frame blocks: active %t, target %d, zombie health %v; want active, no target, untouched", c.Active(), target(c), second.health)
		}

		tx.SetBlock(pos.Add(cube.Pos{1, 1, 1}), Stone{}, nil)
		if c := tick(tx, 6); c.Active() {
			t.Fatal("conduit without water around it is active")
		}
	})
}

type conduitTestState struct {
	id      string
	health  float64
	magic   int
	effects []effect.Effect
}

func (s *conduitTestState) Apply(data *world.EntityData) { data.Data = s }

func (s *conduitTestState) effect() (effect.Effect, bool) {
	for _, e := range s.effects {
		if e.Type() == effect.ConduitPower {
			return e, true
		}
	}
	return effect.Effect{}, false
}

// conduitTestEntityType reports its entities under the identifier given.
type conduitTestEntityType struct{ id string }

func (conduitTestEntityType) Open(_ *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return conduitTestEntity{handle: handle, data: data, state: data.Data.(*conduitTestState)}
}
func (t conduitTestEntityType) EncodeEntity() string { return t.id }
func (conduitTestEntityType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)
}
func (conduitTestEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (conduitTestEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type conduitTestEntity struct {
	handle *world.EntityHandle
	data   *world.EntityData
	state  *conduitTestState
}

func (e conduitTestEntity) Close() error            { return nil }
func (e conduitTestEntity) H() *world.EntityHandle  { return e.handle }
func (e conduitTestEntity) Position() mgl64.Vec3    { return e.data.Pos }
func (e conduitTestEntity) Rotation() cube.Rotation { return e.data.Rot }
func (e conduitTestEntity) Dead() bool              { return e.state.health <= 0 }
func (e conduitTestEntity) AddEffect(eff effect.Effect) {
	e.state.effects = append(e.state.effects, eff)
}
func (e conduitTestEntity) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	if _, ok := src.(effect.InstantDamageSource); ok {
		e.state.magic++
	}
	e.state.health -= damage
	return damage, true
}
