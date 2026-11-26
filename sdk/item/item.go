// Package item provides declarative custom Bedrock item definitions and global
// item registry helpers.
package item

import (
	"image"
	"image/color"
	"slices"
	"time"

	"github.com/dm-vev/adamant/server/block/cube"
	coreitem "github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/category"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Option configures a custom item created by Define.
type Option func(*config)

type config struct {
	meta                int16
	displayName         string
	texture             image.Image
	category            category.Category
	maxCount            int
	attackDamage        float64
	cooldown            time.Duration
	durability          coreitem.DurabilityInfo
	handEquipped        bool
	glinted             bool
	offHand             bool
	fireProof           bool
	drinkable           bool
	throwable           bool
	alwaysConsumable    bool
	consumeDuration     time.Duration
	onConsume           func(*world.Tx, coreitem.Consumer) coreitem.Stack
	onUse               func(*world.Tx, coreitem.User, *coreitem.UseContext) bool
	onUseOnBlock        func(cube.Pos, cube.Face, mgl64.Vec3, *world.Tx, coreitem.User, *coreitem.UseContext) bool
	onUseOnEntity       func(world.Entity, *world.Tx, coreitem.User, *coreitem.UseContext) bool
	onRelease           func(coreitem.Releaser, *world.Tx, *coreitem.UseContext, time.Duration)
	releaseRequirements []coreitem.Stack
}

// Meta sets the metadata value returned by EncodeItem.
func Meta(m int16) Option { return func(c *config) { c.meta = m } }

// DisplayName sets the client-visible item name.
func DisplayName(s string) Option { return func(c *config) { c.displayName = s } }

// Texture sets the icon image used in generated Bedrock resource packs.
func Texture(img image.Image) Option { return func(c *config) { c.texture = img } }

// Category sets the creative inventory category tab.
func Category(cat category.Category) Option { return func(c *config) { c.category = cat } }

// MaxCount sets the maximum stack size (defaults to 64).
func MaxCount(n int) Option { return func(c *config) { c.maxCount = n } }

// AttackDamage sets melee attack damage.
func AttackDamage(d float64) Option { return func(c *config) { c.attackDamage = d } }

// Cooldown sets the use cooldown duration.
func Cooldown(d time.Duration) Option { return func(c *config) { c.cooldown = d } }

// Durability enables item durability.
func Durability(info coreitem.DurabilityInfo) Option { return func(c *config) { c.durability = info } }

// HandEquipped renders the item like a sword/tool in third person.
func HandEquipped() Option { return func(c *config) { c.handEquipped = true } }

// Glinted adds a permanent enchantment glint.
func Glinted() Option { return func(c *config) { c.glinted = true } }

// OffHand allows holding the item in the off-hand slot.
func OffHand() Option { return func(c *config) { c.offHand = true } }

// FireProof prevents dropped items from burning in fire or lava.
func FireProof() Option { return func(c *config) { c.fireProof = true } }

// Drinkable uses the drinking animation when consumed.
func Drinkable() Option { return func(c *config) { c.drinkable = true } }

// Throwable enables arm-swing animation when thrown.
func Throwable() Option { return func(c *config) { c.throwable = true } }

// AlwaysConsumable allows consuming even with full hunger.
func AlwaysConsumable() Option { return func(c *config) { c.alwaysConsumable = true } }

// ConsumeDuration sets the duration required to consume the item.
func ConsumeDuration(d time.Duration) Option { return func(c *config) { c.consumeDuration = d } }

// OnConsume marks the item Consumable and runs fn when consumed.
func OnConsume(fn func(tx *world.Tx, consumer coreitem.Consumer) coreitem.Stack) Option {
	return func(c *config) { c.onConsume = fn }
}

// OnUse marks the item Usable and runs fn when used in the air.
func OnUse(fn func(tx *world.Tx, user coreitem.User, ctx *coreitem.UseContext) bool) Option {
	return func(c *config) { c.onUse = fn }
}

// OnUseOnBlock marks the item UsableOnBlock and runs fn when used on a block.
func OnUseOnBlock(fn func(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user coreitem.User, ctx *coreitem.UseContext) bool) Option {
	return func(c *config) { c.onUseOnBlock = fn }
}

// OnUseOnEntity marks the item UsableOnEntity and runs fn when used on an entity.
func OnUseOnEntity(fn func(e world.Entity, tx *world.Tx, user coreitem.User, ctx *coreitem.UseContext) bool) Option {
	return func(c *config) { c.onUseOnEntity = fn }
}

// OnRelease marks the item Releasable and runs fn when released after charging.
func OnRelease(fn func(r coreitem.Releaser, tx *world.Tx, ctx *coreitem.UseContext, d time.Duration), reqs ...coreitem.Stack) Option {
	return func(c *config) {
		c.onRelease = fn
		c.releaseRequirements = slices.Clone(reqs)
	}
}

type def struct {
	id string
	config
}

func defaultTexture() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	col := color.RGBA{R: 0x38, G: 0xb2, B: 0xac, A: 0xff}
	for y := range 16 {
		for x := range 16 {
			img.SetRGBA(x, y, col)
		}
	}
	return img
}

type base struct{ def *def }

func (b base) EncodeItem() (string, int16)             { return b.def.id, b.def.meta }
func (b base) Name() string                            { return b.def.displayName }
func (b base) Texture() image.Image                    { return b.def.texture }
func (b base) Category() category.Category             { return b.def.category }
func (b base) MaxCount() int                           { return b.def.maxCount }
func (b base) HandEquipped() bool                      { return b.def.handEquipped }
func (b base) Glinted() bool                           { return b.def.glinted }
func (b base) OffHand() bool                           { return b.def.offHand }
func (b base) FireProof() bool                         { return b.def.fireProof }
func (b base) Drinkable() bool                         { return b.def.drinkable }
func (b base) SwingAnimation() bool                    { return b.def.throwable }
func (b base) AttackDamage() float64                   { return b.def.attackDamage }
func (b base) Cooldown() time.Duration                 { return b.def.cooldown }
func (b base) DurabilityInfo() coreitem.DurabilityInfo { return b.def.durability }

func (b base) use(tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return b.def.onUse != nil && b.def.onUse(tx, u, ctx)
}
func (b base) useBlock(pos cube.Pos, f cube.Face, cp mgl64.Vec3, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return b.def.onUseOnBlock != nil && b.def.onUseOnBlock(pos, f, cp, tx, u, ctx)
}
func (b base) useEntity(e world.Entity, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return b.def.onUseOnEntity != nil && b.def.onUseOnEntity(e, tx, u, ctx)
}
func (b base) consume(tx *world.Tx, cr coreitem.Consumer) coreitem.Stack {
	if b.def.onConsume != nil {
		return b.def.onConsume(tx, cr)
	}
	return coreitem.Stack{}
}
func (b base) release(r coreitem.Releaser, tx *world.Tx, ctx *coreitem.UseContext, d time.Duration) {
	if b.def.onRelease != nil {
		b.def.onRelease(r, tx, ctx, d)
	}
}

type (
	iUsable             struct{ base }
	iBlock              struct{ base }
	iEntity             struct{ base }
	iConsumable         struct{ base }
	iReleasable         struct{ base }
	iUsableBlock        struct{ base }
	iUsableEntity       struct{ base }
	iBlockEntity        struct{ base }
	iInteractive        struct{ base }
	iInteractiveConsume struct{ base }
)

func (i iUsable) Use(tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.use(tx, u, ctx)
}
func (i iBlock) UseOnBlock(pos cube.Pos, f cube.Face, cp mgl64.Vec3, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.useBlock(pos, f, cp, tx, u, ctx)
}
func (i iEntity) UseOnEntity(e world.Entity, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.useEntity(e, tx, u, ctx)
}
func (i iConsumable) AlwaysConsumable() bool         { return i.def.alwaysConsumable }
func (i iConsumable) ConsumeDuration() time.Duration { return i.def.consumeDuration }
func (i iConsumable) Consume(tx *world.Tx, cr coreitem.Consumer) coreitem.Stack {
	return i.consume(tx, cr)
}
func (i iReleasable) Release(r coreitem.Releaser, tx *world.Tx, ctx *coreitem.UseContext, d time.Duration) {
	i.release(r, tx, ctx, d)
}
func (i iReleasable) Requirements() []coreitem.Stack { return i.def.releaseRequirements }
func (i iUsableBlock) Use(tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.use(tx, u, ctx)
}
func (i iUsableBlock) UseOnBlock(pos cube.Pos, f cube.Face, cp mgl64.Vec3, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.useBlock(pos, f, cp, tx, u, ctx)
}
func (i iUsableEntity) Use(tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.use(tx, u, ctx)
}
func (i iUsableEntity) UseOnEntity(e world.Entity, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.useEntity(e, tx, u, ctx)
}
func (i iBlockEntity) UseOnBlock(pos cube.Pos, f cube.Face, cp mgl64.Vec3, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.useBlock(pos, f, cp, tx, u, ctx)
}
func (i iBlockEntity) UseOnEntity(e world.Entity, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.useEntity(e, tx, u, ctx)
}
func (i iInteractive) Use(tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.use(tx, u, ctx)
}
func (i iInteractive) UseOnBlock(pos cube.Pos, f cube.Face, cp mgl64.Vec3, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.useBlock(pos, f, cp, tx, u, ctx)
}
func (i iInteractive) UseOnEntity(e world.Entity, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.useEntity(e, tx, u, ctx)
}
func (i iInteractiveConsume) Use(tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.use(tx, u, ctx)
}
func (i iInteractiveConsume) UseOnBlock(pos cube.Pos, f cube.Face, cp mgl64.Vec3, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.useBlock(pos, f, cp, tx, u, ctx)
}
func (i iInteractiveConsume) UseOnEntity(e world.Entity, tx *world.Tx, u coreitem.User, ctx *coreitem.UseContext) bool {
	return i.useEntity(e, tx, u, ctx)
}
func (i iInteractiveConsume) AlwaysConsumable() bool         { return i.def.alwaysConsumable }
func (i iInteractiveConsume) ConsumeDuration() time.Duration { return i.def.consumeDuration }
func (i iInteractiveConsume) Consume(tx *world.Tx, cr coreitem.Consumer) coreitem.Stack {
	return i.consume(tx, cr)
}

// Define constructs a comparable world.CustomItem named id configured by opts.
func Define(id string, opts ...Option) world.CustomItem {
	if id == "" {
		panic("sdk/item: item id must not be empty")
	}
	var c config
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	if c.displayName == "" {
		c.displayName = id
	}
	if c.texture == nil {
		c.texture = defaultTexture()
	}
	if c.category == (category.Category{}) {
		c.category = category.Items()
	}
	if c.maxCount <= 0 {
		c.maxCount = 64
	}
	if c.consumeDuration <= 0 {
		c.consumeDuration = coreitem.DefaultConsumeDuration
	}
	if c.attackDamage <= 0 {
		c.attackDamage = 2
	}
	b := base{def: &def{id: id, config: c}}
	hasUse, hasBlock, hasEntity := c.onUse != nil, c.onUseOnBlock != nil, c.onUseOnEntity != nil
	hasConsume, hasRelease := c.onConsume != nil, c.onRelease != nil

	if hasRelease && !hasUse && !hasBlock && !hasEntity && !hasConsume {
		return iReleasable{b}
	}
	if hasConsume {
		if !hasUse && !hasBlock && !hasEntity {
			return iConsumable{b}
		}
		return iInteractiveConsume{b}
	}
	switch {
	case hasUse && hasBlock && hasEntity:
		return iInteractive{b}
	case hasUse && hasBlock:
		return iUsableBlock{b}
	case hasUse && hasEntity:
		return iUsableEntity{b}
	case hasBlock && hasEntity:
		return iBlockEntity{b}
	case hasUse:
		return iUsable{b}
	case hasBlock:
		return iBlock{b}
	case hasEntity:
		return iEntity{b}
	default:
		return b
	}
}

// Register registers a custom or vanilla item in the global item registry.
func Register(it world.Item) { world.RegisterItem(it) }

// ByName looks up a registered item by namespaced identifier and metadata.
func ByName(name string, meta int16) (world.Item, bool) { return world.ItemByName(name, meta) }
