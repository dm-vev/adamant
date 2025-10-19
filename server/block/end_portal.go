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
		if msg := current.PortalDisabledMessage(targetDim); msg != "" {
			if m, ok := e.(interface{ Message(...any) }); ok {
				m.Message(msg)
			}
		}
		return
	}

	handle := tx.RemoveEntity(e)
	if handle == nil {
		return
	}

	dest.Exec(func(destTx *world.Tx) {
		spawn := dest.Spawn().Add(cube.Pos{0, 1})
		if dest.Dimension() == world.End {
			spawn = ensureEndPortalSpawn(destTx)
		}

		if ent, ok := destTx.AddEntity(handle).(interface{ Teleport(mgl64.Vec3) }); ok {
			ent.Teleport(spawn.Vec3Middle())
		}
	})
}

// ensureEndPortalSpawn builds a safe platform around the spawn point returned by the End portal. It mirrors the
// vanilla obsidian pad so players always arrive on solid ground with enough headroom to move.
func ensureEndPortalSpawn(tx *world.Tx) cube.Pos {
	rng := tx.Range()
	spawn := tx.World().Spawn()

	y := spawn.Y()
	minY := rng.Min()
	if y <= minY || y > rng.Max() {
		y = minY + 1
	}

	centre := cube.Pos{spawn.X(), y, spawn.Z()}
	baseY := centre.Y() - 1
	if baseY < minY {
		baseY = minY
		centre[1] = baseY + 1
	}

	if !endSpawnSafe(tx, centre, baseY) {
		buildEndSpawnPlatform(tx, centre, baseY)
	}
	return centre
}

// endSpawnSafe checks if the spawn column is already safe to use, avoiding unnecessary block edits for worlds that
// already provide a proper platform.
func endSpawnSafe(tx *world.Tx, centre cube.Pos, baseY int) bool {
	// Require a 3x3 platform with solid blocks underneath and a clear two-block-tall column for the player.
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			if tx.Block(cube.Pos{centre.X() + x, baseY, centre.Z() + z}) == nil {
				return false
			}
		}
	}

	for y := 0; y < 2; y++ {
		if tx.Block(cube.Pos{centre.X(), centre.Y() + y, centre.Z()}) != nil {
			return false
		}
	}
	return true
}

// buildEndSpawnPlatform constructs the 5x5 obsidian platform used by vanilla End portals and clears headroom so the
// arriving player is not suffocated.
func buildEndSpawnPlatform(tx *world.Tx, centre cube.Pos, baseY int) {
	platform := Obsidian{}
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			pos := cube.Pos{centre.X() + x, baseY, centre.Z() + z}
			tx.SetBlock(pos, platform, nil)

			for y := 1; y <= 3; y++ {
				tx.SetBlock(pos.Add(cube.Pos{0, y, 0}), nil, nil)
			}
		}
	}
}
