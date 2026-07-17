package entity

import (
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
)

// NewSmallFireball creates a small fireball projectile owned by owner.
func NewSmallFireball(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	conf := smallFireballConf
	if owner != nil {
		conf.Owner = owner.H()
	}
	return opts.New(SmallFireballType, conf)
}

var smallFireballConf = ProjectileBehaviourConfig{
	Gravity: 0,
	Drag:    0.01,
	Damage:  -1,
	Hit:     smallFireballHit,
}

func smallFireballHit(_ *Ent, tx *world.Tx, target trace.Result) {
	switch target := target.(type) {
	case trace.EntityResult:
		living, ok := target.Entity().(Living)
		if !ok {
			return
		}
		living.Hurt(5, block.FireDamageSource{})
		if flammable, ok := living.(Flammable); ok {
			flammable.SetOnFire(5 * time.Second)
		}
	case trace.BlockResult:
		block.Fire{}.Start(tx, target.BlockPosition().Side(target.Face()))
	}
}

// SmallFireballType is a world.EntityType implementation for small fireballs.
var SmallFireballType smallFireballType

type smallFireballType struct{}

func (smallFireballType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (smallFireballType) EncodeEntity() string { return "minecraft:small_fireball" }

func (smallFireballType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.15625, 0, -0.15625, 0.15625, 0.3125, 0.15625)
}

func (smallFireballType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = smallFireballConf.New()
}

func (smallFireballType) EncodeNBT(*world.EntityData) map[string]any { return nil }
