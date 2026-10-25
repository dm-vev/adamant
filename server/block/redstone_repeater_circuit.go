package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type repeaterCircuitState struct {
	repeaterCircuit
	block         RedstoneRepeater
	input, locked bool
	insertAt      int
}

// RedstoneCircuit installs the native capacitor independently of block-state
// updates: changing the output or delay must not discard queued pulses.
func (r RedstoneRepeater) RedstoneCircuit() world.RedstoneCircuitState {
	return repeaterCircuitState{repeaterCircuit: newRepeaterCircuit(r.Powered), block: r, insertAt: r.Delay}
}

// RedstoneCircuitOutput preserves delay changes made between evaluation and
// publication, and rejects the previous orientation's pending output.
func (r RedstoneRepeater) RedstoneCircuitOutput(output world.Block) world.Block {
	next, ok := output.(RedstoneRepeater)
	if !ok || next.Facing != r.Facing {
		return nil
	}
	r.Powered = next.Powered
	return r
}

func (s repeaterCircuitState) Cache(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	r, ok := tx.Block(pos).(RedstoneRepeater)
	if !ok {
		return nil
	}
	if r.Facing != s.block.Facing {
		s.repeaterCircuit = newRepeaterCircuit(r.Powered)
		s.insertAt = r.Delay
	}
	s.block = r
	s.input, s.locked = r.shouldBePowered(pos, tx), r.isLocked(pos, tx)
	return s
}

func (s repeaterCircuitState) Evaluate() (world.RedstoneCircuitState, world.Block) {
	powered := s.step(s.input, s.insertAt, s.locked)
	if powered == s.block.Powered {
		return s, nil
	}
	s.block.Powered = powered
	return s, s.block
}

func (s repeaterCircuitState) Refresh(before, after world.Block) world.RedstoneCircuitState {
	r, ok := after.(RedstoneRepeater)
	if !ok {
		return nil
	}
	previous, ok := before.(RedstoneRepeater)
	if !ok || previous.Facing != r.Facing {
		return r.RedstoneCircuit()
	}
	if !previous.Powered && r.Powered {
		// setupRedstoneComponent sets strength before reading the new delay.
		for i := range s.stages {
			s.stages[i] = 0
			if i <= s.insertAt {
				s.stages[i] = 3
			}
		}
		s.powered = true
	}
	s.insertAt = r.Delay
	s.block = r
	return s
}

// repeaterCircuit holds the four-stage pipeline used by Bedrock's
// RepeaterCapacitor. The high bit of a stage holds a stretched pulse in place;
// this is separate from the side-input lock that freezes the whole capacitor.
type repeaterCircuit struct {
	stages           [4]uint8
	pulseCount       int
	pulse, nextPulse bool
	powered          bool
}

func newRepeaterCircuit(powered bool) repeaterCircuit {
	s := repeaterCircuit{stages: [4]uint8{2, 2, 2, 2}, powered: powered}
	if powered {
		// setupRedstoneComponent calls setStrength before updateDelay, so
		// the newly created capacitor still has insertion index zero.
		s.stages = [4]uint8{3, 0, 0, 0}
	}
	return s
}

// step follows checkLock, cacheValues and evaluate in that order. One call is
// a circuit step (two game ticks), rather than an arbitrary neighbor update.
func (s *repeaterCircuit) step(input bool, delay int, locked bool) bool {
	if locked {
		for i := 1; i < len(s.stages); i++ {
			s.stages[i] = s.stages[0]
		}
		return s.powered
	}
	if s.pulse == input {
		s.pulseCount = 0
	} else {
		if s.pulseCount == 0 {
			s.nextPulse = !s.pulse
		}
		s.pulseCount++
		s.pulse = !s.pulse
	}

	delay = min(max(delay, 0), 3)
	copy(s.stages[:delay], s.stages[1:delay+1])
	for i := delay; i < len(s.stages); i++ {
		s.stages[i] = boolByte(input)
	}
	if delay > 0 {
		if s.pulseCount > delay && !(s.pulseCount == 2 && s.pulse) {
			if s.stages[0] <= 1 {
				value := boolByte(s.nextPulse) | 2
				s.nextPulse = !s.nextPulse
				for i := 0; i <= delay; i++ {
					s.stages[i] = value
				}
			}
		} else if s.stages[0] == 2 && s.stages[1] == 0 {
			s.stages[1] = 2
		} else if s.stages[0] == 3 && s.stages[1] == 1 {
			s.stages[1] = 3
		} else if s.stages[0] <= 1 {
			count := 0
			for _, stage := range s.stages[:delay+1] {
				if stage == s.stages[0] {
					count++
				}
			}
			value := uint8(3)
			if s.stages[0] == 0 && count > delay {
				value = 2
			}
			for i := 0; i <= delay; i++ {
				s.stages[i] = value
			}
		}
	}
	s.powered = s.stages[0]&^2 == 1
	return s.powered
}
