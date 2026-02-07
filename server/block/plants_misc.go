package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SeaGrassType is the type of sea grass.
type SeaGrassType struct {
	seaGrass
}

// SeaGrassDefault returns the default sea grass type.
func SeaGrassDefault() SeaGrassType {
	return SeaGrassType{0}
}

// SeaGrassDoubleBottom returns the bottom part of double sea grass.
func SeaGrassDoubleBottom() SeaGrassType {
	return SeaGrassType{1}
}

// SeaGrassDoubleTop returns the top part of double sea grass.
func SeaGrassDoubleTop() SeaGrassType {
	return SeaGrassType{2}
}

type seaGrass uint8

// Uint8 returns the sea grass type as a uint8.
func (s seaGrass) Uint8() uint8 {
	return uint8(s)
}

// String returns the sea grass type as a string.
func (s seaGrass) String() string {
	switch s {
	case 0:
		return "default"
	case 1:
		return "double_bot"
	case 2:
		return "double_top"
	}
	panic("unknown sea grass type")
}

// SeaGrass is a plant that grows underwater.
type SeaGrass struct {
	empty
	transparent
	sourceWaterDisplacer

	Type SeaGrassType
}

// HasLiquidDrops ...
func (SeaGrass) HasLiquidDrops() bool {
	return true
}

// NeighbourUpdateTick ...
func (s SeaGrass) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	liquid, ok := tx.Liquid(pos)
	if !ok {
		breakBlock(s, pos, tx)
		return
	}
	if _, ok := liquid.(Water); !ok {
		breakBlock(s, pos, tx)
		return
	}
}

// UseOnBlock ...
func (s SeaGrass) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if s.Type != SeaGrassDefault() {
		return false
	}
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}
	liquid, ok := tx.Liquid(pos)
	if !ok {
		return false
	}
	if _, ok := liquid.(Water); !ok {
		return false
	}
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		return false
	}

	place(tx, pos, s, user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (s SeaGrass) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops(item.NewStack(SeaGrass{Type: SeaGrassDefault()}, 1)))
}

// EncodeItem ...
func (SeaGrass) EncodeItem() (name string, meta int16) {
	return "minecraft:seagrass", 0
}

// EncodeBlock ...
func (s SeaGrass) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:seagrass", map[string]any{"sea_grass_type": s.Type.String()}
}

// allSeaGrass returns all sea grass states.
func allSeaGrass() (b []world.Block) {
	for _, t := range []SeaGrassType{SeaGrassDefault(), SeaGrassDoubleBottom(), SeaGrassDoubleTop()} {
		b = append(b, SeaGrass{Type: t})
	}
	return
}

// UnderwaterTorch is a torch that can be placed underwater.
type UnderwaterTorch struct {
	transparent
	empty

	// Facing is the direction from the torch to the block.
	Facing cube.Face
}

// BreakInfo ...
func (t UnderwaterTorch) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

// LightEmissionLevel ...
func (UnderwaterTorch) LightEmissionLevel() uint8 {
	return 14
}

// UseOnBlock ...
func (t UnderwaterTorch) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, t)
	if !used {
		return false
	}
	if face == cube.FaceDown {
		return false
	}
	liquid, ok := tx.Liquid(pos)
	if !ok {
		return false
	}
	if _, ok := liquid.(Water); !ok {
		return false
	}
	if !tx.Block(pos.Side(face.Opposite())).Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		return false
	}
	t.Facing = face.Opposite()

	place(tx, pos, t, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (t UnderwaterTorch) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(t.Facing)).Model().FaceSolid(pos.Side(t.Facing), t.Facing.Opposite(), tx) {
		breakBlock(t, pos, tx)
		return
	}
	liquid, ok := tx.Liquid(pos)
	if !ok {
		breakBlock(t, pos, tx)
		return
	}
	if _, ok := liquid.(Water); !ok {
		breakBlock(t, pos, tx)
	}
}

// HasLiquidDrops ...
func (UnderwaterTorch) HasLiquidDrops() bool {
	return true
}

// EncodeItem ...
func (UnderwaterTorch) EncodeItem() (name string, meta int16) {
	return "minecraft:underwater_torch", 0
}

// EncodeBlock ...
func (t UnderwaterTorch) EncodeBlock() (name string, properties map[string]any) {
	var face string
	if t.Facing == cube.FaceDown {
		face = "top"
	} else if t.Facing == unknownFace {
		face = "unknown"
	} else {
		face = t.Facing.String()
	}
	return "minecraft:underwater_torch", map[string]any{"torch_facing_direction": face}
}

// FireflyBush is a decorative plant.
type FireflyBush struct {
	transparent
	replaceable
	empty
}

// NeighbourUpdateTick ...
func (b FireflyBush) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(b, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(b, pos, tx)
	}
}

// UseOnBlock ...
func (b FireflyBush) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, b)
	if !used || !supportsVegetation(b, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (FireflyBush) HasLiquidDrops() bool {
	return true
}

// FlammabilityInfo ...
func (FireflyBush) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (b FireflyBush) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(b))
}

// CompostChance ...
func (FireflyBush) CompostChance() float64 {
	return 0.65
}

// EncodeItem ...
func (FireflyBush) EncodeItem() (name string, meta int16) {
	return "minecraft:firefly_bush", 0
}

// EncodeBlock ...
func (FireflyBush) EncodeBlock() (string, map[string]any) {
	return "minecraft:firefly_bush", nil
}

// Bush is a decorative plant.
type Bush struct {
	transparent
	replaceable
	empty
}

// NeighbourUpdateTick ...
func (b Bush) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(b, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(b, pos, tx)
	}
}

// UseOnBlock ...
func (b Bush) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, b)
	if !used || !supportsVegetation(b, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (Bush) HasLiquidDrops() bool {
	return true
}

// FlammabilityInfo ...
func (Bush) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (b Bush) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(b))
}

// CompostChance ...
func (Bush) CompostChance() float64 {
	return 0.65
}

// EncodeItem ...
func (Bush) EncodeItem() (name string, meta int16) {
	return "minecraft:bush", 0
}

// EncodeBlock ...
func (Bush) EncodeBlock() (string, map[string]any) {
	return "minecraft:bush", nil
}

// CactusFlower is a decorative flower that can be placed on cactus or solid ground.
type CactusFlower struct {
	transparent
	replaceable
	empty
}

// UseOnBlock ...
func (c CactusFlower) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	down := tx.Block(pos.Side(cube.FaceDown))
	switch down.(type) {
	case Cactus, Farmland:
		place(tx, pos, c, user, ctx)
		return placed(ctx)
	default:
		if down.Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
			place(tx, pos, c, user, ctx)
			return placed(ctx)
		}
	}
	return false
}

// HasLiquidDrops ...
func (CactusFlower) HasLiquidDrops() bool {
	return true
}

// BreakInfo ...
func (c CactusFlower) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(c))
}

// CompostChance ...
func (CactusFlower) CompostChance() float64 {
	return 0.65
}

// EncodeItem ...
func (CactusFlower) EncodeItem() (name string, meta int16) {
	return "minecraft:cactus_flower", 0
}

// EncodeBlock ...
func (CactusFlower) EncodeBlock() (string, map[string]any) {
	return "minecraft:cactus_flower", nil
}

// HangingRoots are decorative roots hanging from blocks.
type HangingRoots struct {
	transparent
	replaceable
	empty
}

// NeighbourUpdateTick ...
func (h HangingRoots) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(cube.FaceUp)).Model().FaceSolid(pos.Side(cube.FaceUp), cube.FaceDown, tx) {
		breakBlock(h, pos, tx)
	}
}

// UseOnBlock ...
func (h HangingRoots) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, h)
	if !used {
		return false
	}
	if !tx.Block(pos.Side(cube.FaceUp)).Model().FaceSolid(pos.Side(cube.FaceUp), cube.FaceDown, tx) {
		return false
	}
	place(tx, pos, h, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (HangingRoots) HasLiquidDrops() bool {
	return true
}

// FlammabilityInfo ...
func (HangingRoots) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(30, 60, true)
}

// BreakInfo ...
func (h HangingRoots) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(h))
}

// CompostChance ...
func (HangingRoots) CompostChance() float64 {
	return 0.65
}

// EncodeItem ...
func (HangingRoots) EncodeItem() (name string, meta int16) {
	return "minecraft:hanging_roots", 0
}

// EncodeBlock ...
func (HangingRoots) EncodeBlock() (string, map[string]any) {
	return "minecraft:hanging_roots", nil
}

// ShortDryGrass is a dry grass variant.
type ShortDryGrass struct {
	replaceable
	transparent
	empty
}

// FlammabilityInfo ...
func (ShortDryGrass) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (g ShortDryGrass) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, func(t item.Tool, enchantments []item.Enchantment) []item.Stack {
		if t.ToolType() == item.TypeShears || hasSilkTouch(enchantments) {
			return []item.Stack{item.NewStack(g, 1)}
		}
		return nil
	})
}

// CompostChance ...
func (ShortDryGrass) CompostChance() float64 {
	return 0.65
}

// NeighbourUpdateTick ...
func (g ShortDryGrass) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(g, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(g, pos, tx)
	}
}

// HasLiquidDrops ...
func (ShortDryGrass) HasLiquidDrops() bool {
	return true
}

// UseOnBlock ...
func (g ShortDryGrass) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, g)
	if !used || !supportsVegetation(g, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, g, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (ShortDryGrass) EncodeItem() (name string, meta int16) {
	return "minecraft:short_dry_grass", 0
}

// EncodeBlock ...
func (ShortDryGrass) EncodeBlock() (string, map[string]any) {
	return "minecraft:short_dry_grass", nil
}

// TallDryGrass is a tall dry grass variant.
type TallDryGrass struct {
	replaceable
	transparent
	empty
}

// FlammabilityInfo ...
func (TallDryGrass) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (g TallDryGrass) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, func(t item.Tool, enchantments []item.Enchantment) []item.Stack {
		if t.ToolType() == item.TypeShears || hasSilkTouch(enchantments) {
			return []item.Stack{item.NewStack(g, 1)}
		}
		return nil
	})
}

// CompostChance ...
func (TallDryGrass) CompostChance() float64 {
	return 0.65
}

// NeighbourUpdateTick ...
func (g TallDryGrass) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(g, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(g, pos, tx)
	}
}

// HasLiquidDrops ...
func (TallDryGrass) HasLiquidDrops() bool {
	return true
}

// UseOnBlock ...
func (g TallDryGrass) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, g)
	if !used || !supportsVegetation(g, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, g, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (TallDryGrass) EncodeItem() (name string, meta int16) {
	return "minecraft:tall_dry_grass", 0
}

// EncodeBlock ...
func (TallDryGrass) EncodeBlock() (string, map[string]any) {
	return "minecraft:tall_dry_grass", nil
}

// LeafLitter is a decorative plant block.
type LeafLitter struct {
	empty
	transparent

	// AdditionalCount is the amount of additional litter.
	AdditionalCount int
	// Facing is the direction the litter is facing.
	Facing cube.Direction
}

// UseOnBlock ...
func (l LeafLitter) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if existing, ok := tx.Block(pos).(LeafLitter); ok {
		if existing.AdditionalCount >= 7 {
			return false
		}
		existing.AdditionalCount++
		place(tx, pos, existing, user, ctx)
		return placed(ctx)
	}

	pos, _, used := firstReplaceable(tx, pos, face, l)
	if !used {
		return false
	}
	if !supportsVegetation(l, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	l.Facing = user.Rotation().Direction().Opposite()
	place(tx, pos, l, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (l LeafLitter) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(l, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(l, pos, tx)
	}
}

// HasLiquidDrops ...
func (LeafLitter) HasLiquidDrops() bool {
	return true
}

// BreakInfo ...
func (l LeafLitter) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops(item.NewStack(l, l.AdditionalCount+1)))
}

// FlammabilityInfo ...
func (LeafLitter) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(30, 100, true)
}

// CompostChance ...
func (LeafLitter) CompostChance() float64 {
	return 0.3
}

// EncodeItem ...
func (LeafLitter) EncodeItem() (name string, meta int16) {
	return "minecraft:leaf_litter", 0
}

// EncodeBlock ...
func (l LeafLitter) EncodeBlock() (string, map[string]any) {
	return "minecraft:leaf_litter", map[string]any{"growth": int32(l.AdditionalCount), "minecraft:cardinal_direction": l.Facing.String()}
}

// Wildflowers are a decorative plant block.
type Wildflowers struct {
	empty
	transparent

	// AdditionalCount is the amount of additional flowers.
	AdditionalCount int
	// Facing is the direction the flowers are facing.
	Facing cube.Direction
}

// UseOnBlock ...
func (w Wildflowers) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if existing, ok := tx.Block(pos).(Wildflowers); ok {
		if existing.AdditionalCount >= 7 {
			return false
		}
		existing.AdditionalCount++
		place(tx, pos, existing, user, ctx)
		return placed(ctx)
	}

	pos, _, used := firstReplaceable(tx, pos, face, w)
	if !used {
		return false
	}
	if !supportsVegetation(w, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	w.Facing = user.Rotation().Direction().Opposite()
	place(tx, pos, w, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (w Wildflowers) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(w, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(w, pos, tx)
	}
}

// HasLiquidDrops ...
func (Wildflowers) HasLiquidDrops() bool {
	return true
}

// BreakInfo ...
func (w Wildflowers) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops(item.NewStack(w, w.AdditionalCount+1)))
}

// FlammabilityInfo ...
func (Wildflowers) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(30, 100, true)
}

// CompostChance ...
func (Wildflowers) CompostChance() float64 {
	return 0.3
}

// EncodeItem ...
func (Wildflowers) EncodeItem() (name string, meta int16) {
	return "minecraft:wildflowers", 0
}

// EncodeBlock ...
func (w Wildflowers) EncodeBlock() (string, map[string]any) {
	return "minecraft:wildflowers", map[string]any{"growth": int32(w.AdditionalCount), "minecraft:cardinal_direction": w.Facing.String()}
}

// PaleHangingMoss is a decorative hanging moss.
type PaleHangingMoss struct {
	transparent
	replaceable
	empty

	Tip bool
}

// NeighbourUpdateTick ...
func (p PaleHangingMoss) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	above := tx.Block(pos.Side(cube.FaceUp))
	if _, ok := above.(PaleHangingMoss); !ok && !above.Model().FaceSolid(pos.Side(cube.FaceUp), cube.FaceDown, tx) {
		breakBlock(p, pos, tx)
		return
	}
	p = p.updateTip(tx, pos)
	tx.SetBlock(pos, p, nil)
}

// UseOnBlock ...
func (p PaleHangingMoss) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, p)
	if !used {
		return false
	}
	above := tx.Block(pos.Side(cube.FaceUp))
	if _, ok := above.(PaleHangingMoss); !ok && !above.Model().FaceSolid(pos.Side(cube.FaceUp), cube.FaceDown, tx) {
		return false
	}
	p = p.updateTip(tx, pos)
	place(tx, pos, p, user, ctx)
	return placed(ctx)
}

func (p PaleHangingMoss) updateTip(tx *world.Tx, pos cube.Pos) PaleHangingMoss {
	if _, ok := tx.Block(pos.Side(cube.FaceDown)).(PaleHangingMoss); ok {
		p.Tip = false
	} else {
		p.Tip = true
	}
	return p
}

// HasLiquidDrops ...
func (PaleHangingMoss) HasLiquidDrops() bool {
	return true
}

// BreakInfo ...
func (p PaleHangingMoss) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(p))
}

// FlammabilityInfo ...
func (PaleHangingMoss) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(30, 60, true)
}

// CompostChance ...
func (PaleHangingMoss) CompostChance() float64 {
	return 0.65
}

// EncodeItem ...
func (PaleHangingMoss) EncodeItem() (name string, meta int16) {
	return "minecraft:pale_hanging_moss", 0
}

// EncodeBlock ...
func (p PaleHangingMoss) EncodeBlock() (string, map[string]any) {
	return "minecraft:pale_hanging_moss", map[string]any{"tip": boolByte(p.Tip)}
}

// allPaleHangingMoss returns all pale hanging moss states.
func allPaleHangingMoss() (b []world.Block) {
	b = append(b, PaleHangingMoss{Tip: false})
	b = append(b, PaleHangingMoss{Tip: true})
	return
}

// allUnderwaterTorches returns all underwater torch states.
func allUnderwaterTorches() (b []world.Block) {
	for _, face := range cube.Faces() {
		if face == cube.FaceUp {
			face = unknownFace
		}
		b = append(b, UnderwaterTorch{Facing: face})
	}
	return
}

// allLeafLitter returns all leaf litter states.
func allLeafLitter() (b []world.Block) {
	for i := 0; i <= 7; i++ {
		for _, d := range cube.Directions() {
			b = append(b, LeafLitter{AdditionalCount: i, Facing: d})
		}
	}
	return
}

// allWildflowers returns all wildflower states.
func allWildflowers() (b []world.Block) {
	for i := 0; i <= 7; i++ {
		for _, d := range cube.Directions() {
			b = append(b, Wildflowers{AdditionalCount: i, Facing: d})
		}
	}
	return
}
