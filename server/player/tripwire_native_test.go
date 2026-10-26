package player

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
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

type tripwireBreakNativeBlock struct {
	ID     string
	States map[string]any
}

type tripwireNativeSound struct {
	SoundType, EntityType string
	ExtraData             int32
	Position              mgl32.Vec3
}
type tripwireNativeSoundObserver struct {
	world.NopHandler
	packets []tripwireNativeSound
}

func (o *tripwireNativeSoundObserver) HandleSound(_ *world.Context, s world.Sound, pos mgl64.Vec3) {
	event, ok := s.(sound.NativeEvent)
	if !ok {
		return
	}
	switch event.SoundType {
	case "attach", "detach", "power.on", "power.off":
		o.packets = append(o.packets, tripwireNativeSound{SoundType: event.SoundType, EntityType: event.EntityType, ExtraData: event.ExtraData, Position: mgl32.Vec3{float32(pos[0]), float32(pos[1]), float32(pos[2])}})
	}
}

func compareTripwireBreakNative(t *testing.T, tx *world.Tx, tick int64, pos cube.Pos, expected tripwireBreakNativeBlock) {
	t.Helper()
	id, states := tx.Block(pos).EncodeBlock()
	if id != expected.ID {
		t.Errorf("tick %d at %v id=%s native=%s", tick, pos, id, expected.ID)
		return
	}
	for key, value := range expected.States {
		actual := states[key]
		switch v := value.(type) {
		case bool:
			if b, ok := actual.(uint8); ok {
				actual = b != 0
			}
			if actual != v {
				t.Errorf("tick %d at %v %s=%v native=%v", tick, pos, key, actual, v)
			}
		case float64:
			if b, ok := actual.(int32); ok {
				actual = float64(b)
			}
			if actual != v {
				t.Errorf("tick %d at %v %s=%v native=%v", tick, pos, key, actual, v)
			}
		default:
			t.Fatalf("unexpected native tripwire property %s=%v", key, value)
		}
	}
}

// Exercise real Player.BreakBlock, including the held tool and the before-
// removal callback. Input timing comes from native absolute game time.
func TestTripwirePlayerBreakMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_tripwire_break.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Sounds    []tripwireNativeSound
		Scenarios []struct {
			Scenario struct {
				GameMode    string `json:"game_mode"`
				Tool        string
				Length, Cut int
				Direction   *int
			}
			Feet    [3]float64
			Changes []struct {
				Tick     int64
				GameTick int64 `json:"game_tick"`
				Held     string
				Before   tripwireBreakNativeBlock
			}
			Results []struct{ Source tripwireBreakNativeBlock }
			Samples []struct {
				Tick              int64
				GameTick          int64 `json:"game_tick"`
				Left, Right, Wire tripwireBreakNativeBlock
				Strings           []tripwireBreakNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 58 {
		t.Fatal("incomplete native player-cutting capture")
	}
	var actualSounds []tripwireNativeSound
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		dir := cube.East
		if c.Direction == nil || *c.Direction < 0 || *c.Direction > 3 {
			t.Fatal("missing native line direction")
		}
		dir = [...]cube.Direction{cube.North, cube.East, cube.South, cube.West}[*c.Direction]
		t.Run(fmt.Sprintf("%s/%s/length%d/cut%d/direction%d", c.GameMode, c.Tool, c.Length, c.Cut, dir), func(t *testing.T) {
			if len(trace.Samples) < 50 || len(trace.Changes) != 1 || len(trace.Results) != 1 || trace.Results[0].Source.ID != "minecraft:air" || trace.Changes[0].Held != "minecraft:"+c.Tool {
				t.Fatal("invalid native player-cutting trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.Overworld, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			observer := &tripwireNativeSoundObserver{}
			w.Handle(observer)
			defer func() { actualSounds = append(actualSounds, observer.packets...) }()
			t.Cleanup(func() { _ = w.Close() })
			if err := w.SetGameRule("block_drops", false); err != nil {
				t.Fatal(err)
			}
			if err := w.SetGameRule("spawn_mobs", false); err != nil {
				t.Fatal(err)
			}
			pos := func(i int) cube.Pos {
				p := cube.Pos{0, 65, 0}
				for i < 0 {
					p = p.Side(dir.Opposite().Face())
					i++
				}
				for i > 0 {
					p = p.Side(dir.Face())
					i--
				}
				return p
			}
			var handle *world.EntityHandle
			<-w.Exec(func(tx *world.Tx) {
				feet := mgl64.Vec3(trace.Feet)
				base := cube.PosFromVec3(feet)
				for x := -2; x <= 2; x++ {
					for z := -2; z <= 2; z++ {
						tx.SetBlock(cube.Pos{base[0] + x, 63, base[2] + z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(pos(-2), block.Stone{}, nil)
				tx.SetBlock(pos(c.Length+1), block.Stone{}, nil)
				tx.SetBlock(pos(-1), block.TripwireHook{Facing: dir}, nil)
				tx.SetBlock(pos(c.Length), block.TripwireHook{Facing: dir.Opposite()}, nil)
				for i := 0; i < c.Length; i++ {
					tx.SetBlock(pos(i), block.Tripwire{}, nil)
				}
				tx.SetBlock(pos(-3).Side(cube.FaceDown), block.Stone{}, nil)
				tx.SetBlock(pos(-3), block.RedstoneWire{}, nil)
				var mode world.GameMode = world.GameModeSurvival
				if c.GameMode == "creative" {
					mode = world.GameModeCreative
				}
				handle = (world.EntitySpawnOpts{Position: feet}).New(Type, Config{Name: "RedstoneAudit", Position: feet, GameMode: mode})
				p := tx.AddEntity(handle).(*Player)
				if c.Tool == "shears" {
					p.SetHeldItems(item.NewStack(item.Shears{}, 1), item.Stack{})
				}
			})
			applied := 0
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					compareTripwireBreakNative(t, tx, sample.Tick, pos(-1), sample.Left)
					compareTripwireBreakNative(t, tx, sample.Tick, pos(c.Length), sample.Right)
					compareTripwireBreakNative(t, tx, sample.Tick, pos(-3), sample.Wire)
					if len(sample.Strings) != c.Length {
						t.Error("missing native string permutations")
						return
					}
					for i, state := range sample.Strings {
						compareTripwireBreakNative(t, tx, sample.Tick, pos(i), state)
					}
					if sample.GameTick == trace.Changes[0].GameTick {
						compareTripwireBreakNative(t, tx, sample.Tick, pos(c.Cut), trace.Changes[0].Before)
						p, ok := handle.Entity(tx)
						if !ok {
							t.Error("player removed before break")
							return
						}
						p.(*Player).BreakBlock(pos(c.Cut))
						applied++
						if _, ok := tx.Block(pos(c.Cut)).(block.Air); !ok {
							t.Error("kernel player break was rejected")
						}
					}
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() && applied != 1 {
				t.Errorf("applied %d native breaks", applied)
			}
		})
	}
	var expectedSounds []tripwireNativeSound
	for _, s := range reference.Sounds {
		switch s.SoundType {
		case "attach", "detach", "power.on", "power.off":
			expectedSounds = append(expectedSounds, s)
		}
	}
	if len(expectedSounds) == 0 {
		t.Fatal("native capture contains no hook sound packets")
	}
	if len(actualSounds) != len(expectedSounds) {
		t.Fatalf("hook sound packets=%d native=%d", len(actualSounds), len(expectedSounds))
	}
	for i, expected := range expectedSounds {
		if actualSounds[i] != expected {
			t.Errorf("hook sound packet %d=%+v native=%+v", i, actualSounds[i], expected)
		}
	}
}
