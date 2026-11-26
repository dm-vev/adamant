package entity

import (
	"math"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/sound"
)

// MinecartTNTBehaviourConfig configures a TNT minecart behaviour.
type minecartTNTBehaviourConfig struct {
	Minecart minecartBehaviourConfig
	Fuse     int
}

// Apply applies the configuration to the entity data.
func (conf minecartTNTBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a new TNT minecart behaviour.
func (conf minecartTNTBehaviourConfig) New() *minecartTNTBehaviour {
	base := conf.Minecart.New()
	return &minecartTNTBehaviour{
		minecartBehaviour: base,
		fuse:              conf.Fuse,
	}
}

// MinecartTNTBehaviour implements TNT minecart behaviour.
type minecartTNTBehaviour struct {
	*minecartBehaviour
	fuse int
}

// Base returns the base minecart behaviour.
func (b *minecartTNTBehaviour) Base() *minecartBehaviour {
	return b.minecartBehaviour
}

// Tick ticks the TNT minecart behaviour.
func (b *minecartTNTBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	m := b.minecartBehaviour.Tick(e, tx)
	if b.fuse == 0 {
		b.explode(e, tx, 0)
		_ = e.CloseIn(tx)
	} else if b.fuse > 0 {
		if b.fuse%5 == 0 {
			b.broadcastState(e, tx)
		}
		b.fuse--
	}
	return m
}

// Activate primes the TNT minecart when powered.
func (b *minecartTNTBehaviour) Activate(e *Ent, tx *world.Tx, powered bool) {
	if !powered || b.fuse >= 0 {
		return
	}
	b.prime(e, tx, 80)
}

// FuseTicks returns the fuse ticks for metadata.
func (b *minecartTNTBehaviour) FuseTicks() int {
	return b.fuse
}

// Ignited reports if the TNT minecart is ignited.
func (b *minecartTNTBehaviour) Ignited() bool {
	return b.fuse >= 0
}

func (b *minecartTNTBehaviour) prime(e *Ent, tx *world.Tx, fuse int) {
	b.fuse = fuse
	tx.PlaySound(e.Position(), sound.TNT{})
	b.broadcastState(e, tx)
}

func (b *minecartTNTBehaviour) explode(e *Ent, tx *world.Tx, speedSq float64) {
	root := math.Sqrt(speedSq)
	if root > 5 {
		root = 5
	}
	size := 4 + tx.RegionRandom().NextDouble()*1.5*root
	block.ExplosionConfig{}.Explode(tx, world.NewEntityExplosionSource(e, size))
}

// MinecartTNT is a minecart with TNT.
type minecartTNT struct {
	*minecart
}

func (m *minecartTNT) tnt() *minecartTNTBehaviour {
	if b, ok := m.Behaviour().(*minecartTNTBehaviour); ok {
		return b
	}
	return nil
}

// Interact handles TNT minecart interaction.
func (m *minecartTNT) Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	b := m.tnt()
	if b == nil {
		return false
	}
	main, _ := user.HeldItems()
	switch main.Item().(type) {
	case item.FlintAndSteel:
		b.prime(m.Ent, tx, 80)
		return true
	case item.FireCharge:
		b.prime(m.Ent, tx, 80)
		return true
	}
	return m.minecart.Interact(tx, user, ctx)
}

// Destroy destroys the TNT minecart and drops its item if appropriate.
func (m *minecartTNT) Destroy(tx *world.Tx, src world.DamageSource, causer world.Entity) bool {
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
func (m *minecartTNT) Links() []Link {
	return m.minecart.Links()
}

// TNTMinecartType is a world.EntityType implementation for TNT minecarts.
var entityTNTMinecartType tntMinecartType

type tntMinecartType struct{}

func (tntMinecartType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &minecartTNT{minecart: &minecart{Ent: &Ent{tx: tx, handle: handle, data: data}}}
}

func (tntMinecartType) EncodeEntity() string { return "minecraft:tnt_minecart" }

func (tntMinecartType) BBox(e world.Entity) cube.BBox { return minecartBBox(e) }
func (tntMinecartType) NetworkOffset() float64        { return minecartNetworkOffset }

func (tntMinecartType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := tntMinecartConf
	base := conf.New()
	readMinecartDisplayNBT(base.minecartBehaviour, m)
	base.fuse = -1
	if v, ok := m["fuse"]; ok {
		base.fuse = int(readNBTInt(v))
	}
	data.Data = base
}

func (tntMinecartType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*minecartTNTBehaviour)
	m := map[string]any{"fuse": int32(b.fuse)}
	writeMinecartDisplayNBT(b.minecartBehaviour, m)
	return m
}

var tntMinecartConf = minecartTNTBehaviourConfig{
	Minecart: minecartBehaviourConfig{
		DisplayBlock:  block.TNT{},
		DisplayOffset: minecartDisplayOffset,
		Rideable:      false,
	},
	Fuse: -1,
}
