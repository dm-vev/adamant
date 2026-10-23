package entity

import "github.com/df-mc/dragonfly/server/internal/vanilla"

// SlimeCore contains source-shared size, jump cadence and split-child rules for
// the Slime and MagmaCube families. Attack and damage transitions stay in each species.
type SlimeCore struct {
	Size int
	JumpDelay int
}

// NewSlimeCore returns source defaults for the requested native slime size.
func NewSlimeCore(size int) *SlimeCore {
	if size < 1 { size = 1 }
	return &SlimeCore{Size: size, JumpDelay: 10}
}

func newSpeciesSlimeCore(kind speciesKind, size int) *SlimeCore {
	if kind != slimeSpecies { return nil }
	return NewSlimeCore(size)
}

// Jump advances the source jump timer and reports when a grounded jump begins.
func (s *SlimeCore) Jump(random *vanilla.Random, onGround bool) bool {
	if s.JumpDelay > 0 { s.JumpDelay-- }
	if !onGround || s.JumpDelay > 0 || random == nil { return false }
	s.JumpDelay = 10 + int(random.NextInt(20))
	return true
}

// ChildSize returns the size used by split offspring.
func (s *SlimeCore) ChildSize() int { return max(1, s.Size/2) }
