package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
)

// NewFallingBlock creates a new FallingBlock entity.
func NewFallingBlock(opts world.EntitySpawnOpts, block world.Block) *world.EntityHandle {
	conf := fallingBlockConf
	conf.Block = block
	return opts.New(FallingBlockType, conf)
}

var fallingBlockConf = FallingBlockBehaviourConfig{
	Gravity: 0.04,
	Drag:    0.02,
}

// FallingBlockType is a world.EntityType implementation for FallingBlock.
var FallingBlockType fallingBlockType

type fallingBlockType struct{}

func (t fallingBlockType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}
func (fallingBlockType) EncodeEntity() string { return "minecraft:falling_block" }

// NetworkOffset returns the height of the origin of a Bedrock actor of the
// type above its feet, which the world format and the physics ported from
// BDS use.
func (fallingBlockType) NetworkOffset() float64 { return 0.5 }
func (fallingBlockType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.49, 0, -0.49, 0.49, 0.98, 0.49)
}

func (fallingBlockType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := fallingBlockConf
	if conf.Block = nbtconv.Block(m, "FallingBlock"); conf.Block == nil {
		// Java FallingBlockEntity reads a missing or unknown block as sand.
		conf.Block = block.Sand{}
	}
	conf.DistanceFallen = nbtconv.Float64(m, "FallDistance")
	b := conf.New()
	if nbtconv.Bool(m, "HurtEntities") {
		b.HurtEntities(float64(nbtconv.Float32(m, "FallHurtAmount")), float64(nbtconv.Int32(m, "FallHurtMax")))
	}
	data.Data = b
}

func (fallingBlockType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*FallingBlockBehaviour)
	m := map[string]any{"FallDistance": b.passive.fallDistance, "FallingBlock": nbtconv.WriteBlock(b.block)}
	if b.hurts {
		m["HurtEntities"], m["FallHurtAmount"], m["FallHurtMax"] = uint8(1), float32(b.hurtPerBlock), int32(b.hurtMax)
	}
	return m
}
