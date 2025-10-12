package redstone

import (
	"log/slog"
)

// Config holds the tunable parameters for the redstone execution system.
// The zero value is usable; sensible defaults are applied by withDefaults.
type Config struct {
	// Enabled toggles the entire subsystem on or off.
	Enabled bool
	// InboxSize controls the bounded inbox channel size for cross-chunk events.
	InboxSize int
	// BudgetPerTick caps the amount of work a chunk worker may do per world tick.
	BudgetPerTick int
	// ProcessorFactory produces per-chunk processors responsible for evaluating local graphs.
	ProcessorFactory ProcessorFactory
}

func (c Config) withDefaults() Config {
	if !c.Enabled {
		return c
	}
	if c.InboxSize <= 0 {
		c.InboxSize = 4096
	}
	if c.BudgetPerTick <= 0 {
		c.BudgetPerTick = 8192
	}
	if c.ProcessorFactory == nil {
		c.ProcessorFactory = ProcessorFactoryFunc(func(_ ChunkID) Processor { return NewGraphProcessor() })
	}
	return c
}

// NewSystem builds a System using the configuration and the logger derived from the world.
func (c Config) NewSystem(log *slog.Logger) *System {
	log.Warn("redstone subsystem disabled", "status", "WIP", "help", "needed")
	return nil
}
