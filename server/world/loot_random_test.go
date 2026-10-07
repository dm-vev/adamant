package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/internal/java26"
)

func TestLootRandomSequenceContinuesPerDimension(t *testing.T) {
	provider := NopProvider{Set: defaultSettings()}
	dimensions := []Dimension{Overworld, registeredTestDimension(t)}
	worlds := make([]*World, len(dimensions))
	want := make([]int64, len(dimensions))
	for i, dimension := range dimensions {
		worlds[i] = Config{Provider: provider, Dim: dimension, Synchronous: true}.New()
		<-worlds[i].Exec(func(tx *Tx) {
			random := tx.LootRandomSequence("test:source", 0)
			for range i + 1 {
				random.NextLong()
			}
			state := random.State()
			restored := java26.FromState(state)
			want[i] = restored.NextLong()
		})
	}
	for _, w := range worlds {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}

	for i, dimension := range dimensions {
		w := Config{Provider: provider, Dim: dimension, Synchronous: true}.New()
		var got int64
		<-w.Exec(func(tx *Tx) { got = tx.LootRandomSequence("test:source", 0).NextLong() })
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if got != want[i] {
			t.Errorf("dimension %v resumed sequence at %d, want %d", dimension, got, want[i])
		}
	}
	if want[0] == want[1] {
		t.Fatal("test setup did not produce distinct dimension stream positions")
	}
}

func TestNopProviderWithoutStoreCanCloseWorld(t *testing.T) {
	w := Config{Provider: NopProvider{}, Synchronous: true}.New()
	<-w.Exec(func(tx *Tx) { tx.LootRandomSequence("test:source", 0).NextLong() })
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
