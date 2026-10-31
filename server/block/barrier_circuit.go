package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Doors, trapdoors and gates register native scene consumers. Their visible
// open bit seeds the consumer, but their handlers ignore its first publication.
// Later electrical changes publish on the shared circuit clock; a manual or
// activating-attack toggle does not overwrite the consumer's input history.
type barrierCircuit struct {
	block                            world.Block
	input, previousInput, otherInput int
	evaluated                        bool
	logicalOpen                      bool
}

func newBarrierCircuit(b world.Block) world.RedstoneCircuitState {
	open, _, _ := barrierOpen(b)
	input := 0
	if open {
		input = 15
	}
	return barrierCircuit{block: b, input: input}
}

func (d WoodDoor) RedstoneCircuit() world.RedstoneCircuitState       { return newBarrierCircuit(d) }
func (d CopperDoor) RedstoneCircuit() world.RedstoneCircuitState     { return newBarrierCircuit(d) }
func (d IronDoor) RedstoneCircuit() world.RedstoneCircuitState       { return newBarrierCircuit(d) }
func (t WoodTrapdoor) RedstoneCircuit() world.RedstoneCircuitState   { return newBarrierCircuit(t) }
func (t CopperTrapdoor) RedstoneCircuit() world.RedstoneCircuitState { return newBarrierCircuit(t) }
func (t IronTrapdoor) RedstoneCircuit() world.RedstoneCircuitState   { return newBarrierCircuit(t) }
func (f WoodFenceGate) RedstoneCircuit() world.RedstoneCircuitState  { return newBarrierCircuit(f) }

func (d WoodDoor) RedstoneCircuitOutput(b world.Block) world.Block   { return mergeBarrierOutput(d, b) }
func (d CopperDoor) RedstoneCircuitOutput(b world.Block) world.Block { return mergeBarrierOutput(d, b) }
func (d IronDoor) RedstoneCircuitOutput(b world.Block) world.Block   { return mergeBarrierOutput(d, b) }
func (t WoodTrapdoor) RedstoneCircuitOutput(b world.Block) world.Block {
	return mergeBarrierOutput(t, b)
}
func (t CopperTrapdoor) RedstoneCircuitOutput(b world.Block) world.Block {
	return mergeBarrierOutput(t, b)
}
func (t IronTrapdoor) RedstoneCircuitOutput(b world.Block) world.Block {
	return mergeBarrierOutput(t, b)
}
func (f WoodFenceGate) RedstoneCircuitOutput(b world.Block) world.Block {
	return mergeBarrierOutput(f, b)
}

func mergeBarrierOutput(live, output world.Block) world.Block {
	open, face, door := barrierOpen(live)
	nextOpen, _, _ := barrierOpen(output)
	if open == nextOpen && !(door && face == cube.FaceDown) {
		return nil
	}
	next := barrierWithOpen(live, nextOpen)
	a, properties := next.Hash()
	b, expected := output.Hash()
	if a != b || properties != expected {
		return nil
	}
	return next
}

func (s barrierCircuit) Cache(cube.Pos, *world.Tx) world.RedstoneCircuitState { return s }
func (s barrierCircuit) CacheAfterSources(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	s.block = tx.Block(pos)
	s.previousInput, s.input = s.input, barrierInput(pos, tx)
	s.logicalOpen, _, _ = barrierOpen(s.block)
	s.otherInput = 0
	if _, face, door := barrierOpen(s.block); door {
		other := pos.Side(face)
		name, _ := s.block.EncodeBlock()
		otherName, _ := tx.Block(other).EncodeBlock()
		if name == otherName {
			s.otherInput = barrierInput(other, tx)
			if face == cube.FaceDown {
				s.logicalOpen, _, _ = barrierOpen(tx.Block(other))
			}
		}
	}
	return s
}
func barrierInput(pos cube.Pos, tx *world.Tx) (power int) {
	for _, face := range cube.Faces() {
		power = max(power, tx.RedstonePowerFrom(pos, face))
	}
	return
}
func (s barrierCircuit) Evaluate() (world.RedstoneCircuitState, world.Block) {
	first := !s.evaluated
	s.evaluated = true
	if first || s.input == s.previousInput {
		return s, nil
	}
	nextOpen := s.input > 0 || s.otherInput > 0
	if s.logicalOpen == nextOpen {
		return s, nil
	}
	return s, barrierWithOpen(s.block, nextOpen)
}
func (s barrierCircuit) CircuitConsumerStrength() int   { return s.input }
func (s barrierCircuit) CircuitInputChange() (int, int) { return s.previousInput, s.input }
func (s barrierCircuit) PublishCircuit(pos cube.Pos, tx *world.Tx, before, after world.Block) bool {
	open, _, _ := barrierOpen(after)
	if _, _, door := barrierOpen(before); door {
		setDoorOpenPublication(pos, before, open, tx, true)
		return true
	}
	ready := tx.Defer(func(*world.Tx) {}).Done()
	tx.SetBlock(pos, markInteraction(after, blockInteractionUpdate{active: true, ready: ready}), nil)
	event := barrierToggleSound(tx, after, open)
	tx.PlaySound(pos.Vec3Centre(), event)
	return true
}
func (s barrierCircuit) Refresh(_, after world.Block) world.RedstoneCircuitState {
	s.block = after
	return s
}

// barrierOpen also identifies the other half of a door. Gates and trapdoors
// have no partner consumer.
func barrierOpen(b world.Block) (open bool, other cube.Face, door bool) {
	switch b := b.(type) {
	case WoodDoor:
		return b.Open, cube.Face(boolByte(!b.Top)), true
	case CopperDoor:
		return b.Open, cube.Face(boolByte(!b.Top)), true
	case IronDoor:
		return b.Open, cube.Face(boolByte(!b.Top)), true
	case WoodTrapdoor:
		return b.Open, 0, false
	case CopperTrapdoor:
		return b.Open, 0, false
	case IronTrapdoor:
		return b.Open, 0, false
	case WoodFenceGate:
		return b.Open, 0, false
	}
	panic("block is not a barrier consumer")
}
func barrierWithOpen(b world.Block, open bool) world.Block {
	switch b := b.(type) {
	case WoodDoor:
		b.Open = open
		return b
	case CopperDoor:
		b.Open = open
		return b
	case IronDoor:
		b.Open = open
		return b
	case WoodTrapdoor:
		b.Open = open
		return b
	case CopperTrapdoor:
		b.Open = open
		return b
	case IronTrapdoor:
		b.Open = open
		return b
	case WoodFenceGate:
		b.Open = open
		return b
	}
	panic("block is not a barrier consumer")
}
