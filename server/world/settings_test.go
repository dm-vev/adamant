package world

import (
	"sync"
	"testing"
	"time"
)

func TestFallDamageSetting(t *testing.T) {
	w := Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })

	if !w.FallDamage() {
		t.Fatal("fall damage is disabled by default")
	}
	w.SetFallDamage(false)
	if w.FallDamage() {
		t.Fatal("fall damage remains enabled after SetFallDamage(false)")
	}

	var nilWorld *World
	nilWorld.SetFallDamage(true)
	if nilWorld.FallDamage() {
		t.Fatal("nil World reports fall damage enabled")
	}
}

func TestSetFallDamageUpdatesSharedWorldViewersOnce(t *testing.T) {
	settings := defaultSettings()
	provider := NopProvider{Set: settings}
	overworld := Config{Synchronous: true, Provider: provider}.New()
	nether := Config{Synchronous: true, Provider: provider, Dim: Nether}.New()
	defer overworld.Close()
	defer nether.Close()

	overworldViewer, netherViewer := new(fallDamageTestViewer), new(fallDamageTestViewer)
	overworldLoader := NewLoader(0, overworld, overworldViewer)
	netherLoader := NewLoader(0, nether, netherViewer)
	defer overworld.Do(overworldLoader.Close)
	defer nether.Do(netherLoader.Close)

	nether.SetFallDamage(false)
	nether.SetFallDamage(false)
	for name, viewer := range map[string]*fallDamageTestViewer{"overworld": overworldViewer, "nether": netherViewer} {
		updates := viewer.Updates()
		if len(updates) != 1 || updates[0] {
			t.Fatalf("%s updates = %v, want [false]", name, updates)
		}
	}
	if overworld.FallDamage() {
		t.Fatal("shared overworld retained fall damage")
	}
}

func TestSetFallDamageViewerMayReadSetting(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	observed := make(chan bool, 1)
	viewer := &fallDamageTestViewer{callback: func(bool) { observed <- w.FallDamage() }}
	loader := NewLoader(0, w, viewer)
	defer w.Do(loader.Close)

	done := make(chan struct{})
	go func() {
		w.SetFallDamage(false)
		close(done)
	}()
	select {
	case got := <-observed:
		if got {
			t.Fatal("viewer observed fall damage enabled during disabled update")
		}
	case <-time.After(time.Second):
		t.Fatal("viewer deadlocked reading fall damage setting")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SetFallDamage did not return after reentrant viewer callback")
	}
}

func TestSetFallDamageSlowViewerDoesNotBlockClose(t *testing.T) {
	settings := defaultSettings()
	provider := NopProvider{Set: settings}
	overworld := Config{Synchronous: true, Provider: provider}.New()
	nether := Config{Synchronous: true, Provider: provider, Dim: Nether}.New()
	defer overworld.Close()
	defer nether.Close()

	entered, release := make(chan struct{}), make(chan struct{})
	viewer := &fallDamageTestViewer{callback: func(bool) {
		close(entered)
		<-release
	}}
	loader := NewLoader(0, overworld, viewer)
	setDone := make(chan struct{})
	go func() {
		overworld.SetFallDamage(false)
		close(setDone)
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("slow viewer was not called")
	}
	loaderClosed := make(chan struct{})
	go func() {
		overworld.Do(loader.Close)
		close(loaderClosed)
	}()
	worldClosed := make(chan struct{})
	go func() {
		_ = nether.Close()
		close(worldClosed)
	}()
	for name, closed := range map[string]<-chan struct{}{"loader": loaderClosed, "world": worldClosed} {
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatalf("%s close blocked on slow viewer callback", name)
		}
	}
	close(release)
	select {
	case <-setDone:
	case <-time.After(time.Second):
		t.Fatal("SetFallDamage did not return after slow viewer was released")
	}
}

func TestSetFallDamageConcurrentWorldAndViewerClose(t *testing.T) {
	settings := defaultSettings()
	provider := NopProvider{Set: settings}
	overworld := Config{Synchronous: true, Provider: provider}.New()
	nether := Config{Synchronous: true, Provider: provider, Dim: Nether}.New()
	overworldViewer, netherViewer := new(fallDamageTestViewer), new(fallDamageTestViewer)
	overworldLoader := NewLoader(0, overworld, overworldViewer)
	NewLoader(0, nether, netherViewer)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		for i := range 200 {
			overworld.SetFallDamage(i%2 != 0)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		overworld.Do(overworldLoader.Close)
	}()
	go func() {
		defer wg.Done()
		<-start
		_ = nether.Close()
	}()
	close(start)
	wg.Wait()
	_ = overworld.Close()
}

type fallDamageTestViewer struct {
	NopViewer
	mu       sync.Mutex
	updates  []bool
	callback func(bool)
}

func (v *fallDamageTestViewer) ViewFallDamage(enabled bool) {
	v.mu.Lock()
	v.updates = append(v.updates, enabled)
	v.mu.Unlock()
	if v.callback != nil {
		v.callback(enabled)
	}
}

func (v *fallDamageTestViewer) Updates() []bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]bool(nil), v.updates...)
}
