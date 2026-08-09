package world

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestPositionTrackingDimensionAndWorldIdentity(t *testing.T) {
	sharedProvider := &lifecycleProvider{freshSettings: true}
	overworld := Config{Provider: sharedProvider, Dim: Overworld, Synchronous: true}.New()
	nether := Config{Provider: sharedProvider, Dim: Nether, Synchronous: true}.New()
	independentProvider := &lifecycleProvider{NopProvider: NopProvider{Set: overworld.set}}
	other := Config{Provider: independentProvider, Dim: Overworld, Synchronous: true}.New()
	t.Cleanup(func() {
		_ = overworld.Close()
		_ = nether.Close()
		_ = other.Close()
	})

	pos := cube.Pos{3, 70, -4}
	if overworld.set == nether.set {
		t.Fatal("shared provider did not return distinct settings")
	}
	overworldHandle := overworld.TrackPosition(pos, 0)
	netherHandle := nether.TrackPosition(pos, 0)
	if overworldHandle == 0 || netherHandle == 0 || overworldHandle == netherHandle {
		t.Fatalf("dimension handles = %d, %d", overworldHandle, netherHandle)
	}
	if got, dim, ok := overworld.TrackedPosition(netherHandle); !ok || got != pos || dim != 1 {
		t.Fatalf("shared nether target = %v, %d, %v", got, dim, ok)
	}
	if _, _, ok := other.TrackedPosition(overworldHandle); ok {
		t.Fatal("tracking handle leaked into an unrelated world")
	}
}

func TestPositionTrackingBreakAndReplacement(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{8, 64, 8}
	handle := w.TrackPosition(pos, 0)
	w.UntrackPosition(pos)
	if _, _, ok := w.TrackedPosition(handle); ok {
		t.Fatal("untracked position remained active")
	}
	if got := w.PositionTrackingHandleAt(pos); got != 0 {
		t.Fatalf("inactive position returned stale handle %d", got)
	}
	if got := w.TrackPosition(pos, 0); got == handle {
		t.Fatalf("replacement revived inactive handle %d", handle)
	}
}

func TestPositionTrackingCleanupIsBounded(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	for x := range maxInactivePositionTrackingEntries + 32 {
		pos := cube.Pos{x, 64, 0}
		w.TrackPosition(pos, 0)
		w.UntrackPosition(pos)
	}
	data := w.positionTracker().data()
	if got := len(data.Entries); got != maxInactivePositionTrackingEntries {
		t.Fatalf("retained inactive entries = %d, want %d", got, maxInactivePositionTrackingEntries)
	}
	if got := w.PositionTrackingHandleAt(cube.Pos{0, 64, 0}); got != 0 {
		t.Fatalf("oldest evicted position still has handle %d", got)
	}
}

func TestPositionTrackingConcurrentWorldClose(t *testing.T) {
	provider := &lifecycleProvider{freshSettings: true}
	first := Config{Provider: provider, Dim: Overworld, Synchronous: true}.New()
	second := Config{Provider: provider, Dim: Nether, Synchronous: true}.New()
	first.TrackPosition(cube.Pos{1, 64, 1}, 0)
	second.TrackPosition(cube.Pos{1, 64, 1}, 0)

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for _, w := range []*World{first, second} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = w.Close()
			}()
		}
		for range 100 {
			_ = first.positionTracker().data()
		}
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent world close deadlocked")
	}
}

func TestPositionTrackingDoesNotWaitForSettingsOwner(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	w.set.Lock()
	done := make(chan struct{})
	go func() {
		w.TrackPosition(cube.Pos{1, 64, 1}, 0)
		close(done)
	}()
	select {
	case <-done:
		w.set.Unlock()
	case <-time.After(time.Second):
		w.set.Unlock()
		t.Fatal("position tracking waited for the settings owner lock")
	}
}

type trackingTestProvider struct {
	NopProvider
	mu               sync.Mutex
	data             PositionTrackingData
	loadErr          error
	failLoads        int
	loads            int
	saves            int
	closes           int
	firstSaveStarted chan struct{}
	releaseFirstSave chan struct{}
}

func (p *trackingTestProvider) LoadPositionTrackingData() (PositionTrackingData, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loads++
	if p.failLoads != 0 {
		if p.failLoads > 0 {
			p.failLoads--
		}
		return PositionTrackingData{}, p.loadErr
	}
	return clonePositionTrackingData(p.data), nil
}

func (p *trackingTestProvider) SavePositionTrackingData(data PositionTrackingData) error {
	p.mu.Lock()
	p.saves++
	save := p.saves
	p.mu.Unlock()
	if save == 1 && p.firstSaveStarted != nil {
		close(p.firstSaveStarted)
		<-p.releaseFirstSave
	}
	p.mu.Lock()
	p.data = clonePositionTrackingData(data)
	p.mu.Unlock()
	return nil
}

func (p *trackingTestProvider) Close() error {
	p.mu.Lock()
	p.closes++
	p.mu.Unlock()
	return nil
}

func (p *trackingTestProvider) snapshot() (PositionTrackingData, int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return clonePositionTrackingData(p.data), p.loads, p.saves, p.closes
}

func clonePositionTrackingData(data PositionTrackingData) PositionTrackingData {
	data.Entries = append([]PositionTrackingEntry(nil), data.Entries...)
	return data
}

func TestPositionTrackingSaveSerialisesSnapshotAndWrite(t *testing.T) {
	provider := &trackingTestProvider{firstSaveStarted: make(chan struct{}), releaseFirstSave: make(chan struct{})}
	first := Config{Provider: provider, Dim: Overworld, Synchronous: true}.New()
	second := Config{Provider: provider, Dim: Nether, Synchronous: true}.New()
	released := false
	t.Cleanup(func() {
		if !released {
			close(provider.releaseFirstSave)
		}
		_ = first.Close()
		_ = second.Close()
	})

	first.TrackPosition(cube.Pos{1, 64, 1}, 0)
	firstDone := make(chan struct{})
	go func() {
		first.Save()
		close(firstDone)
	}()
	<-provider.firstSaveStarted

	second.TrackPosition(cube.Pos{2, 64, 2}, 0)
	secondDone := make(chan struct{})
	go func() {
		second.Save()
		close(secondDone)
	}()
	select {
	case <-secondDone:
		t.Fatal("newer tracking save completed before the earlier write")
	case <-time.After(50 * time.Millisecond):
	}
	close(provider.releaseFirstSave)
	released = true
	for name, done := range map[string]<-chan struct{}{"first": firstDone, "second": secondDone} {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("%s save did not complete", name)
		}
	}

	data, _, saves, _ := provider.snapshot()
	if saves != 2 || data.Next != 2 || len(data.Entries) != 2 {
		t.Fatalf("saved tracking data = %#v after %d saves", data, saves)
	}
}

func TestPositionTrackingFailedLoadPreservesProviderData(t *testing.T) {
	want := PositionTrackingData{Next: 7, Entries: []PositionTrackingEntry{{Handle: 7, Position: cube.Pos{7, 70, 7}, Active: true}}}
	provider := &trackingTestProvider{data: want, loadErr: errors.New("temporary read failure"), failLoads: -1}
	w := Config{Provider: provider, Synchronous: true}.New()
	w.Save()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	got, _, saves, closes := provider.snapshot()
	if saves != 0 || closes != 1 {
		t.Fatalf("failed load produced %d saves and %d closes", saves, closes)
	}
	if got.Next != want.Next || len(got.Entries) != 1 || got.Entries[0] != want.Entries[0] {
		t.Fatalf("provider data changed after failed load: %#v", got)
	}
}

func TestPositionTrackingTransientLoadRecoversBeforeSave(t *testing.T) {
	want := PositionTrackingData{Next: 4, Entries: []PositionTrackingEntry{{Handle: 4, Position: cube.Pos{4, 70, 4}, Active: true}}}
	provider := &trackingTestProvider{data: want, loadErr: errors.New("temporary read failure"), failLoads: 1}
	w := Config{Provider: provider, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })

	if _, _, ok := w.TrackedPosition(4); ok {
		t.Fatal("invalid tracker exposed data before recovery")
	}
	w.Save()
	if got, _, ok := w.TrackedPosition(4); !ok || got != want.Entries[0].Position {
		t.Fatalf("recovered tracking position = %v, %v", got, ok)
	}
	got, loads, saves, _ := provider.snapshot()
	if loads != 2 || saves != 1 || got.Next != want.Next || len(got.Entries) != 1 || got.Entries[0] != want.Entries[0] {
		t.Fatalf("recovery state = %#v, loads=%d saves=%d", got, loads, saves)
	}
}

func TestPositionTrackingFailedLoadBlocksAllocationUntilRecovery(t *testing.T) {
	stored := cube.Pos{3, 70, 3}
	provider := &trackingTestProvider{
		data:      PositionTrackingData{Next: 3, Entries: []PositionTrackingEntry{{Handle: 3, Position: stored, Active: true}}},
		loadErr:   errors.New("temporary read failure"),
		failLoads: 2,
	}
	w := Config{Provider: provider, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })

	placed := cube.Pos{4, 70, 4}
	if handle := w.TrackPosition(placed, 0); handle != 0 {
		t.Fatalf("allocated handle %d while persisted IDs were unavailable", handle)
	}
	w.Save()
	if got, _, ok := w.TrackedPosition(3); !ok || got != stored {
		t.Fatalf("recovered stored target = %v, %v; want %v, true", got, ok, stored)
	}
	if handle := w.TrackPosition(placed, 0); handle != 4 {
		t.Fatalf("post-recovery handle = %d, want 4", handle)
	}
}

func TestPositionTrackingFailedLoadPreservesLoadedHandleUntilRecovery(t *testing.T) {
	pos := cube.Pos{7, 70, 7}
	provider := &trackingTestProvider{
		data:      PositionTrackingData{Next: 7, Entries: []PositionTrackingEntry{{Handle: 7, Position: pos, Active: true}}},
		loadErr:   errors.New("temporary read failure"),
		failLoads: 2,
	}
	w := Config{Provider: provider, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })

	if handle := w.TrackPosition(pos, 7); handle != 7 {
		t.Fatalf("loaded handle = %d, want persisted handle 7", handle)
	}
	if _, _, ok := w.TrackedPosition(7); ok {
		t.Fatal("failed load registered an unverified handle")
	}
	w.Save()
	if got, _, ok := w.TrackedPosition(7); !ok || got != pos {
		t.Fatalf("recovered loaded target = %v, %v; want %v, true", got, ok, pos)
	}
}

func TestPositionTrackingFinalCloseSavesLatestOnce(t *testing.T) {
	provider := &trackingTestProvider{}
	first := Config{Provider: provider, Dim: Overworld, Synchronous: true}.New()
	second := Config{Provider: provider, Dim: Nether, Synchronous: true}.New()

	first.TrackPosition(cube.Pos{1, 64, 1}, 0)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, saves, closes := provider.snapshot(); saves != 0 || closes != 0 {
		t.Fatalf("non-final close produced %d saves and %d closes", saves, closes)
	}
	second.TrackPosition(cube.Pos{2, 64, 2}, 0)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	data, _, saves, closes := provider.snapshot()
	if saves != 1 || closes != 1 || data.Next != 2 || len(data.Entries) != 2 {
		t.Fatalf("final state = %#v, saves=%d closes=%d", data, saves, closes)
	}
}

func TestPositionTrackingWritableChangesSaveWhenReadOnlyWorldClosesLast(t *testing.T) {
	provider := &trackingTestProvider{}
	writable := Config{Provider: provider, Synchronous: true}.New()
	readOnly := Config{Provider: provider, Dim: Nether, ReadOnly: true, Synchronous: true}.New()

	pos := cube.Pos{1, 64, 1}
	handle := writable.TrackPosition(pos, 0)
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, saves, closes := provider.snapshot(); saves != 0 || closes != 0 {
		t.Fatalf("non-final writable close produced %d saves and %d closes", saves, closes)
	}
	if err := readOnly.Close(); err != nil {
		t.Fatal(err)
	}
	data, _, saves, closes := provider.snapshot()
	if saves != 1 || closes != 1 || len(data.Entries) != 1 || data.Entries[0].Handle != handle || data.Entries[0].Position != pos {
		t.Fatalf("final state = %#v, saves=%d closes=%d", data, saves, closes)
	}
}

func TestPositionTrackingProviderIdentityTopology(t *testing.T) {
	first := Config{Provider: NopProvider{}, Synchronous: true}.New()
	second := Config{Provider: NopProvider{}, Dim: Nether, Synchronous: true}.New()
	if got := first.TrackPosition(cube.Pos{1, 64, 1}, 0); got != 1 {
		t.Fatalf("first independent provider handle = %d, want 1", got)
	}
	if got := second.TrackPosition(cube.Pos{2, 64, 2}, 0); got != 1 {
		t.Fatalf("second independent provider handle = %d, want 1", got)
	}
	_ = first.Close()
	_ = second.Close()

	provider := &NopProvider{}
	first = Config{Provider: provider, Synchronous: true}.New()
	second = Config{Provider: provider, Dim: Nether, Synchronous: true}.New()
	if got := first.TrackPosition(cube.Pos{1, 64, 1}, 0); got != 1 {
		t.Fatalf("shared pointer provider first handle = %d, want 1", got)
	}
	if got := second.TrackPosition(cube.Pos{2, 64, 2}, 0); got != 2 {
		t.Fatalf("shared pointer provider second handle = %d, want 2", got)
	}
	_ = first.Close()
	_ = second.Close()
}
