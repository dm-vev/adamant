package block

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

type nativeSwitchSoundObserver struct {
	world.NopHandler
	sounds    []world.Sound
	positions []mgl64.Vec3
}

func (o *nativeSwitchSoundObserver) HandleSound(_ *world.Context, s world.Sound, pos mgl64.Vec3) {
	o.sounds = append(o.sounds, s)
	o.positions = append(o.positions, pos)
}

func TestSwitchSoundsMatchNativePackets(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_switch_sounds.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct{ Scenario struct{ Block string } }
		Sounds    []struct {
			SoundType, EntityType string
			ExtraData             int32
			Position              [3]float64
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 3 || len(reference.Sounds) != 6 {
		t.Fatal("incomplete native switch sound capture")
	}
	for i, trace := range reference.Scenarios {
		t.Run(trace.Scenario.Block, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End}.New()
			t.Cleanup(func() { _ = w.Close() })
			o := &nativeSwitchSoundObserver{}
			w.Handle(o)
			pos := cube.Pos{0, 65, 0}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
				var source world.Block
				switch trace.Scenario.Block {
				case "stone_button":
					source = Button{Facing: cube.FaceUp, Type: StoneButton()}
				case "wooden_button":
					source = Button{Facing: cube.FaceUp, Type: WoodButton()}
				case "lever":
					source = Lever{Facing: cube.FaceUp, Direction: cube.North}
				default:
					t.Error("unknown native sound source")
					return
				}
				tx.SetBlock(pos, source, nil)
				switch b := source.(type) {
				case Button:
					b.Activate(pos, cube.FaceUp, tx, nil, nil)
				case Lever:
					b.Activate(pos, cube.FaceUp, tx, nil, nil)
				}
				if b, ok := tx.Block(pos).(Lever); ok {
					b.Activate(pos, cube.FaceUp, tx, nil, nil)
				}
			})
			for n := 0; n < 40; n++ {
				w.AdvanceTick()
			}
			if len(o.sounds) != 2 {
				t.Fatalf("click sounds=%d native=2", len(o.sounds))
			}
			for n, s := range o.sounds {
				expected := reference.Sounds[i*2+n]
				got, ok := s.(sound.NativeEvent)
				if !ok {
					t.Errorf("sound=%T, native event=%s", s, expected.SoundType)
					continue
				}
				if got.SoundType != expected.SoundType || got.ExtraData != expected.ExtraData || got.EntityType != expected.EntityType || o.positions[n] != mgl64.Vec3(expected.Position) {
					t.Errorf("sound=%+v at %v native=%+v", got, o.positions[n], expected)
				}
			}
		})
	}
}
