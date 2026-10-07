package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestEnderDragonPartBoxes(t *testing.T) {
	data := world.EntityData{Pos: mgl64.Vec3{10, 20, 30}, Data: EnderDragonBehaviourConfig{Phase: DragonHovering}.New()}
	dragon := &EnderDragon{Ent: &Ent{data: &data}}
	parts := dragon.PartBoxes()
	want := [8]string{"head", "neck", "body", "tail1", "tail2", "tail3", "wing1", "wing2"}
	for i, part := range parts {
		if part.Name != want[i] {
			t.Fatalf("part %d = %q, want %q", i, part.Name, want[i])
		}
		min, max := part.Box.Min(), part.Box.Max()
		center := min.Add(max).Mul(.5)
		if got, ok := dragon.PartAt(center); !ok || got != part.Name {
			t.Fatalf("PartAt(%v) = %q, %v; want %q, true", center, got, ok, part.Name)
		}
	}
}
