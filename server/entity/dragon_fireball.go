package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// DragonFireballType is the Java dragon-breath projectile entity type.
var DragonFireballType dragonFireballType

type dragonFireballType struct{}

func (dragonFireballType) EncodeEntity() string { return "minecraft:dragon_fireball" }
func (dragonFireballType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		data.Data = dragonFireballConf.New()
	}
	return &Ent{tx: tx, handle: h, data: data}
}
func (dragonFireballType) BBox(world.Entity) cube.BBox { return cube.Box(-.25, 0, -.25, .25, .5, .25) }
func (dragonFireballType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = dragonFireballConf.New()
}
func (dragonFireballType) EncodeNBT(*world.EntityData) map[string]any { return nil }

var dragonFireballConf = ProjectileBehaviourConfig{Acceleration: .1, Damage: -1, Hit: dragonFireballHit}

// dragonFireballHit ports DragonFireball.onHit: a cloud of dragon's breath
// spreads from where the fireball hit, or from a living entity close by, and
// grows from 3 to 7 blocks over thirty seconds.
func dragonFireballHit(e *Ent, tx *world.Tx, res trace.Result) {
	owner := e.Behaviour().(*ProjectileBehaviour).Owner()
	if r, ok := res.(trace.EntityResult); ok && owner != nil && r.Entity().H() == owner {
		return
	}
	pos := e.Position()
	box := e.H().Type().BBox(e).Translate(pos).GrowVec3(mgl64.Vec3{4, 2, 4})
	for other := range tx.EntitiesWithin(box) {
		if _, living := other.(Living); !living || !box.IntersectsWith(other.H().Type().BBox(other).Translate(other.Position())) {
			continue
		}
		if other.Position().Sub(e.Position()).LenSqr() < 16 {
			pos = other.Position()
			break
		}
	}
	tx.AddEntity(newDragonBreathCloud(pos, owner, 3, 600, float64(float32(7-3)/600), 1))
	tx.PlaySound(e.Position(), sound.Custom{Name: "entity.dragon_fireball.explode", Volume: 1, Pitch: 1})
}

func spawnDragonFireballFrom(owner *Ent, pos, target mgl64.Vec3, tx *world.Tx) {
	delta := target.Sub(pos)
	if delta.Len() == 0 {
		return
	}
	conf := dragonFireballConf
	conf.Owner = owner.handle
	tx.AddEntity(world.EntitySpawnOpts{Position: pos, Velocity: delta.Normalize().Mul(.1)}.New(DragonFireballType, conf))
}

var _ world.EntityType = dragonFireballType{}
