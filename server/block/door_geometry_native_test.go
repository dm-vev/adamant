package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestDoorHalfGeometryMatchesNativeRays(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_door_geometry.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block          string
				Topology       string
				LowerDirection int  `json:"lower_direction"`
				UpperDirection int  `json:"upper_direction"`
				LowerHinge     bool `json:"lower_hinge"`
				UpperHinge     bool `json:"upper_hinge"`
				Open           bool
			}
			Base   cube.Pos
			Blocks []struct {
				ID     string
				States map[string]any
			}
			Rays []struct {
				Half              int
				Origin, Direction mgl64.Vec3
				Hit               *struct {
					Position     cube.Pos
					Face         string
					FaceLocation mgl64.Vec3 `json:"face_location"`
				}
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 384 {
		t.Fatal("incomplete native door geometry corpus")
	}
	invalid, err := os.ReadFile("testdata/bedrock_1_26_51_door_invalid_geometry.json")
	if err != nil {
		t.Fatal(err)
	}
	extra := native
	extra.Scenarios, extra.Complete = nil, false
	if err := json.Unmarshal(invalid, &extra); err != nil {
		t.Fatal(err)
	}
	if !extra.Complete || len(extra.Scenarios) != 240 {
		t.Fatal("incomplete native invalid door geometry corpus")
	}
	native.Scenarios = append(native.Scenarios, extra.Scenarios...)
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	faces := map[string]cube.Face{"North": cube.FaceNorth, "South": cube.FaceSouth, "West": cube.FaceWest, "East": cube.FaceEast}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/%s/lower%d/upper%d/hinges%t_%t/open%t", c.Scenario.Block, c.Scenario.Topology, c.Scenario.LowerDirection, c.Scenario.UpperDirection, c.Scenario.LowerHinge, c.Scenario.UpperHinge, c.Scenario.Open), func(t *testing.T) {
			if len(c.Blocks) != 2 || len(c.Rays) != 24 {
				t.Fatal("native half/ray input controls differ")
			}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(c.Base.Side(cube.FaceDown), Stone{}, nil)
				for half, b := range c.Blocks {
					states := map[string]any{}
					for k, v := range b.States {
						states[k] = v
					}
					if d, ok := states["minecraft:cardinal_direction"].(string); ok {
						if legacy, exists := states["direction"]; exists {
							if legacy != map[string]float64{"south": 0, "west": 1, "north": 2, "east": 3}[d] {
								t.Fatal("native door aliases disagree")
							}
							delete(states, "direction")
						}
					}
					block, ok := world.BlockByName(b.ID, states)
					if !ok {
						t.Fatalf("native door permutation unavailable: %s %v", b.ID, states)
					}
					tx.SetBlock(c.Base.Add(cube.Pos{0, half, 0}), block, nil)
				}
				for _, r := range c.Rays {
					pos := c.Base.Add(cube.Pos{0, r.Half, 0})
					for _, shape := range []trace.Shape{trace.CollisionShape, trace.RayShape} {
						hit, ok := trace.BlockIntercept(pos, tx, tx.Block(pos), r.Origin, r.Origin.Add(r.Direction.Mul(4)), shape)
						if ok != (r.Hit != nil) {
							t.Errorf("half %d shape %d ray %v -> %v hit=%t, native=%t", r.Half, shape, r.Origin, r.Direction, ok, r.Hit != nil)
							return
						}
						if ok {
							face, exists := faces[r.Hit.Face]
							if !exists || r.Hit.Position != pos {
								t.Fatal("native ray hit a different cell or an unknown face")
							}
							if hit.Face() != face {
								t.Errorf("half %d shape %d face=%v, native=%s", r.Half, shape, hit.Face(), r.Hit.Face)
								return
							}
						}
					}
				}
			})
		})
	}
}
