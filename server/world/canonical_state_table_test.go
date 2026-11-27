package world

import (
	"testing"

	"github.com/dm-vev/adamant/server/internal/gamedata"
)

// TestResolvedJavaStatesAreReused resolves every Java state again once the
// registry holds it, with all its properties, by ID, and with each property
// left out, and requires what a registry that resolves it for the first time
// gives.
func TestResolvedCanonicalStatesAreReused(t *testing.T) {
	t.Parallel()
	warm, cold := NewBlockRegistry().(*BasicBlockRegistry), NewBlockRegistry()
	warm.Finalize()
	cold.Finalize()
	for id := uint32(1); id <= gamedata.BlockStateCount(); id++ {
		name, properties, _ := gamedata.BlockStateFromID(id)
		state := BlockState{Name: name, Properties: properties}
		first, err := ResolveState(warm, state)
		if _, stored := warm.resolvedCanonicalState(id); stored != (err == nil) {
			t.Fatalf("%s: stored %t, error %v", formatCanonicalStateLine(name, properties), stored, err)
		}
		if err != nil {
			if _, again := ResolveState(warm, state); again == nil || again.Error() != err.Error() {
				t.Fatalf("%s: error %v, then %v", formatCanonicalStateLine(name, properties), err, again)
			}
			continue
		}
		if again, err := ResolveState(warm, state); err != nil || again != first {
			t.Fatalf("%s: %+v, then %+v (error %v)", formatCanonicalStateLine(name, properties), first, again, err)
		}
		if byID := ResolveStateID(warm, id); byID != first {
			t.Fatalf("%s: %+v, by ID %+v", formatCanonicalStateLine(name, properties), first, byID)
		}
		for key := range properties {
			partial := BlockState{Name: name, Properties: map[string]any{}}
			for k, v := range properties {
				if k != key {
					partial.Properties[k] = v
				}
			}
			want, wantErr := ResolveState(cold, partial)
			got, err := ResolveState(warm, partial)
			if got != want || (err == nil) != (wantErr == nil) || err != nil && err.Error() != wantErr.Error() {
				t.Fatalf("%s without %s: %+v (error %v), want %+v (error %v)", formatCanonicalStateLine(name, properties), key, got, err, want, wantErr)
			}
		}
	}
}

// TestResolveStateReadsOtherTypesAsBefore resolves states with values of
// other types than gamedata gives, which skip the table, as before.
func TestResolveStateReadsOtherTypesAsBefore(t *testing.T) {
	t.Parallel()
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, state := range []BlockState{
		{Name: "minecraft:lantern", Properties: map[string]any{"hanging": "true"}},
		{Name: "minecraft:wheat", Properties: map[string]any{"age": "3"}},
		{Name: "minecraft:oak_log", Properties: map[string]any{"axis": "w"}},
		{Name: "stone"},
	} {
		want, wantErr := resolveCanonicalState(registry, state)
		if wantErr == nil {
			want, wantErr = want.WithCanonicalState(registry, state)
		}
		for range 2 {
			got, err := ResolveState(registry, state)
			if got != want || (err == nil) != (wantErr == nil) || err != nil && err.Error() != wantErr.Error() {
				t.Fatalf("%+v: %+v (error %v), want %+v (error %v)", state, got, err, want, wantErr)
			}
		}
	}
}
