package block

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Constants of SculkSpreader.createLevelSpreader.
const (
	sculkGrowthSpawnCost     = 10
	sculkNoGrowthRadius      = 4
	sculkChargeDecayRate     = 10
	sculkAdditionalDecayRate = 5
	sculkMaxCharge           = 1000
	sculkMaxCursors          = 32
	sculkMaxCursorDistance   = 1024
	sculkMaxGrowthRadius     = 24
)

// sculkSpreader ports the level SculkSpreader owned by a sculk catalyst: the
// charge cursors it moves through sculk.
type sculkSpreader struct {
	cursors []*sculkChargeCursor
}

// sculkChargeCursor ports SculkSpreader.ChargeCursor. hasFacings is false
// while Java's facings set is null.
type sculkChargeCursor struct {
	pos                     cube.Pos
	charge                  int
	updateDelay, decayDelay int
	facings                 MultiFace
	hasFacings              bool
}

// addCursors ports SculkSpreader.addCursors: charge is split into cursors of
// at most 1000.
func (s *sculkSpreader) addCursors(pos cube.Pos, charge int) {
	for charge > 0 {
		n := min(charge, sculkMaxCharge)
		s.addCursor(&sculkChargeCursor{pos: pos, charge: n, decayDelay: 1})
		charge -= n
	}
}

func (s *sculkSpreader) addCursor(c *sculkChargeCursor) {
	if len(s.cursors) < sculkMaxCursors {
		s.cursors = append(s.cursors, c)
	}
}

// updateCursors ports SculkSpreader.updateCursors for the level spreader.
func (s *sculkSpreader) updateCursors(tx *world.Tx, origin cube.Pos, spreadVeins bool) {
	if len(s.cursors) == 0 {
		return
	}
	processed := make([]*sculkChargeCursor, 0, len(s.cursors))
	mergeable := make(map[cube.Pos]*sculkChargeCursor, len(s.cursors))
	charges := make(map[cube.Pos]int, len(s.cursors))
	for _, c := range s.cursors {
		if chessboardDistance(c.pos, origin) > sculkMaxCursorDistance {
			continue
		}
		c.update(tx, origin, spreadVeins)
		if c.charge <= 0 {
			tx.AddParticle(c.pos.Vec3(), particle.SculkChargePop{})
			continue
		}
		charges[c.pos] += c.charge
		existing, ok := mergeable[c.pos]
		switch {
		case !ok:
			mergeable[c.pos] = c
			processed = append(processed, c)
		case c.charge+existing.charge <= sculkMaxCharge:
			existing.charge += c.charge
			c.charge = 0
			existing.updateDelay = min(existing.updateDelay, c.updateDelay)
		default:
			processed = append(processed, c)
			if c.charge < existing.charge {
				mergeable[c.pos] = c
			}
		}
	}
	for pos, charge := range charges {
		c := mergeable[pos]
		if charge > 0 && c != nil && c.hasFacings {
			n := int(math.Log1p(float64(charge))/float64(float32(2.3))) + 1
			tx.AddParticle(pos.Vec3(), particle.SculkCharge{Charge: n, Faces: uint8(c.facings)})
		}
	}
	s.cursors = processed
}

// update ports ChargeCursor.update for a spreader that is not world
// generation.
func (c *sculkChargeCursor) update(tx *world.Tx, origin cube.Pos, spreadVeins bool) {
	if c.charge <= 0 {
		return
	}
	if c.updateDelay > 0 {
		c.updateDelay--
		return
	}
	state := tx.Block(c.pos)
	behaviour := sculkBehaviourOf(state)
	if spreadVeins && behaviour.attemptSpreadVein(tx, c.pos, state, c.facings, c.hasFacings) {
		if behaviour.canChangeBlockStateOnSpread() {
			state = tx.Block(c.pos)
			behaviour = sculkBehaviourOf(state)
		}
		tx.PlaySound(c.pos.Vec3Centre(), sculkSpreadSound())
	}
	c.charge = behaviour.attemptUseCharge(c, tx, origin, spreadVeins)
	if c.charge <= 0 {
		behaviour.onDischarged(tx, state, c.pos)
		return
	}
	if target, ok := sculkMovementPos(tx, c.pos); ok {
		behaviour.onDischarged(tx, state, c.pos)
		c.pos = target
		state = tx.Block(target)
	}
	if faces, ok := sculkFaces(state); ok {
		c.facings, c.hasFacings = faces, true
	}
	c.decayDelay = behaviour.updateDecayDelay(c.decayDelay)
	c.updateDelay = 1
}

// sculkFaces ports MultifaceBlock.availableFaces for blocks implementing
// SculkBehaviour: sculk has none, a vein its covered faces.
func sculkFaces(b world.Block) (MultiFace, bool) {
	switch b := b.(type) {
	case Sculk:
		return 0, true
	case SculkVein:
		return b.Faces, true
	}
	return 0, false
}

// nonCornerNeighbours are the 18 offsets of ChargeCursor.NON_CORNER_NEIGHBOURS.
var nonCornerNeighbours = func() (offsets []cube.Pos) {
	for z := -1; z <= 1; z++ {
		for y := -1; y <= 1; y++ {
			for x := -1; x <= 1; x++ {
				if (x == 0 || y == 0 || z == 0) && (x != 0 || y != 0 || z != 0) {
					offsets = append(offsets, cube.Pos{x, y, z})
				}
			}
		}
	}
	return offsets
}()

// sculkMovementPos ports ChargeCursor.getValidMovementPos.
func sculkMovementPos(tx *world.Tx, pos cube.Pos) (cube.Pos, bool) {
	offsets := make([]cube.Pos, len(nonCornerNeighbours))
	copy(offsets, nonCornerNeighbours)
	rand.Shuffle(len(offsets), func(i, j int) { offsets[i], offsets[j] = offsets[j], offsets[i] })
	target := pos
	for _, offset := range offsets {
		neighbour := pos.Add(offset)
		transferee := tx.Block(neighbour)
		if _, ok := sculkFaces(transferee); !ok || !sculkMovementUnobstructed(tx, pos, neighbour) {
			continue
		}
		target = neighbour
		if sculkVeinHasSubstrateAccess(tx, transferee, neighbour) {
			break
		}
	}
	return target, target != pos
}

// sculkMovementUnobstructed ports ChargeCursor.isMovementUnobstructed.
func sculkMovementUnobstructed(tx *world.Tx, from, to cube.Pos) bool {
	delta := to.Sub(from)
	if abs(delta.X())+abs(delta.Y())+abs(delta.Z()) == 1 {
		return true
	}
	x, y, z := cube.FaceEast, cube.FaceUp, cube.FaceSouth
	if delta.X() < 0 {
		x = cube.FaceWest
	}
	if delta.Y() < 0 {
		y = cube.FaceDown
	}
	if delta.Z() < 0 {
		z = cube.FaceNorth
	}
	switch {
	case delta.X() == 0:
		return sculkUnobstructed(tx, from, y) || sculkUnobstructed(tx, from, z)
	case delta.Y() == 0:
		return sculkUnobstructed(tx, from, x) || sculkUnobstructed(tx, from, z)
	default:
		return sculkUnobstructed(tx, from, x) || sculkUnobstructed(tx, from, y)
	}
}

func sculkUnobstructed(tx *world.Tx, from cube.Pos, face cube.Face) bool {
	test := from.Side(face)
	return !tx.Block(test).Model().FaceSolid(test, face.Opposite(), tx)
}

// sculkBehaviour ports the SculkBehaviour interface.
type sculkBehaviour interface {
	attemptSpreadVein(tx *world.Tx, pos cube.Pos, state world.Block, facings MultiFace, hasFacings bool) bool
	canChangeBlockStateOnSpread() bool
	attemptUseCharge(c *sculkChargeCursor, tx *world.Tx, origin cube.Pos, spreadVeins bool) int
	onDischarged(tx *world.Tx, state world.Block, pos cube.Pos)
	updateDecayDelay(age int) int
}

func sculkBehaviourOf(b world.Block) sculkBehaviour {
	switch b := b.(type) {
	case Sculk:
		return b
	case SculkVein:
		return b
	}
	return defaultSculkBehaviour{}
}

// defaultSculkBehaviour ports SculkBehaviour.DEFAULT, used for every block
// that is neither sculk nor a sculk vein.
type defaultSculkBehaviour struct{}

func (defaultSculkBehaviour) attemptSpreadVein(tx *world.Tx, pos cube.Pos, state world.Block, facings MultiFace, hasFacings bool) bool {
	switch {
	case !hasFacings:
		return sculkVeinSameSpaceSpreader.spreadAll(tx, tx.Block(pos), pos) > 0
	case facings != 0:
		if _, air := state.(Air); !air && !sculkWaterAt(tx, pos) {
			return false
		}
		return regrowSculkVein(tx, pos, facings)
	default:
		return sculkVeinSpreader.spreadAll(tx, state, pos) > 0
	}
}
func (defaultSculkBehaviour) canChangeBlockStateOnSpread() bool { return true }
func (defaultSculkBehaviour) attemptUseCharge(c *sculkChargeCursor, _ *world.Tx, _ cube.Pos, _ bool) int {
	if c.decayDelay > 0 {
		return c.charge
	}
	return 0
}
func (defaultSculkBehaviour) onDischarged(*world.Tx, world.Block, cube.Pos) {}
func (defaultSculkBehaviour) updateDecayDelay(age int) int                  { return max(age-1, 0) }

// sculkWaterAt reports whether pos holds water, as a block or waterlogged.
func sculkWaterAt(tx *world.Tx, pos cube.Pos) bool {
	liquid, ok := tx.Liquid(pos)
	if !ok {
		return false
	}
	_, water := liquid.(Water)
	return water
}

// regrowSculkVein ports SculkVeinBlock.regrow.
func regrowSculkVein(tx *world.Tx, pos cube.Pos, faces MultiFace) bool {
	var grown MultiFace
	for _, face := range faces.Faces() {
		if multifaceCanAttachTo(tx, pos, face) {
			grown = grown.With(face, true)
		}
	}
	if grown == 0 {
		return false
	}
	tx.SetBlock(pos, SculkVein{Faces: grown}, nil)
	return true
}

func (s Sculk) attemptSpreadVein(tx *world.Tx, pos cube.Pos, state world.Block, _ MultiFace, _ bool) bool {
	return sculkVeinSpreader.spreadAll(tx, state, pos) > 0
}
func (Sculk) canChangeBlockStateOnSpread() bool             { return false }
func (Sculk) onDischarged(*world.Tx, world.Block, cube.Pos) {}
func (Sculk) updateDecayDelay(int) int                      { return 1 }
func (v SculkVein) canChangeBlockStateOnSpread() bool       { return true }
func (v SculkVein) updateDecayDelay(int) int                { return 1 }
func (v SculkVein) attemptSpreadVein(tx *world.Tx, pos cube.Pos, state world.Block, _ MultiFace, _ bool) bool {
	return sculkVeinSpreader.spreadAll(tx, state, pos) > 0
}

// attemptUseCharge ports SculkBlock.attemptUseCharge: away from the catalyst
// the charge grows sensors and shriekers, and it decays with distance.
func (Sculk) attemptUseCharge(c *sculkChargeCursor, tx *world.Tx, origin cube.Pos, _ bool) int {
	charge := c.charge
	if charge == 0 || rand.IntN(sculkChargeDecayRate) != 0 {
		return charge
	}
	closeToCatalyst := distanceSquared(c.pos, origin) < sculkNoGrowthRadius*sculkNoGrowthRadius
	if !closeToCatalyst && sculkCanPlaceGrowth(tx, c.pos) {
		if rand.IntN(sculkGrowthSpawnCost) < charge {
			growthPos := c.pos.Side(cube.FaceUp)
			growth := sculkRandomGrowth()
			tx.SetBlock(growthPos, growth, nil)
			tx.PlaySound(c.pos.Vec3Centre(), sound.BlockPlace{Block: growth})
		}
		return max(0, charge-sculkGrowthSpawnCost)
	}
	if rand.IntN(sculkAdditionalDecayRate) != 0 {
		return charge
	}
	if closeToCatalyst {
		return charge - 1
	}
	return charge - sculkDecayPenalty(c.pos, origin, charge)
}

// sculkDecayPenalty ports SculkBlock.getDecayPenalty in Java float precision.
func sculkDecayPenalty(pos, origin cube.Pos, charge int) int {
	outer := float32(math.Sqrt(float64(distanceSquared(pos, origin)))) - sculkNoGrowthRadius
	outer *= outer
	maxReach := float32((sculkMaxGrowthRadius - sculkNoGrowthRadius) * (sculkMaxGrowthRadius - sculkNoGrowthRadius))
	factor := min(1, outer/maxReach)
	return max(1, int(float32(charge)*factor*0.5))
}

// sculkRandomGrowth ports SculkBlock.getRandomGrowthState for a level
// spreader: a shrieker that cannot summon one time in eleven, else a sensor.
func sculkRandomGrowth() world.Block {
	if rand.IntN(11) == 0 {
		return SculkShrieker{}
	}
	return NewSculkSensor()
}

// sculkCanPlaceGrowth ports SculkBlock.canPlaceGrowth: the space above must
// be air or water, and at most two growth inhibitors may be nearby.
func sculkCanPlaceGrowth(tx *world.Tx, pos cube.Pos) bool {
	above := tx.Block(pos.Side(cube.FaceUp))
	if _, air := above.(Air); !air {
		if _, water := above.(Water); !water {
			return false
		}
	}
	inhibitors := 0
	for x := -4; x <= 4; x++ {
		for y := 0; y <= 2; y++ {
			for z := -4; z <= 4; z++ {
				switch tx.Block(pos.Add(cube.Pos{x, y, z})).(type) {
				// The sculk_growth_inhibitors tag: sculk sensors and shriekers.
				case SculkSensor, SculkShrieker:
					if inhibitors++; inhibitors > 2 {
						return false
					}
				}
			}
		}
	}
	return true
}

// attemptUseCharge ports SculkVeinBlock.attemptUseCharge.
func (v SculkVein) attemptUseCharge(c *sculkChargeCursor, tx *world.Tx, _ cube.Pos, spreadVeins bool) int {
	if spreadVeins && v.attemptPlaceSculk(tx, c.pos) {
		return c.charge - 1
	}
	if rand.IntN(sculkChargeDecayRate) == 0 {
		return int(math.Floor(float64(float32(c.charge) * 0.5)))
	}
	return c.charge
}

// attemptPlaceSculk ports SculkVeinBlock.attemptPlaceSculk: a supporting
// sculk-replaceable block becomes sculk and veins spread over it.
func (v SculkVein) attemptPlaceSculk(tx *world.Tx, pos cube.Pos) bool {
	state, ok := tx.Block(pos).(SculkVein)
	if !ok {
		return false
	}
	for _, support := range shuffledFaces(nil) {
		if !state.Faces.Has(support) {
			continue
		}
		supportPos := pos.Side(support)
		if !javaBlockTag(tx, supportPos, "minecraft:sculk_replaceable") {
			continue
		}
		old := tx.Block(supportPos)
		tx.SetBlock(supportPos, Sculk{}, nil)
		pushEntitiesUp(tx, supportPos, old)
		tx.PlaySound(supportPos.Vec3Centre(), sculkSpreadSound())
		sculkVeinSpreader.spreadAll(tx, Sculk{}, supportPos)
		skip := support.Opposite()
		for _, face := range cube.Faces() {
			if face == skip {
				continue
			}
			veinPos := supportPos.Side(face)
			if vein, ok := tx.Block(veinPos).(SculkVein); ok {
				vein.onDischarged(tx, vein, veinPos)
			}
		}
		return true
	}
	return false
}

// onDischarged ports SculkVeinBlock.onDischarged: faces resting on sculk are
// absorbed, and a vein left without faces disappears.
func (v SculkVein) onDischarged(tx *world.Tx, state world.Block, pos cube.Pos) {
	vein, ok := state.(SculkVein)
	if !ok {
		return
	}
	faces := vein.Faces
	for _, face := range cube.Faces() {
		if faces.Has(face) {
			if _, sculk := tx.Block(pos.Side(face)).(Sculk); sculk {
				faces = faces.With(face, false)
			}
		}
	}
	if faces != 0 {
		tx.SetBlock(pos, SculkVein{Faces: faces}, nil)
		return
	}
	if sculkWaterAt(tx, pos) {
		tx.SetBlock(pos, Water{Still: true, Depth: 8}, nil)
		return
	}
	tx.SetBlock(pos, nil, nil)
}

// sculkVeinHasSubstrateAccess ports SculkVeinBlock.hasSubstrateAccess.
func sculkVeinHasSubstrateAccess(tx *world.Tx, b world.Block, pos cube.Pos) bool {
	vein, ok := b.(SculkVein)
	if !ok {
		return false
	}
	for _, face := range vein.Faces.Faces() {
		if javaBlockTag(tx, pos.Side(face), "minecraft:sculk_replaceable") {
			return true
		}
	}
	return false
}

// sculkVeinCanReplace ports SculkVeinSpreaderConfig.stateCanBeReplaced.
func sculkVeinCanReplace(tx *world.Tx, source, placement cube.Pos, face cube.Face, existing world.Block) bool {
	switch tx.Block(placement.Side(face)).(type) {
	case Sculk, SculkCatalyst, MovingBlock:
		return false
	}
	delta := placement.Sub(source)
	if abs(delta.X())+abs(delta.Y())+abs(delta.Z()) == 2 {
		neighbour := source.Side(face.Opposite())
		if tx.Block(neighbour).Model().FaceSolid(neighbour, face, tx) {
			return false
		}
	}
	switch existing.(type) {
	case Lava, Fire:
		return false
	case Air, SculkVein:
		return true
	}
	if water, ok := existing.(Water); ok {
		return water.Depth > 0
	}
	replaceable, ok := existing.(Replaceable)
	return ok && replaceable.ReplaceableBy(SculkVein{})
}

// sculkVeinSpreader and sculkVeinSameSpaceSpreader are SculkVeinBlock's
// spreaders: every other block counts as a source on all of its faces.
var (
	sculkVeinSpreader = multifaceSpreader[SculkVein]{
		block:                   SculkVein{},
		types:                   defaultMultifaceSpreadOrder,
		stateCanBeReplaced:      sculkVeinCanReplace,
		otherBlockValidAsSource: sculkVeinOtherSource,
	}
	sculkVeinSameSpaceSpreader = multifaceSpreader[SculkVein]{
		block:                   SculkVein{},
		types:                   []multifaceSpreadType{spreadSamePosition},
		stateCanBeReplaced:      sculkVeinCanReplace,
		otherBlockValidAsSource: sculkVeinOtherSource,
	}
)

func sculkVeinOtherSource(b world.Block) bool {
	_, vein := b.(SculkVein)
	return !vein
}

// pushEntitiesUp ports Block.pushEntitiesUp for a block replaced by a full
// cube: entities inside the space the new block gained are lifted onto it.
func pushEntitiesUp(tx *world.Tx, pos cube.Pos, old world.Block) {
	top := 0.0
	for _, box := range old.Model().BBox(pos, tx) {
		top = max(top, box.Max().Y())
	}
	if top >= 1 {
		return
	}
	gained := cube.Box(0, top, 0, 1, 1, 1).Translate(pos.Vec3())
	for e := range tx.EntitiesWithin(gained.Grow(1)) {
		box := e.H().Type().BBox(e).Translate(e.Position())
		if !box.IntersectsWith(gained) {
			continue
		}
		if t, ok := e.(interface{ Teleport(mgl64.Vec3) }); ok {
			t.Teleport(e.Position().Add(mgl64.Vec3{0, gained.Max().Y() - box.Min().Y()}))
		}
	}
}

// javaBlockTag reports whether the Java state of the block at pos is in the
// vanilla block tag.
func javaBlockTag(tx *world.Tx, pos cube.Pos, tag string) bool {
	id, err := tx.JavaBlockStateID(pos)
	if err != nil {
		panic(err)
	}
	name, ok := javadata.BlockStateName(id)
	if !ok {
		panic("block has no Java state name")
	}
	contains, err := javadata.BlockTagContains(tag, name)
	if err != nil {
		panic(err)
	}
	return contains
}

func sculkSpreadSound() world.Sound {
	return sound.NativeEvent{SoundType: "block.sculk.spread", ExtraData: -1}
}

func chessboardDistance(a, b cube.Pos) int {
	return max(abs(a.X()-b.X()), abs(a.Y()-b.Y()), abs(a.Z()-b.Z()))
}

func distanceSquared(a, b cube.Pos) int {
	d := a.Sub(b)
	return d.X()*d.X() + d.Y()*d.Y() + d.Z()*d.Z()
}
