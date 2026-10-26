package item

import (
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
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

// UseOnBlock uses the bucket as BucketItem.use does, on what the user looks
// at, or places the block of a powder snow bucket as SolidBucketItem.useOn.
func (b Bucket) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if solid, ok := b.Content.Solid(); ok {
		return b.placeSolid(pos, face, solid, tx, ctx)
	}
	if user == nil {
		return b.dispense(pos, tx, ctx)
	}
	return b.Use(tx, user, ctx)
}

// dispense ports the bucket behaviours of a dispenser at the block it faces:
// a filled bucket empties there as DispensibleContainerItem.emptyContents
// without a hit, an empty one picks up what is there.
func (b Bucket) dispense(pos cube.Pos, tx *world.Tx, ctx *UseContext) bool {
	if b.Content.milk {
		return false
	}
	if b.Empty() {
		return b.fillFrom(pos, tx, nil, ctx)
	}
	if !emptyContents(tx, nil, b.Content.liquid, nil, pos, false, pos, cube.FaceUp) {
		return false
	}
	ctx.NewItem = NewStack(Bucket{}, 1)
	ctx.SubtractFromCount(1)
	return true
}

// Use ports BucketItem.use: an empty bucket picks up the liquid source or
// block the user looks at, and a filled one empties its liquid beside the
// block looked at, or into it if it holds water.
func (b Bucket) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	if b.Content.milk || b.Content.solid != nil {
		return false
	}
	fluids := povNoFluid
	if b.Empty() {
		fluids = povSourceFluid
	}
	_, pos, face, ok := povHit(tx, user, fluids)
	if !ok {
		return false
	}
	if b.Empty() {
		return b.fillFrom(pos, tx, user, ctx)
	}
	if _, ok := emptyBucketAt(tx, user, b.Content.liquid, nil, pos, face); !ok {
		return false
	}
	ctx.NewItem = NewStack(Bucket{}, 1)
	ctx.NewItemSurvivalOnly = true
	ctx.SubtractFromCount(1)
	return true
}

// emptyBucketAt runs the emptying part of BucketItem.use for a liquid: it is
// placed into the block looked at if that takes water, and beside it
// otherwise. It returns the position BucketItem.use places at, where mob
// buckets spawn their mob, and whether the bucket emptied.
func emptyBucketAt(tx *world.Tx, user User, liq world.Liquid, emptySound world.Sound, pos cube.Pos, face cube.Face) (cube.Pos, bool) {
	place := pos.Side(face)
	if liq != nil && liq.LiquidType() == "water" && waterContainer(tx.Block(pos)) {
		place = pos
	}
	return place, emptyContents(tx, user, liq, emptySound, place, true, pos, face)
}

// waterContainer reports if a block is a LiquidBlockContainer: one water can
// be put into, such as a block that may be waterlogged.
func waterContainer(b world.Block) bool {
	_, ok := b.(world.LiquidDisplacer)
	return ok
}

// emptyContents ports BucketItem.emptyContents: the liquid goes into the
// position if it is air, a block the liquid replaces, or a block water may
// be put into, unless the user sneaks with a hit to place beside; otherwise
// it is tried beside the block looked at. Water evaporates where the
// dimension evaporates it, and a replaced block breaks with its drops.
func emptyContents(tx *world.Tx, user User, liq world.Liquid, emptySound world.Sound, pos cube.Pos, hit bool, hitPos cube.Pos, face cube.Face) bool {
	if liq == nil {
		playBucketEmpty(tx, user, liq, emptySound, pos)
		return true
	}
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	b := tx.Block(pos)
	mayReplace := replaceableWith(b, liq)
	_, existing := tx.Liquid(pos)
	water := liq.LiquidType() == "water"
	container := water && canDisplace(b, liq) && !existing
	sneaking := false
	if s, ok := user.(interface{ Sneaking() bool }); ok {
		sneaking = s.Sneaking()
	}
	isAir := isAirBlock(b)
	if isAir || (mayReplace || container) && (!sneaking || !hit) {
		source := liq.WithDepth(8, false)
		switch {
		case water && tx.World().Dimension().WaterEvaporates():
			tx.PlaySound(pos.Vec3Centre(), sound.FireExtinguish{})
			for range 8 {
				tx.AddParticle(pos.Vec3(), particle.Evaporate{})
			}
		case container:
			tx.SetLiquid(pos, source)
			playBucketEmpty(tx, user, liq, emptySound, pos)
		default:
			if _, isLiquid := b.(world.Liquid); mayReplace && !isLiquid && !isAir {
				tx.DestroyBlock(pos, true)
			}
			tx.SetLiquid(pos, source)
			playBucketEmpty(tx, user, liq, emptySound, pos)
		}
		return true
	}
	return hit && emptyContents(tx, user, liq, emptySound, hitPos.Side(face), false, hitPos, face)
}

// isAirBlock reports if a block is air.
func isAirBlock(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return name == "minecraft:air"
}

// playBucketEmpty ports BucketItem.playEmptySound with its fluid_place game
// event; a mob bucket plays its own sound instead.
func playBucketEmpty(tx *world.Tx, user User, liq world.Liquid, emptySound world.Sound, pos cube.Pos) {
	if emptySound == nil {
		emptySound = sound.BucketEmpty{Liquid: liq}
	}
	tx.PlaySound(pos.Vec3Centre(), emptySound)
	tx.EmitGameEvent(world.GameEventFluidPlace, pos.Vec3Centre(), world.GameEventContext{Source: user})
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

// fillFrom ports the BucketPickup half of BucketItem.use: a liquid source,
// the water of a waterlogged block or a block like powder snow is taken into
// the bucket, with its pickup sound and a fluid_pickup game event.
func (b Bucket) fillFrom(pos cube.Pos, tx *world.Tx, user User, ctx *UseContext) bool {
	var content BucketContent
	if pickup, ok := tx.Block(pos).(interface{ BucketPickup() bool }); ok && pickup.BucketPickup() {
		content = SolidBucketContent(tx.Block(pos))
		tx.SetBlock(pos, nil, nil)
		tx.PlaySound(pos.Vec3Centre(), sound.NativeEvent{SoundType: "bucket.fill." + content.String(), ExtraData: -1})
	} else {
		liquid, ok := tx.Liquid(pos)
		if !ok || liquid.LiquidDepth() != 8 || liquid.LiquidFalling() {
			return false
		}
		content = LiquidBucketContent(liquid)
		tx.SetLiquid(pos, nil)
		tx.PlaySound(pos.Vec3Centre(), sound.BucketFill{Liquid: liquid})
	}
	tx.EmitGameEvent(world.GameEventFluidPickup, pos.Vec3Centre(), world.GameEventContext{Source: user})
	ctx.NewItem = NewStack(Bucket{Content: content}, 1)
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
