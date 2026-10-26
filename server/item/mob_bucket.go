package item

import (
	"maps"

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

// UseOnBlock uses the bucket on what the user looks at, as Use.
func (b MobBucket) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if user == nil {
		return b.empty(tx, nil, ctx, pos, cube.FaceUp, false)
	}
	return b.Use(tx, user, ctx)
}

// Use ports MobBucketItem: the bucket empties its water, if it holds any,
// like a water bucket on the block the user looks at, and the mob spawns at
// the position BucketItem.use places at, standing on its floor.
func (b MobBucket) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	_, pos, face, ok := povHit(tx, user, povNoFluid)
	if !ok {
		return false
	}
	return b.empty(tx, user, ctx, pos, face, true)
}

// empty empties the bucket at the block looked at or, for a dispenser
// without a hit, at the block it faces, and spawns its mob.
func (b MobBucket) empty(tx *world.Tx, user User, ctx *UseContext, pos cube.Pos, face cube.Face, hit bool) bool {
	factory := tx.World().EntityRegistry().Config().NewFromBucket
	if factory == nil {
		return false
	}
	var liquid world.Liquid
	if mobBucketHasWater(b.Entity) {
		waterBlock, ok := tx.World().BlockRegistry().BlockByName("minecraft:water", map[string]any{"liquid_depth": int32(0)})
		if !ok {
			return false
		}
		if liquid, ok = waterBlock.(world.Liquid); !ok {
			return false
		}
	}
	emptySound := sound.Custom{Name: mobBucketEmptySound(b.Entity), Volume: 1, Pitch: 1}
	at := pos
	if hit {
		var ok bool
		if at, ok = emptyBucketAt(tx, user, liquid, emptySound, pos, face); !ok {
			return false
		}
	} else if !emptyContents(tx, nil, liquid, emptySound, pos, false, pos, face) {
		return false
	}
	spawnPos := mgl64.Vec3{float64(at[0]) + 0.5, float64(at[1]), float64(at[2]) + 0.5}
	opts := world.EntitySpawnOpts{Position: spawnPos}
	if user != nil {
		opts = stackSpawnOpts(user, opts)
	}
	if handle := factory(opts, b.EntityID(), b.Data); handle != nil {
		tx.AddEntity(handle)
		tx.EmitGameEvent(world.GameEventEntityPlace, at.Vec3Centre(), world.GameEventContext{Source: user})
	}
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

// FishBucket is the mob bucket of a fish. Like every mob bucket it cannot be
// eaten: Java's fish buckets have no food or consumable component.
type FishBucket struct{ MobBucket }

// EncodeNBT stores the bucket entity data.
func (b FishBucket) EncodeNBT() map[string]any { return b.MobBucket.EncodeNBT() }

// DecodeNBT restores the entity data captured by the bucket.
func (b FishBucket) DecodeNBT(data map[string]any) any {
	b.MobBucket = b.MobBucket.DecodeNBT(data).(MobBucket)
	return b
}
