package nether

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type structureBB struct {
	minX, minY, minZ int
	maxX, maxY, maxZ int
}

func (bb structureBB) intersects(o structureBB) bool {
	return bb.maxX >= o.minX && bb.minX <= o.maxX && bb.maxZ >= o.minZ && bb.minZ <= o.maxZ && bb.maxY >= o.minY && bb.minY <= o.maxY
}

func (bb structureBB) intersectsXZ(minX, minZ, maxX, maxZ int) bool {
	return bb.maxX >= minX && bb.minX <= maxX && bb.maxZ >= minZ && bb.minZ <= maxZ
}

func (bb structureBB) isVecInside(x, y, z int) bool {
	return x >= bb.minX && x <= bb.maxX && z >= bb.minZ && z <= bb.maxZ && y >= bb.minY && y <= bb.maxY
}

func (bb *structureBB) expandTo(o structureBB) {
	if o.minX < bb.minX {
		bb.minX = o.minX
	}
	if o.minY < bb.minY {
		bb.minY = o.minY
	}
	if o.minZ < bb.minZ {
		bb.minZ = o.minZ
	}
	if o.maxX > bb.maxX {
		bb.maxX = o.maxX
	}
	if o.maxY > bb.maxY {
		bb.maxY = o.maxY
	}
	if o.maxZ > bb.maxZ {
		bb.maxZ = o.maxZ
	}
}

func (bb *structureBB) offset(x, y, z int) {
	bb.minX += x
	bb.minY += y
	bb.minZ += z
	bb.maxX += x
	bb.maxY += y
	bb.maxZ += z
}

func (bb structureBB) xSize() int { return bb.maxX - bb.minX + 1 }
func (bb structureBB) ySize() int { return bb.maxY - bb.minY + 1 }
func (bb structureBB) zSize() int { return bb.maxZ - bb.minZ + 1 }

func componentBB(structMinX, structMinY, structMinZ, xMin, yMin, zMin, xMax, yMax, zMax int, facing cube.Direction) structureBB {
	switch facing {
	case cube.North:
		return structureBB{
			minX: structMinX + xMin,
			minY: structMinY + yMin,
			minZ: structMinZ - zMax + 1 + zMin,
			maxX: structMinX + xMax - 1 + xMin,
			maxY: structMinY + yMax - 1 + yMin,
			maxZ: structMinZ + zMin,
		}
	case cube.South:
		return structureBB{
			minX: structMinX + xMin,
			minY: structMinY + yMin,
			minZ: structMinZ + zMin,
			maxX: structMinX + xMax - 1 + xMin,
			maxY: structMinY + yMax - 1 + yMin,
			maxZ: structMinZ + zMax - 1 + zMin,
		}
	case cube.West:
		return structureBB{
			minX: structMinX - zMax + 1 + zMin,
			minY: structMinY + yMin,
			minZ: structMinZ + xMin,
			maxX: structMinX + zMin,
			maxY: structMinY + yMax - 1 + yMin,
			maxZ: structMinZ + xMax - 1 + xMin,
		}
	case cube.East:
		return structureBB{
			minX: structMinX + zMin,
			minY: structMinY + yMin,
			minZ: structMinZ + xMin,
			maxX: structMinX + zMax - 1 + zMin,
			maxY: structMinY + yMax - 1 + yMin,
			maxZ: structMinZ + xMax - 1 + xMin,
		}
	default:
		return structureBB{
			minX: structMinX + xMin,
			minY: structMinY + yMin,
			minZ: structMinZ + zMin,
			maxX: structMinX + xMax - 1 + xMin,
			maxY: structMinY + yMax - 1 + yMin,
			maxZ: structMinZ + zMax - 1 + zMin,
		}
	}
}

type fortressRegion interface {
	Block(x uint8, y int16, z uint8, layer uint8) uint32
	SetNativeState(x uint8, y int16, z uint8, state world.NativeState)
	Range() cube.Range
	BlockRegistry() chunk.BlockRegistry
}

type journalFortressRegion struct {
	chunkX, chunkZ int
	env            javastructure.ProcessorEnvironment
	registry       world.BlockRegistry
	writes         map[cube.Pos]uint32
}

func (r *journalFortressRegion) Range() cube.Range                  { return cube.Range{0, 127} }
func (r *journalFortressRegion) BlockRegistry() chunk.BlockRegistry { return r.registry }

func (r *journalFortressRegion) Block(x uint8, y int16, z uint8, _ uint8) uint32 {
	worldX, worldZ := r.chunkX*16+int(x), r.chunkZ*16+int(z)
	pos := cube.Pos{worldX, int(y), worldZ}
	if rid, ok := r.writes[pos]; ok {
		return rid
	}
	block := r.env.BaseBlock(int32(worldX), int32(y), int32(worldZ))
	if block == nil {
		return r.registry.AirRuntimeID()
	}
	return r.registry.BlockRuntimeID(block)
}

func (r *journalFortressRegion) SetNativeState(x uint8, y int16, z uint8, state world.NativeState) {
	r.writes[cube.Pos{r.chunkX*16 + int(x), int(y), r.chunkZ*16 + int(z)}] = state.Block
}

func setBlockWorld(c fortressRegion, chunkX, chunkZ, worldX, worldY, worldZ int, rid uint32, placement *javastructure.Placement) {
	writeFortressBlockWorld(c, chunkX, chunkZ, worldX, worldY, worldZ, rid, placement, true)
}

func writeFortressBlockWorld(c fortressRegion, chunkX, chunkZ, worldX, worldY, worldZ int, rid uint32, placement *javastructure.Placement, blockUpdates bool) {
	if worldX>>4 != chunkX || worldZ>>4 != chunkZ {
		return
	}
	yy := int16(worldY)
	if yy < int16(c.Range().Min()) || yy > int16(c.Range().Max()) {
		return
	}
	x, z := uint8(worldX&15), uint8(worldZ&15)
	c.SetNativeState(x, yy, z, world.NativeState{Block: rid, Liquid: c.BlockRegistry().AirRuntimeID()})
	if placement == nil {
		return
	}
	pos := cube.Pos{worldX, worldY, worldZ}
	state, err := world.JavaState(c.BlockRegistry(), world.NativeState{Block: rid, Liquid: c.BlockRegistry().AirRuntimeID()})
	if err != nil {
		panic(err)
	}
	placement.Blocks = append(placement.Blocks, javastructure.Block{Pos: [3]int32{int32(worldX), int32(worldY), int32(worldZ)}, State: state})
	if !blockUpdates {
		return
	}
	registry, ok := c.BlockRegistry().(world.BlockRegistry)
	if !ok {
		panic("nether: fortress registry cannot resolve native blocks")
	}
	native := world.NativeState{Block: rid, Liquid: c.BlockRegistry().AirRuntimeID()}
	for _, fluidRID := range [2]uint32{native.Block, native.Liquid} {
		if !registry.LiquidBlock(fluidRID) {
			continue
		}
		fluidBlock, ok := registry.BlockByRuntimeID(fluidRID)
		if !ok {
			panic("nether: unknown fortress fluid runtime ID")
		}
		fluid, ok := fluidBlock.(world.Liquid)
		if !ok {
			panic("nether: fortress liquid block has no fluid type")
		}
		typeName := fluid.LiquidType()
		if !strings.Contains(typeName, ":") {
			typeName = "minecraft:" + typeName
		}
		placement.ScheduledTicks = append(placement.ScheduledTicks, chunk.ScheduledTick{Pos: pos, Kind: chunk.FluidTick, Type: typeName})
		break
	}
	name, _, ok := registry.RuntimeIDToState(rid)
	if ok && fortressShapeCheckBlock(name) {
		placement.PostProcessing = append(placement.PostProcessing, pos)
	}
}

func fortressShapeCheckBlock(name string) bool {
	switch name {
	case "minecraft:nether_brick_fence", "minecraft:torch", "minecraft:wall_torch",
		"minecraft:oak_fence", "minecraft:spruce_fence", "minecraft:dark_oak_fence", "minecraft:pale_oak_fence",
		"minecraft:acacia_fence", "minecraft:birch_fence", "minecraft:jungle_fence", "minecraft:ladder", "minecraft:iron_bars":
		return true
	default:
		return false
	}
}

type bridgePiece interface {
	pieceBase() *bridgePieceBase
	buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random)
	addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool
}

type bridgePieceBase struct {
	bb            structureBB
	facing        cube.Direction
	componentType int
	placement     *javastructure.Placement
}

func (p *bridgePieceBase) pieceBase() *bridgePieceBase { return p }

func (p *bridgePieceBase) getXWithOffset(x, z int) int {
	switch p.facing {
	case cube.North, cube.South:
		return p.bb.minX + x
	case cube.West:
		return p.bb.maxX - z
	case cube.East:
		return p.bb.minX + z
	default:
		return x
	}
}

func (p *bridgePieceBase) getYWithOffset(y int) int {
	return y + p.bb.minY
}

func (p *bridgePieceBase) getZWithOffset(x, z int) int {
	switch p.facing {
	case cube.North:
		return p.bb.maxZ - z
	case cube.South:
		return p.bb.minZ + z
	case cube.West, cube.East:
		return p.bb.minZ + x
	default:
		return z
	}
}

// netherBrickFenceRID applies StructurePiece's local-state mirror then rotation
// before resolving the native state, matching placeBlock's transformation order.
func (p *bridgePieceBase) netherBrickFenceRID(g *Nether, mask uint8) uint32 {
	switch p.facing {
	case cube.South: // Mirror.LEFT_RIGHT swaps north and south.
		mask = mask&^3 | (mask&1)<<1 | (mask&2)>>1
	case cube.East: // Rotate.CLOCKWISE_90.
		mask = (mask&1)<<3 | (mask&8)>>2 | (mask&2)<<1 | (mask&4)>>2
	case cube.West: // Mirror.LEFT_RIGHT, then Rotate.CLOCKWISE_90.
		mask = (mask&1)<<2 | (mask&2)<<2 | (mask&8)>>2 | (mask&4)>>2
	}
	return g.netherBrickFenceRIDs[mask]
}

func (p *bridgePieceBase) setBlock(c fortressRegion, chunkX, chunkZ int, sbb structureBB, x, y, z int, rid uint32) {
	wx := p.getXWithOffset(x, z)
	wy := p.getYWithOffset(y)
	wz := p.getZWithOffset(x, z)
	if sbb.isVecInside(wx, wy, wz) {
		setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, rid, p.placement)
	}
}

func (p *bridgePieceBase) blockRIDAt(c fortressRegion, chunkX, chunkZ int, sbb structureBB, x, y, z int, airRID uint32) uint32 {
	wx := p.getXWithOffset(x, z)
	wy := p.getYWithOffset(y)
	wz := p.getZWithOffset(x, z)
	if !sbb.isVecInside(wx, wy, wz) || wx>>4 != chunkX || wz>>4 != chunkZ {
		return airRID
	}
	return c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0)
}

func (p *bridgePieceBase) fillWithAir(c fortressRegion, chunkX, chunkZ int, sbb structureBB, minX, minY, minZ, maxX, maxY, maxZ int, airRID uint32) {
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				p.setBlock(c, chunkX, chunkZ, sbb, x, y, z, airRID)
			}
		}
	}
}

func (p *bridgePieceBase) fillWithBlocks(c fortressRegion, chunkX, chunkZ int, sbb structureBB, minX, minY, minZ, maxX, maxY, maxZ int, boundaryRID, insideRID uint32, existingOnly bool, airRID uint32) {
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				if !existingOnly || p.blockRIDAt(c, chunkX, chunkZ, sbb, x, y, z, airRID) != airRID {
					if y != minY && y != maxY && x != minX && x != maxX && z != minZ && z != maxZ {
						p.setBlock(c, chunkX, chunkZ, sbb, x, y, z, insideRID)
					} else {
						p.setBlock(c, chunkX, chunkZ, sbb, x, y, z, boundaryRID)
					}
				}
			}
		}
	}
}

func (p *bridgePieceBase) replaceAirAndLiquidDownwards(c fortressRegion, chunkX, chunkZ int, sbb structureBB, x, y, z int, rid uint32, g *Nether) {
	wx := p.getXWithOffset(x, z)
	wy := p.getYWithOffset(y)
	wz := p.getZWithOffset(x, z)
	if !sbb.isVecInside(wx, wy, wz) || wx>>4 != chunkX || wz>>4 != chunkZ {
		return
	}
	for wy > 1 {
		cur := c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0)
		if cur != g.airRID && !c.BlockRegistry().LiquidBlock(cur) {
			name, _, ok := c.BlockRegistry().RuntimeIDToState(cur)
			if !ok || (name != "minecraft:glow_lichen" && name != "minecraft:seagrass" && name != "minecraft:tall_seagrass") {
				break
			}
		}
		writeFortressBlockWorld(c, chunkX, chunkZ, wx, wy, wz, rid, p.placement, false)
		wy--
	}
}

func findIntersecting(pieces []bridgePiece, bb structureBB) bridgePiece {
	for _, p := range pieces {
		if p.pieceBase().bb.intersects(bb) {
			return p
		}
	}
	return nil
}

func isAboveGround(bb structureBB) bool {
	return bb.minY > 10
}

// --- Fortress structure rendering ---

func (g *Nether) applyNetherBridgeTo(chunkX, chunkZ int, c fortressRegion, r *java26.Random, placement *javastructure.Placement) {
	const scanRange = 8
	chunkMinX, chunkMinZ := chunkX<<4, chunkZ<<4
	chunkMaxX, chunkMaxZ := chunkMinX+15, chunkMinZ+15
	chunkBB := structureBB{minX: chunkMinX, minY: 0, minZ: chunkMinZ, maxX: chunkMaxX, maxY: 127, maxZ: chunkMaxZ}
	for sx := chunkX - scanRange; sx <= chunkX+scanRange; sx++ {
		for sz := chunkZ - scanRange; sz <= chunkZ+scanRange; sz++ {
			start, err := g.complexStart(int32(sx), int32(sz))
			if err != nil {
				panic(err)
			}
			if start == nil || start.fortress == nil {
				continue
			}
			s := start.fortress
			if !s.bb.intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ) {
				continue
			}
			for _, cached := range s.pieces {
				if cached.pieceBase().bb.intersects(chunkBB) {
					renderFortressPiece(cached, c, chunkX, chunkZ, chunkBB, r, g, placement)
				}
			}
		}
	}
}

func (g *Nether) placeFortressStructureStep(pos world.ChunkPos, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement)) {
	if env.Random == nil {
		panic("nether: Java structure placement has no step random source")
	}
	if env.BaseBlock == nil {
		panic("nether: fortress placement requires base-block queries")
	}
	placement := javastructure.Placement{}
	region := &journalFortressRegion{
		chunkX:   int(pos[0]),
		chunkZ:   int(pos[1]),
		env:      env,
		registry: world.DefaultBlockRegistry,
		writes:   make(map[cube.Pos]uint32),
	}
	g.applyNetherBridgeTo(int(pos[0]), int(pos[1]), region, env.Random, &placement)
	if len(placement.Blocks) != 0 || len(placement.PostProcessing) != 0 || len(placement.ScheduledTicks) != 0 {
		emit(placement)
	}
}

// renderFortressPiece keeps render-time flags and the placement sink off the
// cached graph. Concrete calls keep these short-lived copies on the stack.
func renderFortressPiece(p bridgePiece, c fortressRegion, chunkX, chunkZ int, bb structureBB, r *java26.Random, g *Nether, placement *javastructure.Placement) {
	switch piece := p.(type) {
	case *netherBridgeCorridor:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeCorridor2:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeCorridor3:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeCorridor4:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeCorridor5:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeCrossing:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeCrossing2:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeCrossing3:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeEnd:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeEntrance:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeNetherStalkRoom:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeStairs:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeStraight:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	case *netherBridgeThrone:
		rendered := *piece
		rendered.placement = placement
		rendered.addComponentParts(c, chunkX, chunkZ, bb, r, g)
	default:
		panic("nether: unknown fortress piece type")
	}
}

type bridgePieceKind uint8

const (
	bridgeStraight bridgePieceKind = iota
	bridgeCrossing3
	bridgeCrossing
	bridgeStairs
	bridgeThrone
	bridgeEntrance
	bridgeCorridor5
	bridgeCrossing2
	bridgeCorridor2
	bridgeCorridor
	bridgeCorridor3
	bridgeCorridor4
	bridgeNetherStalkRoom
	bridgeEnd
)

type bridgePieceWeight struct {
	kind          bridgePieceKind
	weight        int
	placeCount    int
	maxPlaceCount int
	allowInRow    bool
}

func (w *bridgePieceWeight) doPlace() bool {
	return w.maxPlaceCount == 0 || w.placeCount < w.maxPlaceCount
}

func (w *bridgePieceWeight) isValid() bool {
	return w.maxPlaceCount == 0 || w.placeCount < w.maxPlaceCount
}

var bridgePrimaryWeights = []bridgePieceWeight{
	{kind: bridgeStraight, weight: 30, maxPlaceCount: 0, allowInRow: true},
	{kind: bridgeCrossing3, weight: 10, maxPlaceCount: 4},
	{kind: bridgeCrossing, weight: 10, maxPlaceCount: 4},
	{kind: bridgeStairs, weight: 10, maxPlaceCount: 3},
	{kind: bridgeThrone, weight: 5, maxPlaceCount: 2},
	{kind: bridgeEntrance, weight: 5, maxPlaceCount: 1},
}

var bridgeSecondaryWeights = []bridgePieceWeight{
	{kind: bridgeCorridor5, weight: 25, maxPlaceCount: 0, allowInRow: true},
	{kind: bridgeCrossing2, weight: 15, maxPlaceCount: 5},
	{kind: bridgeCorridor2, weight: 5, maxPlaceCount: 10},
	{kind: bridgeCorridor, weight: 5, maxPlaceCount: 10},
	{kind: bridgeCorridor3, weight: 10, maxPlaceCount: 3, allowInRow: true},
	{kind: bridgeCorridor4, weight: 7, maxPlaceCount: 2},
	{kind: bridgeNetherStalkRoom, weight: 5, maxPlaceCount: 2},
}

type netherBridgeStart struct {
	base                *netherBridgeCrossing3
	primaryWeights      []bridgePieceWeight
	secondaryWeights    []bridgePieceWeight
	lastPlacedKind      bridgePieceKind
	lastPlacedSecondary bool
	hasLastPlaced       bool
	pendingChildren     []bridgePiece
	pieces              []bridgePiece
	bb                  structureBB
}

func newNetherBridgeStart(r *java26.Random, chunkX, chunkZ int, g *Nether) *netherBridgeStart {
	startPiece := newBridgeCrossing3Start(r, (chunkX<<4)+2, (chunkZ<<4)+2, g)
	s := &netherBridgeStart{
		base:             startPiece,
		primaryWeights:   make([]bridgePieceWeight, len(bridgePrimaryWeights)),
		secondaryWeights: make([]bridgePieceWeight, len(bridgeSecondaryWeights)),
		pieces:           []bridgePiece{startPiece},
		bb:               startPiece.bb,
	}
	copy(s.primaryWeights, bridgePrimaryWeights)
	copy(s.secondaryWeights, bridgeSecondaryWeights)

	startPiece.buildComponent(s, s.pieces, r)
	for len(s.pendingChildren) > 0 {
		i := int(r.NextInt(int32(len(s.pendingChildren))))
		p := s.pendingChildren[i]
		s.pendingChildren = append(s.pendingChildren[:i], s.pendingChildren[i+1:]...)
		p.buildComponent(s, s.pieces, r)
	}

	s.updateBoundingBox()
	s.setRandomHeight(r, 48, 70)
	return s
}

func (s *netherBridgeStart) updateBoundingBox() {
	if len(s.pieces) == 0 {
		return
	}
	bb := s.pieces[0].pieceBase().bb
	for _, p := range s.pieces[1:] {
		bb.expandTo(p.pieceBase().bb)
	}
	s.bb = bb
}

func (s *netherBridgeStart) setRandomHeight(r *java26.Random, minY, maxY int) {
	i := maxY - minY + 1 - s.bb.ySize()
	var j int
	if i > 1 {
		j = minY + int(r.NextInt(int32(i)))
	} else {
		j = minY
	}
	k := j - s.bb.minY
	s.bb.offset(0, k, 0)
	for _, p := range s.pieces {
		p.pieceBase().bb.offset(0, k, 0)
	}
}

func (p *bridgePieceBase) getTotalWeight(weights []bridgePieceWeight) int {
	total := 0
	ok := false
	for _, w := range weights {
		if w.maxPlaceCount > 0 && w.placeCount < w.maxPlaceCount {
			ok = true
		}
		total += w.weight
	}
	if !ok {
		return -1
	}
	return total
}

func (p *bridgePieceBase) generatePiece(start *netherBridgeStart, weights *[]bridgePieceWeight, pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	total := p.getTotalWeight(*weights)
	if total <= 0 || depth > 30 {
		return createBridgePiece(bridgeEnd, pieces, r, x, y, z, facing, depth)
	}

	for attempt := 0; attempt < 5; attempt++ {
		k := int(r.NextInt(int32(total)))
		for i := range *weights {
			w := &(*weights)[i]
			k -= w.weight
			if k >= 0 {
				continue
			}
			if !w.doPlace() || (start.hasLastPlaced && start.lastPlacedKind == w.kind &&
				start.lastPlacedSecondary == (weights == &start.secondaryWeights) && !w.allowInRow) {
				break
			}
			piece := createBridgePiece(w.kind, pieces, r, x, y, z, facing, depth)
			if piece != nil {
				w.placeCount++
				start.lastPlacedKind = w.kind
				start.lastPlacedSecondary = weights == &start.secondaryWeights
				start.hasLastPlaced = true
				if !w.isValid() {
					*weights = append((*weights)[:i], (*weights)[i+1:]...)
				}
				return piece
			}
		}
	}

	return createBridgePiece(bridgeEnd, pieces, r, x, y, z, facing, depth)
}

func (p *bridgePieceBase) generateAndAddPiece(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int, useSecondary bool) bridgePiece {
	if math.Abs(float64(x-start.bb.minX)) > 112 || math.Abs(float64(z-start.bb.minZ)) > 112 {
		return createBridgePiece(bridgeEnd, pieces, r, x, y, z, facing, depth)
	}

	weights := &start.primaryWeights
	if useSecondary {
		weights = &start.secondaryWeights
	}

	piece := p.generatePiece(start, weights, start.pieces, r, x, y, z, facing, depth+1)
	if piece != nil {
		start.pieces = append(start.pieces, piece)
		start.pendingChildren = append(start.pendingChildren, piece)
	}
	return piece
}

func (p *bridgePieceBase) getNextComponentNormal(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random, xOffset, yOffset int, useSecondary bool) bridgePiece {
	switch p.facing {
	case cube.North:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX+xOffset, p.bb.minY+yOffset, p.bb.minZ-1, p.facing, p.componentType, useSecondary)
	case cube.South:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX+xOffset, p.bb.minY+yOffset, p.bb.maxZ+1, p.facing, p.componentType, useSecondary)
	case cube.West:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX-1, p.bb.minY+yOffset, p.bb.minZ+xOffset, p.facing, p.componentType, useSecondary)
	case cube.East:
		return p.generateAndAddPiece(start, pieces, r, p.bb.maxX+1, p.bb.minY+yOffset, p.bb.minZ+xOffset, p.facing, p.componentType, useSecondary)
	default:
		return nil
	}
}

func (p *bridgePieceBase) getNextComponentX(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random, xOffset, yOffset int, useSecondary bool) bridgePiece {
	switch p.facing {
	case cube.North, cube.South:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX-1, p.bb.minY+xOffset, p.bb.minZ+yOffset, cube.West, p.componentType, useSecondary)
	case cube.West, cube.East:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX+yOffset, p.bb.minY+xOffset, p.bb.minZ-1, cube.North, p.componentType, useSecondary)
	default:
		return nil
	}
}

func (p *bridgePieceBase) getNextComponentZ(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random, xOffset, yOffset int, useSecondary bool) bridgePiece {
	switch p.facing {
	case cube.North, cube.South:
		return p.generateAndAddPiece(start, pieces, r, p.bb.maxX+1, p.bb.minY+xOffset, p.bb.minZ+yOffset, cube.East, p.componentType, useSecondary)
	case cube.West, cube.East:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX+yOffset, p.bb.minY+xOffset, p.bb.maxZ+1, cube.South, p.componentType, useSecondary)
	default:
		return nil
	}
}
