package block

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBlockResourceFactoryMatchesLiveNativeConstruction(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_block_resource_random.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete    bool
		Scenarios   []json.RawMessage
		Checkpoints []struct {
			Resources []struct {
				Position         cube.Pos
				Created          bool
				Origin, Velocity [3]float32
				PickupDelay      int `json:"pickup_delay"`
				Words            []struct {
					Value       uint32
					FloatCaller string `json:"float_caller"`
				}
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 13 {
		t.Fatal("incomplete live block-resource trace")
	}
	cases, created := 0, 0
	for _, frame := range native.Checkpoints {
		for _, c := range frame.Resources {
			cases++
			if c.Created {
				created++
			}
			t.Run(fmt.Sprintf("%v/created%t", c.Position, c.Created), func(t *testing.T) {
				if !c.Created && len(c.Words) != 0 {
					t.Fatal("native blocked resource drew from world RNG")
				}
				if c.Created {
					if len(c.Words) != 4 || c.PickupDelay != 10 {
						t.Fatal("native resource constructor budget/delay differs")
					}
					for i, parent := range []string{"0x1a2426d", "0x1a2429c", "0x1a242b9", "0xd1c84d"} {
						if c.Words[i].FloatCaller != parent {
							t.Fatal("native position/actor-constructor draw order differs")
						}
					}
				}
				source := &ejectionNativeWords{}
				var received []world.EntitySpawnOpts
				registry := world.EntityRegistryConfig{Item: func(opts world.EntitySpawnOpts, value any) *world.EntityHandle {
					if s, ok := value.(item.Stack); !ok || s.Count() != 1 {
						t.Fatal("resource item factory lost its stack")
					}
					received = append(received, opts)
					return opts.New(redstoneTNTTestEntityType{}, redstoneTNTTestEntityConfig{})
				}}.New([]world.EntityType{redstoneTNTTestEntityType{}})
				w := world.Config{Synchronous: true, RandomTickSpeed: -1, RandSource: source, Entities: registry}.New()
				defer w.Close()
				_ = w.SetGameRule("block_drops", c.Created)
				<-w.Exec(func(tx *world.Tx) {
					_ = tx.Block(c.Position)
					for _, word := range c.Words {
						source.words = append(source.words, word.Value)
					}
					source.active = true
					if empty := tx.DropBlockResource(c.Position, item.Stack{}); empty != nil || source.used != 0 || len(received) != 0 {
						t.Fatal("empty resource changed RNG or created an entity")
					}
					e := tx.DropBlockResource(c.Position, item.NewStack(Stone{}, 1))
					if (e != nil) != c.Created || len(received) != map[bool]int{false: 0, true: 1}[c.Created] {
						t.Fatal("resource creation guard differs")
					}
					if source.used != len(c.Words) {
						t.Errorf("resource consumed %d words, native=%d", source.used, len(c.Words))
					}
					if c.Created {
						for axis, want := range c.Origin {
							if math.Float32bits(float32(received[0].Position[axis])) != math.Float32bits(want) {
								t.Errorf("axis %d origin=%v, native=%v", axis, received[0].Position[axis], want)
							}
						}
					}
					source.active = false
				})
			})
		}
	}
	if cases != 12 || created != 6 {
		t.Fatal("missing native drop-enabled/disabled resource controls")
	}
}
