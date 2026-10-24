package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestLeafLitterSurvivesNearbyBlockChanges(t *testing.T) {
	for _, ground := range []world.Block{block.Dirt{}, block.Grass{}} {
		name, _ := ground.EncodeBlock()
		t.Run(name, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
			t.Cleanup(func() { _ = w.Close() })
			litter := make(map[cube.Pos]block.LeafLitter)
			<-w.Exec(func(tx *world.Tx) {
				opts := &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true}
				for x := range 3 {
					for z := range 3 {
						pos := cube.Pos{x, 65, z}
						leaf := block.LeafLitter{AdditionalCount: (x*3 + z) % 8, Facing: cube.Direction((x + z) % 4)}
						litter[pos] = leaf
						tx.SetBlock(pos.Side(cube.FaceDown), ground, opts)
						tx.SetBlock(pos, leaf, opts)
					}
				}
			})
			nearby := cube.Pos{3, 65, 1}
			for _, replacement := range []world.Block{block.Stone{}, nil} {
				<-w.Exec(func(tx *world.Tx) { tx.SetBlock(nearby, replacement, nil) })
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					for pos, want := range litter {
						if got := tx.Block(pos); got != want {
							t.Errorf("nearby block change removed or changed leaf litter at %v: %#v, want %#v", pos, got, want)
						}
					}
					if count := leafLitterDropCount(tx); count != 0 {
						t.Errorf("nearby block change dropped %d leaf litter items", count)
					}
				})
			}
			// Removing actual support must break only the litter above it.
			unsupported := cube.Pos{1, 65, 1}
			<-w.Exec(func(tx *world.Tx) { tx.SetBlock(unsupported.Side(cube.FaceDown), nil, nil) })
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) {
				for pos, want := range litter {
					if pos == unsupported {
						if _, air := tx.Block(pos).(block.Air); !air {
							t.Errorf("unsupported leaf litter remains at %v", pos)
						}
					} else if got := tx.Block(pos); got != want {
						t.Errorf("support removal propagated to supported litter at %v: %#v", pos, got)
					}
				}
				if count, want := leafLitterDropCount(tx), litter[unsupported].AdditionalCount+1; count != want {
					t.Errorf("support removal dropped %d items, want %d", count, want)
				}
			})
		})
	}
}

func TestLeafLitterUsesFullTopSupportForPlacementAndSurvival(t *testing.T) {
	for _, tt := range []struct {
		name    string
		support world.Block
		want    bool
	}{
		{"dirt", block.Dirt{}, true},
		{"grass", block.Grass{}, true},
		{"stone", block.Stone{}, true},
		{"top_slab", block.Slab{Block: block.Stone{}, Top: true}, true},
		{"double_slab", block.Slab{Block: block.Stone{}, Double: true}, true},
		{"bottom_slab", block.Slab{Block: block.Stone{}}, false},
		{"farmland", block.Farmland{}, false},
		{"dirt_path", block.DirtPath{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				p := tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Position: mgl64.Vec3{4.5, 65, .5}, GameMode: world.GameModeCreative})).(*player.Player)
				below := cube.Pos{0, 64, 0}
				pos := below.Side(cube.FaceUp)
				opts := &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true}
				tx.SetBlock(below, tt.support, opts)
				leaf := block.LeafLitter{}
				if placed := leaf.UseOnBlock(below, cube.FaceUp, mgl64.Vec3{}, tx, p, &item.UseContext{}); placed != tt.want {
					t.Errorf("leaf litter placement = %v, want %v", placed, tt.want)
				}
				// Install it directly too, covering loaded/generated litter and
				// all native segment counts independently of item placement.
				for count := range 8 {
					leaf.AdditionalCount = count
					tx.SetBlock(pos, leaf, opts)
					leaf.NeighbourUpdateTick(pos, pos.Side(cube.FaceEast), tx)
					_, survives := tx.Block(pos).(block.LeafLitter)
					if survives != tt.want {
						t.Errorf("%d-segment litter survival = %v, want %v", count+1, survives, tt.want)
					}
				}
			})
		})
	}
}

func leafLitterDropCount(tx *world.Tx) int {
	count := 0
	for e := range tx.Entities() {
		if e.H().Type() != entity.ItemType {
			continue
		}
		stack := e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item()
		if name, _ := stack.Item().EncodeItem(); name == "minecraft:leaf_litter" {
			count += stack.Count()
		}
	}
	return count
}
