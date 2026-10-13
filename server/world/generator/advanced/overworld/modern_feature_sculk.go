package overworld

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

var sculkDirections = [...]string{"down", "up", "north", "south", "west", "east"}

// BlockPos.betweenClosed visits X first, then Y, then Z before shuffling.
var sculkMovementOffsets = [...]featurePos{
	{0, -1, -1}, {-1, 0, -1}, {0, 0, -1}, {1, 0, -1}, {0, 1, -1},
	{-1, -1, 0}, {0, -1, 0}, {1, -1, 0}, {-1, 0, 0}, {1, 0, 0},
	{-1, 1, 0}, {0, 1, 0}, {1, 1, 0},
	{0, -1, 1}, {-1, 0, 1}, {0, 0, 1}, {1, 0, 1}, {0, 1, 1},
}

// placeModernSculkPatch ports SculkPatchFeature and the world-generation
// SculkSpreader. Cursor updates are deliberately sequential: each cursor sees
// block changes made by earlier cursors in the same attempt.
func placeModernSculkPatch(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	chargeCount := featureNumber(config["charge_count"])
	amountPerCharge := featureNumber(config["amount_per_charge"])
	spreadAttempts := featureNumber(config["spread_attempts"])
	growthRounds := featureNumber(config["growth_rounds"])
	spreadRounds := featureNumber(config["spread_rounds"])
	if chargeCount < 1 || chargeCount > 32 || amountPerCharge < 1 || amountPerCharge > 500 || spreadAttempts < 1 || spreadAttempts > 64 || growthRounds < 0 || growthRounds > 8 || spreadRounds < 0 || spreadRounds > 8 {
		panic("overworld: invalid Java sculk patch configuration")
	}
	name, props := region.State(origin)
	if !sculkBehavior(name) {
		if name != "minecraft:air" && name != "minecraft:cave_air" && name != "minecraft:void_air" && !(name == "minecraft:water" && featureFluidName(region, origin, props) == "water") {
			return false
		}
		canSpread := false
		for _, dir := range sculkDirections {
			p := sculkOffset(origin, dir)
			if sculkCollisionFull(region, p) {
				canSpread = true
				break
			}
		}
		if !canSpread {
			return false
		}
	}

	cursors := make([]sculkCursor, 0, 32)
	for round := 0; round < spreadRounds+growthRounds; round++ {
		for i := 0; i < chargeCount; i++ {
			charge := amountPerCharge
			for charge > 0 && len(cursors) < 32 {
				part := min(charge, 1000)
				cursors = append(cursors, sculkCursor{pos: origin, charge: part, decayDelay: 1})
				charge -= part
			}
		}
		veins := round < spreadRounds
		for attempt := 0; attempt < spreadAttempts; attempt++ {
			cursors = sculkUpdateCursors(region, origin, cursors, random, veins)
		}
		cursors = cursors[:0]
	}
	return true
}

type sculkCursor struct {
	pos         featurePos
	charge      int
	updateDelay int
	decayDelay  int
	facings     uint8
	hasFacings  bool
}

// The world-generation spreader is configured with isWorldGeneration=true,
// which deliberately disables cursor merging in SculkSpreader.updateCursors.
func sculkUpdateCursors(region *featureRegion, origin featurePos, cursors []sculkCursor, random *java26.Random, spreadVeins bool) []sculkCursor {
	count := 0
	for i := range cursors {
		cursor := &cursors[i]
		if sculkChessboard(cursor.pos, origin) <= 1024 {
			sculkUpdateCursor(region, origin, cursor, random, spreadVeins)
			if cursor.charge > 0 {
				cursors[count] = *cursor
				count++
			}
		}
	}
	return cursors[:count]
}

func sculkUpdateCursor(region *featureRegion, origin featurePos, c *sculkCursor, random *java26.Random, spreadVeins bool) {
	if c.charge <= 0 {
		return
	}
	if c.updateDelay > 0 {
		c.updateDelay--
		return
	}
	name, props := region.State(c.pos)
	if spreadVeins && sculkAttemptSpreadVein(region, c.pos, name, props, c.facings, c.hasFacings) {
		if name != "minecraft:sculk" { // SculkBlock preserves its pre-spread state; other behaviors may change it.
			name, props = region.State(c.pos)
		}
	}
	behaviorName := name
	if name == "minecraft:sculk" {
		c.charge = sculkUseSculkCharge(region, c, origin, random)
	} else if name == "minecraft:sculk_vein" {
		c.charge = sculkUseVeinCharge(region, c, random, spreadVeins)
	} else if c.decayDelay <= 0 {
		c.charge = 0
	}
	if c.charge <= 0 {
		if name == "minecraft:sculk_vein" {
			sculkOnDischarged(region, c.pos, props, random)
		}
		return
	}
	transfer, ok := sculkMovementPos(region, c.pos, origin, random)
	if ok {
		if name == "minecraft:sculk_vein" {
			sculkOnDischarged(region, c.pos, props, random)
		}
		c.pos = transfer
		name, props = region.State(c.pos)
	} else {
		if name == "minecraft:sculk_vein" {
			sculkOnDischarged(region, c.pos, props, random)
		}
		c.charge = 0
		return
	}
	if sculkBehavior(name) {
		c.facings, c.hasFacings = sculkAvailableFaces(name, props), true
	}
	if !sculkBehavior(behaviorName) {
		c.decayDelay = max(c.decayDelay-1, 0)
	} else {
		c.decayDelay = 1
	}
	c.updateDelay = 1
}

func sculkUseSculkCharge(region *featureRegion, c *sculkCursor, origin featurePos, random *java26.Random) int {
	charge := c.charge
	if charge == 0 || random.NextInt(5) != 0 {
		return charge
	}
	close := sculkDistanceSquared(c.pos, origin) < 1
	if !close && sculkCanPlaceGrowth(region, c.pos) {
		if int(random.NextInt(50)) < charge {
			p := featurePos{c.pos.x, c.pos.y + 1, c.pos.z}
			growth := "sculk_sensor"
			properties := map[string]any{}
			if random.NextInt(11) == 0 {
				growth = "sculk_shrieker"
				properties["can_summon"] = true
			}
			if featureFluidName(region, p, nil) == "water" {
				properties["waterlogged"] = true
			}
			state := sculkResolveState(region, growth, properties)
			region.Set(p, state)
		}
		return max(0, charge-50)
	}
	if random.NextInt(10) != 0 {
		return charge
	}
	if close {
		return charge - 1
	}
	return charge - sculkDecayPenalty(c.charge, c.pos, origin)
}

func sculkCanPlaceGrowth(region *featureRegion, pos featurePos) bool {
	above := featurePos{pos.x, pos.y + 1, pos.z}
	name, _ := region.State(above)
	if name != "minecraft:air" && name != "minecraft:cave_air" && name != "minecraft:void_air" && !(name == "minecraft:water" && featureFluidName(region, above, nil) == "water") {
		return false
	}
	count := 0
	for x := int32(-4); x <= 4; x++ {
		for y := int32(0); y <= 2; y++ {
			for z := int32(-4); z <= 4; z++ {
				n, _ := region.State(featurePos{pos.x + x, pos.y + y, pos.z + z})
				if javaBlockTagContains("sculk_growth_inhibitors", normalizeJavaResource(n)) {
					count++
					if count > 2 {
						return false
					}
				}
			}
		}
	}
	return true
}

func sculkDecayPenalty(charge int, pos, origin featurePos) int {
	outer := float32(math.Sqrt(float64(sculkDistanceSquared(pos, origin)))) - 1
	factor := min(float32(1), outer*outer/529)
	return max(1, int(float32(charge)*factor*0.5))
}

func sculkUseVeinCharge(region *featureRegion, c *sculkCursor, random *java26.Random, spreadVeins bool) int {
	if spreadVeins && sculkAttemptPlaceSculk(region, c.pos, random) {
		return c.charge - 1
	}
	if random.NextInt(5) == 0 {
		return int(float32(c.charge) * 0.5)
	}
	return c.charge
}

func sculkAttemptSpreadVein(region *featureRegion, pos featurePos, name string, props map[string]any, facings uint8, hasFacings bool) bool {
	if sculkBehavior(name) {
		return sculkSpreadAll(region, pos, name == "minecraft:sculk_vein")
	}
	if !hasFacings {
		return sculkSpreadSameSpace(region, pos, name == "minecraft:sculk_vein")
	}
	if facings != 0 {
		if name != "minecraft:air" && name != "minecraft:cave_air" && name != "minecraft:void_air" && !sculkWaterFluid(region, pos, props) {
			return false
		}
		return sculkRegrowVein(region, pos, facings)
	}
	return sculkSpreadAll(region, pos, name == "minecraft:sculk_vein")
}

func sculkSpreadSameSpace(region *featureRegion, pos featurePos, sourceIsVein bool) bool {
	sourceFaces := sculkSourceFaces(region, pos, sourceIsVein)
	count := 0
	for i, from := range sculkDirections {
		if sourceIsVein && sourceFaces&(1<<i) == 0 {
			continue
		}
		for j, to := range sculkDirections {
			if sculkAxis(from) == sculkAxis(to) || sourceIsVein && sourceFaces&(1<<j) != 0 {
				continue
			}
			if sculkCanPlaceVein(region, pos, pos, to) && sculkPlaceVein(region, pos, to) {
				count++
			}
		}
	}
	return count > 0
}

func sculkSpreadAll(region *featureRegion, pos featurePos, sourceIsVein bool) bool {
	sourceFaces := sculkSourceFaces(region, pos, sourceIsVein)
	count := 0
	for i, from := range sculkDirections {
		if sourceIsVein && sourceFaces&(1<<i) == 0 {
			continue
		}
		for j, to := range sculkDirections {
			if sculkAxis(from) == sculkAxis(to) || sourceIsVein && sourceFaces&(1<<j) != 0 {
				continue
			}
			for kind := range 3 {
				target, face := pos, to
				switch kind {
				case 1:
					target, face = sculkOffset(pos, to), from
				case 2:
					target, face = sculkOffset(sculkOffset(pos, to), from), sculkOpposite(to)
				}
				if sculkCanPlaceVein(region, pos, target, face) {
					if sculkPlaceVein(region, target, face) {
						count++
					}
					break
				}
			}
		}
	}
	return count > 0
}

func sculkSourceFaces(region *featureRegion, pos featurePos, sourceIsVein bool) uint8 {
	if !sourceIsVein {
		return 0
	}
	name, properties := region.State(pos)
	return sculkAvailableFaces(name, properties)
}

func sculkCanPlaceVein(region *featureRegion, source, target featurePos, face string) bool {
	name, properties := region.State(target)
	if name == "minecraft:sculk_vein" && properties[face] == true {
		return false
	}
	against, _ := region.State(sculkOffset(target, face))
	switch against {
	case "minecraft:sculk", "minecraft:sculk_catalyst", "minecraft:moving_piston":
		return false
	}
	if absFeatureInt(source.x-target.x)+absFeatureInt(source.y-target.y)+absFeatureInt(source.z-target.z) == 2 {
		sturdyPos := sculkOffset(source, sculkOpposite(face))
		sturdyName, sturdyProperties := region.State(sturdyPos)
		if sculkShape(region, sturdyName, sturdyProperties).Sturdy&sculkFaceBit(face) != 0 {
			return false
		}
	}
	fluid := featureFluidName(region, target, properties)
	if fluid != "empty" && fluid != "water" {
		return false
	}
	if javaBlockTagContains("fire", normalizeJavaResource(name)) {
		return false
	}
	shape := sculkShape(region, name, properties)
	replaceable := shape.Replaceable
	if name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air" || name == "minecraft:sculk_vein" || (name == "minecraft:water" && fluid == "water") {
		replaceable = true
	}
	if !replaceable {
		return false
	}
	return sculkCanAttach(region, target, face)
}

func sculkPlaceVein(region *featureRegion, pos featurePos, face string) bool {
	name, props := region.State(pos)
	if name == "minecraft:sculk_vein" {
		props[face] = true
	} else {
		props = map[string]any{face: true}
		if sculkWaterFluid(region, pos, nil) {
			props["waterlogged"] = true
		}
	}
	if featureInsideTarget(region.target, pos) {
		region.result.MarkForPostProcessing(cube.Pos{int(pos.x), int(pos.y), int(pos.z)}, region.c.Range())
	}
	return region.setWithFlags(pos, sculkResolveState(region, "sculk_vein", props), 2)
}

func sculkRegrowVein(region *featureRegion, pos featurePos, faces uint8) bool {
	properties := make(map[string]any, 7)
	any := false
	for i, face := range sculkDirections {
		if faces&(1<<i) != 0 && sculkCanAttach(region, pos, face) {
			properties[face] = true
			any = true
		}
	}
	if !any {
		return false
	}
	if sculkWaterFluid(region, pos, nil) {
		properties["waterlogged"] = true
	}
	region.Set(pos, sculkResolveState(region, "sculk_vein", properties))
	return true
}

func sculkWaterFluid(region *featureRegion, pos featurePos, properties map[string]any) bool {
	switch featureFluidName(region, pos, properties) {
	case "water":
		return true
	default:
		return false
	}
}

func sculkAttemptPlaceSculk(region *featureRegion, pos featurePos, random *java26.Random) bool {
	_, props := region.State(pos)
	directions := sculkDirections
	for i := len(directions) - 1; i > 0; i-- {
		j := int(random.NextInt(int32(i + 1)))
		directions[i], directions[j] = directions[j], directions[i]
	}
	for _, support := range directions {
		if props[support] != true {
			continue
		}
		supportPos := sculkOffset(pos, support)
		name, _ := region.State(supportPos)
		if !javaBlockTagContains("sculk_replaceable_world_gen", normalizeJavaResource(name)) {
			continue
		}
		if !region.Set(supportPos, sculkResolveState(region, "sculk", nil)) {
			continue
		}
		sculkSpreadAll(region, supportPos, false)
		skip := sculkOpposite(support)
		for _, direction := range sculkDirections {
			if direction == skip {
				continue
			}
			p := sculkOffset(supportPos, direction)
			n, properties := region.State(p)
			if n == "minecraft:sculk_vein" {
				sculkOnDischarged(region, p, properties, random)
			}
		}
		return true
	}
	return false
}

func sculkOnDischarged(region *featureRegion, pos featurePos, props map[string]any, random *java26.Random) {
	_ = random
	for _, d := range sculkDirections {
		if props[d] == true {
			n, _ := region.State(sculkOffset(pos, d))
			if n == "minecraft:sculk" {
				props[d] = false
			}
		}
	}
	any := false
	for _, d := range sculkDirections {
		if props[d] == true {
			any = true
			break
		}
	}
	if !any {
		if sculkWaterFluid(region, pos, nil) {
			region.Set(pos, sculkResolveState(region, "water", nil))
		} else {
			region.Set(pos, sculkResolveState(region, "air", nil))
		}
		return
	}
	region.Set(pos, sculkResolveState(region, "sculk_vein", props))
}

func sculkMovementPos(region *featureRegion, pos, origin featurePos, random *java26.Random) (featurePos, bool) {
	offsets := sculkMovementOffsets
	for i := len(offsets) - 1; i > 0; i-- {
		j := int(random.NextInt(int32(i + 1)))
		offsets[i], offsets[j] = offsets[j], offsets[i]
	}
	last := pos
	moved := false
	for _, offset := range offsets {
		target := featurePos{pos.x + offset.x, pos.y + offset.y, pos.z + offset.z}
		if sculkDistanceXZ(target, origin) > 144 {
			continue
		}
		name, props := region.State(target)
		if !sculkBehavior(name) || !sculkMovementUnobstructed(region, pos, target) {
			continue
		}
		last, moved = target, true
		if sculkHasSubstrate(region, target, name, props) {
			break
		}
	}
	return last, moved
}

func sculkMovementUnobstructed(region *featureRegion, from, to featurePos) bool {
	dx, dy, dz := to.x-from.x, to.y-from.y, to.z-from.z
	if absFeatureInt(dx)+absFeatureInt(dy)+absFeatureInt(dz) == 1 {
		return true
	}
	check := func(dir string) bool {
		p := sculkOffset(from, dir)
		n, props := region.State(p)
		shape := sculkShape(region, n, props)
		return shape.Sturdy&sculkFaceBit(sculkOpposite(dir)) == 0
	}
	if dx == 0 {
		return check(sculkDirection(dy, "up", "down")) || check(sculkDirection(dz, "south", "north"))
	}
	if dy == 0 {
		return check(sculkDirection(dx, "east", "west")) || check(sculkDirection(dz, "south", "north"))
	}
	return check(sculkDirection(dx, "east", "west")) || check(sculkDirection(dy, "up", "down"))
}

func sculkHasSubstrate(region *featureRegion, pos featurePos, name string, props map[string]any) bool {
	if name != "minecraft:sculk_vein" {
		return false
	}
	for _, d := range sculkDirections {
		if props[d] == true {
			n, _ := region.State(sculkOffset(pos, d))
			if javaBlockTagContains("sculk_replaceable", normalizeJavaResource(n)) {
				return true
			}
		}
	}
	return false
}

func sculkAvailableFaces(name string, props map[string]any) uint8 {
	if name != "minecraft:sculk_vein" {
		return 0
	}
	var faces uint8
	for i, direction := range sculkDirections {
		if props[direction] == true {
			faces |= 1 << i
		}
	}
	return faces
}
func sculkFaceSet(region *featureRegion, pos featurePos, d string) bool {
	n, p := region.State(pos)
	return n == "minecraft:sculk_vein" && p[d] == true
}
func sculkCanAttach(region *featureRegion, pos featurePos, face string) bool {
	n, p := region.State(sculkOffset(pos, face))
	shape := sculkShape(region, n, p)
	return shape.Attachment&sculkFaceBit(sculkOpposite(face)) != 0
}
func sculkCollisionFull(region *featureRegion, pos featurePos) bool {
	n, p := region.State(pos)
	return sculkShape(region, n, p).CollisionFull
}
func sculkShape(region *featureRegion, name string, props map[string]any) javadata.GenerationShape {
	shape, err := javadata.BlockGenerationShape(normalizeJavaResource(name), props)
	if err != nil {
		panic(err)
	}
	return shape
}
func sculkResolveState(region *featureRegion, name string, props map[string]any) world.NativeState {
	state, err := world.ResolveState(region.c.BlockRegistry(), world.BlockState{Name: "minecraft:" + name, Properties: props})
	if err != nil {
		panic(err)
	}
	return state
}
func sculkBehavior(name string) bool {
	return name == "minecraft:sculk" || name == "minecraft:sculk_vein"
}
func sculkFaceBit(d string) uint8 {
	for i, v := range sculkDirections {
		if d == v {
			return 1 << i
		}
	}
	panic("overworld: invalid Java sculk direction")
}
func sculkOpposite(d string) string {
	switch d {
	case "down":
		return "up"
	case "up":
		return "down"
	case "north":
		return "south"
	case "south":
		return "north"
	case "west":
		return "east"
	case "east":
		return "west"
	}
	panic("overworld: invalid Java sculk direction")
}
func sculkAxis(d string) int {
	if d == "down" || d == "up" {
		return 1
	}
	if d == "north" || d == "south" {
		return 2
	}
	return 0
}
func sculkOffset(p featurePos, d string) featurePos {
	switch d {
	case "down":
		p.y--
	case "up":
		p.y++
	case "north":
		p.z--
	case "south":
		p.z++
	case "west":
		p.x--
	case "east":
		p.x++
	default:
		panic("overworld: invalid Java sculk direction")
	}
	return p
}
func sculkDirection(delta int32, positive, negative string) string {
	if delta < 0 {
		return negative
	}
	return positive
}
func sculkDistanceSquared(a, b featurePos) int {
	dx, dy, dz := int(a.x-b.x), int(a.y-b.y), int(a.z-b.z)
	return dx*dx + dy*dy + dz*dz
}
func sculkDistanceXZ(a, b featurePos) int { dx, dz := int(a.x-b.x), int(a.z-b.z); return dx*dx + dz*dz }
func sculkChessboard(a, b featurePos) int {
	return max(int(absFeatureInt(a.x-b.x)), int(absFeatureInt(a.y-b.y)), int(absFeatureInt(a.z-b.z)))
}
