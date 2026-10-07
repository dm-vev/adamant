package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
)

// NewEgg creates a temperate egg projectile owned by owner.
func NewEgg(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	return NewEggVariant(opts, owner, 0)
}

// NewEggVariant creates a worldless egg projectile preserving Java's chicken variant.
// Variant values are 0=temperate, 1=cold, and 2=warm.
func NewEggVariant(opts world.EntitySpawnOpts, owner world.Entity, variant int) *world.EntityHandle {
	var ownerHandle *world.EntityHandle
	if owner != nil { ownerHandle = owner.H() }
	return opts.New(EggType, eggBehaviourConfig{owner:ownerHandle,variant:max(0,min(variant,2))})
}

type eggBehaviourConfig struct { owner *world.EntityHandle; variant int }
func (c eggBehaviourConfig) Apply(data *world.EntityData) {
	projectile:=ProjectileBehaviourConfig{Owner:c.owner,Gravity:0.03,Drag:0.01,Particle:particle.EggSmash{},ParticleCount:6,Hit:func(e *Ent,tx *world.Tx,_ trace.Result){
		if rand.IntN(8)==0 { count:=1;if rand.IntN(32)==0{count=4};for range count{tx.AddEntity(NewChicken(world.EntitySpawnOpts{Position:e.Position(),Rotation:e.Rotation()},ChickenBehaviourConfig{AgeTicks:-24000,Variant:c.variant}))} }
	}}.New()
	data.Data=&eggBehaviour{projectile:projectile,variant:c.variant}
}
type eggBehaviour struct { projectile *ProjectileBehaviour; variant int }
func (b *eggBehaviour) Tick(e *Ent,tx *world.Tx)*Movement{return b.projectile.Tick(e,tx)}

// EggType is the world.EntityType for thrown eggs.
var EggType eggType
type eggType struct{}
func (t eggType) Open(tx *world.Tx,handle *world.EntityHandle,data *world.EntityData)world.Entity{
	if data.Data==nil{eggBehaviourConfig{}.Apply(data)}
	return &Ent{tx:tx,handle:handle,data:data}
}
func (eggType) EncodeEntity()string{return "minecraft:egg"}
func (eggType) BBox(world.Entity)cube.BBox{return cube.Box(-0.125,0,-0.125,0.125,0.25,0.125)}
func (eggType) DecodeNBT(m map[string]any,data *world.EntityData){variant:=0;if _,ok:=m["Variant"];ok{variant=int(nbtconv.Int32(m,"Variant"))};eggBehaviourConfig{variant:max(0,min(variant,2))}.Apply(data)}
func (eggType) EncodeNBT(data *world.EntityData)map[string]any{return map[string]any{"Variant":int32(data.Data.(*eggBehaviour).variant)}}
