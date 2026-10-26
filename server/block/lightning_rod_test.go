package block

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestLightningRodBodyMatchesInspectedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_lightning_methods.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Bounds   [][6]float64 `json:"visual_bounds_by_facing"`
		Vtables  map[string]map[string]string
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Bounds) != 6 || native.Vtables["CopperLightningRodBlock"]["0x50"] != "0x3459fe0" {
		t.Fatal("incomplete native shape inspection")
	}
	for _, b := range allLightningRods() {
		r := b.(LightningRod)
		bounds := native.Bounds[r.Facing]
		want := cube.Box(bounds[0], bounds[1], bounds[2], bounds[3], bounds[4], bounds[5])
		got := r.Model().BBox(cube.Pos{}, nil)
		if len(got) != 1 || got[0] != want {
			t.Errorf("rod %+v: bounds=%v, native=%v", r, got, want)
		}
	}
}

func TestStruckLightningRodPulsesItsSupport(t *testing.T) {
	w := newMultifaceTestWorld(t)
	rod, support := cube.Pos{0, 65, 0}, cube.Pos{0, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(support, Stone{}, nil)
		tx.SetBlock(rod, LightningRod{Facing: cube.FaceUp, Oxidation: WeatheredOxidation()}, nil)
	})
	// Let setup register the producer before striking. A hit before registration
	// has a visible pulse but no circuit strength in native Bedrock.
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		tx.Block(rod).(LightningRod).Strike(rod, tx)
		if r := tx.Block(rod).(LightningRod); !r.Powered {
			t.Fatal("struck rod is not powered")
		}
		if got := tx.RedstoneStrongPower(support); got != 15 {
			t.Fatalf("support strong power = %d, want 15", got)
		}
	})
	for range 8 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if r := tx.Block(rod).(LightningRod); r.Powered {
			t.Fatal("rod still powered eight ticks after the strike")
		}
	})
}

func TestFlowerPotHoldsOnePlant(t *testing.T) {
	w := newMultifaceTestWorld(t)
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, FlowerPot{}, nil)
		poppy := Flower{Type: Poppy()}
		ctx := &item.UseContext{}
		if !tx.Block(pos).(FlowerPot).Activate(pos, cube.FaceUp, tx, &candleTestUser{held: item.NewStack(poppy, 3)}, ctx) || ctx.CountSub != 1 {
			t.Fatalf("potting a poppy consumed %d", ctx.CountSub)
		}
		if p := tx.Block(pos).(FlowerPot); p.Plant != poppy {
			t.Fatalf("pot holds %v, want the poppy", p.Plant)
		}
		ctx = &item.UseContext{}
		tx.Block(pos).(FlowerPot).Activate(pos, cube.FaceUp, tx, &candleTestUser{held: item.NewStack(Flower{Type: Dandelion()}, 1)}, ctx)
		if p := tx.Block(pos).(FlowerPot); p.Plant != poppy || ctx.CountSub != 0 {
			t.Fatalf("second flower replaced the poppy: %v", p.Plant)
		}
		if tx.Block(pos).(FlowerPot).Activate(pos, cube.FaceUp, tx, &candleTestUser{held: item.NewStack(item.Stick{}, 1)}, &item.UseContext{}) == false {
			t.Fatal("using a stick did not take the poppy out")
		}
		if p := tx.Block(pos).(FlowerPot); p.Plant != nil {
			t.Fatalf("pot still holds %v", p.Plant)
		}
	})
	loaded := (FlowerPot{}).DecodeNBT(FlowerPot{Plant: TreeSapling{Type: "cherry_sapling"}}.EncodeNBT()).(FlowerPot)
	if loaded.Plant != (TreeSapling{Type: "cherry_sapling"}) {
		t.Fatalf("potted sapling reloaded as %#v", loaded.Plant)
	}
	drops := FlowerPot{Plant: Cactus{}}.BreakInfo().Drops(item.ToolNone{}, nil)
	if len(drops) != 2 {
		t.Fatalf("broken pot dropped %v, want the pot and its cactus", drops)
	}
}

func TestEyeblossomOpensAtNightAndWakesNeighbours(t *testing.T) {
	w := newMultifaceTestWorld(t)
	a, b := cube.Pos{0, 1, 0}, cube.Pos{2, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.World().SetTime(18000)
		tx.SetBlock(a.Side(cube.FaceDown), Dirt{}, nil)
		tx.SetBlock(b.Side(cube.FaceDown), Dirt{}, nil)
		tx.SetBlock(a, Eyeblossom{}, nil)
		tx.SetBlock(b, Eyeblossom{}, nil)
		tx.Block(a).(Eyeblossom).RandomTick(a, tx, rand.New(rand.NewPCG(1, 2)))
		if e := tx.Block(a).(Eyeblossom); !e.Open {
			t.Fatal("eyeblossom stayed closed at night")
		}
	})
	for range 21 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if e := tx.Block(b).(Eyeblossom); !e.Open {
			t.Fatal("neighbouring eyeblossom did not follow within 20 ticks")
		}
		tx.World().SetTime(6000)
		tx.Block(a).(Eyeblossom).RandomTick(a, tx, rand.New(rand.NewPCG(1, 2)))
		if e := tx.Block(a).(Eyeblossom); e.Open {
			t.Fatal("eyeblossom stayed open by day")
		}
	})
}
