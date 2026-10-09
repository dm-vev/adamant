package end

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
	"github.com/go-gl/mathgl/mgl64"
)

type endCityRotation uint8

const (
	endCityRotNone endCityRotation = iota
	endCityRotClockwise90
	endCityRotClockwise180
	endCityRotCounterClockwise90
)

func (r endCityRotation) add(o endCityRotation) endCityRotation {
	return endCityRotation((uint8(r) + uint8(o)) & 3)
}
func (r endCityRotation) java() javastructure.Rotation {
	switch r {
	case endCityRotClockwise90:
		return javastructure.Clockwise90
	case endCityRotClockwise180:
		return javastructure.Clockwise180
	case endCityRotCounterClockwise90:
		return javastructure.CounterClockwise90
	default:
		return javastructure.None
	}
}

type endCityPos struct {
	x, y, z int
}

func (r endCityRotation) rotatePos(p endCityPos) endCityPos {
	switch r {
	case endCityRotCounterClockwise90:
		return endCityPos{x: p.z, y: p.y, z: -p.x}
	case endCityRotClockwise90:
		return endCityPos{x: -p.z, y: p.y, z: p.x}
	case endCityRotClockwise180:
		return endCityPos{x: -p.x, y: p.y, z: -p.z}
	default:
		return p
	}
}


type endCityBB struct {
	minX, minY, minZ int
	maxX, maxY, maxZ int
}

func (b endCityBB) intersects(o endCityBB) bool {
	return b.maxX >= o.minX && b.minX <= o.maxX && b.maxZ >= o.minZ && b.minZ <= o.maxZ && b.maxY >= o.minY && b.minY <= o.maxY
}

func (b endCityBB) intersectsXZ(minX, minZ, maxX, maxZ int) bool {
	return b.maxX >= minX && b.minX <= maxX && b.maxZ >= minZ && b.minZ <= maxZ
}

func (b *endCityBB) offset(x, y, z int) {
	b.minX += x
	b.minY += y
	b.minZ += z
	b.maxX += x
	b.maxY += y
	b.maxZ += z
}

func (b *endCityBB) expandTo(o endCityBB) {
	if o.minX < b.minX {
		b.minX = o.minX
	}
	if o.minY < b.minY {
		b.minY = o.minY
	}
	if o.minZ < b.minZ {
		b.minZ = o.minZ
	}
	if o.maxX > b.maxX {
		b.maxX = o.maxX
	}
	if o.maxY > b.maxY {
		b.maxY = o.maxY
	}
	if o.maxZ > b.maxZ {
		b.maxZ = o.maxZ
	}
}

type endCityPiece struct {
	pos       endCityPos
	rot       endCityRotation
	overwrite bool

	template      *endCityTemplate
	componentType int

	bb endCityBB
}

type endCityStructure struct {
	pieces          []*endCityPiece
	bb              endCityBB
}

type endCityBuilder struct {
	shipCreated bool
}

type endCityGenFunc func(b *endCityBuilder, g *End, depth int, parent *endCityPiece, offset *endCityPos, out *[]*endCityPiece, r *endRand) bool

func (b *endCityBuilder) recursiveChildren(g *End, gen endCityGenFunc, depth int, parent *endCityPiece, offset *endCityPos, pieces *[]*endCityPiece, r *endRand) bool {
	if depth > 8 {
		return false
	}
	local := make([]*endCityPiece, 0, 8)
	if !gen(b, g, depth, parent, offset, &local, r) {
		return false
	}

	ct := int(r.Int32())
	for _, p := range local {
		p.componentType = ct
		inter := endCityFindIntersecting(*pieces, p.bb)
		if inter != nil && inter.componentType != parent.componentType {
			return false
		}
	}
	*pieces = append(*pieces, local...)
	return true
}

func endCityFindIntersecting(pieces []*endCityPiece, bb endCityBB) *endCityPiece {
	for _, p := range pieces {
		if p != nil && p.bb.intersects(bb) {
			return p
		}
	}
	return nil
}

func (g *End) newEndCityPiece(name string, pos endCityPos, rot endCityRotation, overwrite bool) (*endCityPiece, error) {
	t, err := getEndCityTemplate(name)
	if err != nil {
		return nil, err
	}
	p := &endCityPiece{
		pos:           pos,
		rot:           rot,
		overwrite:     overwrite,
		template:      t,
		componentType: 0,
	}
	p.bb = endCityPieceBoundingBox(p, t, rot)
	return p, nil
}

func (g *End) addEndCityPiece(parent *endCityPiece, offset endCityPos, name string, rot endCityRotation, overwrite bool) (*endCityPiece, error) {
	p, err := g.newEndCityPiece(name, parent.pos, rot, overwrite)
	if err != nil {
		return nil, err
	}
	delta := parent.rot.rotatePos(offset)
	p.pos.x += delta.x
	p.pos.y += delta.y
	p.pos.z += delta.z
	p.bb = endCityPieceBoundingBox(p, p.template, p.rot)
	return p, nil
}

func endCityPieceBoundingBox(piece *endCityPiece, t *endCityTemplate, rot endCityRotation) endCityBB {
	sizeX, sizeY, sizeZ := t.Size[0], t.Size[1], t.Size[2]
	if rot == endCityRotClockwise90 || rot == endCityRotCounterClockwise90 {
		sizeX, sizeZ = sizeZ, sizeX
	}

	bb := endCityBB{minX: 0, minY: 0, minZ: 0, maxX: sizeX, maxY: sizeY - 1, maxZ: sizeZ}
	switch rot {
	case endCityRotClockwise90:
		bb.offset(-sizeX, 0, 0)
	case endCityRotCounterClockwise90:
		bb.offset(0, 0, -sizeZ)
	case endCityRotClockwise180:
		bb.offset(-sizeX, 0, -sizeZ)
	}
	bb.offset(piece.pos.x, piece.pos.y, piece.pos.z)
	return bb
}

func endCityHouseTowerGenerator(b *endCityBuilder, g *End, depth int, parent *endCityPiece, offset *endCityPos, out *[]*endCityPiece, r *endRand) bool {
	if depth > 8 {
		return false
	}
	attach := endCityPos{}
	if offset != nil {
		attach = *offset
	}

	rotation := parent.rot
	base, err := g.addEndCityPiece(parent, attach, "base_floor", rotation, true)
	if err != nil {
		return false
	}
	*out = append(*out, base)
	current := base

	switch int(r.Intn(3)) {
	case 0:
		roof, err := g.addEndCityPiece(current, endCityPos{-1, 4, -1}, "base_roof", rotation, true)
		if err != nil {
			return false
		}
		*out = append(*out, roof)
	case 1:
		second, err := g.addEndCityPiece(current, endCityPos{-1, 0, -1}, "second_floor_1", rotation, false)
		if err != nil {
			return false
		}
		*out = append(*out, second)
		current = second
		roof, err := g.addEndCityPiece(current, endCityPos{-1, 8, -1}, "second_roof", rotation, false)
		if err != nil {
			return false
		}
		*out = append(*out, roof)
		current = roof
		b.recursiveChildren(g, endCityTowerGenerator, depth+1, current, nil, out, r)
	case 2:
		second, err := g.addEndCityPiece(current, endCityPos{-1, 0, -1}, "second_floor_2", rotation, false)
		if err != nil {
			return false
		}
		*out = append(*out, second)
		current = second
		third, err := g.addEndCityPiece(current, endCityPos{-1, 4, -1}, "third_floor_2", rotation, false)
		if err != nil {
			return false
		}
		*out = append(*out, third)
		current = third
		roof, err := g.addEndCityPiece(current, endCityPos{-1, 8, -1}, "third_roof", rotation, true)
		if err != nil {
			return false
		}
		*out = append(*out, roof)
		current = roof
		b.recursiveChildren(g, endCityTowerGenerator, depth+1, current, nil, out, r)
	}

	return true
}

var endCityTowerBridges = []struct {
	rot endCityRotation
	pos endCityPos
}{
	{endCityRotNone, endCityPos{1, -1, 0}},
	{endCityRotClockwise90, endCityPos{6, -1, 1}},
	{endCityRotCounterClockwise90, endCityPos{0, -1, 5}},
	{endCityRotClockwise180, endCityPos{5, -1, 6}},
}

func endCityTowerGenerator(b *endCityBuilder, g *End, depth int, parent *endCityPiece, _ *endCityPos, out *[]*endCityPiece, r *endRand) bool {
	rotation := parent.rot

	baseOffset := endCityPos{3 + int(r.Intn(2)), -3, 3 + int(r.Intn(2))}
	base, err := g.addEndCityPiece(parent, baseOffset, "tower_base", rotation, true)
	if err != nil {
		return false
	}
	*out = append(*out, base)

	current := base
	piece, err := g.addEndCityPiece(current, endCityPos{0, 7, 0}, "tower_piece", rotation, true)
	if err != nil {
		return false
	}
	*out = append(*out, piece)
	current = piece

	var bridgeAnchor *endCityPiece
	if r.Intn(3) == 0 {
		bridgeAnchor = current
	}
	count := 1 + int(r.Intn(3))
	for i := 0; i < count; i++ {
		next, err := g.addEndCityPiece(current, endCityPos{0, 4, 0}, "tower_piece", rotation, true)
		if err != nil {
			return false
		}
		*out = append(*out, next)
		current = next

		if i < count-1 && r.Bool() {
			bridgeAnchor = current
		}
	}

	if bridgeAnchor != nil {
		for _, t := range endCityTowerBridges {
			if !r.Bool() {
				continue
			}
			bridgeEnd, err := g.addEndCityPiece(bridgeAnchor, t.pos, "bridge_end", rotation.add(t.rot), true)
			if err != nil {
				return false
			}
			*out = append(*out, bridgeEnd)
			b.recursiveChildren(g, endCityTowerBridgeGenerator, depth+1, bridgeEnd, nil, out, r)
		}

		top, err := g.addEndCityPiece(current, endCityPos{-1, 4, -1}, "tower_top", rotation, true)
		if err != nil {
			return false
		}
		*out = append(*out, top)
		return true
	}

	if depth != 7 {
		return b.recursiveChildren(g, endCityFatTowerGenerator, depth+1, current, nil, out, r)
	}
	top, err := g.addEndCityPiece(current, endCityPos{-1, 4, -1}, "tower_top", rotation, true)
	if err != nil {
		return false
	}
	*out = append(*out, top)
	return true
}

func endCityTowerBridgeGenerator(b *endCityBuilder, g *End, depth int, parent *endCityPiece, _ *endCityPos, out *[]*endCityPiece, r *endRand) bool {
	rotation := parent.rot
	count := int(r.Intn(4)) + 1

	bridge, err := g.addEndCityPiece(parent, endCityPos{0, 0, -4}, "bridge_piece", rotation, true)
	if err != nil {
		return false
	}
	bridge.componentType = -1
	*out = append(*out, bridge)

	current := bridge
	yOffset := 0

	for i := 0; i < count; i++ {
		if r.Bool() {
			next, err := g.addEndCityPiece(current, endCityPos{0, yOffset, -4}, "bridge_piece", rotation, true)
			if err != nil {
				return false
			}
			*out = append(*out, next)
			current = next
			yOffset = 0
			continue
		}

		if r.Bool() {
			next, err := g.addEndCityPiece(current, endCityPos{0, yOffset, -4}, "bridge_steep_stairs", rotation, true)
			if err != nil {
				return false
			}
			*out = append(*out, next)
			current = next
		} else {
			next, err := g.addEndCityPiece(current, endCityPos{0, yOffset, -8}, "bridge_gentle_stairs", rotation, true)
			if err != nil {
				return false
			}
			*out = append(*out, next)
			current = next
		}
		yOffset = 4
	}

	if !b.shipCreated && int(r.Intn(int32(10-depth))) == 0 {
		offset := endCityPos{-8 + int(r.Intn(8)), yOffset, -70 + int(r.Intn(10))}
		ship, err := g.addEndCityPiece(current, offset, "ship", rotation, true)
		if err != nil {
			return false
		}
		*out = append(*out, ship)
		b.shipCreated = true
	} else {
		houseOffset := endCityPos{-3, yOffset + 1, -11}
		if !b.recursiveChildren(g, endCityHouseTowerGenerator, depth+1, current, &houseOffset, out, r) {
			return false
		}
	}

	end, err := g.addEndCityPiece(current, endCityPos{4, yOffset, 0}, "bridge_end", rotation.add(endCityRotClockwise180), true)
	if err != nil {
		return false
	}
	end.componentType = -1
	*out = append(*out, end)
	return true
}

var endCityFatTowerBridges = []struct {
	rot endCityRotation
	pos endCityPos
}{
	{endCityRotNone, endCityPos{4, -1, 0}},
	{endCityRotClockwise90, endCityPos{12, -1, 4}},
	{endCityRotCounterClockwise90, endCityPos{0, -1, 8}},
	{endCityRotClockwise180, endCityPos{8, -1, 12}},
}

func endCityFatTowerGenerator(b *endCityBuilder, g *End, depth int, parent *endCityPiece, _ *endCityPos, out *[]*endCityPiece, r *endRand) bool {
	rotation := parent.rot

	base, err := g.addEndCityPiece(parent, endCityPos{-3, 4, -3}, "fat_tower_base", rotation, true)
	if err != nil {
		return false
	}
	*out = append(*out, base)
	current := base

	middle, err := g.addEndCityPiece(current, endCityPos{0, 4, 0}, "fat_tower_middle", rotation, true)
	if err != nil {
		return false
	}
	*out = append(*out, middle)
	current = middle

	for i := 0; i < 2 && r.Intn(3) != 0; i++ {
		next, err := g.addEndCityPiece(current, endCityPos{0, 8, 0}, "fat_tower_middle", rotation, true)
		if err != nil {
			return false
		}
		*out = append(*out, next)
		current = next

		for _, t := range endCityFatTowerBridges {
			if !r.Bool() {
				continue
			}
			bridgeEnd, err := g.addEndCityPiece(current, t.pos, "bridge_end", rotation.add(t.rot), true)
			if err != nil {
				return false
			}
			*out = append(*out, bridgeEnd)
			b.recursiveChildren(g, endCityTowerBridgeGenerator, depth+1, bridgeEnd, nil, out, r)
		}
	}

	top, err := g.addEndCityPiece(current, endCityPos{-2, 8, -2}, "fat_tower_top", rotation, true)
	if err != nil {
		return false
	}
	*out = append(*out, top)
	return true
}


func (g *End) endCityYPosForStructure(chunkX, chunkZ int, rotation endCityRotation) (int, error) {
	temp := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 255})

	s := g.pool.Get().(*endScratch)
	defer g.pool.Put(s)

	g.generateTerrain(chunkX, chunkZ, temp, s)

	i, j := 5, 5
	switch rotation {
	case endCityRotClockwise90:
		i = -5
	case endCityRotClockwise180:
		i, j = -5, -5
	case endCityRotCounterClockwise90:
		j = -5
	}

	h1 := int(temp.HighestBlock(7, 7))
	h2 := int(temp.HighestBlock(7, uint8(7+j)))
	h3 := int(temp.HighestBlock(uint8(7+i), 7))
	h4 := int(temp.HighestBlock(uint8(7+i), uint8(7+j)))
	return min(min(h1, h2), min(h3, h4)) + 1, nil
}

func (g *End) isEndCityStart(chunkX, chunkZ int) bool {
	return g.isIslandChunk(chunkX, chunkZ)
}

func (g *End) isIslandChunk(chunkX, chunkZ int) bool {
	distSq := int64(chunkX)*int64(chunkX) + int64(chunkZ)*int64(chunkZ)
	if distSq <= 4096 {
		return false
	}
	return g.islandHeightValue(chunkX, chunkZ, 1, 1) >= 0.0
}

func floorDiv(x, d int) int {
	if x >= 0 {
		return x / d
	}
	return -((-x + d - 1) / d)
}

func (g *End) endCityStartChunk(cellX, cellZ int) (int, int) {
	seed := int64(cellX)*341873128712 + int64(cellZ)*132897987541 + g.seed + 10387313
	r := newEndRand(seed)
	startX := cellX*20 + int((r.Intn(9)+r.Intn(9))/2)
	startZ := cellZ*20 + int((r.Intn(9)+r.Intn(9))/2)
	return startX, startZ
}

func (g *End) applyEndCityStructures(chunkX, chunkZ int, region *endFeatureRegion, publish bool, result *world.GenerationResult) {
	cellX, cellZ := floorDiv(chunkX, 20), floorDiv(chunkZ, 20)
	chunkMinX, chunkMinZ := chunkX<<4, chunkZ<<4
	chunkMaxX, chunkMaxZ := chunkMinX+15, chunkMinZ+15
	random := g.endCityWorldgenRandom(chunkX, chunkZ).random

	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			startX, startZ := g.endCityStartChunk(cellX+dx, cellZ+dz)
			if !g.isEndCityStart(startX, startZ) {
				continue
			}
			s, err := g.endCityStructure(startX, startZ)
			if err != nil {
				panic(err)
			}
			if s == nil || !s.bb.intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ) {
				continue
			}
			for _, p := range s.pieces {
				if p != nil && p.bb.intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ) {
					g.placeEndCityPiece(chunkX, chunkZ, region, p, &random, publish, result)
				}
			}
		}
	}
}

func (g *End) placeEndCityPiece(chunkX, chunkZ int, region *endFeatureRegion, p *endCityPiece, random *java26.Random, publish bool, result *world.GenerationResult) {
	c := region.chunks[region.index(chunkX, chunkZ)]
	origin := [3]int32{int32(p.pos.x), int32(p.pos.y), int32(p.pos.z)}
	seed := g.seed ^ int64(p.pos.x)*341873128712 ^ int64(p.pos.z)*132897987541
	blockPlacement, err := p.template.Java.PlaceBlocks(
		0,
		origin,
		p.rot.java(),
		javastructure.NoMirror,
		javastructure.Bounds{
			Min: [3]int32{int32(chunkX << 4), int32(c.Range().Min()), int32(chunkZ << 4)},
			Max: [3]int32{int32((chunkX << 4) + 15), int32(c.Range().Max()), int32((chunkZ << 4) + 15)},
		},
		seed,
	)
	if err != nil {
		panic(err)
	}
	registry := c.BlockRegistry()
	positions := make([]cube.Pos, 0, len(blockPlacement.Blocks))
	access := endJavaShapeAccess{
		endFeatureOrigin: endFeatureOrigin{region: region, chunkX: chunkX, chunkZ: chunkZ},
		result: result, random: region.sourceShapeRandom(g.seed, chunkX, chunkZ),
	}
	for _, placed := range blockPlacement.Blocks {
		if placed.Pos[0]>>4 != int32(chunkX) || placed.Pos[2]>>4 != int32(chunkZ) {
			continue
		}
		if placed.State.Name == "minecraft:structure_block" || !p.overwrite && placed.State.Name == "minecraft:air" {
			continue
		}
		state, err := placed.ResolveNative(registry)
		if err != nil {
			panic(err)
		}
		if region.setLayers(int(placed.Pos[0]), int(placed.Pos[1]), int(placed.Pos[2]), state) {
			pos := cube.Pos{int(placed.Pos[0]), int(placed.Pos[1]), int(placed.Pos[2])}
			positions = append(positions, pos)
			if actor, exists := region.blockActors[pos]; exists && actor.record.State.Name != placed.State.Name {
				delete(region.blockActors, pos)
				access.removeBlockEntity(pos)
			}
			javastructure.SeedRandomizableContainerLoot(&placed, random)
			access.recordBlockActor(placed)
			access.markPostProcessing(pos, state.CanonicalJavaID(registry))
		}
	}
	world.UpdateJavaTemplateShapes(access, positions, 2)
	g.placeEndCityDataMarkers(chunkX, chunkZ, region, blockPlacement.Blocks, p, random, publish, result)
}



func (g *End) placeEndCityDataMarkers(chunkX, chunkZ int, region *endFeatureRegion, blocks []javastructure.Block, p *endCityPiece, random *java26.Random, publish bool, result *world.GenerationResult) {
	c := region.chunks[region.index(chunkX, chunkZ)]
	for _, marker := range blocks {
		if marker.State.Name != "minecraft:structure_block" || marker.NBT == nil {
			continue
		}
		mode, _ := marker.NBT["mode"].(string)
		if !strings.EqualFold(mode, "DATA") {
			continue
		}
		metadata, _ := marker.NBT["metadata"].(string)
		switch {
		case strings.HasPrefix(metadata, "Chest"):
			chestPos := [3]int32{marker.Pos[0], marker.Pos[1] - 1, marker.Pos[2]}
			if !endCityPosInChunk(chestPos, chunkX, chunkZ, c) {
				continue
			}
			pos := cube.Pos{int(chestPos[0]), int(chestPos[1]), int(chestPos[2])}
			actor, exists := region.blockActors[pos]
			if !exists || actor.record.State.Name != "minecraft:chest" && actor.record.State.Name != "minecraft:trapped_chest" {
				continue
			}
			actor.record.NBT["LootTable"] = "minecraft:chests/end_city_treasure"
			actor.record.NBT["LootTableSeed"] = random.NextLong()
			access := endJavaShapeAccess{
				endFeatureOrigin: endFeatureOrigin{region: region, chunkX: chunkX, chunkZ: chunkZ},
				result: result,
			}
			access.recordBlockActor(actor.record)
		case strings.HasPrefix(metadata, "Sentry"):
			if !endCityPosInChunk(marker.Pos, chunkX, chunkZ, c) {
				continue
			}
			if !publish {
				continue
			}
			result.Entities = append(result.Entities, entity.NewShulker(world.EntitySpawnOpts{
				Position: mgl64.Vec3{float64(marker.Pos[0]) + 0.5, float64(marker.Pos[1]), float64(marker.Pos[2]) + 0.5},
			}, entity.ShulkerBehaviourConfig{}))
		case strings.HasPrefix(metadata, "Elytra"):
			if !endCityPosInChunk(marker.Pos, chunkX, chunkZ, c) {
				continue
			}
			outward := javastructure.TransformPosition([3]int32{0, 0, 1}, javastructure.NoMirror, p.rot.java())
			var facing cube.Face
			switch {
			case outward[0] > 0:
				facing = cube.FaceEast
			case outward[0] < 0:
				facing = cube.FaceWest
			case outward[2] > 0:
				facing = cube.FaceSouth
			default:
				facing = cube.FaceNorth
			}
			frame := block.ItemFrame{
				Facing:    facing.Opposite(),
				Item:      item.NewStack(item.Elytra{}, 1),
				DropChance: 1,
			}
			pos := cube.Pos{int(marker.Pos[0]), int(marker.Pos[1]), int(marker.Pos[2])}
			logicalState := region.nativeState(pos[0], pos[1], pos[2])
			rid := world.BlockRuntimeID(frame)
			if region.setLayers(pos[0], pos[1], pos[2], world.NativeState{Block: rid, Liquid: c.BlockRegistry().AirRuntimeID()}) {
				region.logicalStates[pos] = logicalState
			}
			if !publish {
				continue
			}
			data := frame.EncodeNBT()
			data["x"], data["y"], data["z"] = marker.Pos[0], marker.Pos[1], marker.Pos[2]
			result.BlockEntities = append(result.BlockEntities, chunk.BlockEntity{
				Pos:  cube.Pos{int(marker.Pos[0]), int(marker.Pos[1]), int(marker.Pos[2])},
				Data: data,
			})
		}
	}
}

func endCityPosInChunk(pos [3]int32, chunkX, chunkZ int, c *chunk.Chunk) bool {
	return pos[0]>>4 == int32(chunkX) && pos[2]>>4 == int32(chunkZ) &&
		pos[1] >= int32(c.Range().Min()) && pos[1] <= int32(c.Range().Max())
}


func (g *End) endCityRotation(random *endRand) endCityRotation {
	return endCityRotation(random.Intn(4))
}
func (g *End) endCityStructure(startChunkX, startChunkZ int) (*endCityStructure, error) {
	pos := world.ChunkPos{int32(startChunkX), int32(startChunkZ)}
	if v, ok := g.endCityCache.Load(pos); ok {
		if v == nil {
			return nil, nil
		}
		if s, ok := v.(*endCityStructure); ok {
			return s, nil
		}
		return nil, nil
	}

	if !g.isIslandChunk(startChunkX, startChunkZ) {
		g.endCityCache.Store(pos, (*endCityStructure)(nil))
		return nil, nil
	}

	structureRandom := g.endCityStartRand(startChunkX, startChunkZ)
	rotation := g.endCityRotation(structureRandom)
	y, err := g.endCityYPosForStructure(startChunkX, startChunkZ, rotation)
	if err != nil {
		g.endCityCache.Store(pos, (*endCityStructure)(nil))
		return nil, err
	}
	if y < 60 {
		g.endCityCache.Store(pos, (*endCityStructure)(nil))
		return nil, nil
	}

	pieces := make([]*endCityPiece, 0, 32)
	builder := &endCityBuilder{}
	builder.shipCreated = false

	start := endCityPos{x: startChunkX*16 + 7, y: y, z: startChunkZ*16 + 7}
	base, err := g.newEndCityPiece("base_floor", start, rotation, true)
	if err != nil {
		g.endCityCache.Store(pos, (*endCityStructure)(nil))
		return nil, err
	}
	pieces = append(pieces, base)
	current := base

	second, err := g.addEndCityPiece(current, endCityPos{-1, 0, -1}, "second_floor_1", rotation, false)
	if err != nil {
		g.endCityCache.Store(pos, (*endCityStructure)(nil))
		return nil, err
	}
	pieces = append(pieces, second)
	current = second

	third, err := g.addEndCityPiece(current, endCityPos{-1, 4, -1}, "third_floor_1", rotation, false)
	if err != nil {
		g.endCityCache.Store(pos, (*endCityStructure)(nil))
		return nil, err
	}
	pieces = append(pieces, third)
	current = third

	roof, err := g.addEndCityPiece(current, endCityPos{-1, 8, -1}, "third_roof", rotation, true)
	if err != nil {
		g.endCityCache.Store(pos, (*endCityStructure)(nil))
		return nil, err
	}
	pieces = append(pieces, roof)
	current = roof

	builder.recursiveChildren(g, endCityTowerGenerator, 1, current, nil, &pieces, structureRandom)

	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	union := endCityBB{minX: maxInt, minY: maxInt, minZ: maxInt, maxX: minInt, maxY: minInt, maxZ: minInt}
	for _, p := range pieces {
		if p != nil {
			union.expandTo(p.bb)
		}
	}

	s := &endCityStructure{pieces: pieces, bb: union}
	g.endCityCache.Store(pos, s)
	return s, nil
}


func (g *End) endCityWorldgenRandom(chunkX, chunkZ int) *endRand {
	random := java26.NewXoroshiroRandom(g.seed)
	decorationSeed := random.SetDecorationSeed(g.seed, int32(chunkX<<4), int32(chunkZ<<4))
	// 22 is End City's registry-order index in the 26.3 surface_structures step.
	random.SetFeatureSeed(decorationSeed, 22, 4)
	return newEndRandWithRandom(random)
}

func (g *End) endCityStartRand(chunkX, chunkZ int) *endRand {
	random := newEndRand(g.seed)
	xScale, zScale := random.Long(), random.Long()
	seed := int64(chunkX)*xScale ^ int64(chunkZ)*zScale ^ g.seed
	return newEndRand(seed)
}
