package overworld

import (
	"maps"
	"math"
	"sync"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/world"
)

var sculkDirections = [...]string{"down", "up", "north", "south", "west", "east"}

const (
	sculkKindOther uint8 = iota
	sculkKindAir
	sculkKindWater
	sculkKindSculk
	sculkKindVein
)

type sculkDefaultData struct {
	vein        [128]world.NativeState
	veinMask    []uint8
	sculk       world.NativeState
	air         world.NativeState
	water       world.NativeState
	sensor      world.NativeState
	sensorWet   world.NativeState
	shrieker    world.NativeState
	shriekerWet world.NativeState
}

var getSculkDefaults = sync.OnceValue(func() *sculkDefaultData {
	reg := world.DefaultBlockRegistry
	d := &sculkDefaultData{
		veinMask: make([]uint8, reg.BlockCount()),
	}
	must := func(name string, props map[string]any) world.NativeState {
		st, err := world.ResolveState(reg, world.BlockState{Name: "minecraft:" + name, Properties: props})
		if err != nil {
			panic(err)
		}
		return st
	}
	d.sculk = must("sculk", nil)
	d.air = must("air", nil)
	d.water = must("water", nil)
	d.sensor = must("sculk_sensor", nil)
	d.sensorWet = must("sculk_sensor", map[string]any{"waterlogged": true})
	d.shrieker = must("sculk_shrieker", map[string]any{"can_summon": true})
	d.shriekerWet = must("sculk_shrieker", map[string]any{"can_summon": true, "waterlogged": true})
	for i := range 128 {
		props := map[string]any{
			"down":        i&1 != 0,
			"up":          i&2 != 0,
			"north":       i&4 != 0,
			"south":       i&8 != 0,
			"west":        i&16 != 0,
			"east":        i&32 != 0,
			"waterlogged": i&64 != 0,
		}
		ns := must("sculk_vein", props)
		d.vein[i] = ns
		if int(ns.Block) < len(d.veinMask) {
			d.veinMask[ns.Block] = 0x80 | uint8(i&63)
		}
	}
	return d
})

// BlockPos.betweenClosed visits X first, then Y, then Z before shuffling.
var sculkMovementOffsets = [...]featurePos{
	{0, -1, -1}, {-1, 0, -1}, {0, 0, -1}, {1, 0, -1}, {0, 1, -1},
	{-1, -1, 0}, {0, -1, 0}, {1, -1, 0}, {-1, 0, 0}, {1, 0, 0},
	{-1, 1, 0}, {0, 1, 0}, {1, 1, 0},
	{0, -1, 1}, {-1, 0, 1}, {0, 0, 1}, {1, 0, 1}, {0, 1, 1},
}

func sculkNodeState(region *featureRegion, pos featurePos) (kind, faces uint8, waterlogged bool) {
	if region.isDefaultReg {
		ns := region.nativeState(pos)
		d := getSculkDefaults()
		if ns.Block == region.airRID && ns.Liquid == region.airRID && ns.StateID == 0 {
			return sculkKindAir, 0, false
		}
		if ns == d.sculk {
			return sculkKindSculk, 0, false
		}
		if int(ns.Block) < len(d.veinMask) {
			if m := d.veinMask[ns.Block]; m != 0 {
				f := m & 63
				if ns == d.vein[int(f)] {
					return sculkKindVein, f, false
				}
				if ns == d.vein[int(f)|64] {
					return sculkKindVein, f, true
				}
			}
		}
		if ns.Liquid == region.airRID && ns.StateID == 0 {
			if ns.Block == d.water.Block {
				return sculkKindWater, 0, false
			}
			flags := featureMetaForDefaultRID(ns.Block)
			if flags&featFlagAir != 0 {
				return sculkKindAir, 0, false
			}
			if flags&featFlagSculkBehavior == 0 {
				return sculkKindOther, 0, false
			}
		}
	}
	name, props := region.State(pos)
	switch name {
	case "minecraft:sculk":
		return sculkKindSculk, 0, false
	case "minecraft:sculk_vein":
		return sculkKindVein, sculkAvailableFaces(name, props), props["waterlogged"] == true
	case "minecraft:air", "minecraft:cave_air", "minecraft:void_air":
		return sculkKindAir, 0, false
	case "minecraft:water":
		if featureFluidName(region, pos, props) == "water" {
			return sculkKindWater, 0, false
		}
	}
	return sculkKindOther, 0, false
}

// placeModernSculkPatch ports SculkPatchFeature and the world-generation
// SculkSpreader. Cursor updates are deliberately sequential: each cursor sees
// block changes made by earlier cursors in the same attempt.
func placeModernSculkPatch(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	chargeCount := featureNumber(config["charge_count"])
	amountPerCharge := featureNumber(config["amount_per_charge"])
	spreadAttempts := featureNumber(config["spread_attempts"])
	growthRounds := featureNumber(config["growth_rounds"])
	spreadRounds := featureNumber(config["spread_rounds"])
	if chargeCount < 1 || chargeCount > 32 || amountPerCharge < 1 || amountPerCharge > 500 || spreadAttempts < 1 || spreadAttempts > 64 || growthRounds < 0 || growthRounds > 8 || spreadRounds < 0 || spreadRounds > 8 {
		panic("overworld: invalid sculk patch configuration")
	}
	kind, _, _ := sculkNodeState(region, origin)
	if kind != sculkKindSculk && kind != sculkKindVein {
		if kind != sculkKindAir && kind != sculkKindWater {
			return false
		}
		canSpread := false
		for i := range 6 {
			if sculkCollisionFull(region, sculkOffsetIdx(origin, i)) {
				canSpread = true
				break
			}
		}
		if !canSpread {
			return false
		}
	}

	var cursorBuf [32]sculkCursor
	cursors := cursorBuf[:0]
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
func sculkUpdateCursors(region *featureRegion, origin featurePos, cursors []sculkCursor, random *vanilla.Random, spreadVeins bool) []sculkCursor {
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

func sculkUpdateCursor(region *featureRegion, origin featurePos, c *sculkCursor, random *vanilla.Random, spreadVeins bool) {
	if c.charge <= 0 {
		return
	}
	if c.updateDelay > 0 {
		c.updateDelay--
		return
	}
	kind, faces, wl := sculkNodeState(region, c.pos)
	if spreadVeins && sculkAttemptSpreadVein(region, c.pos, kind, faces, c.facings, c.hasFacings) {
		if kind != sculkKindSculk { // SculkBlock preserves its pre-spread state; other behaviors may change it.
			kind, faces, wl = sculkNodeState(region, c.pos)
		}
	}
	behaviorKind := kind
	switch {
	case kind == sculkKindSculk:
		c.charge = sculkUseSculkCharge(region, c, origin, random)
	case kind == sculkKindVein:
		c.charge = sculkUseVeinCharge(region, c, faces, random, spreadVeins)
	case c.decayDelay <= 0:
		c.charge = 0
	}
	if c.charge <= 0 {
		if kind == sculkKindVein {
			sculkOnDischarged(region, c.pos, faces, wl)
		}
		return
	}
	transfer, tKind, tFaces, ok := sculkMovementPos(region, c.pos, origin, random)
	if ok {
		if kind == sculkKindVein {
			sculkOnDischarged(region, c.pos, faces, wl)
		}
		c.pos = transfer
		kind, faces = tKind, tFaces
	} else {
		if kind == sculkKindVein {
			sculkOnDischarged(region, c.pos, faces, wl)
		}
		c.charge = 0
		return
	}
	if kind == sculkKindSculk || kind == sculkKindVein {
		c.facings, c.hasFacings = faces, true
	}
	if behaviorKind != sculkKindSculk && behaviorKind != sculkKindVein {
		c.decayDelay = max(c.decayDelay-1, 0)
	} else {
		c.decayDelay = 1
	}
	c.updateDelay = 1
}

func sculkUseSculkCharge(region *featureRegion, c *sculkCursor, origin featurePos, random *vanilla.Random) int {
	charge := c.charge
	if charge == 0 || random.NextInt(5) != 0 {
		return charge
	}
	close := sculkDistanceSquared(c.pos, origin) < 1
	if !close && sculkCanPlaceGrowth(region, c.pos) {
		if int(random.NextInt(50)) < charge {
			p := featurePos{c.pos.x, c.pos.y + 1, c.pos.z}
			isShrieker := random.NextInt(11) == 0
			wet := sculkWaterFluid(region, p, nil)
			region.Set(p, sculkGrowthState(region, isShrieker, wet))
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

func sculkGrowthState(region *featureRegion, shrieker, wet bool) world.NativeState {
	if region.isDefaultReg {
		d := getSculkDefaults()
		if shrieker {
			if wet {
				return d.shriekerWet
			}
			return d.shrieker
		}
		if wet {
			return d.sensorWet
		}
		return d.sensor
	}
	growth := "sculk_sensor"
	properties := map[string]any{}
	if shrieker {
		growth = "sculk_shrieker"
		properties["can_summon"] = true
	}
	if wet {
		properties["waterlogged"] = true
	}
	return sculkResolveState(region, growth, properties)
}

func sculkCanPlaceGrowth(region *featureRegion, pos featurePos) bool {
	above := featurePos{pos.x, pos.y + 1, pos.z}
	kind, _, _ := sculkNodeState(region, above)
	if kind != sculkKindAir && kind != sculkKindWater {
		return false
	}
	count := 0
	for x := int32(-4); x <= 4; x++ {
		for y := int32(0); y <= 2; y++ {
			for z := int32(-4); z <= 4; z++ {
				n := region.stateName(featurePos{pos.x + x, pos.y + y, pos.z + z})
				if blockTagContains("sculk_growth_inhibitors", normalizeResource(n)) {
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

func sculkUseVeinCharge(region *featureRegion, c *sculkCursor, faces uint8, random *vanilla.Random, spreadVeins bool) int {
	if spreadVeins && sculkAttemptPlaceSculk(region, c.pos, faces, random) {
		return c.charge - 1
	}
	if random.NextInt(5) == 0 {
		return int(float32(c.charge) * 0.5)
	}
	return c.charge
}

func sculkAttemptSpreadVein(region *featureRegion, pos featurePos, kind, sourceFaces, facings uint8, hasFacings bool) bool {
	if kind == sculkKindSculk || kind == sculkKindVein {
		return sculkSpreadAll(region, pos, kind == sculkKindVein, sourceFaces)
	}
	if !hasFacings {
		return sculkSpreadSameSpace(region, pos, false, 0)
	}
	if facings != 0 {
		if kind != sculkKindAir && kind != sculkKindWater {
			return false
		}
		return sculkRegrowVein(region, pos, facings)
	}
	return sculkSpreadAll(region, pos, false, 0)
}

func sculkSpreadSameSpace(region *featureRegion, pos featurePos, sourceIsVein bool, sourceFaces uint8) bool {
	count := 0
	for i := range 6 {
		if sourceIsVein && sourceFaces&(1<<i) == 0 {
			continue
		}
		for j := range 6 {
			if i>>1 == j>>1 || sourceIsVein && sourceFaces&(1<<j) != 0 {
				continue
			}
			if sculkCanPlaceVein(region, pos, pos, j) && sculkPlaceVein(region, pos, j) {
				count++
			}
		}
	}
	return count > 0
}

func sculkSpreadAll(region *featureRegion, pos featurePos, sourceIsVein bool, sourceFaces uint8) bool {
	count := 0
	for i := range 6 {
		if sourceIsVein && sourceFaces&(1<<i) == 0 {
			continue
		}
		for j := range 6 {
			if i>>1 == j>>1 || sourceIsVein && sourceFaces&(1<<j) != 0 {
				continue
			}
			for kind := range 3 {
				target, face := pos, j
				switch kind {
				case 1:
					target, face = sculkOffsetIdx(pos, j), i
				case 2:
					target, face = sculkOffsetIdx(sculkOffsetIdx(pos, j), i), j^1
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

func sculkCanPlaceVein(region *featureRegion, source, target featurePos, faceIdx int) bool {
	if region.isDefaultReg {
		return sculkCanPlaceVeinFast(region, source, target, faceIdx)
	}
	return sculkCanPlaceVeinSlow(region, source, target, faceIdx)
}

func sculkCanPlaceVeinFast(region *featureRegion, source, target featurePos, faceIdx int) bool {
	ns := region.nativeState(target)
	d := getSculkDefaults()
	isVein := false
	if int(ns.Block) < len(d.veinMask) {
		if m := d.veinMask[ns.Block]; m != 0 {
			if ns == d.vein[int(m&63)] || ns == d.vein[int(m&63)|64] {
				if m&uint8(1<<faceIdx) != 0 {
					return false
				}
				isVein = true
			}
		}
	}
	againstPos := sculkOffsetIdx(target, faceIdx)
	ans := region.nativeState(againstPos)
	if ans == d.sculk {
		return false
	}
	oppMask := uint32(1) << (featFlagAttachmentShift + uint(faceIdx^1))
	if ans.Liquid == region.airRID && ans.StateID == 0 {
		aFlags := featureMetaForDefaultRID(ans.Block)
		if namePtr := defaultFeatureNames()[ans.Block].Load(); namePtr != nil {
			if *namePtr == "minecraft:sculk" || *namePtr == "minecraft:sculk_catalyst" || *namePtr == "minecraft:moving_piston" {
				return false
			}
		}
		if absFeatureInt(source.x-target.x)+absFeatureInt(source.y-target.y)+absFeatureInt(source.z-target.z) == 2 {
			if sculkSturdyFaceAt(region, sculkOffsetIdx(source, faceIdx^1), faceIdx) {
				return false
			}
		}
		if !sculkTargetReplaceableFast(region, d, target, ns, isVein) {
			return false
		}
		if aFlags&featFlagShapeOK != 0 {
			return aFlags&oppMask != 0
		}
		return sculkCanAttach(region, target, sculkDirections[faceIdx])
	}
	return sculkCanPlaceVeinSlow(region, source, target, faceIdx)
}

func sculkTargetReplaceableFast(region *featureRegion, d *sculkDefaultData, target featurePos, ns world.NativeState, isVein bool) bool {
	if ns.Liquid == region.airRID && ns.StateID == 0 {
		if ns.Block == region.airRID || ns.Block == d.water.Block || isVein {
			return true
		}
		flags := featureMetaForDefaultRID(ns.Block)
		if flags&featFlagFire != 0 {
			return false
		}
		if namePtr := defaultFeatureNames()[ns.Block].Load(); namePtr != nil {
			n := normalizeResource(*namePtr)
			if n == "lava" || n == "flowing_lava" || n == "flowing_water" {
				return false
			}
		}
		return flags&featFlagReplaceable != 0 || flags&featFlagAir != 0
	}
	fluid := featureFluidName(region, target, nil)
	if fluid != "empty" && fluid != "water" {
		return false
	}
	targetID := ns.CanonicalID(region.registry)
	if targetID != 0 && featureBlockTagContainsID("fire", targetID) {
		return false
	}
	name, props := region.State(target)
	if name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air" || name == "minecraft:sculk_vein" || (name == "minecraft:water" && fluid == "water") {
		return true
	}
	return sculkShapeOf(region, targetID, name, props).Replaceable
}

func sculkCanPlaceVeinSlow(region *featureRegion, source, target featurePos, faceIdx int) bool {
	face := sculkDirections[faceIdx]
	name, properties := region.State(target)
	if name == "minecraft:sculk_vein" && properties[face] == true {
		return false
	}
	against := region.stateName(sculkOffsetIdx(target, faceIdx))
	switch against {
	case "minecraft:sculk", "minecraft:sculk_catalyst", "minecraft:moving_piston":
		return false
	}
	if absFeatureInt(source.x-target.x)+absFeatureInt(source.y-target.y)+absFeatureInt(source.z-target.z) == 2 {
		if sculkSturdyFaceAt(region, sculkOffsetIdx(source, faceIdx^1), faceIdx) {
			return false
		}
	}
	fluid := featureFluidName(region, target, properties)
	if fluid != "empty" && fluid != "water" {
		return false
	}
	targetID := region.StateID(target)
	if targetID != 0 {
		if featureBlockTagContainsID("fire", targetID) {
			return false
		}
	} else if blockTagContains("fire", normalizeResource(name)) {
		return false
	}
	replaceable := name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air" || name == "minecraft:sculk_vein" || (name == "minecraft:water" && fluid == "water")
	if !replaceable {
		replaceable = sculkShapeOf(region, targetID, name, properties).Replaceable
	}
	if !replaceable {
		return false
	}
	return sculkCanAttach(region, target, face)
}

func sculkSturdyFaceAt(region *featureRegion, pos featurePos, faceIdx int) bool {
	if region.isDefaultReg {
		ns := region.nativeState(pos)
		if ns.Liquid == region.airRID && ns.StateID == 0 {
			flags := featureMetaForDefaultRID(ns.Block)
			if flags&featFlagShapeOK != 0 {
				return flags&(1<<(featFlagSturdyShift+uint(faceIdx))) != 0
			}
		}
	}
	return sculkShapeAt(region, pos).Sturdy&(1<<uint(faceIdx)) != 0
}

func sculkPlaceVein(region *featureRegion, pos featurePos, faceIdx int) bool {
	if region.isDefaultReg {
		kind, faces, wl := sculkNodeState(region, pos)
		var state world.NativeState
		d := getSculkDefaults()
		if kind == sculkKindVein {
			idx := int(faces | (1 << uint(faceIdx)))
			if wl {
				idx |= 64
			}
			state = d.vein[idx]
		} else {
			idx := 1 << uint(faceIdx)
			if sculkWaterFluid(region, pos, nil) {
				idx |= 64
			}
			state = d.vein[idx]
		}
		region.markPostProcessing(cube.Pos{int(pos.x), int(pos.y), int(pos.z)})
		return region.setWithFlags(pos, state, 2)
	}
	face := sculkDirections[faceIdx]
	name, props := region.State(pos)
	if name == "minecraft:sculk_vein" {
		props = maps.Clone(props)
		props[face] = true
	} else {
		props = map[string]any{face: true}
		if sculkWaterFluid(region, pos, nil) {
			props["waterlogged"] = true
		}
	}
	region.markPostProcessing(cube.Pos{int(pos.x), int(pos.y), int(pos.z)})
	return region.setWithFlags(pos, sculkResolveState(region, "sculk_vein", props), 2)
}

func sculkRegrowVein(region *featureRegion, pos featurePos, faces uint8) bool {
	if region.isDefaultReg {
		var mask uint8
		for i, face := range sculkDirections {
			if faces&(1<<i) != 0 && sculkCanAttach(region, pos, face) {
				mask |= 1 << uint(i)
			}
		}
		if mask == 0 {
			return false
		}
		idx := int(mask)
		if sculkWaterFluid(region, pos, nil) {
			idx |= 64
		}
		region.Set(pos, getSculkDefaults().vein[idx])
		return true
	}
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
	if region.isDefaultReg && properties == nil {
		ns := region.nativeState(pos)
		if ns.Liquid == region.airRID && ns.StateID == 0 {
			if ns.Block == region.airRID {
				return false
			}
			d := getSculkDefaults()
			if ns.Block == d.water.Block {
				return true
			}
			if featureMetaForDefaultRID(ns.Block)&featFlagExactAir != 0 {
				return false
			}
		}
	}
	return featureFluidName(region, pos, properties) == "water"
}

func sculkIsReplaceableWorldGen(region *featureRegion, pos featurePos) bool {
	if region.isDefaultReg {
		ns := region.nativeState(pos)
		if ns.Liquid == region.airRID && ns.StateID == 0 {
			return featureMetaForDefaultRID(ns.Block)&featFlagSculkReplaceableWG != 0
		}
	}
	return blockTagContains("sculk_replaceable_world_gen", normalizeResource(region.stateName(pos)))
}

func sculkIsReplaceable(region *featureRegion, pos featurePos) bool {
	if region.isDefaultReg {
		ns := region.nativeState(pos)
		if ns.Liquid == region.airRID && ns.StateID == 0 {
			return featureMetaForDefaultRID(ns.Block)&featFlagSculkReplaceable != 0
		}
	}
	return blockTagContains("sculk_replaceable", normalizeResource(region.stateName(pos)))
}

func sculkAttemptPlaceSculk(region *featureRegion, pos featurePos, faces uint8, random *vanilla.Random) bool {
	order := [6]int{0, 1, 2, 3, 4, 5}
	for i := 5; i > 0; i-- {
		j := int(random.NextInt(int32(i + 1)))
		order[i], order[j] = order[j], order[i]
	}
	var sculkState world.NativeState
	if region.isDefaultReg {
		sculkState = getSculkDefaults().sculk
	} else {
		sculkState = sculkResolveState(region, "sculk", nil)
	}
	for _, support := range order {
		if faces&(1<<uint(support)) == 0 {
			continue
		}
		supportPos := sculkOffsetIdx(pos, support)
		if !sculkIsReplaceableWorldGen(region, supportPos) {
			continue
		}
		if !region.Set(supportPos, sculkState) {
			continue
		}
		sculkSpreadAll(region, supportPos, false, 0)
		skip := support ^ 1
		for dir := range 6 {
			if dir == skip {
				continue
			}
			p := sculkOffsetIdx(supportPos, dir)
			kind, pFaces, pWL := sculkNodeState(region, p)
			if kind == sculkKindVein {
				sculkOnDischarged(region, p, pFaces, pWL)
			}
		}
		return true
	}
	return false
}

func sculkOnDischarged(region *featureRegion, pos featurePos, faces uint8, waterlogged bool) {
	kept := faces
	for i := range 6 {
		if faces&(1<<uint(i)) != 0 {
			kind, _, _ := sculkNodeState(region, sculkOffsetIdx(pos, i))
			if kind == sculkKindSculk {
				kept &^= 1 << uint(i)
			}
		}
	}
	if kept == 0 {
		if sculkWaterFluid(region, pos, nil) {
			if region.isDefaultReg {
				region.Set(pos, getSculkDefaults().water)
			} else {
				region.Set(pos, sculkResolveState(region, "water", nil))
			}
		} else if region.isDefaultReg {
			region.Set(pos, getSculkDefaults().air)
		} else {
			region.Set(pos, sculkResolveState(region, "air", nil))
		}
		return
	}
	if region.isDefaultReg {
		idx := int(kept)
		if waterlogged {
			idx |= 64
		}
		region.Set(pos, getSculkDefaults().vein[idx])
		return
	}
	props := make(map[string]any, 7)
	for i, d := range sculkDirections {
		props[d] = kept&(1<<uint(i)) != 0
	}
	if waterlogged {
		props["waterlogged"] = true
	}
	region.Set(pos, sculkResolveState(region, "sculk_vein", props))
}

func sculkMovementPos(region *featureRegion, pos, origin featurePos, random *vanilla.Random) (featurePos, uint8, uint8, bool) {
	offsets := sculkMovementOffsets
	for i := len(offsets) - 1; i > 0; i-- {
		j := int(random.NextInt(int32(i + 1)))
		offsets[i], offsets[j] = offsets[j], offsets[i]
	}
	last, lastKind, lastFaces := pos, sculkKindOther, uint8(0)
	moved := false
	for _, offset := range offsets {
		target := featurePos{pos.x + offset.x, pos.y + offset.y, pos.z + offset.z}
		if sculkDistanceXZ(target, origin) > 144 {
			continue
		}
		kind, faces, _ := sculkNodeState(region, target)
		if (kind != sculkKindSculk && kind != sculkKindVein) || !sculkMovementUnobstructed(region, pos, target) {
			continue
		}
		last, lastKind, lastFaces, moved = target, kind, faces, true
		if sculkHasSubstrate(region, target, kind, faces) {
			break
		}
	}
	return last, lastKind, lastFaces, moved
}

func sculkMovementUnobstructed(region *featureRegion, from, to featurePos) bool {
	dx, dy, dz := to.x-from.x, to.y-from.y, to.z-from.z
	if absFeatureInt(dx)+absFeatureInt(dy)+absFeatureInt(dz) == 1 {
		return true
	}
	check := func(dirIdx int) bool {
		return !sculkSturdyFaceAt(region, sculkOffsetIdx(from, dirIdx), dirIdx^1)
	}
	dirY := 0
	if dy >= 0 {
		dirY = 1
	}
	dirZ := 2
	if dz >= 0 {
		dirZ = 3
	}
	dirX := 4
	if dx >= 0 {
		dirX = 5
	}
	if dx == 0 {
		return check(dirY) || check(dirZ)
	}
	if dy == 0 {
		return check(dirX) || check(dirZ)
	}
	return check(dirX) || check(dirY)
}

func sculkHasSubstrate(region *featureRegion, pos featurePos, kind, faces uint8) bool {
	if kind != sculkKindVein {
		return false
	}
	for i := range 6 {
		if faces&(1<<uint(i)) != 0 && sculkIsReplaceable(region, sculkOffsetIdx(pos, i)) {
			return true
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

func sculkCanAttach(region *featureRegion, pos featurePos, face string) bool {
	idx := sculkDirectionIdx(face)
	against := sculkOffsetIdx(pos, idx)
	oppIdx := idx ^ 1
	if region.isDefaultReg {
		ns := region.nativeState(against)
		if ns.Liquid == region.airRID && ns.StateID == 0 {
			flags := featureMetaForDefaultRID(ns.Block)
			if flags&featFlagShapeOK != 0 {
				return flags&(1<<(featFlagAttachmentShift+uint(oppIdx))) != 0
			}
		}
	}
	return sculkShapeAt(region, against).Attachment&(1<<uint(oppIdx)) != 0
}

func sculkCollisionFull(region *featureRegion, pos featurePos) bool {
	if region.isDefaultReg {
		ns := region.nativeState(pos)
		if ns.Liquid == region.airRID && ns.StateID == 0 {
			flags := featureMetaForDefaultRID(ns.Block)
			if flags&featFlagShapeOK != 0 {
				return flags&featFlagCollisionFull != 0
			}
		}
	}
	return sculkShapeAt(region, pos).CollisionFull
}

// sculkShapeAt returns the generation shape of the state at pos, looked up by
// its state ID where it has one.
func sculkShapeAt(region *featureRegion, pos featurePos) gamedata.GenerationShape {
	if id := region.StateID(pos); id != 0 {
		return sculkShapeOf(region, id, "", nil)
	}
	name, props := region.State(pos)
	return sculkShapeOf(region, 0, name, props)
}

// sculkShapeOf returns the generation shape of state ID id, or of name and
// props for a state without an ID.
func sculkShapeOf(region *featureRegion, id uint32, name string, props map[string]any) gamedata.GenerationShape {
	var shape gamedata.GenerationShape
	var err error
	if id != 0 {
		shape, err = gamedata.BlockGenerationShapeID(id)
	} else {
		shape, err = gamedata.BlockGenerationShape(normalizeResource(name), props)
	}
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

func sculkDirectionIdx(d string) int {
	for i, v := range sculkDirections {
		if d == v {
			return i
		}
	}
	panic("overworld: invalid sculk direction")
}

func sculkOffsetIdx(p featurePos, dirIdx int) featurePos {
	switch dirIdx {
	case 0:
		p.y--
	case 1:
		p.y++
	case 2:
		p.z--
	case 3:
		p.z++
	case 4:
		p.x--
	case 5:
		p.x++
	default:
		panic("overworld: invalid sculk direction")
	}
	return p
}

func sculkDistanceSquared(a, b featurePos) int {
	dx, dy, dz := int(a.x-b.x), int(a.y-b.y), int(a.z-b.z)
	return dx*dx + dy*dy + dz*dz
}
func sculkDistanceXZ(a, b featurePos) int { dx, dz := int(a.x-b.x), int(a.z-b.z); return dx*dx + dz*dz }
func sculkChessboard(a, b featurePos) int {
	return max(int(absFeatureInt(a.x-b.x)), int(absFeatureInt(a.y-b.y)), int(absFeatureInt(a.z-b.z)))
}
