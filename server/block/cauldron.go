package block

import (
	"image/color"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/entity/effect"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// CauldronLiquid is the substance a cauldron holds.
type CauldronLiquid uint8

const (
	// CauldronWater is water, which may be dyed or hold a potion.
	CauldronWater CauldronLiquid = iota
	// CauldronLava is lava.
	CauldronLava
	// CauldronPowderSnow is powder snow.
	CauldronPowderSnow
)

// Uint8 returns the liquid as a uint8.
func (l CauldronLiquid) Uint8() uint8 { return uint8(l) }

// String returns the Bedrock cauldron_liquid state value.
func (l CauldronLiquid) String() string {
	switch l {
	case CauldronLava:
		return "lava"
	case CauldronPowderSnow:
		return "powder_snow"
	default:
		return "water"
	}
}

// CauldronPotionKind is the bottle a potion in a cauldron came from.
type CauldronPotionKind uint8

const (
	// CauldronNoPotion means the cauldron holds no potion.
	CauldronNoPotion CauldronPotionKind = iota
	CauldronPotion
	CauldronSplashPotion
	CauldronLingeringPotion
)

// cauldronMaxFill is the Bedrock fill level of a full cauldron.
const cauldronMaxFill = 6

// Cauldron holds water, lava or powder snow. In Bedrock Edition it also holds
// dyed water, which dyes leather armour, and potions, which tip arrows.
type Cauldron struct {
	transparent

	// FillLevel is the Bedrock fill level, from 0 (empty) to 6 (full). A
	// bottle holds two levels.
	FillLevel int
	// Liquid is the substance held while FillLevel is above zero.
	Liquid CauldronLiquid
	// Potion is the potion held in water when PotionKind is set.
	//blockhash:ignore
	Potion potion.Potion
	// PotionKind is the kind of bottle the held potion came from.
	//blockhash:ignore
	PotionKind CauldronPotionKind
	// DyeColour is the colour of dyed water. The zero colour is plain water.
	//blockhash:ignore
	DyeColour color.RGBA
}

// Model ...
func (Cauldron) Model() world.BlockModel { return model.Cauldron{} }

// BreakInfo ...
func (c Cauldron) BreakInfo() BreakInfo {
	return newBreakInfo(2, pickaxeHarvestable, pickaxeEffective, oneOf(Cauldron{})).withBlastResistance(2)
}

// LightEmissionLevel ...
func (c Cauldron) LightEmissionLevel() uint8 {
	if c.lava() {
		return 15
	}
	return 0
}

// ComparatorOutput ports getAnalogOutputSignal: a third of the cauldron per
// signal level, three for lava.
func (c Cauldron) ComparatorOutput(*world.Tx, cube.Pos) uint8 {
	if c.lava() {
		return 3
	}
	return uint8((c.FillLevel + 1) / 2)
}

func (c Cauldron) lava() bool { return c.FillLevel > 0 && c.Liquid == CauldronLava }

// plainWater reports whether the cauldron holds undyed water without potion.
func (c Cauldron) plainWater() bool {
	return c.FillLevel > 0 && c.Liquid == CauldronWater && c.PotionKind == CauldronNoPotion && c.DyeColour == (color.RGBA{})
}

// water reports whether the cauldron holds water, dyed or not, but no potion.
func (c Cauldron) water() bool {
	return c.FillLevel > 0 && c.Liquid == CauldronWater && c.PotionKind == CauldronNoPotion
}

func (c Cauldron) potionHeld() bool {
	return c.FillLevel > 0 && c.Liquid == CauldronWater && c.PotionKind != CauldronNoPotion
}

// withFill returns the cauldron with fill level n; an empty cauldron loses its
// contents.
func (c Cauldron) withFill(n int) Cauldron {
	if n <= 0 {
		return Cauldron{}
	}
	c.FillLevel = min(n, cauldronMaxFill)
	return c
}

// Activate handles buckets, bottles, potions, dyes, arrows and washing items
// used on the cauldron.
func (c Cauldron) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	if held.Empty() {
		return false
	}
	switch it := held.Item().(type) {
	case item.Bucket:
		return c.useBucket(pos, tx, it, ctx)
	case item.GlassBottle:
		return c.takeBottle(pos, tx, ctx)
	case item.Potion:
		return c.pourPotion(pos, tx, it.Type, CauldronPotion, ctx)
	case item.SplashPotion:
		return c.pourPotion(pos, tx, it.Type, CauldronSplashPotion, ctx)
	case item.LingeringPotion:
		return c.pourPotion(pos, tx, it.Type, CauldronLingeringPotion, ctx)
	case item.Dye:
		return c.addDye(pos, tx, it.Colour.RGBA(), ctx)
	case item.Arrow:
		return c.tipArrows(pos, tx, held, ctx)
	case Banner:
		return c.cleanBanner(pos, tx, held, it, ctx)
	case ShulkerBox:
		return c.cleanShulkerBox(pos, tx, held, it, ctx)
	}
	if colour, ok := dyeableColour(held); ok {
		return c.dyeItem(pos, tx, held, colour, ctx)
	}
	return false
}

// useBucket fills the cauldron from a bucket or a full cauldron into an empty
// bucket.
func (c Cauldron) useBucket(pos cube.Pos, tx *world.Tx, b item.Bucket, ctx *item.UseContext) bool {
	if b.Empty() {
		if c.FillLevel < cauldronMaxFill && !c.lava() || c.potionHeld() {
			return false
		}
		var content item.BucketContent
		effect := particle.CauldronTakeWater
		switch c.Liquid {
		case CauldronLava:
			content, effect = item.LiquidBucketContent(Lava{Still: true, Depth: 8}), particle.CauldronTakeLava
			tx.PlaySound(pos.Vec3Centre(), sound.BucketFill{Liquid: Lava{}})
		case CauldronPowderSnow:
			content, effect = item.SolidBucketContent(PowderSnow{}), particle.CauldronTakePowderSnow
			tx.PlaySound(pos.Vec3Centre(), sound.NativeEvent{SoundType: "bucket.fill.powder_snow", ExtraData: -1})
		default:
			content = item.LiquidBucketContent(Water{Still: true, Depth: 8})
			tx.PlaySound(pos.Vec3Centre(), sound.BucketFill{Liquid: Water{}})
		}
		c.setAndNotify(pos, tx, Cauldron{}, effect, color.RGBA{})
		ctx.NewItem = item.NewStack(item.Bucket{Content: content}, 1)
		ctx.NewItemSurvivalOnly = true
		ctx.SubtractFromCount(1)
		return true
	}
	liquid := CauldronWater
	effect := particle.CauldronFillWater
	if l, ok := b.Content.Liquid(); ok {
		if _, lava := l.(Lava); lava {
			liquid, effect = CauldronLava, particle.CauldronFillLava
		}
	} else if _, snow := b.Content.Solid(); snow {
		liquid, effect = CauldronPowderSnow, particle.CauldronFillPowderSnow
	} else {
		return false
	}
	if c.FillLevel > 0 && c.Liquid != liquid || c.potionHeld() {
		// A different substance boils away with the cauldron's contents.
		c.explode(pos, tx)
	} else {
		c.setAndNotify(pos, tx, Cauldron{FillLevel: cauldronMaxFill, Liquid: liquid}, effect, color.RGBA{})
	}
	switch liquid {
	case CauldronLava:
		tx.PlaySound(pos.Vec3Centre(), sound.BucketEmpty{Liquid: Lava{}})
	case CauldronPowderSnow:
		tx.PlaySound(pos.Vec3Centre(), sound.NativeEvent{SoundType: "bucket.empty.powder_snow", ExtraData: -1})
	default:
		tx.PlaySound(pos.Vec3Centre(), sound.BucketEmpty{Liquid: Water{}})
	}
	ctx.NewItem = item.NewStack(item.Bucket{}, 1)
	ctx.NewItemSurvivalOnly = true
	ctx.SubtractFromCount(1)
	return true
}

// takeBottle fills a glass bottle with two levels of water or potion.
func (c Cauldron) takeBottle(pos cube.Pos, tx *world.Tx, ctx *item.UseContext) bool {
	if c.FillLevel < 2 || c.Liquid != CauldronWater {
		return false
	}
	var bottle world.Item = item.Potion{Type: potion.Water()}
	effect, colour := particle.CauldronTakeWater, c.DyeColour
	switch c.PotionKind {
	case CauldronPotion:
		bottle, effect = item.Potion{Type: c.Potion}, particle.CauldronTakePotion
	case CauldronSplashPotion:
		bottle, effect = item.SplashPotion{Type: c.Potion}, particle.CauldronTakePotion
	case CauldronLingeringPotion:
		bottle, effect = item.LingeringPotion{Type: c.Potion}, particle.CauldronTakePotion
	}
	c.setAndNotify(pos, tx, c.withFill(c.FillLevel-2), effect, colour)
	ctx.NewItem = item.NewStack(bottle, 1)
	ctx.SubtractFromCount(1)
	return true
}

// pourPotion pours a bottle into the cauldron: water joins water and resets
// its dye, a potion joins the same potion, and anything else boils away.
func (c Cauldron) pourPotion(pos cube.Pos, tx *world.Tx, p potion.Potion, kind CauldronPotionKind, ctx *item.UseContext) bool {
	if kind == CauldronPotion && p == potion.Water() {
		kind = CauldronNoPotion
	}
	switch {
	case c.FillLevel == 0:
		c = Cauldron{Liquid: CauldronWater, PotionKind: kind}
		if kind != CauldronNoPotion {
			c.Potion = p
		}
	case c.Liquid != CauldronWater || c.PotionKind != kind || kind != CauldronNoPotion && c.Potion != p:
		c.explode(pos, tx)
		ctx.NewItem = item.NewStack(item.GlassBottle{}, 1)
		ctx.SubtractFromCount(1)
		return true
	case c.FillLevel >= cauldronMaxFill:
		return false
	}
	if kind == CauldronNoPotion {
		c.DyeColour = color.RGBA{}
		c.setAndNotify(pos, tx, c.withFill(c.FillLevel+2), particle.CauldronFillWater, color.RGBA{})
	} else {
		c.setAndNotify(pos, tx, c.withFill(c.FillLevel+2), particle.CauldronFillPotion, potionColour(p))
	}
	ctx.NewItem = item.NewStack(item.GlassBottle{}, 1)
	ctx.SubtractFromCount(1)
	return true
}

// addDye dyes the water, mixing with an existing dye as
// c = floor((current + dye) / 2) per channel.
func (c Cauldron) addDye(pos cube.Pos, tx *world.Tx, dye color.RGBA, ctx *item.UseContext) bool {
	if !c.water() {
		return false
	}
	if c.DyeColour == (color.RGBA{}) {
		c.DyeColour = dye
	} else {
		c.DyeColour = color.RGBA{
			R: uint8((int(c.DyeColour.R) + int(dye.R)) / 2),
			G: uint8((int(c.DyeColour.G) + int(dye.G)) / 2),
			B: uint8((int(c.DyeColour.B) + int(dye.B)) / 2),
			A: uint8((int(c.DyeColour.A) + int(dye.A)) / 2),
		}
	}
	c.setAndNotify(pos, tx, c, particle.CauldronAddDye, c.DyeColour)
	ctx.SubtractFromCount(1)
	return true
}

// dyeItem dyes leather armour, leather horse armour or wolf armour in dyed
// water for two levels, or washes its dye off in plain water for one.
func (c Cauldron) dyeItem(pos cube.Pos, tx *world.Tx, held item.Stack, current color.RGBA, ctx *item.UseContext) bool {
	if !c.water() {
		return false
	}
	if c.DyeColour == (color.RGBA{}) {
		if current == (color.RGBA{}) {
			return false
		}
		c.setAndNotify(pos, tx, c.withFill(c.FillLevel-1), particle.CauldronCleanArmour, color.RGBA{})
		ctx.NewItem, ctx.ReplaceHeldItem = withDyeColour(held, color.RGBA{}), true
		return true
	}
	if c.FillLevel < 2 {
		return false
	}
	c.setAndNotify(pos, tx, c.withFill(c.FillLevel-2), particle.CauldronDyeArmour, c.DyeColour)
	ctx.NewItem, ctx.ReplaceHeldItem = withDyeColour(held, c.DyeColour), true
	return true
}

// cleanBanner washes the top pattern off a banner in plain water.
func (c Cauldron) cleanBanner(pos cube.Pos, tx *world.Tx, held item.Stack, b Banner, ctx *item.UseContext) bool {
	if !c.plainWater() || len(b.Patterns) == 0 || b.Illager {
		return false
	}
	b.Patterns = b.Patterns[:len(b.Patterns)-1]
	c.setAndNotify(pos, tx, c.withFill(c.FillLevel-1), particle.CauldronCleanBanner, color.RGBA{})
	ctx.NewItem, ctx.ReplaceHeldItem = held.WithItem(b), true
	return true
}

// cleanShulkerBox washes the dye off a shulker box in plain water.
func (c Cauldron) cleanShulkerBox(pos cube.Pos, tx *world.Tx, held item.Stack, s ShulkerBox, ctx *item.UseContext) bool {
	if !c.plainWater() || s.Type == NormalShulkerBox() {
		return false
	}
	s.Type = NormalShulkerBox()
	c.setAndNotify(pos, tx, c.withFill(c.FillLevel-1), particle.CauldronCleanArmour, color.RGBA{})
	ctx.NewItem, ctx.ReplaceHeldItem = held.WithItem(s), true
	return true
}

// tipArrows turns arrows into tipped arrows of the held potion. Below four
// levels any stack empties the cauldron and at most 16 arrows are tipped;
// otherwise 1-16 arrows cost one level, 17-32 four and more six.
func (c Cauldron) tipArrows(pos cube.Pos, tx *world.Tx, held item.Stack, ctx *item.UseContext) bool {
	if !c.potionHeld() {
		return false
	}
	if a := held.Item().(item.Arrow); a.Tip != (potion.Potion{}) {
		return false
	}
	n, cost := held.Count(), 0
	switch {
	case c.FillLevel < 4:
		n, cost = min(n, 16), c.FillLevel
	case n <= 16:
		cost = 1
	case n <= 32:
		cost = 4
	default:
		cost = 6
	}
	n = min(n, held.Count())
	colour := potionColour(c.Potion)
	c.setAndNotify(pos, tx, c.withFill(c.FillLevel-cost), particle.CauldronTakePotion, colour)
	ctx.SubtractFromCount(n)
	ctx.NewItem = item.NewStack(item.Arrow{Tip: c.Potion}, n)
	return true
}

// explode empties the cauldron with the fizz of mixed substances.
func (c Cauldron) explode(pos cube.Pos, tx *world.Tx) {
	c.setAndNotify(pos, tx, Cauldron{}, particle.CauldronExplode, color.RGBA{})
	tx.PlaySound(pos.Vec3Centre(), sound.Fizz{})
}

// setAndNotify stores the new cauldron state with its effect and updates
// comparators and listeners of the block change.
func (c Cauldron) setAndNotify(pos cube.Pos, tx *world.Tx, n Cauldron, effect particle.CauldronEffectKind, colour color.RGBA) {
	tx.SetBlock(pos, n, nil)
	tx.AddParticle(pos.Vec3Centre(), particle.CauldronEffect{Kind: effect, Colour: colour})
	notifyComparatorUpdate(pos, tx)
	tx.EmitGameEvent(world.GameEventBlockChange, pos.Vec3Centre(), world.GameEventContext{AffectedState: n})
}

// EntityInside ports the cauldron entityInside effects for an entity that
// reached the contents: water and powder snow extinguish a burning entity,
// melting one level, and lava sets it alight.
func (c Cauldron) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if c.FillLevel == 0 {
		return
	}
	box := e.H().Type().BBox(e).Translate(e.Position())
	if box.Min().Y() >= float64(pos.Y())+c.contentHeight() || box.Max().Y() <= float64(pos.Y())+0.25 {
		return
	}
	if c.Liquid == CauldronLava {
		Lava{}.EntityInside(pos, tx, e)
		return
	}
	flammable, ok := e.(flammableEntity)
	if !ok || flammable.OnFireDuration() <= 0 {
		return
	}
	flammable.Extinguish()
	_, player := e.(interface{ GameMode() world.GameMode })
	if !player && !tx.World().GameRuleBool("mob_griefing") {
		return
	}
	melted := c.withFill(c.FillLevel - 1)
	if c.Liquid == CauldronPowderSnow && melted.FillLevel > 0 {
		melted.Liquid = CauldronWater
	}
	c.setAndNotify(pos, tx, melted, particle.CauldronTakeWater, color.RGBA{})
}

// contentHeight ports getContentHeight: lava fills to 15 pixels, water and
// powder snow to 6 + 1.5 pixels per Bedrock level.
func (c Cauldron) contentHeight() float64 {
	if c.Liquid == CauldronLava {
		return 0.9375
	}
	return (6 + 1.5*float64(c.FillLevel)) / 16
}

// HandlePrecipitation ports CauldronBlock and LayeredCauldronBlock
// handlePrecipitation: rain fills plain water one level with a 5% chance and
// snow fills powder snow with a 10% chance.
func (c Cauldron) HandlePrecipitation(pos cube.Pos, tx *world.Tx, p world.Precipitation) {
	switch p {
	case world.PrecipitationRain:
		if rand.Float32() >= 0.05 || c.FillLevel >= cauldronMaxFill || c.FillLevel > 0 && !c.plainWater() {
			return
		}
		c.setAndNotify(pos, tx, Cauldron{FillLevel: c.FillLevel + 1, Liquid: CauldronWater}, particle.CauldronFillWater, color.RGBA{})
	case world.PrecipitationSnow:
		if rand.Float32() >= 0.1 || c.FillLevel >= cauldronMaxFill || c.FillLevel > 0 && c.Liquid != CauldronPowderSnow {
			return
		}
		c.setAndNotify(pos, tx, Cauldron{FillLevel: c.FillLevel + 1, Liquid: CauldronPowderSnow}, particle.CauldronFillPowderSnow, color.RGBA{})
	}
}

// UseOnBlock ...
func (c Cauldron) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	place(tx, pos, Cauldron{}, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (Cauldron) EncodeItem() (name string, meta int16) {
	return "minecraft:cauldron", 0
}

// EncodeBlock ...
func (c Cauldron) EncodeBlock() (string, map[string]any) {
	return "minecraft:cauldron", map[string]any{"fill_level": int32(c.FillLevel), "cauldron_liquid": c.Liquid.String()}
}

// EncodeNBT encodes the native Cauldron block actor.
func (c Cauldron) EncodeNBT() map[string]any {
	m := map[string]any{"id": "Cauldron", "Items": []any{}, "PotionId": int16(-1), "PotionType": int16(-1)}
	if c.PotionKind != CauldronNoPotion {
		m["PotionId"], m["PotionType"] = int16(c.Potion.Uint8()), int16(c.PotionKind-1)
	}
	if c.DyeColour != (color.RGBA{}) {
		m["CustomColor"] = nbtconv.Int32FromRGBA(c.DyeColour)
	}
	return m
}

// DecodeNBT ...
func (c Cauldron) DecodeNBT(m map[string]any) any {
	c.Potion, c.PotionKind, c.DyeColour = potion.Potion{}, CauldronNoPotion, color.RGBA{}
	if kind := nbtconv.Int16(m, "PotionType"); kind >= 0 && kind <= 2 {
		if id := nbtconv.Int16(m, "PotionId"); id >= 0 {
			c.Potion, c.PotionKind = potion.From(int32(id)), CauldronPotionKind(kind+1)
		}
	}
	if _, ok := m["CustomColor"]; ok {
		c.DyeColour = nbtconv.RGBAFromInt32(nbtconv.Int32(m, "CustomColor"))
	}
	return c
}

// allCauldrons returns every cauldron state.
func allCauldrons() (b []world.Block) {
	for _, liquid := range []CauldronLiquid{CauldronWater, CauldronLava, CauldronPowderSnow} {
		for level := 0; level <= cauldronMaxFill; level++ {
			b = append(b, Cauldron{FillLevel: level, Liquid: liquid})
		}
	}
	return
}

// potionColour returns the colour of a potion's liquid.
func potionColour(p potion.Potion) color.RGBA {
	if c, ok := effect.ResultingColour(p.Effects()); ok {
		return c
	}
	return color.RGBA{R: 0x38, G: 0x5d, B: 0xc6, A: 0xff}
}

// dyeableColour returns the dye colour of a dyeable item: leather armour,
// leather horse armour or wolf armour.
func dyeableColour(s item.Stack) (color.RGBA, bool) {
	switch it := s.Item().(type) {
	case item.Helmet:
		t, ok := it.Tier.(item.ArmourTierLeather)
		return t.Colour, ok
	case item.Chestplate:
		t, ok := it.Tier.(item.ArmourTierLeather)
		return t.Colour, ok
	case item.Leggings:
		t, ok := it.Tier.(item.ArmourTierLeather)
		return t.Colour, ok
	case item.Boots:
		t, ok := it.Tier.(item.ArmourTierLeather)
		return t.Colour, ok
	case item.LeatherHorseArmour:
		return it.Colour, true
	case item.WolfArmor:
		return it.Colour, true
	}
	return color.RGBA{}, false
}

// withDyeColour returns the dyeable stack recoloured.
func withDyeColour(s item.Stack, c color.RGBA) item.Stack {
	switch it := s.Item().(type) {
	case item.Helmet:
		it.Tier = item.ArmourTierLeather{Colour: c}
		return s.WithItem(it)
	case item.Chestplate:
		it.Tier = item.ArmourTierLeather{Colour: c}
		return s.WithItem(it)
	case item.Leggings:
		it.Tier = item.ArmourTierLeather{Colour: c}
		return s.WithItem(it)
	case item.Boots:
		it.Tier = item.ArmourTierLeather{Colour: c}
		return s.WithItem(it)
	case item.LeatherHorseArmour:
		it.Colour = c
		return s.WithItem(it)
	case item.WolfArmor:
		it.Colour = c
		return s.WithItem(it)
	}
	return s
}
