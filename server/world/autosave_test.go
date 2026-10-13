package world

import (
	"sync/atomic"
	"testing"
	"time"
)

type autoSaveTestProvider struct {
	NopProvider
	saves atomic.Int32
}

func (p *autoSaveTestProvider) SaveSettings(*Settings) { p.saves.Add(1) }

func TestAutoSaveToggleSuppressesPeriodicSaves(t *testing.T) {
	provider := &autoSaveTestProvider{}
	w := Config{Provider: provider, SaveInterval: 10 * time.Millisecond, ChunkUnloadInterval: time.Hour, GeneratorWorkers: 1}.New()
	defer w.Close()
	w.SetAutoSaveEnabled(false)
	time.Sleep(60 * time.Millisecond)
	if got := provider.saves.Load(); got != 0 {
		t.Fatalf("periodic saves while disabled = %d, want 0", got)
	}
	w.SetAutoSaveEnabled(true)
	deadline := time.Now().Add(time.Second)
	for provider.saves.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := provider.saves.Load(); got == 0 {
		t.Fatal("periodic save did not resume after enabling autosave")
	}
}
