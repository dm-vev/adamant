package api

import (
	"image"
	"image/color"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// CustomItemSpec declaratively configures a custom Bedrock item constructed by
// NewCustomItem without manually implementing world.CustomItem and optional
// item behaviour interfaces.
type CustomItemSpec struct {
	// ID is the namespaced Bedrock identifier (e.g. "myplugin:ruby_blade").
	ID string
	// Meta is the optional metadata value returned by EncodeItem (default 0).
	Meta int16
	// DisplayName is the display name shown to clients (defaults to ID).
	DisplayName string
	// Texture is the 16x16+ item icon sent in generated resource packs (defaults
	// to a 16x16 placeholder image when nil).
	Texture image.Image
	// Category controls which creative inventory tab lists this item.
	Category category.Category
	// MaxCount sets the maximum stack size (defaults to 64 when <= 0).
	MaxCount int
	// AttackDamage sets melee attack damage when > 0 (implements item.Weapon).
	AttackDamage float64
	// Cooldown sets the item use cooldown when > 0 (implements item.Cooldown).
	Cooldown time.Duration
	// Durability enables item durability when MaxDurability > 0 (implements item.Durable).
	Durability item.DurabilityInfo
	// HandEquipped renders the item like a tool/sword in third person.
	HandEquipped bool
	// Glinted renders a permanent enchantment glint on the item.
	Glinted bool
	// OffHand allows holding the item in the off-hand slot.
	OffHand bool
	// FireProof prevents the dropped item from burning in fire or lava.
	FireProof bool
	// Drinkable shows the drinking animation when consumed.
	Drinkable bool
	// Throwable enables arm-swing animation when thrown.
	Throwable bool

	// AlwaysConsumable allows consuming even when hunger is full.
	AlwaysConsumable bool
	// ConsumeDuration overrides the eat/drink duration (defaults to item.DefaultConsumeDuration).
	ConsumeDuration time.Duration
	// OnConsume runs when the player finishes consuming the item (enables item.Consumable).
	OnConsume func(tx *world.Tx, c item.Consumer) item.Stack

	// OnUse runs when the player uses the item in the air (enables item.Usable).
	OnUse func(tx *world.Tx, user item.User, ctx *item.UseContext) bool
	// OnUseOnBlock runs when the player uses the item on a block (enables item.UsableOnBlock).
	OnUseOnBlock func(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool
	// OnUseOnEntity runs when the player uses the item on an entity (enables item.UsableOnEntity).
	OnUseOnEntity func(e world.Entity, tx *world.Tx, user item.User, ctx *item.UseContext) bool
	// OnRelease runs when the player releases a held/charged item (enables item.Releasable).
	OnRelease func(releaser item.Releaser, tx *world.Tx, ctx *item.UseContext, duration time.Duration)
	// ReleaseRequirements specifies ammo/items required to release OnRelease items.
	ReleaseRequirements []item.Stack
}

type customItemDef struct {
	id                  string
	meta                int16
	displayName         string
	texture             image.Image
	category            category.Category
	maxCount            int
	attackDamage        float64
	cooldown            time.Duration
	durability          item.DurabilityInfo
	handEquipped        bool
	glinted             bool
	offHand             bool
	fireProof           bool
	drinkable           bool
	throwable           bool
	alwaysConsumable    bool
	consumeDuration     time.Duration
	onConsume           func(tx *world.Tx, c item.Consumer) item.Stack
	onUse               func(tx *world.Tx, user item.User, ctx *item.UseContext) bool
	onUseOnBlock        func(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool
	onUseOnEntity       func(e world.Entity, tx *world.Tx, user item.User, ctx *item.UseContext) bool
	onRelease           func(releaser item.Releaser, tx *world.Tx, ctx *item.UseContext, duration time.Duration)
	releaseRequirements []item.Stack
}

func defaultItemTexture() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	c := color.RGBA{R: 0x38, G: 0xb2, B: 0xac, A: 0xff}
	for y := range 16 {
		for x := range 16 {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// CustomItem is a comparable value-type world.CustomItem built by NewCustomItem.
type customItemBase struct {
	def *customItemDef
}

func (c customItemBase) EncodeItem() (string, int16)         { return c.def.id, c.def.meta }
func (c customItemBase) Name() string                        { return c.def.displayName }
func (c customItemBase) Texture() image.Image                { return c.def.texture }
func (c customItemBase) Category() category.Category         { return c.def.category }
func (c customItemBase) MaxCount() int                       { return c.def.maxCount }
func (c customItemBase) HandEquipped() bool                  { return c.def.handEquipped }
func (c customItemBase) Glinted() bool                       { return c.def.glinted }
func (c customItemBase) OffHand() bool                       { return c.def.offHand }
func (c customItemBase) FireProof() bool                     { return c.def.fireProof }
func (c customItemBase) Drinkable() bool                     { return c.def.drinkable }
func (c customItemBase) SwingAnimation() bool                { return c.def.throwable }
func (c customItemBase) AttackDamage() float64               { return c.def.attackDamage }
func (c customItemBase) Cooldown() time.Duration             { return c.def.cooldown }
func (c customItemBase) DurabilityInfo() item.DurabilityInfo { return c.def.durability }

func (c customItemBase) use(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if c.def.onUse != nil {
		return c.def.onUse(tx, user, ctx)
	}
	return false
}
func (c customItemBase) useOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if c.def.onUseOnBlock != nil {
		return c.def.onUseOnBlock(pos, face, clickPos, tx, user, ctx)
	}
	return false
}
func (c customItemBase) useOnEntity(e world.Entity, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if c.def.onUseOnEntity != nil {
		return c.def.onUseOnEntity(e, tx, user, ctx)
	}
	return false
}
func (c customItemBase) alwaysConsumable() bool         { return c.def.alwaysConsumable }
func (c customItemBase) consumeDuration() time.Duration { return c.def.consumeDuration }
func (c customItemBase) consume(tx *world.Tx, consumer item.Consumer) item.Stack {
	if c.def.onConsume != nil {
		return c.def.onConsume(tx, consumer)
	}
	return item.Stack{}
}
func (c customItemBase) release(releaser item.Releaser, tx *world.Tx, ctx *item.UseContext, duration time.Duration) {
	if c.def.onRelease != nil {
		c.def.onRelease(releaser, tx, ctx, duration)
	}
}
func (c customItemBase) requirements() []item.Stack { return c.def.releaseRequirements }

// CustomItem wrappers selectively expose interactive interfaces only when
// configured, so passive items do not advertise consumable/usable behaviour.
type (
	ciUsable             struct{ customItemBase }
	ciBlock              struct{ customItemBase }
	ciEntity             struct{ customItemBase }
	ciConsumable         struct{ customItemBase }
	ciReleasable         struct{ customItemBase }
	ciUsableBlock        struct{ customItemBase }
	ciUsableEntity       struct{ customItemBase }
	ciBlockEntity        struct{ customItemBase }
	ciInteractive        struct{ customItemBase }
	ciInteractiveConsume struct{ customItemBase }
)

func (c ciUsable) Use(tx *world.Tx, u item.User, ctx *item.UseContext) bool { return c.use(tx, u, ctx) }
func (c ciBlock) UseOnBlock(pos cube.Pos, f cube.Face, cp mgl64.Vec3, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.useOnBlock(pos, f, cp, tx, u, ctx)
}
func (c ciEntity) UseOnEntity(e world.Entity, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.useOnEntity(e, tx, u, ctx)
}
func (c ciConsumable) AlwaysConsumable() bool                            { return c.alwaysConsumable() }
func (c ciConsumable) ConsumeDuration() time.Duration                    { return c.consumeDuration() }
func (c ciConsumable) Consume(tx *world.Tx, cr item.Consumer) item.Stack { return c.consume(tx, cr) }
func (c ciReleasable) Release(r item.Releaser, tx *world.Tx, ctx *item.UseContext, d time.Duration) {
	c.release(r, tx, ctx, d)
}
func (c ciReleasable) Requirements() []item.Stack { return c.requirements() }

func (c ciUsableBlock) Use(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.use(tx, u, ctx)
}
func (c ciUsableBlock) UseOnBlock(pos cube.Pos, f cube.Face, cp mgl64.Vec3, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.useOnBlock(pos, f, cp, tx, u, ctx)
}

func (c ciUsableEntity) Use(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.use(tx, u, ctx)
}
func (c ciUsableEntity) UseOnEntity(e world.Entity, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.useOnEntity(e, tx, u, ctx)
}

func (c ciBlockEntity) UseOnBlock(pos cube.Pos, f cube.Face, cp mgl64.Vec3, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.useOnBlock(pos, f, cp, tx, u, ctx)
}
func (c ciBlockEntity) UseOnEntity(e world.Entity, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.useOnEntity(e, tx, u, ctx)
}

func (c ciInteractive) Use(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.use(tx, u, ctx)
}
func (c ciInteractive) UseOnBlock(pos cube.Pos, f cube.Face, cp mgl64.Vec3, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.useOnBlock(pos, f, cp, tx, u, ctx)
}
func (c ciInteractive) UseOnEntity(e world.Entity, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.useOnEntity(e, tx, u, ctx)
}

func (c ciInteractiveConsume) Use(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.use(tx, u, ctx)
}
func (c ciInteractiveConsume) UseOnBlock(pos cube.Pos, f cube.Face, cp mgl64.Vec3, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.useOnBlock(pos, f, cp, tx, u, ctx)
}
func (c ciInteractiveConsume) UseOnEntity(e world.Entity, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.useOnEntity(e, tx, u, ctx)
}
func (c ciInteractiveConsume) AlwaysConsumable() bool         { return c.alwaysConsumable() }
func (c ciInteractiveConsume) ConsumeDuration() time.Duration { return c.consumeDuration() }
func (c ciInteractiveConsume) Consume(tx *world.Tx, cr item.Consumer) item.Stack {
	return c.consume(tx, cr)
}

// NewCustomItem creates a comparable world.CustomItem implementing optional
// item interfaces according to spec.
func NewCustomItem(spec CustomItemSpec) world.CustomItem {
	if spec.ID == "" {
		panic("api: CustomItemSpec.ID must not be empty")
	}
	display := spec.DisplayName
	if display == "" {
		display = spec.ID
	}
	tex := spec.Texture
	if tex == nil {
		tex = defaultItemTexture()
	}
	cat := spec.Category
	if cat == (category.Category{}) {
		cat = category.Items()
	}
	maxCount := spec.MaxCount
	if maxCount <= 0 {
		maxCount = 64
	}
	consumeDur := spec.ConsumeDuration
	if consumeDur <= 0 {
		consumeDur = item.DefaultConsumeDuration
	}
	attack := spec.AttackDamage
	if attack <= 0 {
		attack = 2
	}
	def := &customItemDef{
		id:                  spec.ID,
		meta:                spec.Meta,
		displayName:         display,
		texture:             tex,
		category:            cat,
		maxCount:            maxCount,
		attackDamage:        attack,
		cooldown:            spec.Cooldown,
		durability:          spec.Durability,
		handEquipped:        spec.HandEquipped,
		glinted:             spec.Glinted,
		offHand:             spec.OffHand,
		fireProof:           spec.FireProof,
		drinkable:           spec.Drinkable,
		throwable:           spec.Throwable,
		alwaysConsumable:    spec.AlwaysConsumable,
		consumeDuration:     consumeDur,
		onConsume:           spec.OnConsume,
		onUse:               spec.OnUse,
		onUseOnBlock:        spec.OnUseOnBlock,
		onUseOnEntity:       spec.OnUseOnEntity,
		onRelease:           spec.OnRelease,
		releaseRequirements: spec.ReleaseRequirements,
	}
	base := customItemBase{def: def}
	hasUse := spec.OnUse != nil
	hasBlock := spec.OnUseOnBlock != nil
	hasEntity := spec.OnUseOnEntity != nil
	hasConsume := spec.OnConsume != nil
	hasRelease := spec.OnRelease != nil

	if hasRelease && !hasUse && !hasBlock && !hasEntity && !hasConsume {
		return ciReleasable{base}
	}
	if hasConsume {
		if !hasUse && !hasBlock && !hasEntity {
			return ciConsumable{base}
		}
		return ciInteractiveConsume{base}
	}
	switch {
	case hasUse && hasBlock && hasEntity:
		return ciInteractive{base}
	case hasUse && hasBlock:
		return ciUsableBlock{base}
	case hasUse && hasEntity:
		return ciUsableEntity{base}
	case hasBlock && hasEntity:
		return ciBlockEntity{base}
	case hasUse:
		return ciUsable{base}
	case hasBlock:
		return ciBlock{base}
	case hasEntity:
		return ciEntity{base}
	default:
		return base
	}
}

// RegisterItem registers a custom or vanilla world.Item in the global item registry.
func RegisterItem(it world.Item) {
	world.RegisterItem(it)
}

// ItemByName looks up a registered item by namespaced identifier and metadata.
func ItemByName(name string, meta int16) (world.Item, bool) {
	return world.ItemByName(name, meta)
}
