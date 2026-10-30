package block

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestDaylightProducerRegistrationMatchesNativeHeap(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_daylight_components.json")
	if err != nil {
		t.Fatal(err)
	}
	type state struct {
		Pos      cube.Pos
		Strength int
		Removed  bool
	}
	var ref struct {
		Complete    bool
		Checkpoints []struct {
			Kind            string
			Tick            int64
			Phase, Age      int
			Blocks          []torchNetworkBlock
			Active, Pending []state
		}
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	if !ref.Complete || len(ref.Checkpoints) != 10 {
		t.Fatal("incomplete native producer checkpoint capture")
	}
	settings := (world.NopProvider{}).Settings()
	settings.Time = 6000
	settings.TimeCycle = false
	settings.CurrentTick = ref.Checkpoints[0].Tick - 1
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
	t.Cleanup(func() { _ = w.Close() })
	for i, point := range ref.Checkpoints {
		if point.Phase != i/5 || point.Age != i%5 || len(point.Blocks) != 2 {
			t.Fatal("invalid producer checkpoint phase")
		}
		for w.CurrentTick() < point.Tick {
			w.AdvanceTick()
		}
		<-w.Exec(func(tx *world.Tx) {
			if point.Kind == "setup" {
				for _, z := range []int{0, 4} {
					p := cube.Pos{8, 65, z}
					tx.SetBlock(p, Air{}, nil)
					tx.SetBlock(p.Side(cube.FaceDown), Stone{}, nil)
					initial := 0
					if z == 4 {
						initial = 15
					}
					tx.SetBlock(p, DaylightDetector{Signal: initial}, nil)
				}
			}
			for j, z := range []int{0, 4} {
				p := cube.Pos{8, 65, z}
				var expected *state
				active := false
				for _, s := range point.Active {
					if s.Pos == p && !s.Removed {
						copy := s
						expected = &copy
						active = true
					}
				}
				if expected == nil {
					for _, s := range point.Pending {
						if s.Pos == p {
							copy := s
							expected = &copy
						}
					}
				}
				if expected == nil {
					t.Fatalf("missing native producer at %v", p)
				}
				got, registered := tx.Redstone().ProducerStrength(p)
				if got != expected.Strength || registered != active {
					t.Errorf("tick%d %v producer=%d active%v native=%d active%v", point.Tick, p, got, registered, expected.Strength, active)
				}
				if signal := tx.Block(p).(DaylightDetector).Signal; signal != point.Blocks[j].States.Signal {
					t.Errorf("tick%d %v block signal=%d native=%d", point.Tick, p, signal, point.Blocks[j].States.Signal)
				}
			}
		})
	}
}

type liveLightStructure struct{ world.Block }

func (liveLightStructure) Dimensions() [3]int { return [3]int{1, 1, 1} }
func (s liveLightStructure) At(int, int, int, func(int, int, int) world.Block) (world.Block, world.Liquid) {
	return s.Block, nil
}

func TestLiveLightingQueriesStayInLoadedTerrain(t *testing.T) {
	for _, structure := range []bool{false, true} {
		name := "SetBlock"
		if structure {
			name = "BuildStructure"
		}
		t.Run(name, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				p := cube.Pos{15, 64, 15}
				receiver := cube.Pos{14, 64, 15}
				change := func(b world.Block) {
					if structure {
						tx.BuildStructure(p, liveLightStructure{b})
					} else {
						tx.SetBlock(p, b, nil)
					}
				}
				change(Glowstone{})
				if got := tx.BlockLight(receiver); got != 14 {
					t.Errorf("new light=%d, native unobstructed neighbour=14", got)
				}
				change(Air{})
				if got := tx.BlockLight(receiver); got != 0 {
					t.Errorf("removed light=%d, native=0", got)
				}
				for x := -1; x <= 1; x++ {
					for z := -1; z <= 1; z++ {
						if x == 0 && z == 0 {
							continue
						}
						if loaded, _ := tx.ChunkState(world.ChunkPos{int32(x), int32(z)}); loaded {
							t.Errorf("relighting loaded neighbour %d,%d", x, z)
						}
					}
				}
			})
		})
	}
}

func BenchmarkLiveLightingSourceReplacement(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	b.Cleanup(func() { _ = w.Close() })
	p := cube.Pos{15, 64, 15}
	<-w.Exec(func(tx *world.Tx) { tx.SetBlock(p, Air{}, nil) })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(p, Glowstone{}, nil)
			_ = tx.BlockLight(p.Side(cube.FaceWest))
			tx.SetBlock(p, Air{}, nil)
			_ = tx.BlockLight(p.Side(cube.FaceWest))
		})
	}
}
