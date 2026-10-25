package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Settled lamp readback checks actual activation, including strong power
// through a solid block. It does not assert initial publication timing.
func TestTorchOutputFacesMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_torch_faces.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Facing, Output string
				Conducted      bool
			}
			Samples []struct {
				Tick          int64
				Torch, Output struct{ ID string }
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 60 {
		t.Fatal("incomplete native torch output capture")
	}
	faces := map[string]cube.Face{"down": cube.FaceDown, "up": cube.FaceUp, "north": cube.FaceNorth, "south": cube.FaceSouth, "west": cube.FaceWest, "east": cube.FaceEast}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/conducted%v", c.Facing, c.Output, c.Conducted), func(t *testing.T) {
			if len(trace.Samples) != 20 {
				t.Fatal("incomplete native torch trace")
			}
			facing, ok := faces[c.Facing]
			if !ok {
				t.Fatal("unknown mounting face")
			}
			out, ok := faces[c.Output]
			if !ok {
				t.Fatal("unknown output face")
			}
			pos := cube.Pos{0, 65, 0}
			lamp := pos.Side(out)
			if c.Conducted {
				lamp = lamp.Side(out)
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos.Side(facing), Stone{}, nil)
				if c.Conducted {
					tx.SetBlock(pos.Side(out), Stone{}, nil)
				}
				tx.SetBlock(lamp, RedstoneLamp{}, nil)
				tx.SetBlock(pos, RedstoneTorch{Facing: facing, Lit: true}, nil)
			})
			for range trace.Samples {
				w.AdvanceTick()
			}
			expected := trace.Samples[len(trace.Samples)-1]
			<-w.Exec(func(tx *world.Tx) {
				for _, test := range []struct {
					Pos cube.Pos
					ID  string
				}{{pos, expected.Torch.ID}, {lamp, expected.Output.ID}} {
					id, _ := tx.Block(test.Pos).EncodeBlock()
					if id != test.ID {
						t.Errorf("at %v: %s, native %s", test.Pos, id, test.ID)
					}
				}
			})
		})
	}
}
