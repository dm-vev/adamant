package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EvokerFangsType is the delayed, damaging spell entity created by Evokers.
var EvokerFangsType evokerFangsType

type evokerFangsType struct{}

func (evokerFangsType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		d.Data = &evokerFangsBehaviour{}
	}
	return &Ent{tx: tx, handle: h, data: d}
}
func (evokerFangsType) EncodeEntity() string        { return "minecraft:evoker_fangs" }
func (evokerFangsType) BBox(world.Entity) cube.BBox { return cube.Box(-.5, 0, -.5, .5, .8, .5) }
func (evokerFangsType) DecodeNBT(n map[string]any, d *world.EntityData) {
	d.Data = &evokerFangsBehaviour{warmup: int(nbtconv.Int32(n, "Warmup"))}
}
func (evokerFangsType) EncodeNBT(d *world.EntityData) map[string]any {
	return map[string]any{"Warmup": int32(d.Data.(*evokerFangsBehaviour).warmup)}
}

type evokerFangsBehaviour struct {
	owner       *world.EntityHandle
	warmup, age int
	hit         bool
}

func (b *evokerFangsBehaviour) Apply(d *world.EntityData) { d.Data = b }
func (b *evokerFangsBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	b.age++
	if b.warmup > 0 {
		b.warmup--
	}
	if b.age >= 14 && b.warmup <= 0 && !b.hit {
		b.hit = true
		var attacker world.Entity
		if b.owner != nil {
			attacker, _ = b.owner.Entity(tx)
		}
		for p := range tx.Players() {
			d := p.Position().Sub(e.Position())
			d[1] = 0
			if d.Len() < .85 {
				if living, ok := p.(Living); ok {
					living.Hurt(6, AttackDamageSource{Attacker: attacker})
				}
			}
		}
	}
	if b.age >= 24 {
		_ = e.CloseIn(tx)
	}
	return nil
}

// NewEvokerFangs creates a spell fang at the supplied position and warmup delay.
func NewEvokerFangs(opts world.EntitySpawnOpts, owner world.Entity, warmup int) *world.EntityHandle {
	var h *world.EntityHandle
	if owner != nil {
		h = owner.H()
	}
	return opts.New(EvokerFangsType, &evokerFangsBehaviour{owner: h, warmup: warmup})
}

func spawnEvokerFangs(e *Ent, tx *world.Tx, target mgl64.Vec3) {
	origin := e.Position()
	delta := target.Sub(origin)
	delta[1] = 0
	if delta.Len() == 0 {
		return
	}
	direction := delta.Normalize()
	for i := 1; i <= 5; i++ {
		pos := origin.Add(direction.Mul(float64(i)))
		opts := world.EntitySpawnOpts{Position: pos}
		tx.AddEntity(NewEvokerFangs(opts, e, (i-1)*2))
	}
}

var _ world.EntityType = evokerFangsType{}
