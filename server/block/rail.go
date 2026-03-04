package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var (
	hashRail          = NextHash()
	hashPoweredRail   = NextHash()
	hashDetectorRail  = NextHash()
	hashActivatorRail = NextHash()
)

// RailDirection represents the shape of a rail block.
type RailDirection int32

const (
	RailNorthSouth RailDirection = iota
	RailEastWest
	RailAscendingEast
	RailAscendingWest
	RailAscendingNorth
	RailAscendingSouth
	RailSouthEast
	RailSouthWest
	RailNorthWest
	RailNorthEast
)

// Ascending reports if the rail direction is ascending.
func (r RailDirection) Ascending() bool {
	switch r {
	case RailAscendingEast, RailAscendingWest, RailAscendingNorth, RailAscendingSouth:
		return true
	default:
		return false
	}
}

// AscendingFace returns the direction the rail ascends towards.
func (r RailDirection) AscendingFace() (cube.Face, bool) {
	switch r {
	case RailAscendingEast:
		return cube.FaceEast, true
	case RailAscendingWest:
		return cube.FaceWest, true
	case RailAscendingNorth:
		return cube.FaceNorth, true
	case RailAscendingSouth:
		return cube.FaceSouth, true
	default:
		return cube.FaceDown, false
	}
}

// ConnectingFaces returns the horizontal faces that this rail connects to.
func (r RailDirection) ConnectingFaces() []cube.Face {
	switch r {
	case RailNorthSouth, RailAscendingNorth, RailAscendingSouth:
		return []cube.Face{cube.FaceNorth, cube.FaceSouth}
	case RailEastWest, RailAscendingEast, RailAscendingWest:
		return []cube.Face{cube.FaceEast, cube.FaceWest}
	case RailSouthEast:
		return []cube.Face{cube.FaceSouth, cube.FaceEast}
	case RailSouthWest:
		return []cube.Face{cube.FaceSouth, cube.FaceWest}
	case RailNorthWest:
		return []cube.Face{cube.FaceNorth, cube.FaceWest}
	case RailNorthEast:
		return []cube.Face{cube.FaceNorth, cube.FaceEast}
	default:
		return []cube.Face{cube.FaceNorth, cube.FaceSouth}
	}
}

func railStraight(face cube.Face) RailDirection {
	if face == cube.FaceEast || face == cube.FaceWest {
		return RailEastWest
	}
	return RailNorthSouth
}

func railAscending(face cube.Face) RailDirection {
	switch face {
	case cube.FaceEast:
		return RailAscendingEast
	case cube.FaceWest:
		return RailAscendingWest
	case cube.FaceNorth:
		return RailAscendingNorth
	case cube.FaceSouth:
		return RailAscendingSouth
	default:
		return RailNorthSouth
	}
}

func railCurved(a, b cube.Face) RailDirection {
	switch {
	case (a == cube.FaceNorth && b == cube.FaceEast) || (a == cube.FaceEast && b == cube.FaceNorth):
		return RailNorthEast
	case (a == cube.FaceNorth && b == cube.FaceWest) || (a == cube.FaceWest && b == cube.FaceNorth):
		return RailNorthWest
	case (a == cube.FaceSouth && b == cube.FaceEast) || (a == cube.FaceEast && b == cube.FaceSouth):
		return RailSouthEast
	case (a == cube.FaceSouth && b == cube.FaceWest) || (a == cube.FaceWest && b == cube.FaceSouth):
		return RailSouthWest
	}
	return RailNorthSouth
}

func railStraightOrCurved(a, b cube.Face) RailDirection {
	if (a == cube.FaceNorth || a == cube.FaceSouth) && (b == cube.FaceNorth || b == cube.FaceSouth) {
		return RailNorthSouth
	}
	if (a == cube.FaceEast || a == cube.FaceWest) && (b == cube.FaceEast || b == cube.FaceWest) {
		return RailEastWest
	}
	return railCurved(a, b)
}

type railNeighbor struct {
	pos  cube.Pos
	face cube.Face
	rail railBlock
}

func railIsAbstract(r railBlock) bool {
	_, ok := r.(Rail)
	return ok
}

func railHasConnectingDirections(dir RailDirection, faces ...cube.Face) bool {
	connecting := dir.ConnectingFaces()
	for _, face := range faces {
		found := false
		for _, c := range connecting {
			if c == face {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func railNeighborForFace(neighbors []railNeighbor, face cube.Face) (railNeighbor, bool) {
	for _, n := range neighbors {
		if n.face == face {
			return n, true
		}
	}
	return railNeighbor{}, false
}

func railNeighborFaces(neighbors []railNeighbor) []cube.Face {
	faces := make([]cube.Face, 0, len(neighbors))
	for _, n := range neighbors {
		faces = append(faces, n.face)
	}
	return faces
}

func facesContainAll(faces []cube.Face, want []cube.Face) bool {
	for _, face := range want {
		found := false
		for _, f := range faces {
			if f == face {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func railCheckAround(pos cube.Pos, tx *world.Tx, faces []cube.Face) []railNeighbor {
	neighbors := make([]railNeighbor, 0, len(faces))
	for _, face := range faces {
		for _, target := range []cube.Pos{pos.Side(face), pos.Side(face).Side(cube.FaceUp), pos.Side(face).Side(cube.FaceDown)} {
			if r, ok := railAt(target, tx); ok {
				neighbors = append(neighbors, railNeighbor{pos: target, face: face, rail: r})
			}
		}
	}
	return neighbors
}

func railCheckConnected(pos cube.Pos, r railBlock, tx *world.Tx) []railNeighbor {
	neighbors := railCheckAround(pos, tx, r.RailDirection().ConnectingFaces())
	connected := make([]railNeighbor, 0, len(neighbors))
	for _, n := range neighbors {
		if railHasConnectingDirections(n.rail.RailDirection(), n.face.Opposite()) {
			connected = append(connected, n)
		}
	}
	return connected
}

func railCheckAroundAffected(pos cube.Pos, tx *world.Tx) []railNeighbor {
	neighbors := railCheckAround(pos, tx, []cube.Face{cube.FaceSouth, cube.FaceEast, cube.FaceWest, cube.FaceNorth})
	affected := make([]railNeighbor, 0, len(neighbors))
	for _, n := range neighbors {
		if len(railCheckConnected(n.pos, n.rail, tx)) != 2 {
			affected = append(affected, n)
		}
	}
	return affected
}

func railSetDirection(pos cube.Pos, tx *world.Tx, r railBlock, dir RailDirection) {
	if r.RailDirection() == dir {
		return
	}
	switch rail := r.(type) {
	case Rail:
		rail.Direction = dir
		tx.SetBlock(pos, rail, nil)
	case PoweredRail:
		rail.Direction = dir
		tx.SetBlock(pos, rail, nil)
	case DetectorRail:
		rail.Direction = dir
		tx.SetBlock(pos, rail, nil)
	case ActivatorRail:
		rail.Direction = dir
		tx.SetBlock(pos, rail, nil)
	}
}

func railConnectOne(pos cube.Pos, tx *world.Tx, other railNeighbor) RailDirection {
	delta := pos[1] - other.pos[1]
	connected := railCheckConnected(other.pos, other.rail, tx)
	if len(connected) == 0 {
		if delta == 1 {
			railSetDirection(other.pos, tx, other.rail, railAscending(other.face.Opposite()))
		} else {
			railSetDirection(other.pos, tx, other.rail, railStraight(other.face))
		}
		if delta == -1 {
			return railAscending(other.face)
		}
		return railStraight(other.face)
	}
	if len(connected) == 1 {
		faceConnected := connected[0].face
		if railIsAbstract(other.rail) && faceConnected != other.face {
			railSetDirection(other.pos, tx, other.rail, railCurved(other.face.Opposite(), faceConnected))
			if delta == -1 {
				return railAscending(other.face)
			}
			return railStraight(other.face)
		}
		if faceConnected == other.face {
			if !other.rail.RailDirection().Ascending() {
				if delta == 1 {
					railSetDirection(other.pos, tx, other.rail, railAscending(other.face.Opposite()))
				} else {
					railSetDirection(other.pos, tx, other.rail, railStraight(other.face))
				}
			}
			if delta == -1 {
				return railAscending(other.face)
			}
			return railStraight(other.face)
		}
		if railHasConnectingDirections(other.rail.RailDirection(), cube.FaceNorth, cube.FaceSouth) {
			if delta == 1 {
				railSetDirection(other.pos, tx, other.rail, railAscending(other.face.Opposite()))
			} else {
				railSetDirection(other.pos, tx, other.rail, railStraight(other.face))
			}
			if delta == -1 {
				return railAscending(other.face)
			}
			return railStraight(other.face)
		}
	}
	return RailNorthSouth
}

func railConnectTwo(pos cube.Pos, tx *world.Tx, rail1, rail2 railNeighbor) RailDirection {
	railConnectOne(pos, tx, rail1)
	railConnectOne(pos, tx, rail2)
	if rail1.face.Opposite() == rail2.face {
		delta1 := pos[1] - rail1.pos[1]
		delta2 := pos[1] - rail2.pos[1]
		if delta1 == -1 {
			return railAscending(rail1.face)
		} else if delta2 == -1 {
			return railAscending(rail2.face)
		}
	}
	return railStraightOrCurved(rail1.face, rail2.face)
}

func railPlaceDirection(pos cube.Pos, tx *world.Tx, rail railBlock, allowCurves bool) RailDirection {
	neighbors := railCheckAroundAffected(pos, tx)
	if len(neighbors) == 0 {
		return rail.RailDirection()
	}
	if len(neighbors) == 1 {
		return railConnectOne(pos, tx, neighbors[0])
	}
	if len(neighbors) == 4 {
		if allowCurves {
			south, okSouth := railNeighborForFace(neighbors, cube.FaceSouth)
			east, okEast := railNeighborForFace(neighbors, cube.FaceEast)
			if okSouth && okEast {
				return railConnectTwo(pos, tx, south, east)
			}
		} else {
			east, okEast := railNeighborForFace(neighbors, cube.FaceEast)
			west, okWest := railNeighborForFace(neighbors, cube.FaceWest)
			if okEast && okWest {
				return railConnectTwo(pos, tx, east, west)
			}
		}
	}
	if allowCurves {
		if len(neighbors) == 2 {
			return railConnectTwo(pos, tx, neighbors[0], neighbors[1])
		}
		faces := railNeighborFaces(neighbors)
		for _, dir := range []RailDirection{RailSouthEast, RailNorthEast, RailSouthWest} {
			connecting := dir.ConnectingFaces()
			if !facesContainAll(faces, connecting) {
				continue
			}
			first, okFirst := railNeighborForFace(neighbors, connecting[0])
			second, okSecond := railNeighborForFace(neighbors, connecting[1])
			if okFirst && okSecond {
				return railConnectTwo(pos, tx, first, second)
			}
		}
	} else {
		var face cube.Face
		var ok bool
		for _, n := range neighbors {
			if !ok || n.face > face {
				face = n.face
				ok = true
			}
		}
		if ok {
			if first, okFirst := railNeighborForFace(neighbors, face); okFirst {
				if second, okSecond := railNeighborForFace(neighbors, face.Opposite()); okSecond {
					return railConnectTwo(pos, tx, first, second)
				}
				return railConnectOne(pos, tx, first)
			}
		}
	}
	return RailNorthSouth
}

func railCanStay(pos cube.Pos, tx *world.Tx, dir RailDirection) bool {
	down := pos.Side(cube.FaceDown)
	if !tx.Block(down).Model().FaceSolid(down, cube.FaceUp, tx) {
		return false
	}
	if face, ok := dir.AscendingFace(); ok {
		side := pos.Side(face)
		if !tx.Block(side).Model().FaceSolid(side, cube.FaceUp, tx) {
			return false
		}
	}
	return true
}

type railPowerMatcher func(world.Block) (RailDirection, bool)

func poweredRailMatch(b world.Block) (RailDirection, bool) {
	if r, ok := b.(PoweredRail); ok {
		return r.Direction, true
	}
	return RailNorthSouth, false
}

func activatorRailMatch(b world.Block) (RailDirection, bool) {
	if r, ok := b.(ActivatorRail); ok {
		return r.Direction, true
	}
	return RailNorthSouth, false
}

func railCheckSurrounding(pos cube.Pos, tx *world.Tx, relative bool, power int, match railPowerMatcher) bool {
	if power >= 8 {
		return false
	}
	rail, ok := railAt(pos, tx)
	if !ok {
		return false
	}
	dx, dy, dz := pos[0], pos[1], pos[2]
	onStraight := true
	base := RailNorthSouth
	hasBase := false
	switch rail.RailDirection() {
	case RailNorthSouth:
		if relative {
			dz++
		} else {
			dz--
		}
	case RailEastWest:
		if relative {
			dx--
		} else {
			dx++
		}
	case RailAscendingEast:
		if relative {
			dx--
		} else {
			dx++
			dy++
			onStraight = false
		}
		base = RailEastWest
		hasBase = true
	case RailAscendingWest:
		if relative {
			dx--
			dy++
			onStraight = false
		} else {
			dx++
		}
		base = RailEastWest
		hasBase = true
	case RailAscendingNorth:
		if relative {
			dz++
		} else {
			dz--
			dy++
			onStraight = false
		}
		base = RailNorthSouth
		hasBase = true
	case RailAscendingSouth:
		if relative {
			dz++
			dy++
			onStraight = false
		} else {
			dz--
		}
		base = RailNorthSouth
		hasBase = true
	default:
		return false
	}
	if railCanPowered(cube.Pos{dx, dy, dz}, tx, base, hasBase, relative, power, match) {
		return true
	}
	return onStraight && railCanPowered(cube.Pos{dx, dy - 1, dz}, tx, base, hasBase, relative, power, match)
}

func railCanPowered(pos cube.Pos, tx *world.Tx, base RailDirection, hasBase bool, relative bool, power int, match railPowerMatcher) bool {
	dir, ok := match(tx.Block(pos))
	if !ok {
		return false
	}
	if hasBase {
		if base == RailEastWest {
			if dir == RailNorthSouth || dir == RailAscendingNorth || dir == RailAscendingSouth {
				return false
			}
		} else if base == RailNorthSouth {
			if dir == RailEastWest || dir == RailAscendingEast || dir == RailAscendingWest {
				return false
			}
		}
	}
	if redstonePowered(pos, tx) {
		return true
	}
	return railCheckSurrounding(pos, tx, relative, power+1, match)
}

// Rail is a basic rail block that can curve.
type Rail struct {
	transparent

	Direction RailDirection
}

// BreakInfo ...
func (r Rail) BreakInfo() BreakInfo {
	return newBreakInfo(0.7, alwaysHarvestable, nothingEffective, oneOf(r))
}

// Model ...
func (Rail) Model() world.BlockModel {
	return model.SnowLayer{Layers: 1}
}

// SideClosed ...
func (Rail) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// NeighbourUpdateTick ...
func (r Rail) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !railCanStay(pos, tx, r.Direction) {
		breakBlock(r, pos, tx)
	}
}

// UseOnBlock ...
func (r Rail) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	r.Direction = railPlaceDirection(pos, tx, r, true)
	if !railCanStay(pos, tx, r.Direction) {
		return false
	}
	place(tx, pos, r, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (Rail) EncodeItem() (name string, meta int16) {
	return "minecraft:rail", 0
}

// EncodeBlock ...
func (r Rail) EncodeBlock() (string, map[string]any) {
	return "minecraft:rail", map[string]any{"rail_direction": int32(r.Direction)}
}

// Hash ...
func (r Rail) Hash() (uint64, uint64) {
	return hashRail, uint64(r.Direction)
}

// PoweredRail is a rail that can accelerate minecarts when powered.
type PoweredRail struct {
	transparent

	Direction RailDirection
	Powered   bool
}

// BreakInfo ...
func (r PoweredRail) BreakInfo() BreakInfo {
	return newBreakInfo(0.7, alwaysHarvestable, nothingEffective, oneOf(r))
}

// Model ...
func (PoweredRail) Model() world.BlockModel {
	return model.SnowLayer{Layers: 1}
}

// SideClosed ...
func (PoweredRail) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// NeighbourUpdateTick ...
func (r PoweredRail) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	r.updatePowered(pos, tx)
}

// ScheduledTick ...
func (r PoweredRail) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	r.updatePowered(pos, tx)
}

func (r PoweredRail) updatePowered(pos cube.Pos, tx *world.Tx) {
	if !railCanStay(pos, tx, r.Direction) {
		breakBlock(r, pos, tx)
		return
	}
	powered := redstonePowered(pos, tx) ||
		railCheckSurrounding(pos, tx, true, 0, poweredRailMatch) ||
		railCheckSurrounding(pos, tx, false, 0, poweredRailMatch)
	if powered == r.Powered {
		return
	}
	r.Powered = powered
	tx.SetBlock(pos, r, nil)
	tx.DoBlockUpdatesAround(pos.Side(cube.FaceDown))
	if r.Direction.Ascending() {
		tx.DoBlockUpdatesAround(pos.Side(cube.FaceUp))
	}
}

// UseOnBlock ...
func (r PoweredRail) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	r.Direction = railPlaceDirection(pos, tx, r, false)
	if !railCanStay(pos, tx, r.Direction) {
		return false
	}
	place(tx, pos, r, user, ctx)
	if !placed(ctx) {
		return false
	}
	tx.ScheduleBlockUpdate(pos, r, 0)
	return true
}

// EncodeItem ...
func (PoweredRail) EncodeItem() (name string, meta int16) {
	return "minecraft:golden_rail", 0
}

// EncodeBlock ...
func (r PoweredRail) EncodeBlock() (string, map[string]any) {
	return "minecraft:golden_rail", map[string]any{"rail_direction": int32(r.Direction), "rail_data_bit": boolByte(r.Powered)}
}

// Hash ...
func (r PoweredRail) Hash() (uint64, uint64) {
	return hashPoweredRail, uint64(r.Direction) | uint64(boolByte(r.Powered))<<4
}

// DetectorRail is a rail that emits power when a minecart passes over it.
type DetectorRail struct {
	transparent

	Direction RailDirection
	Powered   bool
}

// BreakInfo ...
func (r DetectorRail) BreakInfo() BreakInfo {
	return newBreakInfo(0.7, alwaysHarvestable, nothingEffective, oneOf(r))
}

// Model ...
func (DetectorRail) Model() world.BlockModel {
	return model.SnowLayer{Layers: 1}
}

// SideClosed ...
func (DetectorRail) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// NeighbourUpdateTick ...
func (r DetectorRail) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !railCanStay(pos, tx, r.Direction) {
		breakBlock(r, pos, tx)
		return
	}
}

// EntityInside ...
func (r DetectorRail) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if !isMinecartEntity(e) {
		return
	}
	r.updatePowered(pos, tx, true)
}

// ScheduledTick ...
func (r DetectorRail) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	r.updatePowered(pos, tx, detectorRailHasMinecart(pos, tx))
}

// RedstoneWeakPower ...
func (r DetectorRail) RedstoneWeakPower(cube.Face) uint8 {
	if r.Powered {
		return 15
	}
	return 0
}

// RedstoneStrongPower ...
func (r DetectorRail) RedstoneStrongPower(face cube.Face) uint8 {
	if face == cube.FaceDown && r.Powered {
		return 15
	}
	return 0
}

// UseOnBlock ...
func (r DetectorRail) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	r.Direction = railPlaceDirection(pos, tx, r, false)
	if !railCanStay(pos, tx, r.Direction) {
		return false
	}
	place(tx, pos, r, user, ctx)
	if !placed(ctx) {
		return false
	}
	tx.ScheduleBlockUpdate(pos, r, 0)
	return true
}

// EncodeItem ...
func (DetectorRail) EncodeItem() (name string, meta int16) {
	return "minecraft:detector_rail", 0
}

// EncodeBlock ...
func (r DetectorRail) EncodeBlock() (string, map[string]any) {
	return "minecraft:detector_rail", map[string]any{"rail_direction": int32(r.Direction), "rail_data_bit": boolByte(r.Powered)}
}

// Hash ...
func (r DetectorRail) Hash() (uint64, uint64) {
	return hashDetectorRail, uint64(r.Direction) | uint64(boolByte(r.Powered))<<4
}

// ActivatorRail is a rail that activates minecart behaviours when powered.
type ActivatorRail struct {
	transparent

	Direction RailDirection
	Powered   bool
}

// BreakInfo ...
func (r ActivatorRail) BreakInfo() BreakInfo {
	return newBreakInfo(0.7, alwaysHarvestable, nothingEffective, oneOf(r))
}

// Model ...
func (ActivatorRail) Model() world.BlockModel {
	return model.SnowLayer{Layers: 1}
}

// SideClosed ...
func (ActivatorRail) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// NeighbourUpdateTick ...
func (r ActivatorRail) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	r.updatePowered(pos, tx)
}

// ScheduledTick ...
func (r ActivatorRail) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	r.updatePowered(pos, tx)
}

func (r ActivatorRail) updatePowered(pos cube.Pos, tx *world.Tx) {
	if !railCanStay(pos, tx, r.Direction) {
		breakBlock(r, pos, tx)
		return
	}
	powered := redstonePowered(pos, tx) ||
		railCheckSurrounding(pos, tx, true, 0, activatorRailMatch) ||
		railCheckSurrounding(pos, tx, false, 0, activatorRailMatch)
	if powered == r.Powered {
		return
	}
	r.Powered = powered
	tx.SetBlock(pos, r, nil)
	tx.DoBlockUpdatesAround(pos.Side(cube.FaceDown))
	if r.Direction.Ascending() {
		tx.DoBlockUpdatesAround(pos.Side(cube.FaceUp))
	}
}

// UseOnBlock ...
func (r ActivatorRail) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	r.Direction = railPlaceDirection(pos, tx, r, false)
	if !railCanStay(pos, tx, r.Direction) {
		return false
	}
	place(tx, pos, r, user, ctx)
	if !placed(ctx) {
		return false
	}
	tx.ScheduleBlockUpdate(pos, r, 0)
	return true
}

// EncodeItem ...
func (ActivatorRail) EncodeItem() (name string, meta int16) {
	return "minecraft:activator_rail", 0
}

// EncodeBlock ...
func (r ActivatorRail) EncodeBlock() (string, map[string]any) {
	return "minecraft:activator_rail", map[string]any{"rail_direction": int32(r.Direction), "rail_data_bit": boolByte(r.Powered)}
}

// Hash ...
func (r ActivatorRail) Hash() (uint64, uint64) {
	return hashActivatorRail, uint64(r.Direction) | uint64(boolByte(r.Powered))<<4
}

type railBlock interface {
	world.Block
	RailDirection() RailDirection
	RailPowered() (bool, bool)
}

// RailDirection returns the rail direction of r.
func (r Rail) RailDirection() RailDirection { return r.Direction }

// RailPowered returns the powered state and false for base rails.
func (Rail) RailPowered() (bool, bool) { return false, false }

// RailDirection returns the rail direction of r.
func (r PoweredRail) RailDirection() RailDirection { return r.Direction }

// RailPowered returns the powered state and true for powered rails.
func (r PoweredRail) RailPowered() (bool, bool) { return r.Powered, true }

// RailDirection returns the rail direction of r.
func (r DetectorRail) RailDirection() RailDirection { return r.Direction }

// RailPowered returns the powered state and true for detector rails.
func (r DetectorRail) RailPowered() (bool, bool) { return r.Powered, true }

// RailDirection returns the rail direction of r.
func (r ActivatorRail) RailDirection() RailDirection { return r.Direction }

// RailPowered returns the powered state and true for activator rails.
func (r ActivatorRail) RailPowered() (bool, bool) { return r.Powered, true }

func railAt(pos cube.Pos, tx *world.Tx) (railBlock, bool) {
	b := tx.Block(pos)
	if r, ok := b.(railBlock); ok {
		return r, true
	}
	return nil, false
}

// IsRail reports if the block is any rail type.
func IsRail(b world.Block) bool {
	_, ok := b.(railBlock)
	return ok
}

// RailInfo returns the direction and powered state for a rail.
func RailInfo(b world.Block) (RailDirection, bool, bool) {
	r, ok := b.(railBlock)
	if !ok {
		return RailNorthSouth, false, false
	}
	powered, hasPower := r.RailPowered()
	return r.RailDirection(), powered, hasPower
}

func (r DetectorRail) updatePowered(pos cube.Pos, tx *world.Tx, powered bool) {
	if r.Powered == powered {
		if powered {
			tx.ScheduleBlockUpdate(pos, r, redstoneTicks(1))
		}
		return
	}
	r.Powered = powered
	tx.SetBlock(pos, r, nil)
	tx.DoBlockUpdatesAround(pos)
	tx.DoBlockUpdatesAround(pos.Side(cube.FaceDown))
	if powered {
		tx.ScheduleBlockUpdate(pos, r, redstoneTicks(1))
	}
}

func detectorRailHasMinecart(pos cube.Pos, tx *world.Tx) bool {
	box := cube.Box(
		float64(pos[0])+0.125,
		float64(pos[1]),
		float64(pos[2])+0.125,
		float64(pos[0])+0.875,
		float64(pos[1])+0.75,
		float64(pos[2])+0.875,
	)
	for e := range tx.EntitiesWithin(box) {
		if isMinecartEntity(e) {
			return true
		}
	}
	return false
}

func isMinecartEntity(e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:minecart", "minecraft:chest_minecart", "minecraft:hopper_minecart", "minecraft:tnt_minecart":
		return true
	}
	return false
}

func allRails() (rails []world.Block) {
	for d := RailNorthSouth; d <= RailNorthEast; d++ {
		rails = append(rails, Rail{Direction: d})
	}
	return
}

func allPoweredRails() (rails []world.Block) {
	for _, powered := range []bool{false, true} {
		for d := RailNorthSouth; d <= RailAscendingSouth; d++ {
			rails = append(rails, PoweredRail{Direction: d, Powered: powered})
		}
	}
	return
}

func allDetectorRails() (rails []world.Block) {
	for _, powered := range []bool{false, true} {
		for d := RailNorthSouth; d <= RailAscendingSouth; d++ {
			rails = append(rails, DetectorRail{Direction: d, Powered: powered})
		}
	}
	return
}

func allActivatorRails() (rails []world.Block) {
	for _, powered := range []bool{false, true} {
		for d := RailNorthSouth; d <= RailAscendingSouth; d++ {
			rails = append(rails, ActivatorRail{Direction: d, Powered: powered})
		}
	}
	return
}
