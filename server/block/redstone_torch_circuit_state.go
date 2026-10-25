package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

type torchCircuitState struct {
	torchCircuit
	block     RedstoneTorch
	group     []cube.Pos
	direction cube.Face
}

func (t RedstoneTorch) RedstoneCircuit() world.RedstoneCircuitState {
	return torchCircuitState{torchCircuit: torchCircuit{on: t.Lit, changed: true}, block: t, direction: t.facing()}
}

func (s torchCircuitState) CircuitDirection() cube.Face { return s.direction }

func (t RedstoneTorch) RedstoneCircuitOutput(output world.Block) world.Block {
	next, ok := output.(RedstoneTorch)
	if !ok || next.facing() != t.facing() {
		return nil
	}
	t.Lit = next.Lit
	return t
}

func (s torchCircuitState) Cache(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	t, ok := tx.Block(pos).(RedstoneTorch)
	if !ok {
		return nil
	}
	// Same-identifier permutation edits do not rerun native setup. Preserve
	// the configured electrical direction while updating the visible block.
	s.block = t
	s.nextOrder, s.group = nil, nil
	s.cache(pos, t.circuitInputs(pos, tx))
	return s
}

func (s torchCircuitState) Evaluate() (world.RedstoneCircuitState, world.Block) {
	s.evaluate()
	if s.on == s.block.Lit {
		return s, nil
	}
	s.block.Lit = s.on
	return s, s.block
}

func (s torchCircuitState) Refresh(before, after world.Block) world.RedstoneCircuitState {
	t, ok := after.(RedstoneTorch)
	if !ok {
		return nil
	}
	beforeName, _ := before.EncodeBlock()
	afterName, _ := after.EncodeBlock()
	if beforeName != afterName {
		return t.RedstoneCircuit()
	}
	s.block = t
	return s
}

func (s torchCircuitState) RefreshPublished(_, after world.Block) world.RedstoneCircuitState {
	if t, ok := after.(RedstoneTorch); ok {
		s.block = t
	}
	return s
}

func (s torchCircuitState) CircuitHalfPulse() bool { return s.halfFrame }

func (s torchCircuitState) CircuitNeighbourChange(_, _ cube.Pos, _ *world.Tx) world.RedstoneCircuitState {
	if s.canReignite {
		s.selfPowerCount, s.canReignite = 0, false
	}
	return s
}

func (s torchCircuitState) CircuitBurnoutStatus() (bool, bool) {
	return s.selfPowerCount >= 16, s.selfPowerCount < 16 || s.canReignite
}

func (s torchCircuitState) CircuitPublished(pos cube.Pos, tx *world.Tx, before, after world.Block) {
	old, ok := before.(RedstoneTorch)
	next, nextOK := after.(RedstoneTorch)
	if ok && nextOK && old.Lit && !next.Lit && s.selfPowerCount >= 16 {
		tx.PlaySound(pos.Vec3Centre(), sound.Fizz{})
	}
}

func (s torchCircuitState) ClearCircuitBurnout() world.RedstoneCircuitState {
	s.selfPowerCount, s.canReignite = 0, false
	return s
}
