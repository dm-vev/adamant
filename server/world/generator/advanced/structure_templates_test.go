package advanced

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// TestVanillaStructureTemplatesLoad checks that structure blocks can load the
// templates of the vanilla data pack, with their block entities and entities,
// and that only names in the minecraft namespace resolve to them.
func TestVanillaStructureTemplatesLoad(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	top, ok, err := VanillaStructureTemplate(w, "minecraft:igloo/top")
	if err != nil || !ok {
		t.Fatalf("igloo top not found: %v", err)
	}
	if top.Size() != [3]int{7, 5, 8} {
		t.Fatalf("igloo top is %v, want the 7x5x8 of the Java template", top.Size())
	}
	bottom, ok, err := VanillaStructureTemplate(w, "igloo/bottom")
	if err != nil || !ok {
		t.Fatalf("igloo bottom not found without namespace: %v", err)
	}
	if bottom.EntityCount() != 2 {
		t.Fatalf("igloo bottom holds %d entities, want its villager and zombie villager", bottom.EntityCount())
	}
	chests := 0
	s := bottom.Size()
	for x := range s[0] {
		for y := range s[1] {
			for z := range s[2] {
				if b, _ := bottom.Block(cube.Pos{x, y, z}); b != nil {
					if _, ok := b.(block.Chest); ok {
						chests++
					}
				}
			}
		}
	}
	if chests != 1 {
		t.Fatalf("igloo bottom holds %d chests, want 1", chests)
	}
	for _, name := range []string{"minecraft:igloo/missing", "other:igloo/top"} {
		if _, ok, err := VanillaStructureTemplate(w, name); ok || err != nil {
			t.Fatalf("%s resolved to a template (err %v), want none", name, err)
		}
	}
}
