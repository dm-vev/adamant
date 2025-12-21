package world

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
)

const (
	redstoneBatchSize           = 16
	redstoneMaxApplyPerTick     = 512
	redstoneMaxWireUpdatesBatch = 2048
	redstoneMaxWireDistance     = 15
	redstoneVerticalRange       = 1
)

// RedstonePowerSource exposes redstone power output for a block.
type RedstonePowerSource interface {
	RedstoneWeakPower(face cube.Face) uint8
	RedstoneStrongPower(face cube.Face) uint8
}

// RedstoneWire exposes redstone wire-specific behaviour.
type RedstoneWire interface {
	RedstoneWirePower() uint8
	RedstoneWirePowerTo(pos cube.Pos, face cube.Face, src BlockSource) uint8
	WithRedstoneWirePower(power uint8) Block
}

// RedstoneDiode marks a redstone diode (repeaters/comparators) with a facing direction.
type RedstoneDiode interface {
	RedstoneDiodeFacing() cube.Direction
}

// RedstoneConnectable marks a block that redstone wire can connect to.
type RedstoneConnectable interface {
	RedstoneConnectsTo(face cube.Face) bool
}

// RedstonePowerAt returns the redstone power level emitted from the block at pos towards the neighbour on face.
func RedstonePowerAt(src BlockSource, pos cube.Pos, face cube.Face) uint8 {
	if isNormalBlock(src, pos) {
		return redstoneStrongPowerFromNeighbours(src, pos)
	}
	return redstoneWeakPowerAt(src, pos, face)
}

// RedstonePowerFromSide returns the power level emitted from the neighbour on face towards pos.
func RedstonePowerFromSide(src BlockSource, pos cube.Pos, face cube.Face) uint8 {
	return RedstonePowerAt(src, pos.Side(face), face.Opposite())
}

// RedstoneSidePowered returns true if the block adjacent to pos on face emits any power towards pos.
func RedstoneSidePowered(src BlockSource, pos cube.Pos, face cube.Face) bool {
	return RedstonePowerFromSide(src, pos, face) > 0
}

func redstoneWeakPowerAt(src BlockSource, pos cube.Pos, face cube.Face) uint8 {
	if wire, ok := src.Block(pos).(RedstoneWire); ok {
		return wire.RedstoneWirePowerTo(pos, face, src)
	}
	if source, ok := src.Block(pos).(RedstonePowerSource); ok {
		return source.RedstoneWeakPower(face)
	}
	return 0
}

func redstoneStrongPowerAt(src BlockSource, pos cube.Pos, face cube.Face) uint8 {
	if wire, ok := src.Block(pos).(RedstoneWire); ok {
		return wire.RedstoneWirePowerTo(pos, face, src)
	}
	if source, ok := src.Block(pos).(RedstonePowerSource); ok {
		return source.RedstoneStrongPower(face)
	}
	return 0
}

func redstoneStrongPowerFromNeighbours(src BlockSource, pos cube.Pos) uint8 {
	var power uint8
	for _, face := range cube.Faces() {
		blockPower := redstoneStrongPowerAt(src, pos.Side(face), face.Opposite())
		if blockPower >= 15 {
			return 15
		}
		if blockPower > power {
			power = blockPower
		}
	}
	return power
}

func redstoneStrongPowerFromNeighboursNoWire(src BlockSource, pos cube.Pos) uint8 {
	var power uint8
	for _, face := range cube.Faces() {
		if _, ok := src.Block(pos.Side(face)).(RedstoneWire); ok {
			continue
		}
		blockPower := redstoneStrongPowerAt(src, pos.Side(face), face.Opposite())
		if blockPower >= 15 {
			return 15
		}
		if blockPower > power {
			power = blockPower
		}
	}
	return power
}

func isNormalBlock(src BlockSource, pos cube.Pos) bool {
	b := src.Block(pos)
	if _, ok := b.(RedstonePowerSource); ok {
		return false
	}
	for _, face := range cube.Faces() {
		if !b.Model().FaceSolid(pos, face, src) {
			return false
		}
	}
	return true
}

type redstoneChange struct {
	pos   cube.Pos
	block Block
}

// redstoneEngine queues redstone wire recalculations and applies them in small batches to avoid TPS drops.
type redstoneEngine struct {
	w *World

	wakeCh    chan struct{}
	closeCh   chan struct{}
	applyCh   chan []redstoneChange
	waitGroup sync.WaitGroup

	pendingMu sync.Mutex
	pending   map[cube.Pos]struct{}

	applyPending map[cube.Pos]Block
	applyOrder   []cube.Pos
}

func newRedstoneEngine(w *World) *redstoneEngine {
	e := &redstoneEngine{
		w:            w,
		wakeCh:       make(chan struct{}, 1),
		closeCh:      make(chan struct{}),
		applyCh:      make(chan []redstoneChange, 8),
		pending:      map[cube.Pos]struct{}{},
		applyPending: map[cube.Pos]Block{},
	}
	e.waitGroup.Add(1)
	go e.run()
	return e
}

func (e *redstoneEngine) Close() {
	close(e.closeCh)
	e.waitGroup.Wait()
}

func (e *redstoneEngine) QueueUpdate(pos cube.Pos) {
	if e == nil {
		return
	}
	e.pendingMu.Lock()
	e.pending[pos] = struct{}{}
	e.pendingMu.Unlock()
	select {
	case e.wakeCh <- struct{}{}:
	default:
	}
}

func (e *redstoneEngine) run() {
	defer e.waitGroup.Done()
	for {
		select {
		case <-e.closeCh:
			return
		case <-e.wakeCh:
		}

		for {
			batch := e.drainBatch(redstoneBatchSize)
			if len(batch) == 0 {
				break
			}
			e.processBatch(batch)
			if !e.hasPending() {
				break
			}
		}
	}
}

func (e *redstoneEngine) hasPending() bool {
	e.pendingMu.Lock()
	defer e.pendingMu.Unlock()
	return len(e.pending) > 0
}

func (e *redstoneEngine) drainBatch(max int) []cube.Pos {
	e.pendingMu.Lock()
	defer e.pendingMu.Unlock()
	if len(e.pending) == 0 {
		return nil
	}
	if max <= 0 {
		max = 1
	}
	batch := make([]cube.Pos, 0, minInt(max, len(e.pending)))
	for pos := range e.pending {
		batch = append(batch, pos)
		delete(e.pending, pos)
		if len(batch) >= max {
			break
		}
	}
	return batch
}

func (e *redstoneEngine) requeue(positions []cube.Pos) {
	if len(positions) == 0 {
		return
	}
	e.pendingMu.Lock()
	for _, pos := range positions {
		e.pending[pos] = struct{}{}
	}
	e.pendingMu.Unlock()
	select {
	case e.wakeCh <- struct{}{}:
	default:
	}
}

func (e *redstoneEngine) processBatch(batch []cube.Pos) {
	positions := make(map[cube.Pos]struct{}, len(batch)*128)
	for _, base := range batch {
		for dx := -redstoneMaxWireDistance; dx <= redstoneMaxWireDistance; dx++ {
			for dz := -redstoneMaxWireDistance; dz <= redstoneMaxWireDistance; dz++ {
				for dy := -redstoneVerticalRange; dy <= redstoneVerticalRange; dy++ {
					pos := cube.Pos{base[0] + dx, base[1] + dy, base[2] + dz}
					if pos.OutOfBounds(e.w.ra) {
						continue
					}
					positions[pos] = struct{}{}
				}
			}
		}
	}

	snap := e.w.redstoneSnapshot(positions)
	changes := map[cube.Pos]Block{}
	overlay := redstoneOverlay{base: snap, changes: changes}

	queue := make([]cube.Pos, 0, len(batch)*7)
	queued := map[cube.Pos]struct{}{}
	enqueue := func(pos cube.Pos) {
		if _, ok := queued[pos]; ok {
			return
		}
		queued[pos] = struct{}{}
		queue = append(queue, pos)
	}
	for _, pos := range batch {
		enqueue(pos)
		for _, face := range cube.Faces() {
			enqueue(pos.Side(face))
		}
	}

	processed := 0
	for len(queue) > 0 && processed < redstoneMaxWireUpdatesBatch {
		pos := queue[0]
		queue = queue[1:]
		delete(queued, pos)

		wire, ok := overlay.Block(pos).(RedstoneWire)
		if !ok {
			continue
		}
		processed++
		newPower := computeWirePower(pos, wire, overlay)
		if newPower == wire.RedstoneWirePower() {
			continue
		}
		changes[pos] = wire.WithRedstoneWirePower(newPower)
		for _, face := range cube.Faces() {
			enqueue(pos.Side(face))
		}
		enqueue(pos)
	}

	if len(queue) > 0 {
		e.requeue(queue)
	}

	if len(changes) == 0 {
		return
	}
	updates := make([]redstoneChange, 0, len(changes))
	for pos, block := range changes {
		updates = append(updates, redstoneChange{pos: pos, block: block})
	}
	select {
	case e.applyCh <- updates:
	case <-e.closeCh:
		return
	}
}

func (e *redstoneEngine) Apply(tx *Tx) {
	if e == nil {
		return
	}
	for {
		select {
		case batch := <-e.applyCh:
			for _, change := range batch {
				if _, ok := e.applyPending[change.pos]; !ok {
					e.applyOrder = append(e.applyOrder, change.pos)
				}
				e.applyPending[change.pos] = change.block
			}
		default:
			goto apply
		}
	}
apply:
	for i := 0; i < redstoneMaxApplyPerTick && len(e.applyOrder) > 0; i++ {
		pos := e.applyOrder[0]
		e.applyOrder = e.applyOrder[1:]
		block := e.applyPending[pos]
		delete(e.applyPending, pos)
		tx.SetBlock(pos, block, nil)
		e.notifyIndirectUpdates(pos, tx)
	}
}

func (e *redstoneEngine) notifyIndirectUpdates(pos cube.Pos, tx *Tx) {
	w := tx.World()
	for _, face := range cube.Faces() {
		normalPos := pos.Side(face)
		if normalPos.OutOfBounds(w.ra) {
			continue
		}
		if !isNormalBlock(tx, normalPos) {
			continue
		}
		w.updateNeighbour(normalPos, pos)
		normalPos.Neighbours(func(p cube.Pos) {
			w.updateNeighbour(p, normalPos)
		}, w.ra)
	}
}

type redstoneSnapshot struct {
	blocks map[cube.Pos]Block
}

func (s redstoneSnapshot) Block(pos cube.Pos) Block {
	if b, ok := s.blocks[pos]; ok {
		return b
	}
	return air()
}

func (w *World) redstoneSnapshot(positions map[cube.Pos]struct{}) redstoneSnapshot {
	if len(positions) == 0 {
		return redstoneSnapshot{blocks: map[cube.Pos]Block{}}
	}
	blocks := make(map[cube.Pos]Block, len(positions))
	<-w.Exec(func(tx *Tx) {
		for pos := range positions {
			if pos.OutOfBounds(w.ra) {
				blocks[pos] = air()
				continue
			}
			blocks[pos] = tx.Block(pos)
		}
	})
	return redstoneSnapshot{blocks: blocks}
}

type redstoneOverlay struct {
	base    redstoneSnapshot
	changes map[cube.Pos]Block
}

func (o redstoneOverlay) Block(pos cube.Pos) Block {
	if b, ok := o.changes[pos]; ok {
		return b
	}
	return o.base.Block(pos)
}

func computeWirePower(pos cube.Pos, wire RedstoneWire, src BlockSource) uint8 {
	current := int(wire.RedstoneWirePower())
	maxStrength := current
	power := indirectWirePower(pos, src)
	if power > 0 && power > maxStrength-1 {
		maxStrength = power
	}

	strength := 0
	for _, face := range cube.HorizontalFaces() {
		sidePos := pos.Side(face)
		strength = maxInt(strength, maxWireStrengthAt(sidePos, src))
		sideNormal := isNormalBlock(src, sidePos)

		if sideNormal && !isNormalBlock(src, pos.Side(cube.FaceUp)) {
			strength = maxInt(strength, maxWireStrengthAt(sidePos.Side(cube.FaceUp), src))
		} else if !sideNormal {
			strength = maxInt(strength, maxWireStrengthAt(sidePos.Side(cube.FaceDown), src))
		}
	}

	if strength > maxStrength {
		maxStrength = strength - 1
	} else if maxStrength > 0 {
		maxStrength--
	} else {
		maxStrength = 0
	}

	if power > maxStrength-1 {
		maxStrength = power
	} else if power < maxStrength && strength <= maxStrength {
		maxStrength = maxInt(power, strength-1)
	}

	if maxStrength < 0 {
		maxStrength = 0
	} else if maxStrength > 15 {
		maxStrength = 15
	}
	return uint8(maxStrength)
}

func indirectWirePower(pos cube.Pos, src BlockSource) int {
	power := 0
	for _, face := range cube.Faces() {
		blockPower := indirectPowerFrom(pos.Side(face), face, src)
		if blockPower >= 15 {
			return 15
		}
		if blockPower > power {
			power = blockPower
		}
	}
	return power
}

func indirectPowerFrom(pos cube.Pos, face cube.Face, src BlockSource) int {
	if _, ok := src.Block(pos).(RedstoneWire); ok {
		return 0
	}
	if isNormalBlock(src, pos) {
		return int(redstoneStrongPowerFromNeighboursNoWire(src, pos))
	}
	return int(redstoneWeakPowerAt(src, pos, face.Opposite()))
}

func maxWireStrengthAt(pos cube.Pos, src BlockSource) int {
	if wire, ok := src.Block(pos).(RedstoneWire); ok {
		return int(wire.RedstoneWirePower())
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
