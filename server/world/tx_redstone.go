package world

import "github.com/df-mc/dragonfly/server/block/cube"

// Redstone returns a transaction-scoped handle for redstone engine operations.
func (tx *Tx) Redstone() RedstoneTransaction {
	return RedstoneTransaction{tx: tx}
}

// RedstoneTransaction provides access to redstone engine operations within a transaction.
type RedstoneTransaction struct {
	tx *Tx
}

// ScheduleUpdate marks pos and its neighbours for re-evaluation during the next redstone phase.
func (r RedstoneTransaction) ScheduleUpdate(pos cube.Pos) {
	b, _ := r.tx.BlockLoaded(pos)
	r.tx.World().redstone.invalidateAroundBlockChange(pos, b, b, RedstoneUpdateCauseScheduledTick, r.tx.Range())
}

// RefreshCircuit applies an explicit component configuration change, such as
// a player's repeater delay adjustment, while retaining the pulse pipeline.
// Native setup callbacks for block identifier changes refresh automatically.
func (r RedstoneTransaction) RefreshCircuit(pos cube.Pos) {
	e := r.tx.World().redstone
	state := e.circuits[pos]
	if state == nil {
		return
	}
	b, loaded := r.tx.BlockLoaded(pos)
	if loaded {
		e.circuits[pos] = state.Refresh(b, b)
	}
}

// NotifyCircuitNeighbour delivers a synchronous native neighbour notification
// to a component's transient state, independently of electrical evaluation.
func (r RedstoneTransaction) NotifyCircuitNeighbour(pos, changed cube.Pos) {
	e := r.tx.World().redstone
	if state, ok := e.circuits[pos].(RedstoneCircuitNeighbourChange); ok {
		e.circuits[pos] = state.CircuitNeighbourChange(pos, changed, r.tx)
	}
}

// CircuitStrength returns a capacitor's published electrical output. This may
// differ from the block's lit/powered bit during the circuit evaluation phase.
func (r RedstoneTransaction) CircuitStrength(pos cube.Pos) int {
	e := r.tx.World().redstone
	if e.readingCircuitOutputs {
		if power, pending := e.circuitPowerOutputs[pos]; pending {
			return power
		}
	}
	return r.tx.World().redstone.circuitPowers[pos]
}

// CircuitDirection returns a native component's configured direction, if it
// maintains one independently of its current block permutation.
func (r RedstoneTransaction) CircuitDirection(pos cube.Pos) (cube.Face, bool) {
	if state, ok := r.tx.World().redstone.circuits[pos].(RedstoneCircuitDirection); ok {
		return state.CircuitDirection(), true
	}
	return 0, false
}

// SetCircuitStrength changes a capacitor input for the next circuit step.
func (r RedstoneTransaction) SetCircuitStrength(pos cube.Pos, power int) {
	e := r.tx.World().redstone
	if state, ok := e.circuits[pos].(RedstoneCircuitStrength); ok {
		e.circuits[pos] = state.WithCircuitStrength(ClampRedstonePower(power))
	}
}

// ObservedPower returns the input power the engine last observed at pos when
// it updated the block there.
func (r RedstoneTransaction) ObservedPower(pos cube.Pos) int {
	return r.tx.World().redstone.power[pos]
}

// Torch returns a transaction-scoped handle for transient redstone torch state at pos.
func (r RedstoneTransaction) Torch(pos cube.Pos) RedstoneTorchTransaction {
	return RedstoneTorchTransaction{tx: r.tx, pos: pos}
}

// RedstoneTorchTransaction provides access to transient redstone torch state within a transaction.
type RedstoneTorchTransaction struct {
	tx  *Tx
	pos cube.Pos
}

// BurnoutStatus returns the transient burnout state for the redstone torch.
func (t RedstoneTorchTransaction) BurnoutStatus() (burnedOut, recoverable bool) {
	if state, ok := t.tx.World().redstone.circuits[t.pos].(RedstoneCircuitBurnout); ok {
		return state.CircuitBurnoutStatus()
	}
	return false, true
}

// ClearBurnout explicitly resets the world-owned native capacitor's counter.
func (t RedstoneTorchTransaction) ClearBurnout() {
	if state, ok := t.tx.World().redstone.circuits[t.pos].(RedstoneCircuitBurnout); ok {
		t.tx.World().redstone.circuits[t.pos] = state.ClearCircuitBurnout()
	}
}

// RedstonePower returns the strongest redstone power currently applied to the position passed. Custom redstone block
// implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstonePower(pos cube.Pos) int {
	return tx.World().redstone.powerTo(pos, tx)
}

// RedstoneDirectPower returns the strongest direct redstone power currently applied to the position passed, excluding
// power conducted through solid blocks. Custom redstone block implementations may use this method to query the
// transaction's current redstone state.
func (tx *Tx) RedstoneDirectPower(pos cube.Pos) int {
	return tx.World().redstone.directPower(pos, tx)
}

// RedstoneStrongPower returns the strongest strong redstone power currently applied to the position passed. Custom
// redstone block implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstoneStrongPower(pos cube.Pos) int {
	return tx.World().redstone.strongPower(pos, tx)
}

// RedstoneConductivePower returns the power held by pos as a conductive block, excluding direct component activation.
// Custom redstone block implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstoneConductivePower(pos cube.Pos) int {
	return tx.World().redstone.conductivePowerTo(pos, tx)
}

// RedstonePowerFrom returns the strongest redstone power reaching pos from the side passed. Custom redstone block
// implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstonePowerFrom(pos cube.Pos, face cube.Face) int {
	return tx.World().redstone.powerFrom(pos, tx, face)
}

// RedstoneDirectPowerFrom returns the strongest direct redstone power reaching pos from the side passed. Custom
// redstone block implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstoneDirectPowerFrom(pos cube.Pos, face cube.Face) int {
	return tx.World().redstone.directPowerFrom(pos, tx, face)
}

// RedstoneStrongPowerFrom returns the strongest strong redstone power reaching pos from the side passed. Custom
// redstone block implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstoneStrongPowerFrom(pos cube.Pos, face cube.Face) int {
	return tx.World().redstone.strongPowerFrom(pos, tx, face)
}
