package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math"
	"math/rand/v2"
)

// FallingBlockBehaviourConfig holds optional parameters for
// FallingBlockBehaviour.
type FallingBlockBehaviourConfig struct {
	Block world.Block
	// Gravity is the amount of Y velocity subtracted every tick.
	Gravity float64
	// Drag is used to reduce all axes of the velocity every tick. Velocity is
	// multiplied with (1-Drag) every tick.
	Drag float64
	// DistanceFallen specifies how far the falling block has already fallen.
	// Blocks that damage entities on impact, like anvils, deal increased damage
	// based on the distance fallen.
	DistanceFallen float64
}

func (conf FallingBlockBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a FallingBlockBehaviour using the optional parameters in conf and
// a block type.
func (conf FallingBlockBehaviourConfig) New() *FallingBlockBehaviour {
	behaviour := &FallingBlockBehaviour{block: conf.Block}
	behaviour.passive = PassiveBehaviourConfig{
		Gravity: conf.Gravity,
		Drag:    conf.Drag,
		Tick:    behaviour.tick,
	}.New()
	behaviour.passive.fallDistance = conf.DistanceFallen
	return behaviour
}

// FallingBlockBehaviour implements the behaviour for falling block entities.
type FallingBlockBehaviour struct {
	passive *PassiveBehaviour
	block   world.Block

	hurts                 bool
	hurtPerBlock, hurtMax float64
}

// Block returns the world.Block of the entity.
func (f *FallingBlockBehaviour) Block() world.Block {
	return f.block
}

// Tick implements the movement and solidification behaviour of falling blocks.
func (f *FallingBlockBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	return f.passive.Tick(e, tx)
}

// tick checks if the falling block should solidify.
func (f *FallingBlockBehaviour) tick(e *Ent, tx *world.Tx) {
	pos := e.Position()
	bpos := cube.PosFromVec3(pos)
	if a, ok := f.block.(Solidifiable); (ok && a.Solidifies(bpos, tx)) || f.passive.mc.OnGround() {
		f.solidify(e, pos, tx)
	}
}

// solidify attempts to solidify the falling block at the position passed. It
// also deals damage to any entities standing at that position. If the block at
// the position could not be replaced by the falling block, the block will drop
// as an item.
func (f *FallingBlockBehaviour) solidify(e *Ent, pos mgl64.Vec3, tx *world.Tx) {
	bpos := cube.PosFromVec3(pos)

	f.damageEntities(e, pos, tx)
	if l, ok := f.block.(landable); ok {
		l.Landed(tx, bpos)
	}
	f.passive.close = true

	r, ok := tx.Block(bpos).(replaceable)
	survives := true
	if s, ok := f.block.(fallSurvivor); ok {
		survives = s.SurvivesFall(bpos, tx)
	}
	if ok && r.ReplaceableBy(f.block) && survives {
		tx.SetBlock(bpos, f.block, nil)
		return
	}
	if !tx.World().GameRuleBool("entity_drops") {
		return
	}
	if b, ok := f.block.(brokenAfterFall); ok {
		b.BrokenAfterFall(tx, bpos)
	}
	if i, ok := f.block.(world.Item); ok {
		opts := world.EntitySpawnOpts{Position: bpos.Vec3Middle()}
		tx.AddEntity(NewItem(opts, item.NewStack(i, 1)))
	}
}

// HurtEntities makes the falling block hurt entities it lands on by
// damagePerBlock per block fallen, up to maxDamage, overriding the damage of
// its block. Falling stalactites deal damage scaled by their column length.
func (f *FallingBlockBehaviour) HurtEntities(damagePerBlock, maxDamage float64) {
	f.hurts, f.hurtPerBlock, f.hurtMax = true, damagePerBlock, maxDamage
}

// damageEntities attempts to damage any entities standing below the falling
// block. This functionality is used by falling anvils and stalactites.
func (f *FallingBlockBehaviour) damageEntities(e *Ent, pos mgl64.Vec3, tx *world.Tx) {
	damagePerBlock, maxDamage := f.hurtPerBlock, f.hurtMax
	if d, ok := f.block.(damager); ok && !f.hurts {
		damagePerBlock, maxDamage = d.Damage()
	} else if !f.hurts {
		return
	}
	dist := math.Ceil(f.passive.fallDistance - 1.0)
	if dist <= 0 {
		return
	}
	dmg := math.Min(math.Floor(dist*damagePerBlock), maxDamage)
	src := block.DamageSource{Block: f.block}

	for e := range filterLiving(tx.EntitiesWithin(e.H().Type().BBox(e).Translate(pos).Grow(0.05))) {
		e.(Living).Hurt(dmg, src)
	}
	if b, ok := f.block.(breakable); ok && dmg > 0.0 && rand.Float64() < (dist+1)*0.05 {
		f.block = b.Break()
	}
}

// Solidifiable represents a block that can solidify by specific adjacent blocks. An example is concrete
// powder, which can turn into concrete by touching water.
type Solidifiable interface {
	// Solidifies returns whether the falling block can solidify at the position it is currently in. If so,
	// the block will immediately stop falling.
	Solidifies(pos cube.Pos, tx *world.Tx) bool
}

type replaceable interface {
	ReplaceableBy(b world.Block) bool
}

// damager ...
type damager interface {
	Damage() (damagePerBlock, maxDamage float64)
}

// breakable ...
type breakable interface {
	Break() world.Block
}

// landable ...
type landable interface {
	Landed(tx *world.Tx, pos cube.Pos)
}

// fallSurvivor is a block that can only be placed by a falling block entity
// where it would survive, like a stalactite needing a ceiling.
type fallSurvivor interface {
	SurvivesFall(pos cube.Pos, tx *world.Tx) bool
}

// brokenAfterFall is a block reacting to its falling block entity breaking
// instead of being placed.
type brokenAfterFall interface {
	BrokenAfterFall(tx *world.Tx, pos cube.Pos)
}
