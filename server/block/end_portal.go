package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EndPortal is a block that teleports players to the End dimension when entered.
// When found inside the End, it instead returns players to the overworld spawn.
type EndPortal struct {
	replaceable
	transparent
	empty
}

// LightEmissionLevel returns the light level emitted by the portal.
func (EndPortal) LightEmissionLevel() uint8 {
	return 1
}

// EncodeItem returns the portal's block representation.
func (EndPortal) EncodeItem() (name string, meta int16) {
	return "minecraft:end_portal", 0
}

// EncodeBlock returns the block state for the portal.
func (EndPortal) EncodeBlock() (string, map[string]any) {
	return "minecraft:end_portal", nil
}

// EntityInside handles players entering the portal, transferring them between the overworld and the End.
func (EndPortal) EntityInside(_ cube.Pos, tx *world.Tx, e world.Entity) {
	if _, ok := e.(interface{ Teleport(mgl64.Vec3) }); !ok {
		return
	}

	if dead, ok := e.(interface{ Dead() bool }); ok && dead.Dead() {
		return
	}

	current := tx.World()
	var targetDim world.Dimension
	if current.Dimension() == world.End {
		targetDim = world.Overworld
	} else {
		targetDim = world.End
	}

	dest := current.PortalDestination(targetDim)
	if dest == nil || dest == current {
		return
	}

	handle := tx.RemoveEntity(e)
	if handle == nil {
		return
	}

	spawn := dest.Spawn().Add(cube.Pos{0, 1})
	destVec := spawn.Vec3Middle()

	dest.Exec(func(destTx *world.Tx) {
		if ent, ok := destTx.AddEntity(handle).(interface{ Teleport(mgl64.Vec3) }); ok {
			ent.Teleport(destVec)
		}
	})
}
