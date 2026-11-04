package world

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

// TestEntityLoadsWithinJavaBounds loads entities as Java Entity.load does: a
// motion over 10 blocks a tick, which would have the entity sweep its
// collision over as many blocks, is dropped, the position is clamped, and
// an entity with a position that is not a number or a rotation that is not
// finite is not loaded.
func TestEntityLoadsWithinJavaBounds(t *testing.T) {
	t.Parallel()
	nan, inf := float32(math.NaN()), float32(math.Inf(1))
	h := entityFromData(testEntityType{}, 1, map[string]any{
		"Pos":    []float32{1e9, inf, -1e9},
		"Motion": []float32{0.5, nan, -2.3e18},
	})
	if h == nil {
		t.Fatal("an entity with a position out of bounds was not loaded")
	}
	if want := (mgl64.Vec3{3.0000512e7, 2e7, -3.0000512e7}); h.data.Pos != want {
		t.Errorf("position = %v, want %v", h.data.Pos, want)
	}
	if want := (mgl64.Vec3{0.5, 0, 0}); h.data.Vel != want {
		t.Errorf("motion = %v, want %v", h.data.Vel, want)
	}
	for name, data := range map[string]map[string]any{
		"position": {"Pos": []float32{0, nan, 0}},
		"rotation": {"Rotation": []float32{inf, 0}},
	} {
		if entityFromData(testEntityType{}, 1, data) != nil {
			t.Errorf("an entity with an invalid %s was loaded", name)
		}
	}

	w := Config{Entities: (EntityRegistryConfig{}).New([]EntityType{testEntityType{}}), Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *Tx) {
		h := tx.AddEntity(NewEntity(testEntityType{}, testEntityConfig{})).H()
		if err := h.MergeNBT(tx, map[string]any{"Pos": []float32{0, nan, 0}}); err == nil {
			t.Error("merged a position that is not a number")
		}
		if h.data.Pos != (mgl64.Vec3{}) {
			t.Errorf("a refused merge moved the entity to %v", h.data.Pos)
		}
	})
}
