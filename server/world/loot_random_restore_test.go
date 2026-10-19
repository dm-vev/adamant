package world

import "testing"

func TestPersistedZeroLootSequenceUsesJavaRepair(t *testing.T) {
	provider := NopProvider{WorldData: &WorldDataMemory{}}
	w := Config{Provider: provider, Synchronous: true}.New()
	namespace := w.lootRandomSequenceNamespace()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveWorldData(namespace, []byte(`{"sequences":{"test:zero":{}}}`)); err != nil {
		t.Fatal(err)
	}
	w = Config{Provider: provider, Synchronous: true}.New()
	defer w.Close()
	w.Do(func(tx *Tx) {
		random := tx.LootRandomSequence("test:zero", 0)
		// Check the Java oracle before rejection sampling, so the old absorbing
		// state fails immediately instead of hanging the regression itself.
		if got := random.NextLong(); got != 6807859099481836695 {
			t.Fatalf("restored first long = %d, want Java zero-state repair", got)
		}
		for range 64 {
			if got := random.NextInt(3); got < 0 || got >= 3 {
				t.Fatalf("restored bounded draw = %d", got)
			}
		}
	})
}
