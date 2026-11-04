package entity

import (
	"reflect"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// TestEntitiesLoadJavaDefaults loads entities from NBT that lacks the field
// they are made from, or holds something else in it, which Java loads with
// a default: a falling block of sand, and a plain firework rocket.
func TestEntitiesLoadJavaDefaults(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, Entities: DefaultRegistry, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	behaviour := func(data map[string]any) (b Behaviour) {
		h, err := NewFromStructureNBT(data, world.EntitySpawnOpts{})
		if err != nil {
			t.Fatal(err)
		}
		<-w.Exec(func(tx *world.Tx) {
			e := tx.AddEntity(h)
			b = e.(interface{ Behaviour() Behaviour }).Behaviour()
			tx.RemoveEntity(e)
		})
		return b
	}
	for _, data := range []map[string]any{
		{"id": "minecraft:falling_block"},
		{"id": "minecraft:falling_block", "FallingBlock": map[string]any{"name": "minecraft:nothing"}},
	} {
		if got := behaviour(data).(*FallingBlockBehaviour).Block(); got != (block.Sand{}) {
			t.Errorf("falling block from %v is %v, want sand", data, got)
		}
	}
	for _, data := range []map[string]any{
		{"id": "minecraft:fireworks_rocket"},
		{"id": "minecraft:fireworks_rocket", "Item": map[string]any{"Name": "minecraft:stone", "Count": byte(1)}},
	} {
		if got, want := behaviour(data).(*FireworkBehaviour).Firework(), (item.Firework{Duration: time.Second / 2}); !reflect.DeepEqual(got, want) {
			t.Errorf("firework from %v is %v, want %v", data, got, want)
		}
	}
}
