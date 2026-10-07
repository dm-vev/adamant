package entity

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// NewOminousItemSpawner creates the timed item entity emitted by an ominous
// trial spawner. Delay is the number of entity ticks before dispensing.
func NewOminousItemSpawner(opts world.EntitySpawnOpts, stack item.Stack, delay int64) *world.EntityHandle {
	return opts.New(OminousItemSpawnerType, OminousItemSpawnerConfig{Item: stack, Delay: delay})
}

// OminousItemSpawnerType is the entity type used for ominous trial-spawner loot.
var OminousItemSpawnerType ominousItemSpawnerType

type ominousItemSpawnerType struct{}

// OminousItemSpawnerConfig configures the stack and source countdown.
type OminousItemSpawnerConfig struct {
	Item  item.Stack
	Delay int64
}

func (c OminousItemSpawnerConfig) Apply(data *world.EntityData) {
	if c.Delay <= 0 {
		c.Delay = 60
	}
	data.Data = &ominousItemSpawnerBehaviour{item: c.Item, delay: c.Delay}
}

func (ominousItemSpawnerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil { data.Data = &ominousItemSpawnerBehaviour{delay: 60} }
	return &OminousItemSpawner{Ent: &Ent{tx: tx, handle: handle, data: data}}
}
func (ominousItemSpawnerType) EncodeEntity() string { return "minecraft:ominous_item_spawner" }
func (ominousItemSpawnerType) NetworkEncodeEntity() string { return "minecraft:ominous_item_spawner" }
func (ominousItemSpawnerType) BBox(world.Entity) cube.BBox { return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125) }
func (ominousItemSpawnerType) DecodeNBT(nbt map[string]any, data *world.EntityData) {
	data.NativeNBT = CloneNativeMobNBT(nbt)
	data.Data = &ominousItemSpawnerBehaviour{item: item.MapNBT(nbt, "item"), delay: nbtconv.Int64(nbt, "spawn_item_after_ticks")}
}
func (ominousItemSpawnerType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*ominousItemSpawnerBehaviour)
	return MergeNativeMobNBT(data.NativeNBT, map[string]any{"item": item.WriteNBT(b.item, true), "spawn_item_after_ticks": b.delay})
}

// OminousItemSpawner exposes the stack used by native entity metadata.
type OminousItemSpawner struct { *Ent }

// DisplayItem returns the stack orbiting the spawner.
func (e *OminousItemSpawner) DisplayItem() item.Stack {
	return e.Behaviour().(*ominousItemSpawnerBehaviour).item
}

// ominousItemSpawnerBehaviour retains the selected loot stack until its source
// countdown completes, then releases it as the corresponding native entity.
type ominousItemSpawnerBehaviour struct {
	item  item.Stack
	delay int64
}

func (b *ominousItemSpawnerBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.item.Empty() {
		_ = e.CloseIn(tx)
		return nil
	}
	ticks := int64(e.Age() / (time.Second / 20))
	if ticks == b.delay-36 {
		tx.PlaySound(e.Position(), sound.Custom{Name: "block.trial_spawner.about_to_spawn_item", Volume: 1, Pitch: 1})
	}
	if ticks < b.delay {
		return nil
	}
	opts := world.EntitySpawnOpts{Position: e.Position(), Velocity: mgl64.Vec3{0, -0.5, 0}}
	switch stack := b.item.Item().(type) {
	case item.LingeringPotion:
		tx.AddEntity(NewLingeringPotion(opts, stack.Type, e))
	case item.SplashPotion:
		tx.AddEntity(NewSplashPotion(opts, stack.Type, e))
	default:
		tx.AddEntity(NewItem(opts, b.item))
	}
	_ = e.CloseIn(tx)
	return nil
}
