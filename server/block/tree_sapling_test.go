package block_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMangrovePropaguleHangingSupportAndDropBoundary(t *testing.T) {
	for _, age := range []int{3, 4} {
		t.Run(strconv.Itoa(age), func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
			defer w.Close()
			if err := w.Do(func(tx *world.Tx) {
				pos := cube.Pos{0, 65, 0}
				above := pos.Side(cube.FaceUp)
				tx.SetBlock(above, block.Leaves{Type: block.MangroveLeaves()}, nil)
				s := block.TreeSapling{Type: "mangrove_propagule", Age: age, Hanging: true}
				tx.SetBlock(pos, s, nil)
				s.NeighbourUpdateTick(pos, above, tx)
				if got, ok := tx.Block(pos).(block.TreeSapling); !ok || !got.Hanging || got.Age != age {
					t.Fatalf("leaf-supported propagule was removed or changed: %#v", tx.Block(pos))
				}

				// Ground support cannot rescue a hanging propagule: only mangrove
				// leaves belong to Source's supports_hanging_mangrove_propagule tag.
				tx.SetBlock(pos.Side(cube.FaceDown), block.Clay{}, nil)
				tx.SetBlock(above, block.Leaves{Type: block.OakLeaves()}, nil)
				s.NeighbourUpdateTick(pos, above, tx)
				if _, air := tx.Block(pos).(block.Air); !air {
					t.Fatalf("unsupported hanging propagule remains: %#v", tx.Block(pos))
				}
				count := 0
				for e := range tx.Entities() {
					if e.H().Type() != entity.ItemType {
						continue
					}
					stack := e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item()
					if id, _ := stack.Item().EncodeItem(); id != "minecraft:mangrove_propagule" || stack.Count() != 1 {
						t.Fatalf("propagule drop = %#v", stack)
					}
					count++
				}
				want := 0
				if age == 4 {
					want = 1
				}
				if count != want {
					t.Fatalf("age %d dropped %d items, Source requires %d", age, count, want)
				}
			}).Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMangrovePropagulePlantingRestoresSourceWater(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	h := world.EntitySpawnOpts{}.New(player.Type, player.Config{Position: mgl64.Vec3{2.5, 65, .5}, GameMode: world.GameModeSurvival})
	defer h.Close()
	if err := w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(h).(*player.Player)
		defer tx.RemoveEntity(p)
		pos := cube.Pos{0, 65, 0}
		below := pos.Side(cube.FaceDown)
		tx.SetBlock(below, block.Clay{}, nil)
		tx.SetLiquid(pos, block.Water{Depth: 5, Still: true})
		ctx := &item.UseContext{}
		if !(block.TreeSapling{Type: "mangrove_propagule"}).UseOnBlock(below, cube.FaceUp, mgl64.Vec3{}, tx, p, ctx) {
			t.Fatal("Source-valid planting on clay in flowing water was rejected")
		}
		if got, ok := tx.Block(pos).(block.TreeSapling); !ok || got.Hanging || got.Age != 4 {
			t.Fatalf("planted propagule is not mature/non-hanging: %#v", tx.Block(pos))
		}
		if liquid, ok := tx.Liquid(pos); !ok || liquid.LiquidDepth() != 8 || liquid.LiquidFalling() {
			t.Fatalf("waterlogged planting must restore Source water: %#v, present=%t", liquid, ok)
		}
		tx.SetBlock(below, block.Stone{}, nil)
		tx.Block(pos).(block.TreeSapling).NeighbourUpdateTick(pos, below, tx)
		if _, water := tx.Block(pos).(block.Water); !water {
			t.Fatalf("losing soil support must leave the water, got %#v", tx.Block(pos))
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
