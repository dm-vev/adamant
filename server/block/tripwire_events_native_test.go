package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type hookEventSoundObserver struct {
	nativeSwitchSoundObserver
	before func()
}

func (o *hookEventSoundObserver) HandleSound(ctx *world.Context, s world.Sound, pos mgl64.Vec3) {
	o.before()
	o.nativeSwitchSoundObserver.HandleSound(ctx, s, pos)
}

func TestTripwireHookEventsMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_hook_events.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			BeforeAttached   bool `json:"before_attached"`
			BeforePowered    bool `json:"before_powered"`
			AfterAttached    bool `json:"after_attached"`
			AfterPowered     bool `json:"after_powered"`
			Visible          string
			Pos              cube.Pos
			VisibleRuntimeID uint32 `json:"visible_runtime_id"`
			Output           []struct {
				Kind        string
				SoundID     int   `json:"sound_id"`
				EventID     int   `json:"event_id"`
				ExtraData   int32 `json:"extra_data"`
				Pos         mgl64.Vec3
				SourceNil   bool `json:"source_nil"`
				AffectedNil bool `json:"affected_nil"`
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 64 {
		t.Fatal("incomplete executed-client hook event capture")
	}
	sounds := map[int]string{73: "power.on", 74: "power.off", 75: "attach", 76: "detach"}
	events := map[int]world.GameEvent{1: world.GameEventBlockActivate, 5: world.GameEventBlockDeactivate, 2: world.GameEventBlockAttach, 7: world.GameEventBlockDetach}
	for i, c := range native.Scenarios {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			w := newMultifaceTestWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(c.Pos.Side(cube.FaceWest), Stone{}, nil)
				tx.SetBlock(c.Pos, TripwireHook{Facing: cube.East}, nil)
				visible := TripwireHook{Facing: cube.East, Attached: c.BeforeAttached, Powered: c.BeforePowered}
				if c.Visible == "after" {
					visible.Attached, visible.Powered = c.AfterAttached, c.AfterPowered
				}
				tx.SetBlock(c.Pos, visible, nil)
				sensorPos := c.Pos.Add(cube.Pos{4, 0, 0})
				tx.SetBlock(sensorPos, NewSculkSensor(), nil)
				listener := tx.Block(sensorPos).(SculkSensor).VibrationData()
				observer := &hookEventSoundObserver{before: func() {
					if listener.Selector.HasCandidate {
						t.Error("game event preceded sound; native broadcasts sound first")
					}
				}}
				w.Handle(observer)
				previous := TripwireHook{Facing: cube.East, Attached: c.BeforeAttached, Powered: c.BeforePowered}
				emitTripwireHookState(c.Pos, tx, previous, c.AfterAttached, c.AfterPowered)
				if len(c.Output) == 0 {
					if len(observer.sounds) != 0 || listener.Selector.HasCandidate {
						t.Fatal("unchanged native flags emitted a sound/event")
					}
					return
				}
				if len(c.Output) != 2 || c.Output[0].Kind != "sound" || c.Output[1].Kind != "event" {
					t.Fatal("unexpected native callback sequence")
				}
				s, e := c.Output[0], c.Output[1]
				if !e.SourceNil || !e.AffectedNil || s.ExtraData != int32(c.VisibleRuntimeID) {
					t.Fatal("invalid native callback controls")
				}
				if len(observer.sounds) != 1 {
					t.Fatalf("sounds=%d, native 1", len(observer.sounds))
				}
				got, ok := observer.sounds[0].(sound.NativeEvent)
				if !ok || got.SoundType != sounds[s.SoundID] || got.ExtraData != s.ExtraData {
					t.Fatalf("sound=%+v, native=%+v", observer.sounds[0], s)
				}
				point := observer.positions[0]
				for axis := range 3 {
					point[axis] = float64(float32(point[axis]))
				}
				if point != s.Pos {
					t.Errorf("sound point=%v, native %v", point, s.Pos)
				}
				if !listener.Selector.HasCandidate {
					t.Fatal("missing native hook vibration")
				}
				info := listener.Selector.Candidate
				if info.Event != events[e.EventID] || cube.PosFromVec3(info.Position) != cube.PosFromVec3(e.Pos) || info.SourceID != uuid.Nil || info.ProjectileOwnerID != uuid.Nil {
					t.Errorf("event=%+v, native=%+v", info, e)
				}
			})
		})
	}
}
