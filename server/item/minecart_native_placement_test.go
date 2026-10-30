package item_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMinecartItemSpawnRequestMatchesClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_minecart_placement_request.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Cases    []struct {
			Kind          int
			BlockPosition cube.Pos `json:"block_position"`
			Position      [3]float64
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Cases) != 120 {
		t.Fatal("incomplete original placement requests")
	}
	for _, c := range native.Cases {
		t.Run(fmt.Sprintf("kind%d/%v", c.Kind, c.BlockPosition), func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
			defer w.Close()
			var use interface {
				UseOnBlock(cube.Pos, cube.Face, mgl64.Vec3, *world.Tx, item.User, *item.UseContext) bool
			}
			switch c.Kind {
			case 0:
				use = item.Minecart{}
			case 1:
				use = item.MinecartChest{}
			case 3:
				use = item.MinecartTNT{}
			case 4:
				use = item.MinecartHopper{}
			case 5:
				use = item.MinecartCommandBlock{}
			default:
				t.Fatal("unknown client cart kind")
			}
			<-w.Exec(func(tx *world.Tx) {
				p := c.BlockPosition
				tx.SetBlock(p.Side(cube.FaceDown), block.Stone{}, nil)
				for _, direction := range []block.RailDirection{block.RailNorthSouth, block.RailAscendingEast, block.RailAscendingWest, block.RailAscendingNorth, block.RailAscendingSouth} {
					tx.SetBlock(p, block.Rail{Direction: direction}, nil)
					ctx := &item.UseContext{}
					if !use.UseOnBlock(p, cube.FaceUp, mgl64.Vec3{.5, .5, .5}, tx, nil, ctx) || ctx.CountSub != 1 {
						t.Fatal("cart item placement failed")
					}
					count := 0
					for e := range tx.Entities() {
						count++
						network := e.Position()
						network[1] += e.H().Type().(interface{ NetworkOffset() float64 }).NetworkOffset()
						for axis := range 3 {
							if float64(float32(network[axis])) != c.Position[axis] {
								t.Fatalf("rail %d axis %d native spawn origin=%g, client=%g", direction, axis, float32(network[axis]), c.Position[axis])
							}
						}
						tx.RemoveEntity(e)
					}
					if count != 1 {
						t.Fatal("cart placement did not create exactly one actor")
					}
				}
			})
		})
	}
}
