package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

// BenchmarkResolveState resolves Java states as structure templates and
// features do, from a registry that resolved each of them before: full is
// every state with all its properties, name every block by its name alone.
func BenchmarkResolveState(b *testing.B) {
	registry := NewBlockRegistry()
	registry.Finalize()
	var full, named []BlockState
	for id := uint32(1); ; id++ {
		name, properties, ok := gamedata.BlockStateFromID(id)
		if !ok {
			break
		}
		full = append(full, BlockState{Name: name, Properties: properties})
		if len(named) == 0 || named[len(named)-1].Name != name {
			named = append(named, BlockState{Name: name})
		}
	}
	for _, set := range []struct {
		name   string
		states []BlockState
	}{{"full", full}, {"name", named}} {
		for _, state := range set.states {
			_, _ = ResolveState(registry, state)
		}
		b.Run(set.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				_, _ = ResolveState(registry, set.states[i%len(set.states)])
			}
		})
	}
}
