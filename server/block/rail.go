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

func railPlaceDirection(pos cube.Pos, tx *world.Tx, user item.User, allowCurves bool) RailDirection {
	type neighbor struct {
		face cube.Face
		dy   int
	}
	neighbors := make([]neighbor, 0, 4)
	seen := map[cube.Face]struct{}{}
	for _, face := range cube.HorizontalFaces() {
		if _, ok := seen[face]; ok {
			continue
		}
		if _, ok := railAt(pos.Side(face), tx); ok {
			neighbors = append(neighbors, neighbor{face: face, dy: 0})
			seen[face] = struct{}{}
			continue
		}
		if _, ok := railAt(pos.Side(face).Side(cube.FaceUp), tx); ok {
			neighbors = append(neighbors, neighbor{face: face, dy: 1})
			seen[face] = struct{}{}
			continue
		}
		if _, ok := railAt(pos.Side(face).Side(cube.FaceDown), tx); ok {
			neighbors = append(neighbors, neighbor{face: face, dy: -1})
			seen[face] = struct{}{}
		}
	}
	if len(neighbors) == 0 {
		if user != nil {
			if dir := user.Rotation().Direction(); dir == cube.East || dir == cube.West {
				return RailEastWest
			}
		}
		return RailNorthSouth
	}
	if len(neighbors) == 1 {
		n := neighbors[0]
		if n.dy == 1 {
			return railAscending(n.face)
		}
		return railStraight(n.face)
	}
	var (
		hasNorth = false
		hasSouth = false
		hasEast  = false
		hasWest  = false
		northUp  = false
		southUp  = false
		eastUp   = false
		westUp   = false
	)
	for _, n := range neighbors {
		switch n.face {
		case cube.FaceNorth:
			hasNorth = true
			northUp = n.dy == 1
		case cube.FaceSouth:
			hasSouth = true
			southUp = n.dy == 1
		case cube.FaceEast:
			hasEast = true
			eastUp = n.dy == 1
		case cube.FaceWest:
			hasWest = true
			westUp = n.dy == 1
		}
	}
	if hasNorth && hasSouth {
		if northUp {
			return RailAscendingNorth
		}
		if southUp {
			return RailAscendingSouth
		}
		return RailNorthSouth
	}
	if hasEast && hasWest {
		if eastUp {
			return RailAscendingEast
		}
		if westUp {
			return RailAscendingWest
		}
		return RailEastWest
	}
	if allowCurves {
		// Use the first two perpendicular faces for a curve.
		return railCurved(neighbors[0].face, neighbors[1].face)
	}
	return railStraight(neighbors[0].face)
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
	r.Direction = railPlaceDirection(pos, tx, user, true)
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
	if !railCanStay(pos, tx, r.Direction) {
		breakBlock(r, pos, tx)
		return
	}
	r.Powered = redstonePowered(pos, tx)
	tx.SetBlock(pos, r, nil)
}

// UseOnBlock ...
func (r PoweredRail) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	r.Direction = railPlaceDirection(pos, tx, user, false)
	if !railCanStay(pos, tx, r.Direction) {
		return false
	}
	place(tx, pos, r, user, ctx)
	return placed(ctx)
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
	r.Direction = railPlaceDirection(pos, tx, user, false)
	if !railCanStay(pos, tx, r.Direction) {
		return false
	}
	place(tx, pos, r, user, ctx)
	return placed(ctx)
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
	if !railCanStay(pos, tx, r.Direction) {
		breakBlock(r, pos, tx)
		return
	}
	r.Powered = redstonePowered(pos, tx)
	tx.SetBlock(pos, r, nil)
}

// UseOnBlock ...
func (r ActivatorRail) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	r.Direction = railPlaceDirection(pos, tx, user, false)
	if !railCanStay(pos, tx, r.Direction) {
		return false
	}
	place(tx, pos, r, user, ctx)
	return placed(ctx)
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
	box := cube.Box(float64(pos[0]), float64(pos[1]), float64(pos[2]), float64(pos[0]+1), float64(pos[1])+1, float64(pos[2]+1))
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
