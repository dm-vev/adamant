package item

import (
	"maps"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// MobBucket stores a bucketable entity and the entity data captured with it.
type MobBucket struct {
	Entity string
	Data   map[string]any
}

// MaxCount returns the one-item stack limit for filled mob buckets.
func (MobBucket) MaxCount() int { return 1 }

// EncodeItem returns the entity-specific filled bucket identifier.
func (b MobBucket) EncodeItem() (string, int16) { return b.Entity + "_bucket", 0 }

// EncodeNBT stores the bucket entity data.
func (b MobBucket) EncodeNBT() map[string]any {
	if len(b.Data) == 0 {
		return nil
	}
	return map[string]any{"BucketEntityData": maps.Clone(b.Data)}
}

// DecodeNBT restores the entity data captured by the bucket.
func (b MobBucket) DecodeNBT(data map[string]any) any {
	if bucketData, ok := data["BucketEntityData"].(map[string]any); ok {
		b.Data = maps.Clone(bucketData)
	} else {
		b.Data = nil
	}
	return b
}

// EntityID returns the entity type spawned when the bucket is emptied.
func (b MobBucket) EntityID() string { return b.Entity }

// BucketEntityData returns the mob data encoded in the bucket.
func (b MobBucket) BucketEntityData() map[string]any { return maps.Clone(b.Data) }

// UseOnBlock empties the bucket, places its fluid when applicable, and spawns its entity.
func (b MobBucket) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	factory := tx.World().EntityRegistry().Config().NewFromBucket
	if factory == nil {
		return false
	}
	target := pos.Side(face)
	var liquid world.Liquid
	if mobBucketHasWater(b.Entity) {
		waterBlock, ok := tx.World().BlockRegistry().BlockByName("minecraft:water", nil)
		if !ok {
			return false
		}
		water, ok := waterBlock.(world.Liquid)
		if !ok {
			return false
		}
		liquid = water.WithDepth(8, false)
		if bl := tx.Block(target); !canDisplace(bl, liquid) && !replaceableWith(bl, liquid) {
			target = target.Side(face)
			bl = tx.Block(target)
			if !canDisplace(bl, liquid) && !replaceableWith(bl, liquid) {
				return false
			}
		}
	}
	spawnPos := target.Vec3Middle()
	opts := world.EntitySpawnOpts{Position: spawnPos}
	handle := factory(opts, b.EntityID(), b.Data)
	if handle == nil {
		return false
	}
	if liquid != nil {
		tx.SetLiquid(target, liquid)
	}
	tx.AddEntity(handle)
	tx.PlaySound(spawnPos, sound.Custom{Name: mobBucketEmptySound(b.Entity), Volume: 1, Pitch: 1})
	ctx.NewItem = NewStack(Bucket{}, 1)
	ctx.NewItemSurvivalOnly = true
	ctx.SubtractFromCount(1)
	return true
}

func mobBucketHasWater(entity string) bool {
	switch entity {
	case "minecraft:axolotl", "minecraft:cod", "minecraft:pufferfish", "minecraft:salmon", "minecraft:tadpole", "minecraft:tropical_fish":
		return true
	default:
		return false
	}
}

func mobBucketEmptySound(entity string) string {
	switch entity {
	case "minecraft:axolotl":
		return "item.bucket.empty_axolotl"
	case "minecraft:sulfur_cube":
		return "item.bucket.empty_sulfur_cube"
	case "minecraft:tadpole":
		return "item.bucket.empty_tadpole"
	default:
		return "item.bucket.empty_fish"
	}
}

// FishBucket is a mob bucket that can also be eaten as food.
type FishBucket struct{ MobBucket }

// ConsumeDuration returns the normal item eating duration.
func (FishBucket) ConsumeDuration() time.Duration { return DefaultConsumeDuration }

// AlwaysConsumable reports whether the bucket can be eaten while hunger is full.
func (FishBucket) AlwaysConsumable() bool { return false }

// Consume applies the contained fish's food effects and returns an empty bucket.
func (b FishBucket) Consume(tx *world.Tx, c Consumer) Stack {
	switch b.Entity {
	case "minecraft:cod":
		(Cod{}).Consume(tx, c)
	case "minecraft:pufferfish":
		(Pufferfish{}).Consume(tx, c)
	case "minecraft:salmon":
		(Salmon{}).Consume(tx, c)
	case "minecraft:tropical_fish":
		(TropicalFish{}).Consume(tx, c)
	}
	return NewStack(Bucket{}, 1)
}

// EncodeNBT stores the bucket entity data.
func (b FishBucket) EncodeNBT() map[string]any { return b.MobBucket.EncodeNBT() }

// DecodeNBT restores the entity data captured by the bucket.
func (b FishBucket) DecodeNBT(data map[string]any) any {
	b.MobBucket = b.MobBucket.DecodeNBT(data).(MobBucket)
	return b
}
