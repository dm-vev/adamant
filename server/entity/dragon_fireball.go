package entity

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// DragonFireballType is the Java dragon-breath projectile entity type.
var DragonFireballType dragonFireballType

type dragonFireballType struct{}

func (dragonFireballType) EncodeEntity() string { return "minecraft:dragon_fireball" }
func (dragonFireballType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil { data.Data = dragonFireballConf.New() }
	return &Ent{tx: tx, handle: h, data: data}
}
func (dragonFireballType) BBox(world.Entity) cube.BBox { return cube.Box(-.25, 0, -.25, .25, .5, .25) }
func (dragonFireballType) DecodeNBT(_ map[string]any, data *world.EntityData) { data.Data = dragonFireballConf.New() }
func (dragonFireballType) EncodeNBT(*world.EntityData) map[string]any { return nil }

var dragonFireballConf = ProjectileBehaviourConfig{Acceleration: .1, Damage: -1, Hit: dragonFireballHit}

func dragonFireballHit(e *Ent, tx *world.Tx, _ trace.Result) {
	tx.AddEntity(NewAreaEffectCloudWith(world.EntitySpawnOpts{Position: e.Position()}, potion.Harming(), 10*time.Second, time.Second/2, 0, 4, -.5, -.005))
	tx.PlaySound(e.Position(), sound.Custom{Name: "entity.dragon_fireball.explode", Volume: 1, Pitch: 1})
}

func spawnDragonFireball(owner *Ent, target mgl64.Vec3, tx *world.Tx) {
	spawnDragonFireballFrom(owner, owner.Position().Add(mgl64.Vec3{0, 2, 0}), target, tx)
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
