package block

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Conduit is an underwater beacon: framed by prismarine and sea lanterns it
// gives players in water Conduit Power and, with a full frame, attacks hostile
// mobs in water around it.
type Conduit struct {
	transparent
	solid
	sourceWaterDisplacer

	// active reports whether the conduit is surrounded by water and enough
	// frame blocks to power players.
	active bool
	// target is the unique ID of the mob the conduit attacks, set if
	// hasTarget is true.
	target    int64
	hasTarget bool
}

// Active reports whether the conduit powers players around it.
func (c Conduit) Active() bool {
	return c.active
}

// BreakInfo ...
func (c Conduit) BreakInfo() BreakInfo {
	return newBreakInfo(3, pickaxeHarvestable, pickaxeEffective, oneOf(Conduit{})).withBlastResistance(3)
}

// SideClosed reports that water flows freely out of a waterlogged conduit.
func (Conduit) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// EncodeItem ...
func (Conduit) EncodeItem() (name string, meta int16) {
	return "minecraft:conduit", 0
}

// EncodeBlock ...
func (Conduit) EncodeBlock() (string, map[string]any) {
	return "minecraft:conduit", nil
}

// DecodeNBT ...
func (c Conduit) DecodeNBT(data map[string]any) any {
	c.active = nbtconv.Bool(data, "Active")
	c.target, c.hasTarget = 0, false
	if target, ok := data["Target"].(int64); ok && target != -1 {
		c.target, c.hasTarget = target, true
	}
	return c
}

// EncodeNBT ...
func (c Conduit) EncodeNBT() map[string]any {
	target := int64(-1)
	if c.hasTarget {
		target = c.target
	}
	return map[string]any{"id": "Conduit", "Active": boolByte(c.active), "Target": target}
}

// Tick ports ConduitBlockEntity.serverTick on the BDS timeline: every 40
// ticks the conduit checks its water and frame, powers players and hunts.
// The client plays the activation and ambient sounds and animates the
// active state itself, so only target changes are sent.
func (c Conduit) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	if currentTick%40 != 0 {
		return
	}
	frame, active := conduitShape(pos, tx)
	before := c
	c.active = active
	if active {
		conduitPowerPlayers(pos, tx, frame)
		c.hunt(pos, tx, frame >= 42)
	}
	switch {
	case c.target != before.target || c.hasTarget != before.hasTarget:
		tx.SetBlock(pos, c, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
	case c.active != before.active:
		tx.SetBlockEntity(pos, c)
	}
}

// hunt updates and attacks the conduit's target as BDS does: without a full
// frame the target is dropped. A live target within 8 blocks takes 4 magic
// damage. Otherwise a hostile mob in water or rain becomes the new target,
// first attacked on the next cycle, and with none around the stale target is
// kept.
func (c *Conduit) hunt(pos cube.Pos, tx *world.Tx, hunting bool) {
	if !hunting {
		c.target, c.hasTarget = 0, false
		return
	}
	if target, ok := c.liveTarget(pos, tx); ok {
		if l, ok := target.(livingEntity); ok {
			l.Hurt(4, effect.InstantDamageSource{})
		}
		tx.PlaySound(pos.Vec3(), sound.ConduitAttack{})
		return
	}
	var candidates []world.Entity
	box := cube.Box(float64(pos[0]), float64(pos[1]), float64(pos[2]), float64(pos[0]+1), float64(pos[1]+1), float64(pos[2]+1)).Grow(8)
	for e := range tx.EntitiesWithin(box) {
		if _, ok := e.(livingEntity); ok && conduitEnemy(e) && inWaterOrRain(tx, e) {
			candidates = append(candidates, e)
		}
	}
	if len(candidates) != 0 {
		c.target, c.hasTarget = candidates[rand.IntN(len(candidates))].H().UniqueID(), true
	}
}

// liveTarget returns the conduit's target if it is alive and closer than 8
// blocks.
func (c Conduit) liveTarget(pos cube.Pos, tx *world.Tx) (world.Entity, bool) {
	if !c.hasTarget {
		return nil, false
	}
	e, ok := tx.EntityByUniqueID(c.target)
	if !ok {
		return nil, false
	}
	if d, ok := e.(interface{ Dead() bool }); ok && d.Dead() {
		return nil, false
	}
	return e, conduitCloser(pos, cube.PosFromVec3(e.Position()), 8)
}

// conduitShape ports ConduitBlockEntity.updateShape: the 3x3x3 around the
// conduit must be water, and the conduit_effect_block blocks on the three
// rings of radius 2 form its frame. It returns the frame size and whether the
// 16 blocks needed to activate are there.
func conduitShape(pos cube.Pos, tx *world.Tx) (int, bool) {
	for x := -1; x <= 1; x++ {
		for y := -1; y <= 1; y++ {
			for z := -1; z <= 1; z++ {
				if l, ok := tx.Liquid(pos.Add(cube.Pos{x, y, z})); !ok || l.LiquidType() != "water" {
					return 0, false
				}
			}
		}
	}
	frame := 0
	for x := -2; x <= 2; x++ {
		for y := -2; y <= 2; y++ {
			for z := -2; z <= 2; z++ {
				ax, ay, az := abs(x), abs(y), abs(z)
				if ax <= 1 && ay <= 1 && az <= 1 {
					continue
				}
				if (x == 0 && (ay == 2 || az == 2)) || (y == 0 && (ax == 2 || az == 2)) || (z == 0 && (ax == 2 || ay == 2)) {
					if javaBlockTag(tx, pos.Add(cube.Pos{x, y, z}), "conduit_effect_block") {
						frame++
					}
				}
			}
		}
	}
	return frame, frame >= 16
}

// conduitPowerPlayers ports ConduitBlockEntity.applyEffects: players within
// 16 blocks per 7 frame blocks, in water or rain, get 13 seconds of ambient
// Conduit Power.
func conduitPowerPlayers(pos cube.Pos, tx *world.Tx, frame int) {
	r := frame / 7 * 16
	box := cube.Box(float64(pos[0]-r), float64(pos[1]-r), float64(pos[2]-r), float64(pos[0]+1+r), math.MaxFloat64, float64(pos[2]+1+r))
	for e := range tx.EntitiesWithin(box) {
		if e.H().Type().EncodeEntity() != "minecraft:player" || !conduitCloser(pos, cube.PosFromVec3(e.Position()), float64(r)) || !inWaterOrRain(tx, e) {
			continue
		}
		if p, ok := e.(interface{ AddEffect(effect.Effect) }); ok {
			p.AddEffect(effect.NewAmbient(effect.ConduitPower, 1, 13*time.Second))
		}
	}
}

// conduitCloser ports Vec3i.closerThan.
func conduitCloser(a, b cube.Pos, distance float64) bool {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return dx*dx+dy*dy+dz*dz < distance*distance
}

// conduitEnemies are the Bedrock identifiers of Java's Enemy mobs.
var conduitEnemies = map[string]struct{}{
	"minecraft:blaze": {}, "minecraft:bogged": {}, "minecraft:breeze": {}, "minecraft:cave_spider": {},
	"minecraft:creaking": {}, "minecraft:creeper": {}, "minecraft:drowned": {}, "minecraft:elder_guardian": {},
	"minecraft:ender_dragon": {}, "minecraft:enderman": {}, "minecraft:endermite": {}, "minecraft:evocation_illager": {},
	"minecraft:evoker": {}, "minecraft:ghast": {}, "minecraft:giant": {}, "minecraft:guardian": {},
	"minecraft:hoglin": {}, "minecraft:husk": {}, "minecraft:illusioner": {}, "minecraft:magma_cube": {},
	"minecraft:parched": {}, "minecraft:phantom": {}, "minecraft:piglin": {}, "minecraft:piglin_brute": {},
	"minecraft:pillager": {}, "minecraft:ravager": {}, "minecraft:shulker": {}, "minecraft:silverfish": {},
	"minecraft:skeleton": {}, "minecraft:slime": {}, "minecraft:spider": {}, "minecraft:stray": {},
	"minecraft:vex": {}, "minecraft:vindicator": {}, "minecraft:warden": {}, "minecraft:witch": {},
	"minecraft:wither": {}, "minecraft:wither_skeleton": {}, "minecraft:zoglin": {}, "minecraft:zombie": {},
	"minecraft:zombie_pigman": {}, "minecraft:zombie_villager": {}, "minecraft:zombie_villager_v2": {},
	"minecraft:zombified_piglin": {},
}

// conduitEnemy reports whether the conduit attacks the entity.
func conduitEnemy(e world.Entity) bool {
	_, ok := conduitEnemies[e.H().Type().EncodeEntity()]
	return ok
}

// inWaterOrRain ports Entity.isInWaterOrRain: the entity's box, shrunk by
// 0.001, reaches below the surface of water, or rain falls at its feet or at
// the top of its box.
func inWaterOrRain(tx *world.Tx, e world.Entity) bool {
	full := e.H().Type().BBox(e).Translate(e.Position())
	box := full.Grow(-0.001)
	minimum, maximum := box.Min(), box.Max()
	for x := int(math.Floor(minimum[0])); x < int(math.Ceil(maximum[0])); x++ {
		for y := int(math.Floor(minimum[1])); y < int(math.Ceil(maximum[1])); y++ {
			for z := int(math.Floor(minimum[2])); z < int(math.Ceil(maximum[2])); z++ {
				pos := cube.Pos{x, y, z}
				if height, ok := waterHeight(tx, pos); ok && float64(y)+height >= minimum[1] {
					return true
				}
			}
		}
	}
	feet := cube.PosFromVec3(e.Position())
	return tx.RainingAt(feet) || tx.RainingAt(cube.PosFromVec3(mgl64.Vec3{e.Position()[0], full.Max()[1], e.Position()[2]}))
}

// waterHeight ports FlowingFluid.getHeight for water: a full block under more
// water, otherwise the water's amount in ninths.
func waterHeight(tx *world.Tx, pos cube.Pos) (float64, bool) {
	l, ok := tx.Liquid(pos)
	if !ok || l.LiquidType() != "water" {
		return 0, false
	}
	if above, ok := tx.Liquid(pos.Side(cube.FaceUp)); ok && above.LiquidType() == "water" {
		return 1, true
	}
	amount := l.LiquidDepth()
	if l.LiquidFalling() {
		amount = 8
	}
	return float64(amount) / 9, true
}
