package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// PaintingMotive is a painting canvas. Name is the Bedrock motive ID sent in
// AddPainting and stored as the actor's Motif.
type PaintingMotive struct {
	Name          string
	Width, Height int
}

// PaintingMotives lists every Bedrock canvas in BDS registration order, with
// the sizes of Java's PaintingVariants.
var PaintingMotives = []PaintingMotive{
	{"Kebab", 1, 1}, {"Aztec", 1, 1}, {"Alban", 1, 1}, {"Aztec2", 1, 1}, {"Bomb", 1, 1}, {"Plant", 1, 1},
	{"Wasteland", 1, 1}, {"Pool", 2, 1}, {"Courbet", 2, 1}, {"Sea", 2, 1}, {"Sunset", 2, 1}, {"Creebet", 2, 1},
	{"Wanderer", 1, 2}, {"Graham", 1, 2}, {"Match", 2, 2}, {"Bust", 2, 2}, {"Stage", 2, 2}, {"Void", 2, 2},
	{"SkullAndRoses", 2, 2}, {"Wither", 2, 2}, {"Fighters", 4, 2}, {"Pointer", 4, 4}, {"Pigscene", 4, 4},
	{"BurningSkull", 4, 4}, {"Skeleton", 4, 3}, {"DonkeyKong", 4, 3}, {"Earth", 2, 2}, {"Wind", 2, 2},
	{"Fire", 2, 2}, {"Water", 2, 2}, {"baroque", 2, 2}, {"humble", 2, 2}, {"meditative", 1, 1},
	{"prairie_ride", 1, 2}, {"unpacked", 4, 4}, {"backyard", 3, 4}, {"bouquet", 3, 3}, {"cavebird", 3, 3},
	{"changing", 4, 2}, {"cotan", 3, 3}, {"endboss", 3, 3}, {"fern", 3, 3}, {"finding", 4, 2}, {"lowmist", 4, 2},
	{"orb", 4, 4}, {"owlemons", 3, 3}, {"passage", 4, 2}, {"pond", 3, 4}, {"sunflowers", 3, 3}, {"tides", 3, 3},
	{"dennis", 3, 3},
}

// PaintingMotiveByName looks up a canvas by its Bedrock motive ID.
func PaintingMotiveByName(name string) (PaintingMotive, bool) {
	for _, m := range PaintingMotives {
		if m.Name == name {
			return m, true
		}
	}
	return PaintingMotive{}, false
}

// placeable reports membership of the painting_variant/placeable tag: the
// four elemental canvases are never chosen by a placed painting item.
func (m PaintingMotive) placeable() bool {
	switch m.Name {
	case "Earth", "Wind", "Fire", "Water":
		return false
	}
	return true
}

// PaintingType is the world.EntityType of a painting.
var PaintingType paintingType

type paintingType struct{}

type paintingConfig struct {
	motive PaintingMotive
	facing cube.Direction
	pos    cube.Pos
}

// Apply ...
func (c paintingConfig) Apply(d *world.EntityData) {
	d.Data = &paintingBehaviour{motive: c.motive, facing: c.facing, pos: c.pos}
}

// paintingBehaviour is Java's Painting state: pos is the block the painting
// occupies in front of the wall and facing points away from the wall.
type paintingBehaviour struct {
	motive          PaintingMotive
	facing          cube.Direction
	pos             cube.Pos
	ticksSinceCheck int
	removed         bool
}

// Painting is a canvas hung on the side of a block.
type Painting struct{ *Ent }

// NewPainting creates a painting of the motive passed, occupying pos and
// facing away from the wall behind it.
func NewPainting(opts world.EntitySpawnOpts, pos cube.Pos, facing cube.Direction, motive PaintingMotive) *world.EntityHandle {
	box := paintingBox(pos, facing, motive)
	opts.Position = box.Min().Add(box.Max()).Mul(0.5)
	opts.Velocity = mgl64.Vec3{}
	return opts.New(PaintingType, paintingConfig{motive: motive, facing: facing, pos: pos})
}

// CreatePainting ports Painting.create: of the placeable canvases that
// survive at pos, one of the largest is chosen at random. Nil is returned if
// no canvas fits.
func CreatePainting(tx *world.Tx, pos cube.Pos, facing cube.Direction) *world.EntityHandle {
	var candidates []PaintingMotive
	largest := 0
	for _, m := range PaintingMotives {
		if !m.placeable() || !paintingSurvives(tx, pos, facing, m, nil) {
			continue
		}
		switch area := m.Width * m.Height; {
		case area > largest:
			largest, candidates = area, append(candidates[:0], m)
		case area == largest:
			candidates = append(candidates, m)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	return NewPainting(world.EntitySpawnOpts{}, pos, facing, candidates[rand.IntN(len(candidates))])
}

// paintingBox is Painting.calculateBoundingBox in absolute coordinates.
func paintingBox(pos cube.Pos, facing cube.Direction, m PaintingMotive) cube.BBox {
	step := cube.Pos{}.Side(facing.Face()).Vec3()
	left := cube.Pos{}.Side(facing.RotateLeft().Face()).Vec3()
	centre := pos.Vec3Centre().Sub(step.Mul(0.46875)).
		Add(left.Mul(paintingOffset(m.Width))).
		Add(mgl64.Vec3{0, paintingOffset(m.Height)})
	size := mgl64.Vec3{float64(m.Width), float64(m.Height), 0.0625}
	if facing == cube.West || facing == cube.East {
		size = mgl64.Vec3{0.0625, float64(m.Height), float64(m.Width)}
	}
	return cube.Box(centre[0]-size[0]/2, centre[1]-size[1]/2, centre[2]-size[2]/2, centre[0]+size[0]/2, centre[1]+size[1]/2, centre[2]+size[2]/2)
}

// paintingOffset shifts even-sized canvases half a block left/up.
func paintingOffset(size int) float64 {
	if size%2 == 0 {
		return 0.5
	}
	return 0
}

// paintingSurvives ports HangingEntity.survives: the painting may not collide
// with blocks, every block behind it must be solid (or a diode), and no other
// painting may intersect it.
func paintingSurvives(tx *world.Tx, pos cube.Pos, facing cube.Direction, m PaintingMotive, self *Painting) bool {
	box := paintingBox(pos, facing, m)
	if !tx.World().WithinBorderBounds(box) || blocksCollide(tx, box) {
		return false
	}
	supported := true
	support := box.Translate(cube.Pos{}.Side(facing.Face()).Vec3().Mul(-0.5)).Grow(-1e-7)
	forBlocksIn(support, func(p cube.Pos) bool {
		supported = legacySolid(tx, tx.Block(p), p)
		return supported
	})
	if !supported {
		return false
	}
	// Entity lookups match positions, so widen by the largest canvas half-extent.
	for e := range tx.EntitiesWithin(box.Grow(2)) {
		other, ok := e.(*Painting)
		if ok && (self == nil || other.H() != self.H()) && other.box().IntersectsWith(box) {
			return false
		}
	}
	return true
}

// blocksCollide reports whether any block collision box intersects box.
func blocksCollide(tx *world.Tx, box cube.BBox) bool {
	collides := false
	forBlocksIn(box, func(p cube.Pos) bool {
		for _, b := range tx.Block(p).Model().BBox(p, tx) {
			if b.Translate(p.Vec3()).IntersectsWith(box) {
				collides = true
				return false
			}
		}
		return true
	})
	return collides
}

// forBlocksIn calls f for every block position overlapped by box until f
// returns false.
func forBlocksIn(box cube.BBox, f func(cube.Pos) bool) {
	lo, hi := box.Min(), box.Max()
	for x := int(math.Floor(lo[0])); x <= int(math.Floor(hi[0])); x++ {
		for y := int(math.Floor(lo[1])); y <= int(math.Floor(hi[1])); y++ {
			for z := int(math.Floor(lo[2])); z <= int(math.Floor(hi[2])); z++ {
				if !f(cube.Pos{x, y, z}) {
					return
				}
			}
		}
	}
}

// legacySolid ports BlockStateBase.calculateSolid without Java's per-block
// force overrides: a block is solid when its collision bounds average at
// least 0.7291666 blocks per axis or are at least a block tall. Redstone
// diodes also support hanging entities.
func legacySolid(tx world.BlockSource, b world.Block, pos cube.Pos) bool {
	switch b.(type) {
	case block.RedstoneRepeater, block.RedstoneComparator:
		return true
	}
	boxes := b.Model().BBox(pos, tx)
	if len(boxes) == 0 {
		return false
	}
	lo, hi := boxes[0].Min(), boxes[0].Max()
	for _, bb := range boxes[1:] {
		for i := range 3 {
			lo[i], hi[i] = min(lo[i], bb.Min()[i]), max(hi[i], bb.Max()[i])
		}
	}
	size := hi.Sub(lo)
	return (size[0]+size[1]+size[2])/3 >= 0.7291666666666666 || size[1] >= 1
}

// Open ...
func (paintingType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		paintingConfig{motive: PaintingMotives[0], facing: cube.South}.Apply(d)
	}
	return &Painting{Ent: &Ent{tx: tx, handle: h, data: d}}
}

// EncodeEntity ...
func (paintingType) EncodeEntity() string { return "minecraft:painting" }

// BBox ...
func (paintingType) BBox(e world.Entity) cube.BBox {
	p, ok := e.(*Painting)
	if !ok {
		return cube.Box(-0.5, -0.5, -0.5, 0.5, 0.5, 0.5)
	}
	return p.box().Translate(p.Position().Mul(-1))
}

// bedrockPaintingDirection maps directions to Bedrock's legacy 2D direction
// IDs: 0 south, 1 west, 2 north, 3 east.
var bedrockPaintingDirection = [...]cube.Direction{cube.South, cube.West, cube.North, cube.East}

// BedrockDirection returns the legacy 2D direction ID of a painting facing.
func BedrockDirection(d cube.Direction) byte {
	for i, dir := range bedrockPaintingDirection {
		if dir == d {
			return byte(i)
		}
	}
	return 0
}

// DecodeNBT reads the Bedrock Motif and Dir fields. The occupied block is
// recovered from the centred actor position.
func (paintingType) DecodeNBT(m map[string]any, d *world.EntityData) {
	motive, ok := PaintingMotiveByName(stringNBT(m, "Motif"))
	if !ok {
		motive = PaintingMotives[0]
	}
	dir, ok := m["Dir"].(byte)
	if !ok {
		dir, _ = m["Direction"].(byte)
	}
	facing := bedrockPaintingDirection[dir%4]
	step := cube.Pos{}.Side(facing.Face()).Vec3()
	left := cube.Pos{}.Side(facing.RotateLeft().Face()).Vec3()
	anchor := d.Pos.Add(step.Mul(0.46875)).
		Sub(left.Mul(paintingOffset(motive.Width))).
		Sub(mgl64.Vec3{0, paintingOffset(motive.Height)})
	paintingConfig{motive: motive, facing: facing, pos: cube.PosFromVec3(anchor)}.Apply(d)
}

// EncodeNBT ...
func (paintingType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*paintingBehaviour)
	dir := BedrockDirection(b.facing)
	return map[string]any{"Motif": b.motive.Name, "Dir": dir, "Direction": dir}
}

func stringNBT(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func (p *Painting) behaviour() *paintingBehaviour { return p.data.Data.(*paintingBehaviour) }

// Motive returns the canvas shown by the painting.
func (p *Painting) Motive() PaintingMotive { return p.behaviour().motive }

// Facing returns the direction the painting faces, away from its wall.
func (p *Painting) Facing() cube.Direction { return p.behaviour().facing }

func (p *Painting) box() cube.BBox {
	b := p.behaviour()
	return paintingBox(b.pos, b.facing, b.motive)
}

// Tick ports BlockAttachedEntity.tick: every 100 ticks a painting that no
// longer survives is removed and dropped.
func (b *paintingBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.ticksSinceCheck++; b.ticksSinceCheck >= 100 {
		b.ticksSinceCheck = 0
		if !b.removed && !paintingSurvives(tx, b.pos, b.facing, b.motive, &Painting{Ent: e}) {
			b.breakPainting(e, tx, nil)
		}
	}
	return nil
}

// Attack ports BlockAttachedEntity.hurtServer for melee attacks.
func (b *paintingBehaviour) Attack(e *Ent, attacker world.Entity, tx *world.Tx) {
	b.hurt(e, tx, attacker)
}

// Explode breaks the painting when an explosion affects it.
func (b *paintingBehaviour) Explode(e *Ent, src world.ExplosionSource, impact float64) {
	if impact <= 0 {
		return
	}
	b.hurt(e, e.tx, world.ExplosionCausingEntity(src, e.tx))
}

// hurt breaks the painting unless mob griefing is off and a mob caused it.
func (b *paintingBehaviour) hurt(e *Ent, tx *world.Tx, causer world.Entity) {
	if causer != nil && !tx.World().GameRuleBool("mob_griefing") {
		if _, mob := causer.(Living); mob && !isPlayerEntity(causer) {
			return
		}
	}
	b.breakPainting(e, tx, causer)
}

// breakPainting removes the painting and, with entity_drops on, plays the
// break sound and drops an item unless broken by a creative player.
func (b *paintingBehaviour) breakPainting(e *Ent, tx *world.Tx, causer world.Entity) {
	if b.removed {
		return
	}
	b.removed = true
	pos := e.Position()
	_ = e.CloseIn(tx)
	if !tx.World().GameRuleBool("entity_drops") {
		return
	}
	tx.PlaySound(pos, sound.HangingBreak{})
	if mode, ok := causer.(interface{ GameMode() world.GameMode }); ok && mode.GameMode().CreativeInventory() {
		return
	}
	stack := item.NewStack(item.Painting{}, 1)
	if name := e.data.Name; name != "" {
		stack = stack.WithCustomName(name)
	}
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos.Add(cube.Pos{}.Side(b.facing.Face()).Vec3().Mul(0.15))}, stack))
}

func isPlayerEntity(e world.Entity) bool {
	return e.H().Type().EncodeEntity() == "minecraft:player"
}
