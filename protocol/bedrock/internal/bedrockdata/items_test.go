package bedrockdata_test

import (
	"image"
	"os"
	"os/exec"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/bedrockdata"
	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/nbt"
)

// customItem is an item Bedrock Edition does not have, registered by
// TestCustomItemsTakeRuntimeIDsPastTheVanillaItems.
type customItem struct {
	name string
	meta int16
}

func (c customItem) EncodeItem() (string, int16) { return c.name, c.meta }
func (c customItem) Name() string                { return c.name }
func (customItem) Texture() image.Image          { return image.NewRGBA(image.Rect(0, 0, 1, 1)) }
func (customItem) Category() category.Category   { return category.Items() }

// TestEveryRegisteredItemHasARuntimeID checks every item the game registers
// has a runtime ID, which Bedrock Edition finds it back by.
func TestEveryRegisteredItemHasARuntimeID(t *testing.T) {
	for _, it := range world.Items() {
		name, meta := it.EncodeItem()
		rid, gotMeta, ok := bedrockdata.ItemRuntimeID(it)
		if !ok {
			t.Errorf("item %v:%v has no runtime ID", name, meta)
			continue
		}
		if gotMeta != meta {
			t.Errorf("item %v:%v has metadata %v", name, meta, gotMeta)
		}
		if back, ok := bedrockdata.ItemByRuntimeID(rid, meta); !ok || !reflect.DeepEqual(back, it) {
			t.Errorf("item %v:%v of runtime ID %v is found back as %#v, %v", name, meta, rid, back, ok)
		}
	}
	if _, _, ok := bedrockdata.ItemRuntimeID(customItem{name: "adamant:unregistered"}); ok {
		t.Error("an item of a name Bedrock Edition does not know has a runtime ID")
	}
	if _, ok := bedrockdata.ItemByRuntimeID(1<<20, 0); ok {
		t.Error("a runtime ID no item has gives an item")
	}
}

// TestBoatsHaveRuntimeIDs checks the boats and chest boats of every variant
// have runtime IDs.
func TestBoatsHaveRuntimeIDs(t *testing.T) {
	for _, v := range item.BoatVariants() {
		for _, it := range []world.Item{item.Boat{Variant: v}, item.ChestBoat{Variant: v}} {
			if _, _, ok := bedrockdata.ItemRuntimeID(it); !ok {
				name, _ := it.EncodeItem()
				t.Errorf("runtime ID missing for %q", name)
			}
		}
	}
}

// TestKnowledgeBookHasARuntimeIDOfItsOwn checks the knowledge book, which BDS
// does not have, is a custom item with a runtime ID other than the book's.
func TestKnowledgeBookHasARuntimeIDOfItsOwn(t *testing.T) {
	bookID, _, ok := bedrockdata.ItemRuntimeID(item.KnowledgeBook{Recipes: []string{"minecraft:oak_planks"}})
	nativeBookID, _, _ := bedrockdata.ItemRuntimeID(item.Book{})
	if !ok || bookID == nativeBookID {
		t.Fatal("knowledge book was not registered as a distinct item")
	}
}

// TestCustomItemsTakeRuntimeIDsPastTheVanillaItems checks custom items take
// the runtime IDs past the names known, in the order they are registered, and
// that a custom item of a vanilla name takes the name over. It runs in a
// process of its own, which the items it registers stay in.
func TestCustomItemsTakeRuntimeIDsPastTheVanillaItems(t *testing.T) {
	if isolated(t) {
		return
	}
	var vanilla map[string]struct {
		RuntimeID      int32          `nbt:"runtime_id"`
		ComponentBased bool           `nbt:"component_based"`
		Version        int32          `nbt:"version"`
		Data           map[string]any `nbt:"data,omitempty"`
	}
	if err := nbt.Unmarshal(bedrockdata.VanillaItems, &vanilla); err != nil {
		t.Fatal(err)
	}
	a, b := customItem{name: "adamant:custom_a"}, customItem{name: "adamant:custom_b"}
	world.RegisterItem(a)
	world.RegisterItem(b)
	ridA, _, okA := bedrockdata.ItemRuntimeID(a)
	ridB, _, okB := bedrockdata.ItemRuntimeID(b)
	if !okA || !okB || ridA < int32(len(vanilla)) || ridB != ridA+1 {
		t.Fatalf("custom items have runtime IDs %v, %v and %v, %v; want consecutive IDs from %v on", ridA, okA, ridB, okB, len(vanilla))
	}
	for rid, want := range map[int32]world.Item{ridA: a, ridB: b} {
		if it, ok := bedrockdata.ItemByRuntimeID(rid, 0); !ok || it != want {
			t.Fatalf("runtime ID %v gives %#v, %v; want %#v", rid, it, ok, want)
		}
	}

	apple := customItem{name: "minecraft:apple", meta: 3}
	world.RegisterItem(apple)
	for _, it := range []world.Item{apple, item.Apple{}} {
		if rid, _, ok := bedrockdata.ItemRuntimeID(it); !ok || rid != ridB+1 {
			t.Fatalf("%#v has runtime ID %v, %v; want the custom item's, %v", it, rid, ok, ridB+1)
		}
	}
}

// BenchmarkItemRuntimeID measures the lookup of a runtime ID the session runs
// for every stack it sends.
func BenchmarkItemRuntimeID(b *testing.B) {
	it := item.Diamond{}
	b.Run("serial", func(b *testing.B) {
		for b.Loop() {
			bedrockdata.ItemRuntimeID(it)
		}
	})
	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				bedrockdata.ItemRuntimeID(it)
			}
		})
	})
}

// isolated runs the test in a process of its own and reports true, unless it
// is that process.
func isolated(t *testing.T) bool {
	t.Helper()
	if os.Getenv("BEDROCKDATA_CHILD") == t.Name() {
		return false
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.timeout=5m")
	cmd.Env = append(os.Environ(), "BEDROCKDATA_CHILD="+t.Name())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s in a process of its own: %v\n%s", t.Name(), err, out)
	}
	return true
}
