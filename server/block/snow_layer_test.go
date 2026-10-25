package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

// TestSnowLayersKeepTheirHeight checks every height of snow layers is a
// registered state that is read back from the world as itself.
func TestSnowLayersKeepTheirHeight(t *testing.T) {
	w := world.Config{Synchronous: true, Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, Stone{}, nil)
		for _, b := range allSnowLayers() {
			tx.SetBlock(cube.Pos{0, 64, 0}, b, nil)
			if got := tx.Block(cube.Pos{0, 64, 0}); got != b {
				t.Fatalf("set %#v, read back %#v", b, got)
			}
		}
	})
}

// TestSnowLayerSurvivalAndDrops checks snow follows SnowLayerBlock: it does
// not stay on ice, stays on soul sand, melts away by block light above 11,
// and drops a snowball a layer to a shovel.
func TestSnowLayerSurvivalAndDrops(t *testing.T) {
	w := world.Config{Synchronous: true, Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		tx.SetBlock(pos.Side(cube.FaceDown), Ice{}, nil)
		if snowLayerSurvives(tx, pos) {
			t.Fatal("snow survives on ice")
		}
		tx.SetBlock(pos.Side(cube.FaceDown), SoulSand{}, nil)
		if !snowLayerSurvives(tx, pos) {
			t.Fatal("snow does not survive on soul sand")
		}
		tx.SetBlock(pos.Side(cube.FaceDown), Air{}, nil)
		tx.SetBlock(pos, SnowLayer{Layers: 3}, nil)
		SnowLayer{Layers: 3}.NeighbourUpdateTick(pos, pos.Side(cube.FaceDown), tx)
		if _, ok := tx.Block(pos).(Air); !ok {
			t.Fatalf("snow over air became %#v, want air", tx.Block(pos))
		}

		drops := SnowLayer{Layers: 5}.BreakInfo().Drops(item.Shovel{Tier: item.ToolTierIron}, nil)
		if len(drops) != 1 || drops[0].Count() != 5 {
			t.Fatalf("five layers dropped %v, want 5 snowballs", drops)
		}
		silk := []item.Enchantment{item.NewEnchantment(enchantment.SilkTouch, 1)}
		if drops := (SnowLayer{Layers: 8}).BreakInfo().Drops(item.Shovel{Tier: item.ToolTierIron}, silk); len(drops) != 1 || drops[0].Item() != (Snow{}) {
			t.Fatalf("eight layers with Silk Touch dropped %v, want a snow block", drops)
		}
		if (SnowLayer{Layers: 1}).BreakInfo().Harvestable(item.ToolNone{}) {
			t.Fatal("snow is harvestable without a shovel")
		}
	})
}

// TestFramesHoldingMapsHaveTheirOwnState checks a frame holding a map is
// stored as the item_frame_map_bit state Bedrock keeps for it.
func TestFramesHoldingMapsHaveTheirOwnState(t *testing.T) {
	w := world.Config{Synchronous: true, Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		tx.SetBlock(pos.Side(cube.FaceNorth), Stone{}, nil)
		tx.SetBlock(pos, ItemFrame{Facing: cube.FaceNorth, Item: item.NewStack(item.FilledMap{NativeID: 7, HasNativeID: true}, 1)}, nil)
		frame, ok := tx.Block(pos).(ItemFrame)
		if !ok {
			t.Fatalf("frame holding a map read back as %#v", tx.Block(pos))
		}
		if _, isMap := frame.Item.Item().(item.FilledMap); !isMap {
			t.Fatalf("frame read back holding %v, want the map", frame.Item)
		}
		if _, props := frame.EncodeBlock(); props["item_frame_map_bit"] != uint8(1) {
			t.Fatalf("frame holding a map encodes %v, want item_frame_map_bit 1", props)
		}
	})
}
