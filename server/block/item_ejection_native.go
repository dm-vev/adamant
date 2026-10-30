package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// nativeItemEjectionVelocity follows DispenserBlock::ejectItem. Vertical
// ejection has the same initial upward speed as horizontal ejection; facing
// changes only the horizontal components before Gaussian spread is added.
func nativeItemEjectionVelocity(face cube.Face, tx *world.Tx) mgl64.Vec3 {
	var dx, dz float32
	switch face {
	case cube.FaceNorth:
		dz = -1
	case cube.FaceSouth:
		dz = 1
	case cube.FaceWest:
		dx = -1
	case cube.FaceEast:
		dx = 1
	}
	speed := float32(tx.RandomFloat32()*.1) + .2
	v := [3]float32{float32(dx * speed), .2, float32(dz * speed)}
	for i := range v {
		noise := float32(tx.RandomGaussianFloat32() * .0075)
		noise = float32(noise * 6)
		v[i] = float32(v[i] + noise)
	}
	return mgl64.Vec3{float64(v[0]), float64(v[1]), float64(v[2])}
}

// Crafters and ordinary dispenser/dropper items share this launch point.
// Sounds and smoke use it before the item's vertical correction.
func nativeItemEjectionLaunch(pos cube.Pos, face cube.Face) mgl64.Vec3 {
	x, y, z := float32(pos[0])+.5, float32(pos[1])+.3, float32(pos[2])+.5
	switch face {
	case cube.FaceDown:
		y = float32(y - .7)
	case cube.FaceUp:
		y = float32(y + .7)
	case cube.FaceNorth:
		z = float32(z - .7)
	case cube.FaceSouth:
		z = float32(z + .7)
	case cube.FaceWest:
		x = float32(x - .7)
	case cube.FaceEast:
		x = float32(x + .7)
	}
	return mgl64.Vec3{float64(x), float64(y), float64(z)}
}

// Keep each separate binary32 addition from DispenserBlock::ejectItem.
func nativeItemEjectionPosition(pos cube.Pos, face cube.Face) mgl64.Vec3 {
	p := nativeItemEjectionLaunch(pos, face)
	y := float32(p[1])
	if face == cube.FaceDown || face == cube.FaceUp {
		y = float32(y - .125)
	} else {
		y = float32(y - .15625)
	}
	p[1] = float64(y)
	return p
}

func ejectContainerItem(pos cube.Pos, face cube.Face, tx *world.Tx, stack item.Stack) {
	// ItemActor construction draws a shared float for an internal actor flag
	// before ejectItem assigns speed and Gaussian spread.
	_ = tx.RandomFloat32()
	opts := world.EntitySpawnOpts{Position: nativeItemEjectionPosition(pos, face), Velocity: nativeItemEjectionVelocity(face, tx)}
	spawned := tx.AddEntity(tx.World().EntityRegistry().Config().Item(opts, stack))
	if item, ok := spawned.(interface{ SetItemPickupDelay(time.Duration) }); ok {
		item.SetItemPickupDelay(0)
	}
	tx.QueueEntityTick(spawned)
}

func playContainerSound(pos mgl64.Vec3, tx *world.Tx, name string) {
	registry := tx.World().BlockRegistry()
	hash, _ := registry.RuntimeIDToHash(registry.BlockRuntimeID(tx.Block(cube.PosFromVec3(pos))))
	tx.PlaySound(pos, sound.NativeEvent{SoundType: name, ExtraData: int32(hash), EntityUniqueID: -1})
}

func playItemEjectionEffects(pos cube.Pos, face cube.Face, tx *world.Tx) {
	launch := nativeItemEjectionLaunch(pos, face)
	tx.AddParticle(launch, particle.DispenserSmoke{Face: face})
	playContainerSound(launch, tx, "block.click")
}
