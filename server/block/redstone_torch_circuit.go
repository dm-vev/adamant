package block

import "github.com/df-mc/dragonfly/server/block/cube"

// torchCircuit is the native capacitor's cache/evaluate state. It deliberately
// takes source lists, not just an on/off attachment input: feedback identity,
// attenuation and directness participate in burnout and dependency ordering.
// torchCircuitState supplies the world graph inputs and publication phases.
type torchCircuit struct {
	selfPowerCount                     int
	on, halfFrame, changed             bool
	nextOn, nextHalfFrame, nextChanged bool
	canReignite                        bool
	nextOrder                          *torchCircuit
}

type torchCircuitSource struct {
	group, componentType uint64
	power, distance      int
	pos                  cube.Pos
	direct               bool
	sources              []torchCircuitSource
}

const (
	torchSourceProducer     = 0x80000
	torchSourceCapacitor    = 0x200000
	torchSourcePoweredBlock = 0x40000
	torchSourceTorch        = 0x200003
)

// cache follows RedstoneTorchCapacitor::cacheValues (client RVA 0x66d97d0).
// In a dependency ring the cache may also adjust following torches' counters.
func (s *torchCircuit) cache(pos cube.Pos, sources []torchCircuitSource) {
	if s.nextOrder != nil {
		s.cacheOrdered(sources)
		return
	}
	var selected, own, other *torchCircuitSource
	input := 0
	selfSelected := false
	for i := range sources {
		source := &sources[i]
		if source.group == torchSourceProducer || source.group == torchSourceCapacitor && len(source.sources) == 0 {
			if source.power >= input && source.power != 0 {
				selected, input = source, source.power
			}
			continue
		}
		selfStrength, lastStrength := 0, 0
		for j := range source.sources {
			child := &source.sources[j]
			if !child.direct {
				continue
			}
			lastStrength = max(child.power-child.distance, 0)
			if lastStrength > 0 && lastStrength >= input {
				selected, input = child, lastStrength
				if child.pos != pos {
					other = child
				}
			}
			if child.pos == pos {
				selfStrength, own = lastStrength, child
			}
		}
		// The native traversal retains references across source lists. A tied
		// external source takes precedence; zero-strength own sources still
		// retain structural feedback when no external input wins.
		if lastStrength == selfStrength && other != nil {
			own, selected = nil, other
		}
		if own != nil && input == 0 {
			selected = own
		}
		selfSelected = own != nil && selected == own
	}
	if selfSelected {
		if s.selfPowerCount < 33 && !s.canReignite {
			s.selfPowerCount++
		}
		if s.selfPowerCount <= 15 {
			s.nextOn, s.nextHalfFrame = !s.on, true
		} else {
			s.nextOn, s.nextHalfFrame = false, s.selfPowerCount == 16
			s.canReignite = s.canReignite || s.selfPowerCount >= 33
		}
	} else {
		s.selfPowerCount, s.canReignite = 0, false
		s.nextOn, s.nextHalfFrame = input == 0, false
	}
	s.nextChanged = s.on != s.nextOn
}

func (s *torchCircuit) cacheOrdered(sources []torchCircuitSource) {
	for _, source := range sources {
		if source.group != torchSourcePoweredBlock && source.group != 0x20000 {
			continue
		}
		for _, child := range source.sources {
			if child.direct && child.componentType != torchSourceTorch && child.power > child.distance {
				s.nextOn, s.nextHalfFrame, s.nextChanged = true, false, !s.on
				s.selfPowerCount = -1
				return
			}
		}
	}
	s.nextHalfFrame = false
	s.nextOn = s.selfPowerCount == 0
	s.nextChanged = s.on != s.nextOn
	if s.selfPowerCount != 0 {
		return
	}
	s.nextOrder.selfPowerCount = -1
	following := s.nextOrder.nextOrder
	for range 4 {
		if following == nil || following == s {
			break
		}
		following.selfPowerCount = 1
		following = following.nextOrder
	}
}

// evaluate follows client RVA 0x66d9c60. Cache and evaluate must be separate:
// evaluating one component cannot change another component's cached input.
func (s *torchCircuit) evaluate() bool {
	s.on, s.halfFrame, s.changed = s.nextOn, s.nextHalfFrame, s.nextChanged
	if s.nextOrder != nil && s.selfPowerCount < 33 && !s.canReignite {
		s.selfPowerCount++
	}
	return s.changed
}
