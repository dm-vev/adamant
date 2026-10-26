package item

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// ChorusFruit is an edible item obtained by breaking chorus plants. It teleports the consumer to a random nearby
// location on consumption.
type ChorusFruit struct {
	defaultFood
}

// AlwaysConsumable ports can_always_eat of the food component of chorus fruit.
func (ChorusFruit) AlwaysConsumable() bool {
	return true
}

// Cooldown ports the use_cooldown component of chorus fruit: one second
// passes before another can be eaten.
func (ChorusFruit) Cooldown() time.Duration {
	return time.Second
}

// Consume feeds the consumer and teleports it as the chorus fruit's
// TeleportRandomlyConsumeEffect: up to 16 attempts at a random point within
// 8 blocks on each axis, kept within the logical height.
func (ChorusFruit) Consume(tx *world.Tx, c Consumer) Stack {
	c.Saturate(4, 2.4)
	if t, ok := c.(chorusTeleporter); ok {
		chorusTeleport(tx, t, 16)
	}
	return Stack{}
}

// chorusTeleporter is an entity a chorus fruit can teleport.
type chorusTeleporter interface {
	world.Entity
	Teleport(pos mgl64.Vec3)
	ResetFallDistance()
}

// chorusTeleport ports TeleportRandomlyConsumeEffect.apply.
func chorusTeleport(tx *world.Tx, e chorusTeleporter, diameter float64) bool {
	minY := float64(tx.Range().Min())
	maxY := minY + float64(chorusLogicalHeight(tx)) - 1
	for range 16 {
		origin := e.Position()
		x := origin[0] + (rand.Float64()-0.5)*diameter
		y := min(max(origin[1]+(rand.Float64()-0.5)*diameter, minY), maxY)
		z := origin[2] + (rand.Float64()-0.5)*diameter
		chorusStopRiding(tx, e)
		if chorusRandomTeleport(tx, e, x, y, z) {
			tx.PlaySound(e.Position(), sound.Teleport{})
			e.ResetFallDistance()
			return true
		}
	}
	return false
}

// chorusStopRiding takes the entity off what it rides, as
// Entity.stopRiding.
func chorusStopRiding(tx *world.Tx, e world.Entity) {
	rider, ok := e.(interface{ Riding() *world.EntityHandle })
	if !ok || rider.Riding() == nil {
		return
	}
	vehicle, ok := rider.Riding().Entity(tx)
	if !ok {
		return
	}
	if v, ok := vehicle.(interface{ Dismount(*world.Tx, world.Entity) }); ok {
		v.Dismount(tx, e)
	}
}

// chorusLogicalHeight is the dimension type's logical height: 128 in the
// Nether, the whole build height elsewhere.
func chorusLogicalHeight(tx *world.Tx) int {
	if tx.World().Dimension() == world.Nether {
		return 128
	}
	return tx.Range().Height()
}

// chorusRandomTeleport ports LivingEntity.randomTeleport for the chorus
// fruit, whose consumable_does_not_teleport_to tag is empty: the entity drops
// from the point to the first block entities may teleport onto and moves
// there if its box collides with nothing and touches no liquid.
func chorusRandomTeleport(tx *world.Tx, e chorusTeleporter, x, y, z float64) bool {
	pos := cube.PosFromVec3(mgl64.Vec3{x, y, z})
	if !tx.ChunkLoaded(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}) {
		return false
	}
	for pos[1] > tx.Range().Min() {
		pos = pos.Side(cube.FaceDown)
		if !chorusTeleportOnto(tx.Block(pos)) {
			y--
			continue
		}
		dest := mgl64.Vec3{x, y, z}
		box := e.H().Type().BBox(e).Translate(dest)
		if !chorusBoxFree(tx, box) {
			return false
		}
		e.Teleport(dest)
		return true
	}
	return false
}

// chorusTeleportOnto reports if a block is in entities_can_teleport_to.
func chorusTeleportOnto(b world.Block) bool {
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return false
	}
	in, _ := gamedata.BlockTagContains("minecraft:entities_can_teleport_to", state.Name)
	return in
}

// chorusBoxFree ports Level.noCollision and containsAnyLiquid for a box: no
// block, boat or shulker collides with it and no liquid is inside it.
func chorusBoxFree(tx *world.Tx, box cube.BBox) bool {
	for x := int(math.Floor(box.Min()[0])); x < int(math.Ceil(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])) - 1; y < int(math.Ceil(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z < int(math.Ceil(box.Max()[2])); z++ {
				pos := cube.Pos{x, y, z}
				b, ok := tx.BlockLoaded(pos)
				if !ok {
					return false
				}
				for _, bb := range b.Model().BBox(pos, tx) {
					if bb.Translate(pos.Vec3()).IntersectsWith(box) {
						return false
					}
				}
				if _, liquid := tx.Liquid(pos); liquid && y >= int(math.Floor(box.Min()[1])) {
					return false
				}
			}
		}
	}
	for e := range tx.EntitiesWithin(box) {
		switch e.H().Type().EncodeEntity() {
		case "minecraft:boat", "minecraft:chest_boat", "minecraft:shulker":
			if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
				return false
			}
		}
	}
	return true
}

// EncodeItem ...
func (ChorusFruit) EncodeItem() (name string, meta int16) {
	return "minecraft:chorus_fruit", 0
}
