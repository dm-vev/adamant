package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// MinecartTNTBehaviourConfig configures a TNT minecart behaviour.
type MinecartTNTBehaviourConfig struct {
	Minecart MinecartBehaviourConfig
	Fuse     int
}

// Apply applies the configuration to the entity data.
func (conf MinecartTNTBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a new TNT minecart behaviour.
func (conf MinecartTNTBehaviourConfig) New() *MinecartTNTBehaviour {
	base := conf.Minecart.New()
	return &MinecartTNTBehaviour{
		MinecartBehaviour: base,
		fuse:              conf.Fuse,
	}
}

// MinecartTNTBehaviour implements TNT minecart behaviour.
type MinecartTNTBehaviour struct {
	*MinecartBehaviour
	fuse int
}

// Base returns the base minecart behaviour.
func (b *MinecartTNTBehaviour) Base() *MinecartBehaviour {
	return b.MinecartBehaviour
}

// Tick ticks the TNT minecart behaviour.
func (b *MinecartTNTBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	m := b.MinecartBehaviour.Tick(e, tx)
	if b.fuse > 0 {
		if b.fuse%5 == 0 {
			b.broadcastState(e, tx)
		}
		b.fuse--
		if b.fuse <= 0 {
			b.explode(e, tx, 0)
			_ = e.CloseIn(tx)
			return m
		}
	}
	return m
}

// Activate primes the TNT minecart when powered.
func (b *MinecartTNTBehaviour) Activate(e *Ent, tx *world.Tx, powered bool) {
	if !powered {
		return
	}
	if b.fuse > 0 {
		return
	}
	b.prime(e, tx, 80)
}

// FuseTicks returns the fuse ticks for metadata.
func (b *MinecartTNTBehaviour) FuseTicks() int {
	return b.fuse
}

// Ignited reports if the TNT minecart is ignited.
func (b *MinecartTNTBehaviour) Ignited() bool {
	return b.fuse > 0
}

func (b *MinecartTNTBehaviour) prime(e *Ent, tx *world.Tx, fuse int) {
	b.fuse = fuse
	tx.PlaySound(e.Position(), sound.TNT{})
	b.broadcastState(e, tx)
}

func (b *MinecartTNTBehaviour) explode(e *Ent, tx *world.Tx, speedSq float64) {
	root := math.Sqrt(speedSq)
	if root > 5 {
		root = 5
	}
	size := 4 + rand.Float64()*1.5*root
	block.ExplosionConfig{Size: size, ItemDropChance: 1}.Explode(tx, e.Position())
}

// MinecartTNT is a minecart with TNT.
type MinecartTNT struct {
	*Minecart
}

func (m *MinecartTNT) tnt() *MinecartTNTBehaviour {
	if b, ok := m.Behaviour().(*MinecartTNTBehaviour); ok {
		return b
	}
	return nil
}

// Interact handles TNT minecart interaction.
func (m *MinecartTNT) Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	b := m.tnt()
	if b == nil {
		return false
	}
	main, _ := user.HeldItems()
	switch main.Item().(type) {
	case item.FlintAndSteel:
		ctx.DamageItem(1)
		b.prime(m.Ent, tx, 80)
		return true
	case item.FireCharge:
		ctx.SubtractFromCount(1)
		b.prime(m.Ent, tx, 80)
		return true
	}
	return m.Minecart.Interact(tx, user, ctx)
}

// Destroy destroys the TNT minecart and drops its item if appropriate.
func (m *MinecartTNT) Destroy(tx *world.Tx, src world.DamageSource, causer world.Entity) bool {
	b := m.tnt()
	if b == nil {
		_ = m.CloseIn(tx)
		return true
	}
	destroy := b.Hurt(minecartDamageFromSource(src, causer))
	if destroy {
		b.DismountAll(m.Ent, tx)
		if canDropMinecart(causer, tx.World()) {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, item.NewStack(item.MinecartTNT{}, 1)))
		}
		_ = m.CloseIn(tx)
		return true
	}
	b.broadcastState(m.Ent, tx)
	return true
}

// Links returns the active links for the minecart.
func (m *MinecartTNT) Links() []Link {
	return m.Minecart.Links()
}

// TNTMinecartType is a world.EntityType implementation for TNT minecarts.
var TNTMinecartType tntMinecartType

type tntMinecartType struct{}

func (tntMinecartType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &MinecartTNT{Minecart: &Minecart{Ent: &Ent{tx: tx, handle: handle, data: data}}}
}

func (tntMinecartType) EncodeEntity() string { return "minecraft:tnt_minecart" }

func (tntMinecartType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.49, 0, -0.49, 0.49, 0.7, 0.49)
}

func (tntMinecartType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := tntMinecartConf
	base := conf.New()
	readMinecartDisplayNBT(base.MinecartBehaviour, m)
	base.fuse = -1
	if v, ok := m["fuse"]; ok {
		base.fuse = int(readNBTInt(v))
	}
	data.Data = base
}

func (tntMinecartType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*MinecartTNTBehaviour)
	m := map[string]any{"fuse": int32(b.fuse)}
	writeMinecartDisplayNBT(b.MinecartBehaviour, m)
	return m
}

var tntMinecartConf = MinecartTNTBehaviourConfig{
	Minecart: MinecartBehaviourConfig{
		DisplayBlock:  block.TNT{},
		DisplayOffset: minecartDisplayOffset,
		Rideable:      true,
	},
	Fuse: -1,
}
