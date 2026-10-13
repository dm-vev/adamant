package block

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBuddingAmethystGrowsBudsOutwardToClusters(t *testing.T) {
	w := newMultifaceTestWorld(t)
	origin := cube.Pos{0, 10, 0}
	r := rand.New(rand.NewPCG(3, 4))
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(origin, BuddingAmethyst{}, nil)
		for range 1000 {
			BuddingAmethyst{}.RandomTick(origin, tx, r)
		}
		for _, face := range cube.Faces() {
			if got := tx.Block(origin.Side(face)); got != (AmethystCluster{Stage: 3, Facing: face}) {
				t.Errorf("face %v: %#v, want a cluster pointing away", face, got)
			}
		}
	})
}

func TestBuddingAmethystLeavesBudsOfOtherFacingsAndBlocks(t *testing.T) {
	w := newMultifaceTestWorld(t)
	origin := cube.Pos{0, 10, 0}
	r := rand.New(rand.NewPCG(5, 6))
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(origin, BuddingAmethyst{}, nil)
		tx.SetBlock(origin.Side(cube.FaceUp), AmethystCluster{Facing: cube.FaceNorth}, nil)
		tx.SetBlock(origin.Side(cube.FaceDown), Stone{}, nil)
		for range 1000 {
			BuddingAmethyst{}.RandomTick(origin, tx, r)
		}
		if got := tx.Block(origin.Side(cube.FaceUp)); got != (AmethystCluster{Facing: cube.FaceNorth}) {
			t.Errorf("bud facing another way became %#v", got)
		}
		if got := tx.Block(origin.Side(cube.FaceDown)); got != (Stone{}) {
			t.Errorf("stone beside budding amethyst became %#v", got)
		}
	})
}

func TestAmethystClusterDrops(t *testing.T) {
	pickaxe := item.Pickaxe{Tier: item.ToolTierIron}
	silk := []item.Enchantment{item.NewEnchantment(enchantment.SilkTouch, 1)}
	cases := []struct {
		name  string
		stage int
		tool  item.Tool
		ench  []item.Enchantment
		want  []item.Stack
	}{
		{"cluster mined with a pickaxe", 3, pickaxe, nil, []item.Stack{item.NewStack(item.AmethystShard{}, 4)}},
		{"cluster mined by hand", 3, item.ToolNone{}, nil, []item.Stack{item.NewStack(item.AmethystShard{}, 2)}},
		{"bud drops nothing", 2, pickaxe, nil, nil},
		{"silk touch keeps a bud", 1, pickaxe, silk, []item.Stack{item.NewStack(AmethystCluster{Stage: 1, Facing: cube.FaceUp}, 1)}},
	}
	for _, c := range cases {
		got := AmethystCluster{Stage: c.stage, Facing: cube.FaceNorth}.BreakInfo().Drops(c.tool, c.ench)
		if len(got) != len(c.want) {
			t.Fatalf("%s: drops %v, want %v", c.name, got, c.want)
		}
		for i := range got {
			if !got[i].Equal(c.want[i]) || got[i].Count() != c.want[i].Count() {
				t.Fatalf("%s: drops %v, want %v", c.name, got, c.want)
			}
		}
	}
}

func TestAmethystBudNeedsSturdySupport(t *testing.T) {
	w := newMultifaceTestWorld(t)
	support, bud := cube.Pos{0, 0, 0}, cube.Pos{1, 0, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(support, Stone{}, nil)
		tx.SetBlock(bud, AmethystCluster{Facing: cube.FaceEast}, nil)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(bud).(AmethystCluster); !ok {
			t.Fatalf("supported bud broke")
		}
		tx.SetBlock(support, nil, nil)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if got := tx.Block(bud); got != (Air{}) {
			t.Fatalf("unsupported bud left %#v", got)
		}
	})
}
