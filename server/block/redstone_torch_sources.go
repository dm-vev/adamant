package block

import (
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// circuitInputs retains actual source identity instead of treating a powered
// wire as a new producer. An unlit own torch remains in the source list: the
// native capacitor distinguishes a dormant feedback path from no feedback.
// Capacitors such as repeaters are endpoints, not transparent wire relayers.
func (t RedstoneTorch) circuitInputs(pos cube.Pos, tx *world.Tx) []torchCircuitSource {
	attached := pos.Side(t.circuitFacing(pos, tx))
	b, loaded := tx.BlockLoaded(attached)
	if !loaded {
		return nil
	}
	if source, ok := b.(world.RedstonePowerSource); ok {
		power := 0
		for _, face := range cube.Faces() {
			power = max(power, source.RedstonePower(attached, tx, face))
		}
		return []torchCircuitSource{{group: torchSourceProducer, pos: attached, power: power}}
	}
	if !world.RedstoneFullPowerConductor(attached, b, tx) {
		return nil
	}
	sources := make(map[cube.Pos]torchCircuitSource)
	for _, face := range cube.Faces() {
		sourcePos := attached.Side(face)
		source, loaded := tx.BlockLoaded(sourcePos)
		if !loaded {
			continue
		}
		if wire, ok := source.(RedstoneWire); ok {
			wire.Power = 15
			if wire.RedstonePower(sourcePos, tx, face.Opposite()) > 0 {
				torchWireSources(sourcePos, tx, sources)
			}
			continue
		}
		if contribution, ok := torchCircuitSourceAt(sourcePos, source, tx, face.Opposite(), true); ok {
			torchAddSource(sources, contribution)
		} else if weak, ok := source.(world.RedstoneWeakBlockPowerer); ok && weak.RedstoneWeaklyPowersBlocks() {
			if contribution, ok := torchCircuitSourceAt(sourcePos, source, tx, face.Opposite(), false); ok {
				torchAddSource(sources, contribution)
			}
		}
	}
	children := make([]torchCircuitSource, 0, len(sources))
	for _, source := range sources {
		children = append(children, source)
	}
	slices.SortFunc(children, func(a, b torchCircuitSource) int {
		for _, axis := range [...]int{1, 2, 0} {
			if a.pos[axis] != b.pos[axis] {
				return a.pos[axis] - b.pos[axis]
			}
		}
		return 0
	})
	return []torchCircuitSource{{group: torchSourcePoweredBlock, pos: attached, sources: children}}
}

// torchWireSources walks incoming dust edges, retaining shortest source
// attenuation. Every read is limited to loaded blocks and signal range.
func torchWireSources(start cube.Pos, tx *world.Tx, sources map[cube.Pos]torchCircuitSource) {
	type step struct {
		pos      cube.Pos
		distance int
	}
	queue := []step{{pos: start}}
	seen := make(map[cube.Pos]int)
	for head := 0; head < len(queue); head++ {
		current := queue[head]
		if current.distance >= 15 {
			continue
		}
		if previous, ok := seen[current.pos]; ok && previous <= current.distance {
			continue
		}
		seen[current.pos] = current.distance
		for _, face := range cube.Faces() {
			neighbour := current.pos.Side(face)
			b, loaded := tx.BlockLoaded(neighbour)
			if !loaded {
				continue
			}
			if _, wire := b.(RedstoneWire); wire {
				continue
			}
			if contribution, ok := torchCircuitSourceAt(neighbour, b, tx, face.Opposite(), false); ok {
				contribution.distance = current.distance
				torchAddSource(sources, contribution)
			}
			if !world.RedstoneFullPowerConductor(neighbour, b, tx) {
				continue
			}
			for _, sourceFace := range cube.Faces() {
				p := neighbour.Side(sourceFace)
				if source, loaded := tx.BlockLoaded(p); loaded {
					if contribution, ok := torchCircuitSourceAt(p, source, tx, sourceFace.Opposite(), true); ok {
						contribution.distance = current.distance
						torchAddSource(sources, contribution)
					}
				}
			}
		}
		// Incoming links can differ from outgoing links on vertical steps.
		for x := current.pos[0] - 1; x <= current.pos[0]+1; x++ {
			for y := current.pos[1] - 1; y <= current.pos[1]+1; y++ {
				for z := current.pos[2] - 1; z <= current.pos[2]+1; z++ {
					p := cube.Pos{x, y, z}
					if p == current.pos || p.OutOfBounds(tx.Range()) {
						continue
					}
					if previous, ok := seen[p]; ok && previous <= current.distance+1 {
						continue
					}
					b, loaded := tx.BlockLoaded(p)
					wire, ok := b.(RedstoneWire)
					if loaded && ok && slices.Contains(wire.RedstoneRelayerNeighbours(p, tx), current.pos) {
						queue = append(queue, step{p, current.distance + 1})
					}
				}
			}
		}
	}
}

func torchAddSource(sources map[cube.Pos]torchCircuitSource, source torchCircuitSource) {
	previous, exists := sources[source.pos]
	if !exists || source.distance < previous.distance || source.distance == previous.distance && source.power > previous.power {
		sources[source.pos] = source
	}
}

// torchCircuitSourceAt keeps inactive directional capacitors, whose potential
// connections remain present in the scene graph while their strength is zero.
func torchCircuitSourceAt(pos cube.Pos, b world.Block, tx *world.Tx, face cube.Face, strong bool) (torchCircuitSource, bool) {
	source := torchCircuitSource{pos: pos, direct: true, group: torchSourceProducer}
	switch b := b.(type) {
	case RedstoneWire:
		return source, false
	case RedstoneTorch:
		if face == b.circuitFacing(pos, tx) || strong && face != cube.FaceUp {
			return source, false
		}
		source.group, source.componentType = torchSourceCapacitor, torchSourceTorch
		if b.Lit {
			source.power = 15
		}
	case RedstoneRepeater:
		if face != b.Facing.Opposite().Face() {
			return source, false
		}
		source.group, source.componentType = torchSourceCapacitor, 0x200004
		if b.Powered {
			source.power = 15
		}
	case RedstoneComparator:
		if face != b.circuitFacing(pos, tx).Opposite() {
			return source, false
		}
		source.group = torchSourceCapacitor
		source.power = b.RedstonePower(pos, tx, face)
	case Observer:
		if face != b.Facing.Opposite() {
			return source, false
		}
		source.group = torchSourceCapacitor
		source.power = b.RedstonePower(pos, tx, face)
	default:
		if strong {
			if producer, ok := b.(world.RedstoneStrongPowerSource); ok {
				source.power = producer.RedstoneStrongPower(pos, tx, face)
			}
		} else if producer, ok := b.(world.RedstonePowerSource); ok {
			source.power = producer.RedstonePower(pos, tx, face)
		}
		if source.power == 0 {
			return source, false
		}
	}
	source.power = world.ClampRedstonePower(source.power)
	return source, true
}
