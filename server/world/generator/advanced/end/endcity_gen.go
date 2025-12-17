package end

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
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

func rotateHorizontalDirection(d cube.Direction, r endCityRotation) cube.Direction {
	switch r {
	case endCityRotClockwise90:
		return d.RotateRight()
	case endCityRotCounterClockwise90:
		return d.RotateLeft()
	case endCityRotClockwise180:
		return d.Opposite()
	default:
		return d
	}
}

func rotateAxis(a cube.Axis, r endCityRotation) cube.Axis {
	switch r {
	case endCityRotClockwise90, endCityRotCounterClockwise90:
		if a == cube.X {
			return cube.Z
		}
		if a == cube.Z {
			return cube.X
		}
	}
	return a
}

func rotateFace(f cube.Face, r endCityRotation) cube.Face {
	if f == cube.FaceUp || f == cube.FaceDown {
		return f
	}
	d := cube.North
	switch f {
	case cube.FaceNorth:
		d = cube.North
	case cube.FaceSouth:
		d = cube.South
	case cube.FaceWest:
		d = cube.West
	case cube.FaceEast:
		d = cube.East
	}
	d = rotateHorizontalDirection(d, r)
	switch d {
	case cube.South:
		return cube.FaceSouth
	case cube.West:
		return cube.FaceWest
	case cube.East:
		return cube.FaceEast
	default:
		return cube.FaceNorth
	}
}

func parseHorizontalDirection(s string) (cube.Direction, bool) {
	switch s {
	case "north":
		return cube.North, true
	case "south":
		return cube.South, true
	case "west":
		return cube.West, true
	case "east":
		return cube.East, true
	default:
		return cube.North, false
	}
}

func parseAxisString(s string) (cube.Axis, bool) {
	switch s {
	case "x":
		return cube.X, true
	case "y":
		return cube.Y, true
	case "z":
		return cube.Z, true
	default:
		return cube.Y, false
	}
}

func parseFaceString(s string) (cube.Face, bool) {
	switch s {
	case "up":
		return cube.FaceUp, true
	case "down":
		return cube.FaceDown, true
	case "north":
		return cube.FaceNorth, true
	case "south":
		return cube.FaceSouth, true
	case "west":
		return cube.FaceWest, true
	case "east":
		return cube.FaceEast, true
	default:
		return cube.FaceUp, false
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
	name      string
	pos       endCityPos
	rot       endCityRotation
	overwrite bool

	template      *endCityTemplate
	stateRIDs     []uint32
	componentType int

	bb endCityBB
}

type endCityStructure struct {
	start  world.ChunkPos
	pieces []*endCityPiece
	bb     endCityBB
}

type endCityBuilder struct {
	shipCreated bool
}

type endCityGenFunc func(b *endCityBuilder, g *End, depth int, parent *endCityPiece, offset *endCityPos, out *[]*endCityPiece, r *mc112.Rand) bool

func (b *endCityBuilder) recursiveChildren(g *End, gen endCityGenFunc, depth int, parent *endCityPiece, offset *endCityPos, pieces *[]*endCityPiece, r *mc112.Rand) bool {
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
		name:          name,
		pos:           pos,
		rot:           rot,
		overwrite:     overwrite,
		template:      t,
		componentType: 0,
	}
	p.stateRIDs, err = g.endCityStateRIDs(t, rot)
	if err != nil {
		return nil, err
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

func endCityHouseTowerGenerator(b *endCityBuilder, g *End, depth int, parent *endCityPiece, offset *endCityPos, out *[]*endCityPiece, r *mc112.Rand) bool {
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
		second, err := g.addEndCityPiece(current, endCityPos{-1, 0, -1}, "second_floor_2", rotation, false)
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
		third, err := g.addEndCityPiece(current, endCityPos{-1, 4, -1}, "third_floor_c", rotation, false)
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

func endCityTowerGenerator(b *endCityBuilder, g *End, depth int, parent *endCityPiece, _ *endCityPos, out *[]*endCityPiece, r *mc112.Rand) bool {
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

func endCityTowerBridgeGenerator(b *endCityBuilder, g *End, depth int, parent *endCityPiece, _ *endCityPos, out *[]*endCityPiece, r *mc112.Rand) bool {
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

func endCityFatTowerGenerator(b *endCityBuilder, g *End, depth int, parent *endCityPiece, _ *endCityPos, out *[]*endCityPiece, r *mc112.Rand) bool {
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

func (g *End) endCityRotation(chunkX, chunkZ int) endCityRotation {
	seed := int64(int32(chunkX) + int32(chunkZ)*int32(10387313))
	r := mc112.NewRand(seed)
	return endCityRotation(r.Intn(4))
}

func (g *End) endCityYPosForStructure(chunkX, chunkZ int, rotation endCityRotation) (int, error) {
	temp := chunk.New(g.airRID, cube.Range{0, 255})

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
	return min(min(h1, h2), min(h3, h4)), nil
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
	r := mc112.NewRand(seed)
	startX := cellX*20 + int((r.Intn(9)+r.Intn(9))/2)
	startZ := cellZ*20 + int((r.Intn(9)+r.Intn(9))/2)
	return startX, startZ
}

func (g *End) applyEndCityStructures(chunkX, chunkZ int, c *chunk.Chunk) {
	cellX, cellZ := floorDiv(chunkX, 20), floorDiv(chunkZ, 20)
	chunkMinX, chunkMinZ := chunkX<<4, chunkZ<<4
	chunkMaxX, chunkMaxZ := chunkMinX+15, chunkMinZ+15

	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			startX, startZ := g.endCityStartChunk(cellX+dx, cellZ+dz)
			if !g.isEndCityStart(startX, startZ) {
				continue
			}
			s, err := g.endCityStructure(startX, startZ)
			if err != nil || s == nil {
				continue
			}
			if !s.bb.intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ) {
				continue
			}
			for _, p := range s.pieces {
				if p == nil || !p.bb.intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ) {
					continue
				}
				g.placeEndCityPiece(chunkX, chunkZ, c, p)
			}
		}
	}
}

func (g *End) placeEndCityPiece(chunkX, chunkZ int, c *chunk.Chunk, p *endCityPiece) {
	for _, b := range p.template.Blocks {
		if b.State < 0 || b.State >= len(p.template.States) {
			continue
		}
		state := p.template.States[b.State]
		if state.Name == "minecraft:structure_block" {
			continue
		}
		if !p.overwrite && state.Name == "minecraft:air" {
			continue
		}

		local := endCityPos{x: b.Pos[0], y: b.Pos[1], z: b.Pos[2]}
		delta := p.rot.rotatePos(local)
		wx := p.pos.x + delta.x
		wy := p.pos.y + delta.y
		wz := p.pos.z + delta.z

		if wx>>4 != chunkX || wz>>4 != chunkZ {
			continue
		}
		if int16(wy) < int16(c.Range().Min()) || int16(wy) > int16(c.Range().Max()) {
			continue
		}

		rid := p.stateRIDs[b.State]
		if rid == endCityDynamicRID {
			rid = g.endCityDynamicRID(state, b.NBT, p.rot)
		}
		c.SetBlock(uint8(wx&15), int16(wy), uint8(wz&15), 0, rid)
	}
}

const endCityDynamicRID = ^uint32(0)

func (g *End) endCityDynamicRID(state endCityState, nbt map[string]any, rot endCityRotation) uint32 {
	switch state.Name {
	case "minecraft:skull":
		return g.endCitySkullRID(state, nbt, rot)
	default:
		return g.airRID
	}
}

func (g *End) endCitySkullRID(state endCityState, nbt map[string]any, rot endCityRotation) uint32 {
	skullType := uint8(0)
	if nbt != nil {
		if raw, ok := nbt["SkullType"]; ok {
			switch v := raw.(type) {
			case int8:
				skullType = uint8(v)
			case uint8:
				skullType = v
			case int32:
				skullType = uint8(v)
			}
		}
	}
	blockName := "skeleton_skull"
	switch skullType {
	case 1:
		blockName = "wither_skeleton_skull"
	case 2:
		blockName = "zombie_head"
	case 3:
		blockName = "player_head"
	case 4:
		blockName = "creeper_head"
	case 5:
		blockName = "dragon_head"
	case 6:
		blockName = "piglin_head"
	}

	facing := ""
	if state.Properties != nil {
		facing = state.Properties["facing"]
	}
	if facing == "up" {
		return mustStateRID("minecraft:"+blockName, map[string]any{"facing_direction": int32(1)})
	}
	dir, _ := parseHorizontalDirection(facing)
	dir = rotateHorizontalDirection(dir, rot)
	return mustStateRID("minecraft:"+blockName, map[string]any{"facing_direction": int32(int(dir) + 2)})
}

func (g *End) endCityStateRIDs(t *endCityTemplate, rot endCityRotation) ([]uint32, error) {
	out := make([]uint32, len(t.States))
	for i, s := range t.States {
		switch s.Name {
		case "minecraft:structure_block":
			out[i] = g.airRID
			continue
		case "minecraft:skull":
			out[i] = endCityDynamicRID
			continue
		}

		rid, ok := g.endCityRuntimeID(s, rot)
		if !ok {
			return nil, fmt.Errorf("unsupported end city state %q", s.Name)
		}
		out[i] = rid
	}
	return out, nil
}

func (g *End) endCityRuntimeID(s endCityState, rot endCityRotation) (uint32, bool) {
	switch s.Name {
	case "minecraft:air":
		return g.airRID, true
	case "minecraft:end_bricks":
		return world.BlockRuntimeID(block.EndBricks{}), true
	case "minecraft:obsidian":
		return g.obsidianRID, true
	case "minecraft:purpur_block":
		return world.BlockRuntimeID(block.Purpur{}), true
	case "minecraft:purpur_pillar":
		axisStr := ""
		if s.Properties != nil {
			axisStr = s.Properties["axis"]
		}
		axis, _ := parseAxisString(axisStr)
		axis = rotateAxis(axis, rot)
		return mustStateRID("minecraft:purpur_pillar", map[string]any{"pillar_axis": axis.String()}), true
	case "minecraft:purpur_slab":
		typ := "bottom"
		if s.Properties != nil {
			if v := s.Properties["type"]; v != "" {
				typ = v
			}
		}
		name := "minecraft:purpur_slab"
		if typ == "double" {
			name = "minecraft:purpur_double_slab"
			typ = "bottom"
		}
		return mustStateRID(name, map[string]any{"minecraft:vertical_half": typ}), true
	case "minecraft:purpur_stairs":
		half := "bottom"
		facing := "north"
		if s.Properties != nil {
			if v := s.Properties["half"]; v != "" {
				half = v
			}
			if v := s.Properties["facing"]; v != "" {
				facing = v
			}
		}
		dir, _ := parseHorizontalDirection(facing)
		dir = rotateHorizontalDirection(dir, rot)
		upsideDown := half == "top"
		weirdo := int32(3 - int32(dir))
		return mustStateRID("minecraft:purpur_stairs", map[string]any{"upside_down_bit": upsideDown, "weirdo_direction": weirdo}), true
	case "minecraft:ladder":
		facing := "north"
		if s.Properties != nil {
			if v := s.Properties["facing"]; v != "" {
				facing = v
			}
		}
		dir, _ := parseHorizontalDirection(facing)
		dir = rotateHorizontalDirection(dir, rot)
		return mustStateRID("minecraft:ladder", map[string]any{"facing_direction": int32(int(dir) + 2)}), true
	case "minecraft:end_rod":
		facing := "up"
		if s.Properties != nil {
			if v := s.Properties["facing"]; v != "" {
				facing = v
			}
		}
		face, _ := parseFaceString(facing)
		face = rotateFace(face, rot)
		facingDir := face
		if face.Axis() != cube.Y {
			facingDir = face.Opposite()
		}
		return mustStateRID("minecraft:end_rod", map[string]any{"facing_direction": int32(facingDir)}), true
	case "minecraft:chest":
		facing := "north"
		if s.Properties != nil {
			if v := s.Properties["facing"]; v != "" {
				facing = v
			}
		}
		dir, _ := parseHorizontalDirection(facing)
		dir = rotateHorizontalDirection(dir, rot)
		return mustStateRID("minecraft:chest", map[string]any{"minecraft:cardinal_direction": dir.String()}), true
	case "minecraft:ender_chest":
		facing := "north"
		if s.Properties != nil {
			if v := s.Properties["facing"]; v != "" {
				facing = v
			}
		}
		dir, _ := parseHorizontalDirection(facing)
		dir = rotateHorizontalDirection(dir, rot)
		return mustStateRID("minecraft:ender_chest", map[string]any{"minecraft:cardinal_direction": dir.String()}), true
	case "minecraft:brewing_stand":
		var a, bBit, cBit bool
		if s.Properties != nil {
			a = s.Properties["has_bottle_0"] == "true"
			bBit = s.Properties["has_bottle_1"] == "true"
			cBit = s.Properties["has_bottle_2"] == "true"
		}
		return mustStateRID("minecraft:brewing_stand", map[string]any{
			"brewing_stand_slot_a_bit": a,
			"brewing_stand_slot_b_bit": bBit,
			"brewing_stand_slot_c_bit": cBit,
		}), true
	case "minecraft:stained_glass":
		colour := "white"
		if s.Properties != nil {
			if v := s.Properties["color"]; v != "" {
				colour = v
			}
		}
		return mustStateRID("minecraft:"+colour+"_stained_glass", nil), true
	case "minecraft:wall_banner":
		facing := "north"
		if s.Properties != nil {
			if v := s.Properties["facing"]; v != "" {
				facing = v
			}
		}
		dir, _ := parseHorizontalDirection(facing)
		dir = rotateHorizontalDirection(dir, rot)
		return mustStateRID("minecraft:wall_banner", map[string]any{"facing_direction": int32(int(dir) + 2)}), true
	default:
		return 0, false
	}
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

	rotation := g.endCityRotation(startChunkX, startChunkZ)
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

	start := endCityPos{x: startChunkX*16 + 8, y: y, z: startChunkZ*16 + 8}
	base, err := g.newEndCityPiece("base_floor", start, rotation, true)
	if err != nil {
		g.endCityCache.Store(pos, (*endCityStructure)(nil))
		return nil, err
	}
	pieces = append(pieces, base)
	current := base

	second, err := g.addEndCityPiece(current, endCityPos{-1, 0, -1}, "second_floor", rotation, false)
	if err != nil {
		g.endCityCache.Store(pos, (*endCityStructure)(nil))
		return nil, err
	}
	pieces = append(pieces, second)
	current = second

	third, err := g.addEndCityPiece(current, endCityPos{-1, 4, -1}, "third_floor", rotation, false)
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

	startRand := g.endCityStartRand(startChunkX, startChunkZ)
	builder.recursiveChildren(g, endCityTowerGenerator, 1, current, nil, &pieces, startRand)

	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	union := endCityBB{minX: maxInt, minY: maxInt, minZ: maxInt, maxX: minInt, maxY: minInt, maxZ: minInt}
	for _, p := range pieces {
		if p != nil {
			union.expandTo(p.bb)
		}
	}

	s := &endCityStructure{start: pos, pieces: pieces, bb: union}
	g.endCityCache.Store(pos, s)
	return s, nil
}

func (g *End) endCityStartRand(chunkX, chunkZ int) *mc112.Rand {
	j, k := g.endCityRandConstants()
	seed := int64(chunkX)*j ^ int64(chunkZ)*k ^ g.seed
	r := mc112.NewRand(seed)
	r.Int32()
	return r
}

func (g *End) endCityRandConstants() (int64, int64) {
	g.endCityRandOnce.Do(func() {
		r := mc112.NewRand(g.seed)
		g.endCityRandJ = r.Long()
		g.endCityRandK = r.Long()
	})
	return g.endCityRandJ, g.endCityRandK
}
