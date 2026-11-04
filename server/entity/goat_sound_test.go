package entity_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// goatSoundRecorder records the names of the goat sounds a world plays.
type goatSoundRecorder struct {
	world.NopHandler
	names []string
}

func (r *goatSoundRecorder) HandleSound(_ *world.Context, s world.Sound, _ mgl64.Vec3) {
	if c, ok := s.(sound.Custom); ok && strings.HasPrefix(c.Name, "entity.goat.") {
		r.names = append(r.names, c.Name)
	}
}

// TestGoatsEatWithTheirJavaSound checks Goat.playEatingSound: a goat fed
// wheat plays entity.goat.eat, and a screaming goat
// entity.goat.screaming.eat.
func TestGoatsEatWithTheirJavaSound(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		screaming byte
		want      string
	}{{0, "entity.goat.eat"}, {1, "entity.goat.screaming.eat"}} {
		t.Run(c.want, func(t *testing.T) {
			t.Parallel()
			w := breedingTestWorld(t)
			r := &goatSoundRecorder{}
			w.Handle(r)
			<-w.Exec(func(tx *world.Tx) {
				p := addDeathTestPlayer(tx)
				goat := spawnAnimal(t, tx, "minecraft:goat", .5, map[string]any{"IsScreamingGoat": c.screaming})
				if !feed(t, tx, p, goat, item.Wheat{}) {
					t.Fatal("goat did not eat wheat")
				}
			})
			if want := []string{c.want}; !slices.Equal(r.names, want) {
				t.Fatalf("goat played %v, want %v", r.names, want)
			}
		})
	}
}
