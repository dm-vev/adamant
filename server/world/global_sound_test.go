package world

import (
	"context"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
)

func TestGlobalSoundDoesNotSuspendSourceBehindBusyDimension(t *testing.T) {
	provider := NopProvider{Set: defaultSettings()}
	source := Config{Provider: provider, Dim: Overworld, GeneratorWorkers: 1}.New()
	target := Config{Provider: provider, Dim: Nether, GeneratorWorkers: 1}.New()
	busy, release := make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
		_ = source.Close()
		_ = target.Close()
	}()
	heard := make(chan Sound, 1)
	listener := EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}.New(globalSoundListenerType{heard: heard}, reentrantEntityConfig{})
	if err := target.Do(func(tx *Tx) { tx.AddEntity(listener) }).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	target.Do(func(*Tx) {
		close(busy)
		<-release
	})
	<-busy
	for range cap(target.queue) {
		target.Do(func(*Tx) {})
	}

	emitted := source.Do(func(tx *Tx) {
		tx.PlayGlobalSound(mgl64.Vec3{}, globalSoundLivenessSound{})
		tx.World().SetTime(6000)
	})
	completed := false
	select {
	case <-emitted.Done():
		completed = true
	case <-time.After(time.Second):
	}
	observed := source.Time()
	close(release)
	released = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := emitted.Wait(ctx); err != nil {
		t.Fatalf("global event did not finish after releasing target: %v", err)
	}
	if !completed || observed < 6000 {
		t.Fatalf("source world stopped behind busy dimension: event completed=%v, time=%d", completed, observed)
	}
	select {
	case sound := <-heard:
		if _, ok := sound.(globalSoundLivenessSound); !ok {
			t.Fatalf("destination heard unexpected sound %T", sound)
		}
	case <-ctx.Done():
		t.Fatal("queued global sound was lost after destination resumed")
	}
}

type globalSoundLivenessSound struct{}

func (globalSoundLivenessSound) Play(*World, mgl64.Vec3) {}

type globalSoundListenerType struct {
	reentrantEntityType
	heard chan Sound
}

func (t globalSoundListenerType) EncodeEntity() string { return "minecraft:player" }
func (t globalSoundListenerType) Open(tx *Tx, h *EntityHandle, data *EntityData) Entity {
	return &globalSoundListener{reentrantEntity: &reentrantEntity{tx: tx, handle: h, data: data}, heard: t.heard}
}

type globalSoundListener struct {
	*reentrantEntity
	heard chan Sound
}

func (p *globalSoundListener) PlaySound(s Sound) { p.heard <- s }
