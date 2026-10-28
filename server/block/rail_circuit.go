package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// BaseRailTransporter evaluates live source strengths after capacitors and
// publishes the rail flag in the next chunk pass. Its strength is remaining
// rail range (normally 1..9), not the visible powered bit.
type railCircuit struct {
	rail                 world.Block
	input, previousInput int
	direction            cube.Face
}

func newRailCircuit(b world.Block, powered bool) world.RedstoneCircuitState {
	s := railCircuit{rail: b}
	s.direction = railComponentDirection(b.(railBlock).RailDirection())
	if powered {
		s.input = 15
	}
	return s
}

func railComponentDirection(dir RailDirection) cube.Face {
	switch dir {
	case RailEastWest, RailAscendingEast, RailAscendingWest:
		return cube.FaceWest
	default:
		return cube.FaceSouth
	}
}

func (s railCircuit) CircuitDirection() cube.Face { return s.direction }

func (r PoweredRail) RedstoneCircuit() world.RedstoneCircuitState {
	return newRailCircuit(r, r.Powered)
}
func (r ActivatorRail) RedstoneCircuit() world.RedstoneCircuitState {
	return newRailCircuit(r, r.Powered)
}

func (r PoweredRail) RedstoneCircuitOutput(output world.Block) world.Block {
	if next, ok := output.(PoweredRail); ok {
		r.Powered = next.Powered
		return r
	}
	return nil
}
func (r ActivatorRail) RedstoneCircuitOutput(output world.Block) world.Block {
	if next, ok := output.(ActivatorRail); ok {
		// ActivatorRailBlock::_onRedstoneUpdate first flips the live flag,
		// then forces it on only for positive strength. Do this at publication
		// so intervening property edits are observed by the native callback.
		if next.Powered {
			r.Powered = true
		} else {
			r.Powered = !r.Powered
		}
		return r
	}
	return nil
}

func (s railCircuit) Cache(cube.Pos, *world.Tx) world.RedstoneCircuitState { return s }
func (s railCircuit) CacheAfterSources(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	s.rail = tx.Block(pos)
	var match railPowerMatcher
	switch s.rail.(type) {
	case PoweredRail:
		match = poweredRailMatch
	case ActivatorRail:
		match = activatorRailMatch
	default:
		return nil
	}
	s.previousInput = s.input
	s.input = max(railCheckSurrounding(pos, tx, true, 0, match), railCheckSurrounding(pos, tx, false, 0, match))
	if railDirectlyPowered(pos, tx, match) {
		s.input = 9
	}
	return s
}

func railDirectlyPowered(pos cube.Pos, tx *world.Tx, match railPowerMatcher) bool {
	r, ok := railAt(pos, tx)
	if !ok {
		return false
	}
	direction, configured := tx.Redstone().CircuitDirection(pos)
	if !configured {
		direction = railComponentDirection(r.RailDirection())
	}
	ascending, sloped := r.RailDirection().AscendingFace()
	for _, face := range cube.Faces() {
		if tx.RedstonePowerFrom(pos, face) == 0 {
			continue
		}
		if face.Axis() != direction.Axis() {
			return true
		}
		source := pos.Side(face)
		if _, directSource := tx.Block(source).(world.RedstonePowerSource); !directSource {
			return true
		}
		replaced := false
		// addSourceItem replaces the record for the same source and entry
		// direction, even when its remaining range is smaller. A source also
		// feeding a connected rail vertically therefore reaches this rail
		// through that rail; it must not restart the range at nine here.
		for _, vertical := range []cube.Face{cube.FaceDown, cube.FaceUp} {
			if vertical == cube.FaceUp && (!sloped || ascending != face) {
				continue
			}
			if vertical == cube.FaceDown && sloped && ascending == face {
				continue
			}
			neighbor := source.Side(vertical)
			shape, sameFamily := match(tx.Block(neighbor))
			if !sameFamily {
				continue
			}
			axis, known := tx.Redstone().CircuitDirection(neighbor)
			if !known {
				axis = railComponentDirection(shape)
			}
			if axis != direction {
				continue
			}
			if tx.RedstoneDirectPowerFrom(neighbor, vertical.Opposite()) > 0 {
				replaced = true
				break
			}
		}
		if !replaced {
			return true
		}
	}
	return false
}
func (s railCircuit) CircuitInputChange() (int, int) { return s.previousInput, s.input }
func (s railCircuit) Evaluate() (world.RedstoneCircuitState, world.Block) {
	if s.input == s.previousInput {
		return s, nil
	}
	switch r := s.rail.(type) {
	case PoweredRail:
		r.Powered = s.input > 0
		s.rail = r
	case ActivatorRail:
		r.Powered = s.input > 0
		s.rail = r
	}
	return s, s.rail
}
func (s railCircuit) Refresh(_, after world.Block) world.RedstoneCircuitState {
	switch after.(type) {
	case PoweredRail, ActivatorRail:
		s.rail = after
		s.direction = railComponentDirection(after.(railBlock).RailDirection())
		return s
	}
	return nil
}

func (r PoweredRail) Placed(pos cube.Pos, tx *world.Tx, previous world.Block) {
	if _, same := previous.(PoweredRail); !same {
		railPlaced(pos, tx, r)
	}
}
func (r ActivatorRail) Placed(pos cube.Pos, tx *world.Tx, previous world.Block) {
	if _, same := previous.(ActivatorRail); !same {
		railPlaced(pos, tx, r)
	}
}
func railPlaced(pos cube.Pos, tx *world.Tx, r railBlock) {
	railSetDirection(pos, tx, r, railNativePlacedDirection(pos, tx, r))
	tx.Redstone().RefreshCircuit(pos)
	placed, _ := railAt(pos, tx)
	for _, n := range railCheckAround(pos, tx, placed.RailDirection().ConnectingFaces()) {
		connected := railCheckConnected(n.pos, n.rail, tx)
		eligible := len(connected) != 2
		for _, connection := range connected {
			if connection.pos == pos {
				eligible = true
				break
			}
		}
		if !eligible {
			continue
		}
		railSetDirection(n.pos, tx, n.rail, railNativePlacedDirection(n.pos, tx, n.rail))
		tx.Redstone().RefreshCircuit(n.pos)
	}
}

// Rail::place resolves the straight axes before checking elevated neighbours.
// East/west wins when both axes are available on a non-curving rail.
func railNativePlacedDirection(pos cube.Pos, tx *world.Tx, r railBlock) RailDirection {
	var neighbors [6]*railNeighbor
	for _, n := range railCheckAround(pos, tx, []cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast}) {
		connected := railCheckConnected(n.pos, n.rail, tx)
		eligible := len(connected) != 2
		for _, connection := range connected {
			if connection.pos == pos {
				eligible = true
				break
			}
		}
		if eligible && neighbors[n.face] == nil {
			neighbor := n
			neighbors[n.face] = &neighbor
		}
	}
	dir := r.RailDirection()
	if neighbors[cube.FaceNorth] != nil || neighbors[cube.FaceSouth] != nil {
		dir = RailNorthSouth
	}
	if neighbors[cube.FaceWest] != nil || neighbors[cube.FaceEast] != nil {
		dir = RailEastWest
	}
	if _, curves := r.(Rail); curves {
		north, south, west, east := neighbors[cube.FaceNorth] != nil, neighbors[cube.FaceSouth] != nil, neighbors[cube.FaceWest] != nil, neighbors[cube.FaceEast] != nil
		switch {
		case south && east && !north && !west:
			dir = RailSouthEast
		case south && west && !north && !east:
			dir = RailSouthWest
		case north && west && !south && !east:
			dir = RailNorthWest
		case north && east && !south && !west:
			dir = RailNorthEast
		}
	}
	if dir == RailNorthSouth {
		if n := neighbors[cube.FaceNorth]; n != nil && n.pos[1] > pos[1] {
			dir = RailAscendingNorth
		}
		if n := neighbors[cube.FaceSouth]; n != nil && n.pos[1] > pos[1] {
			dir = RailAscendingSouth
		}
	} else if dir == RailEastWest {
		if n := neighbors[cube.FaceEast]; n != nil && n.pos[1] > pos[1] {
			dir = RailAscendingEast
		}
		if n := neighbors[cube.FaceWest]; n != nil && n.pos[1] > pos[1] {
			dir = RailAscendingWest
		}
	}
	return dir
}
func (s railCircuit) CircuitPublished(pos cube.Pos, tx *world.Tx, _, after world.Block) {
	tx.DoBlockUpdatesAround(pos.Side(cube.FaceDown))
	if r, ok := after.(railBlock); ok && r.RailDirection().Ascending() {
		tx.DoBlockUpdatesAround(pos.Side(cube.FaceUp))
	}
}
