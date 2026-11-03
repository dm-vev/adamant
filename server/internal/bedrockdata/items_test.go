package bedrockdata_test

import (
	"image"
	"os"
	"os/exec"
	"reflect"
	"testing"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/internal/bedrockdata"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/nbt"
)

// customItem is an item Bedrock Edition does not have, registered by
// TestItemRuntimeIDsMatchWorld.
type customItem struct {
	name string
	meta int16
}

func (c customItem) EncodeItem() (string, int16) { return c.name, c.meta }
func (c customItem) Name() string                { return c.name }
func (customItem) Texture() image.Image          { return image.NewRGBA(image.Rect(0, 0, 1, 1)) }
func (customItem) Category() category.Category   { return category.Items() }

// TestItemRuntimeIDsMatchWorld checks the runtime IDs of the items are those
// world.ItemRuntimeID gives, and the items of runtime IDs those
// world.ItemByRuntimeID gives: for every item registered and every runtime ID
// of the vanilla items, then again with a custom item registered that takes
// the name of a vanilla item with other metadata, and one of a name of its
// own. It runs in a process of its own, which the items it registers stay in.
func TestItemRuntimeIDsMatchWorld(t *testing.T) {
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
	rids := map[int32]bool{-1 << 20: true, 1 << 20: true}
	for _, e := range vanilla {
		rids[e.RuntimeID] = true
	}
	// The IDs of custom items follow the names known.
	for rid := int32(len(vanilla)) - 2; rid < int32(len(vanilla))+8; rid++ {
		rids[rid] = true
	}

	compare := func(name string) {
		t.Helper()
		its := append(world.Items(), item.Apple{}, customItem{name: "adamant:unregistered"})
		for _, it := range its {
			rid, meta, ok := bedrockdata.ItemRuntimeID(it)
			wantRID, wantMeta, wantOK := world.ItemRuntimeID(it)
			if rid != wantRID || meta != wantMeta || ok != wantOK {
				n, m := it.EncodeItem()
				t.Fatalf("%s: ItemRuntimeID of %v:%v = %v, %v, %v; the world gives %v, %v, %v", name, n, m, rid, meta, ok, wantRID, wantMeta, wantOK)
			}
		}
		for rid := range rids {
			for _, meta := range []int16{0, 1, 3, 15, -1} {
				it, ok := bedrockdata.ItemByRuntimeID(rid, meta)
				want, wantOK := world.ItemByRuntimeID(rid, meta)
				if ok != wantOK || !reflect.DeepEqual(it, want) {
					t.Fatalf("%s: ItemByRuntimeID(%v, %v) = %#v, %v; the world gives %#v, %v", name, rid, meta, it, ok, want, wantOK)
				}
			}
		}
	}
	compare("registered items")

	world.RegisterItem(customItem{name: "minecraft:apple", meta: 3})
	compare("with a custom item of a vanilla name")
	world.RegisterItem(customItem{name: "adamant:custom_item"})
	compare("with a custom item of a name of its own")

	if _, _, ok := bedrockdata.ItemRuntimeID(customItem{name: "adamant:custom_item"}); !ok {
		t.Fatal("the custom item has no runtime ID")
	}
}

// BenchmarkItemRuntimeID measures the lookup of a runtime ID the session runs
// for every stack it sends, by the world's maps and by the adapter's table.
func BenchmarkItemRuntimeID(b *testing.B) {
	it := item.Diamond{}
	for _, bench := range []struct {
		name string
		f    func(world.Item) (int32, int16, bool)
	}{
		{"world", world.ItemRuntimeID},
		{"bedrockdata", bedrockdata.ItemRuntimeID},
	} {
		b.Run(bench.name, func(b *testing.B) {
			for b.Loop() {
				bench.f(it)
			}
		})
		b.Run(bench.name+"/parallel", func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					bench.f(it)
				}
			})
		})
	}
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
