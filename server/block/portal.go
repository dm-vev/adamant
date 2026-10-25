package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/portal"
)

// Portal is the translucent part of the nether portal that teleports the player to and from the Nether.
type Portal struct {
	transparent

	// Axis is the axis which the portal faces.
	Axis cube.Axis
}

// portalTraveller represents an entity that can handle touching a portal block.
type portalTraveller interface {
	TravelThroughPortal(tx *world.Tx, target world.Dimension)
}

// Model ...
func (p Portal) Model() world.BlockModel {
	return model.Portal{Axis: p.Axis}
}

// Portal ...
func (Portal) Portal() world.Dimension {
	return world.Nether
}

// LightEmissionLevel returns 11.
func (Portal) LightEmissionLevel() uint8 {
	return 11
}

// HasLiquidDrops ...
func (p Portal) HasLiquidDrops() bool {
	return false
}

// EncodeBlock ...
func (p Portal) EncodeBlock() (string, map[string]interface{}) {
	return "minecraft:portal", map[string]interface{}{"portal_axis": p.Axis.String()}
}

// RandomTick ports NetherPortalBlock.randomTick: in the Overworld, while
// monsters spawn and a player is near enough to spawn mobs, a portal now and
// then, more often the harder the difficulty, lets a zombified piglin
// through onto the block beneath it, which then waits out its portal
// cooldown.
func (p Portal) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	w := tx.World()
	if w.Dimension() != world.Overworld || !w.MonsterSpawningEnabled() || r.IntN(2000) >= difficultyID(w.Difficulty()) || !playerNearForSpawning(tx, pos) {
		return
	}
	for {
		if _, ok := tx.Block(pos).(Portal); !ok {
			break
		}
		pos = pos.Side(cube.FaceDown)
	}
	if !validSpawnBlock(tx, pos, true) {
		return
	}
	spawn := w.EntityRegistry().Config().SpawnMob
	if spawn == nil {
		return
	}
	h := spawn("minecraft:zombified_piglin", world.EntitySpawnOpts{Position: pos.Side(cube.FaceUp).Vec3Middle(), SpawnReason: "structure"})
	if h == nil {
		return
	}
	if e, ok := tx.AddEntity(h).(interface{ SetPortalCooldown() }); ok {
		e.SetPortalCooldown()
	}
}

// difficultyID is Difficulty.getId.
func difficultyID(d world.Difficulty) int {
	switch d {
	case world.DifficultyEasy:
		return 1
	case world.DifficultyNormal:
		return 2
	case world.DifficultyHard:
		return 3
	}
	return 0
}

// playerNearForSpawning ports ChunkMap.anyPlayerCloseEnoughForSpawning: a
// player who is not spectating is within 128 blocks of the centre of the
// chunk of pos.
func playerNearForSpawning(tx *world.Tx, pos cube.Pos) bool {
	x, z := float64(pos[0]>>4)*16+8, float64(pos[2]>>4)*16+8
	for p := range tx.Players() {
		if m, ok := p.(interface{ GameMode() world.GameMode }); ok && m.GameMode() == world.GameModeSpectator {
			continue
		}
		dx, dz := x-p.Position()[0], z-p.Position()[2]
		if dx*dx+dz*dz < 16384 {
			return true
		}
	}
	return false
}

// validSpawnBlock ports BlockState.isValidSpawn for a mob, fire immune or
// not, standing on the block at pos: a block sturdy on top that is not too
// bright, with the exceptions Blocks registers.
func validSpawnBlock(tx *world.Tx, pos cube.Pos, fireImmune bool) bool {
	switch tx.Block(pos).(type) {
	case Bedrock, Glass, StainedGlass, Barrier, Scaffolding, ChorusFlower, WoodTrapdoor, CopperTrapdoor, IronTrapdoor, Ice, FrostedIce, Leaves, FireflyBush:
		return false
	case SoulSand, LitPumpkin, Mud, RedstoneLamp:
		return true
	case Pumpkin:
		if tx.Block(pos).(Pumpkin).Carved {
			return true
		}
	case Magma:
		return fireImmune
	}
	id, err := tx.BlockStateID(pos)
	if err != nil {
		return false
	}
	shape, err := gamedata.BlockGenerationShapeID(id)
	return err == nil && shape.Sturdy&(1<<uint(cube.FaceUp)) != 0 && shape.LightEmission < 14
}

// NeighbourUpdateTick ...
func (p Portal) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if n, ok := portal.NetherPortalFromPos(tx, pos); ok && (!n.Framed() || !n.Activated()) {
		n.Deactivate()
	}
}

// EntityInside ...
func (p Portal) EntityInside(_ cube.Pos, tx *world.Tx, e world.Entity) {
	if traveller, ok := e.(interface{ CanUsePortal() bool }); ok && !traveller.CanUsePortal() {
		return
	}
	if t, ok := e.(portalTraveller); ok {
		t.TravelThroughPortal(tx, p.Portal())
	}
}
