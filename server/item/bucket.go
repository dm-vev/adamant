package item

import (
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// BucketContent is the content of a bucket.
type BucketContent struct {
	liquid world.Liquid
	solid  world.Block
	milk   bool
}

// LiquidBucketContent returns a new BucketContent with the liquid passed in.
func LiquidBucketContent(l world.Liquid) BucketContent {
	return BucketContent{liquid: l}
}

// SolidBucketContent returns a new BucketContent carrying a solid block, such
// as powder snow, which the bucket places like a block item.
func SolidBucketContent(b world.Block) BucketContent {
	return BucketContent{solid: b}
}

// MilkBucketContent returns a new BucketContent with the milk flag set.
func MilkBucketContent() BucketContent {
	return BucketContent{milk: true}
}

// Liquid returns the world.Liquid that a Bucket with this BucketContent places.
// If this BucketContent does not place a liquid block, false is returned.
func (b BucketContent) Liquid() (world.Liquid, bool) {
	return b.liquid, b.liquid != nil
}

// Solid returns the solid block that a Bucket with this BucketContent places.
func (b BucketContent) Solid() (world.Block, bool) {
	return b.solid, b.solid != nil
}

// String converts the BucketContent to a string.
func (b BucketContent) String() string {
	switch {
	case b.milk:
		return "milk"
	case b.liquid != nil:
		return b.liquid.LiquidType()
	case b.solid != nil:
		name, _ := b.solid.EncodeBlock()
		return strings.TrimPrefix(name, "minecraft:")
	}
	return ""
}

// LiquidType returns the type of liquid the bucket contains.
func (b BucketContent) LiquidType() string {
	if b.liquid != nil {
		return b.liquid.LiquidType()
	}
	return "milk"
}

// Bucket is a tool used to carry water, lava and fish.
type Bucket struct {
	// Content is the content that the bucket has. By default, this value resolves to an empty bucket.
	Content BucketContent
}

// MaxCount returns 16.
func (b Bucket) MaxCount() int {
	if b.Empty() {
		return 16
	}
	return 1
}

// AlwaysConsumable ...
func (b Bucket) AlwaysConsumable() bool {
	return b.Content.milk
}

// CanConsume ...
func (b Bucket) CanConsume() bool {
	return b.Content.milk
}

// ConsumeDuration ...
func (b Bucket) ConsumeDuration() time.Duration {
	return DefaultConsumeDuration
}

// Consume ...
func (b Bucket) Consume(_ *world.Tx, c Consumer) Stack {
	for _, effect := range c.Effects() {
		c.RemoveEffect(effect.Type())
	}
	return NewStack(Bucket{}, 1)
}

// Empty returns true if the bucket is empty.
func (b Bucket) Empty() bool {
	return b.Content.liquid == nil && b.Content.solid == nil && !b.Content.milk
}

// FuelInfo ...
func (b Bucket) FuelInfo() FuelInfo {
	if liq := b.Content.liquid; liq != nil && liq.LiquidType() == "lava" {
		return newFuelInfo(time.Second * 1000).WithResidue(NewStack(Bucket{}, 1))
	}
	return FuelInfo{}
}

// UseOnEntity captures a bucketable entity into its filled bucket item.
func (b Bucket) UseOnEntity(target world.Entity, tx *world.Tx, _ User, ctx *UseContext) bool {
	if b.Empty() {
		if milkable, ok := target.(interface {
			Milk(*world.Tx) bool
			MilkSound() world.Sound
		}); ok && milkable.Milk(tx) {
			ctx.NewItem = NewStack(Bucket{Content: MilkBucketContent()}, 1)
			ctx.ReplaceHeldItem = true
			ctx.SubtractFromCount(1)
			tx.PlaySound(target.Position(), milkable.MilkSound())
			return true
		}
	}
	mob, ok := target.(interface {
		CanBePickedUpWithBucket(Bucket) bool
		BucketItem() Stack
		SaveToBucket(Stack) Stack
		BucketPickupSound() world.Sound
	})
	if !ok || !mob.CanBePickedUpWithBucket(b) {
		return false
	}
	filled := mob.SaveToBucket(mob.BucketItem())
	if filled.Empty() {
		return false
	}
	closer, ok := target.(interface{ CloseIn(*world.Tx) error })
	if !ok {
		return false
	}
	if err := closer.CloseIn(tx); err != nil {
		return false
	}
	ctx.NewItem = filled
	ctx.ReplaceHeldItem = true
	ctx.SubtractFromCount(1)
	tx.PlaySound(target.Position(), mob.BucketPickupSound())
	return true
}

// UseOnBlock handles the bucket filling and emptying logic.
func (b Bucket) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	if b.Content.milk {
		return false
	}
	if b.Empty() {
		return b.fillFrom(pos, tx, ctx)
	}
	if solid, ok := b.Content.Solid(); ok {
		return b.placeSolid(pos, face, solid, tx, ctx)
	}
	liq := b.Content.liquid.WithDepth(8, false)
	if !b.placeLiquid(pos, liq, tx) && !b.placeLiquid(pos.Side(face), liq, tx) {
		return false
	}

	tx.PlaySound(pos.Vec3Centre(), sound.BucketEmpty{Liquid: b.Content.liquid})
	ctx.NewItem = NewStack(Bucket{}, 1)
	ctx.NewItemSurvivalOnly = true
	ctx.SubtractFromCount(1)
	return true
}

// placeSolid ports SolidBucketItem: the block is placed like a block item, in
// the clicked block if replaceable and against it otherwise.
func (b Bucket) placeSolid(pos cube.Pos, face cube.Face, solid world.Block, tx *world.Tx, ctx *UseContext) bool {
	if !replaceableWith(tx.Block(pos), solid) {
		pos = pos.Side(face)
	}
	if pos.OutOfBounds(tx.Range()) || !replaceableWith(tx.Block(pos), solid) {
		return false
	}
	tx.SetBlock(pos, solid, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.NativeEvent{SoundType: "bucket.empty." + b.Content.String(), ExtraData: -1})
	ctx.NewItem = NewStack(Bucket{}, 1)
	ctx.NewItemSurvivalOnly = true
	ctx.SubtractFromCount(1)
	return true
}

func (b Bucket) placeLiquid(pos cube.Pos, liq world.Liquid, tx *world.Tx) bool {
	block := tx.Block(pos)
	if receiver, ok := block.(interface {
		ReceiveLiquid(cube.Pos, *world.Tx, world.Liquid) bool
	}); ok && receiver.ReceiveLiquid(pos, tx, liq) {
		return true
	}
	if canDisplace(block, liq) || replaceableWith(block, liq) {
		tx.SetLiquid(pos, liq)
		return true
	}
	return false
}

// fillFrom fills a bucket from the liquid at the position passed in the world. If there is no liquid or if
// the liquid is no source, fillFrom returns false.
func (b Bucket) fillFrom(pos cube.Pos, tx *world.Tx, ctx *UseContext) bool {
	if pickup, ok := tx.Block(pos).(interface{ BucketPickup() bool }); ok && pickup.BucketPickup() {
		content := SolidBucketContent(tx.Block(pos))
		tx.SetBlock(pos, nil, nil)
		tx.PlaySound(pos.Vec3Centre(), sound.NativeEvent{SoundType: "bucket.fill." + content.String(), ExtraData: -1})
		ctx.NewItem = NewStack(Bucket{Content: content}, 1)
		ctx.NewItemSurvivalOnly = true
		ctx.SubtractFromCount(1)
		return true
	}
	liquid, ok := tx.Liquid(pos)
	if !ok {
		return false
	}
	if liquid.LiquidDepth() != 8 || liquid.LiquidFalling() {
		// Only allow picking up liquid source blocks.
		return false
	}
	tx.SetLiquid(pos, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.BucketFill{Liquid: liquid})

	ctx.NewItem = NewStack(Bucket{Content: LiquidBucketContent(liquid)}, 1)
	ctx.NewItemSurvivalOnly = true
	ctx.SubtractFromCount(1)
	return true
}

// EncodeItem ...
func (b Bucket) EncodeItem() (name string, meta int16) {
	if !b.Empty() {
		return "minecraft:" + b.Content.String() + "_bucket", 0
	}
	return "minecraft:bucket", 0
}

type replaceable interface {
	ReplaceableBy(b world.Block) bool
}

func replaceableWith(b world.Block, with world.Block) bool {
	if r, ok := b.(replaceable); ok {
		return r.ReplaceableBy(with)
	}
	return false
}

func canDisplace(b world.Block, liq world.Liquid) bool {
	if d, ok := b.(world.LiquidDisplacer); ok {
		return d.CanDisplace(liq)
	}
	return false
}
