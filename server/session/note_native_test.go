package session

import (
	"encoding/json"
	"fmt"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"os"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

type nativeNotePacket struct {
	Event    *v2193.BlockEventPacket `json:"block_event"`
	Sound    *v2193.LevelSoundEventPacket
	Particle *v2193.LevelEventPacket `json:"level_event"`
}

func TestNoteEventsMatchOwnedBDS(t *testing.T) {
	replayNativeNotes(t, "bedrock_1_26_51_note_block_events.json", 112, true, false)
}
func TestNoteEffectsMatchNativePackets(t *testing.T) {
	replayNativeNotes(t, "bedrock_1_26_51_note_packets.json", 46, false, false)
}

func TestNoteLoadedStateMatchesOwnedBDS(t *testing.T) {
	replayNativeNotes(t, "bedrock_1_26_51_note_load_events.json", 12, true, true)
}

func TestNoteUnsealedWaterCommandSceneMatchesBDS(t *testing.T) {
	replayNativeNotes(t, "bedrock_1_26_51_note_water_flow.json", 2, false, false)
}

func replayNativeNotes(t *testing.T, filename string, count int, timed, load bool) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete    bool
		Checkpoints []struct {
			Tick   int64
			Events []struct {
				Position cube.Pos
				Type     int32 `json:"event_type"`
				Data     int32 `json:"event_data"`
				Caller   string
			} `json:"block_events"`
		}
		Scenarios []struct {
			Scenario struct {
				Below, Above  string
				Pitch, Phase  int
				Edges         []int
				SealedWater   bool `json:"sealed_water"`
				InitialPower  bool `json:"initial_power"`
				StoredPowered bool `json:"stored_powered"`
			}
			Base, Source cube.Pos
			SetupTick    int64 `json:"setup_tick"`
			Changes      []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick          int64
				Block, Source struct {
					ID     string
					States map[string]any
				}
			}
			Packets []nativeNotePacket
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native note corpus")
	}
	if timed {
		if len(native.Checkpoints) != 128 {
			t.Fatal("missing native note event checkpoints")
		}
		events := 0
		instruments := map[int32]bool{}
		for _, cp := range native.Checkpoints {
			for _, e := range cp.Events {
				if e.Caller != "0x65edfb8" || e.Type < 0 || e.Type > 15 || e.Data < 0 || e.Data > 24 {
					t.Fatal("unexpected native note blockEvent context")
				}
				events++
				instruments[e.Type] = true
			}
		}
		wantEvents, wantInstruments := 96, 16
		if load {
			wantEvents, wantInstruments = 12, 1
		}
		if events != wantEvents || len(instruments) != wantInstruments {
			t.Fatal("incomplete native note instrument/event controls")
		}
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/pitch%d/phase%d/stored%t/%v", c.Below, c.Above, c.Pitch, c.Phase, c.StoredPowered, c.Edges), func(t *testing.T) {
			if len(trace.Samples) != 128 || len(trace.Changes) != len(c.Edges) || trace.SetupTick%20 != 2 || trace.Changes[0].Tick%2 != int64(c.Phase) {
				t.Fatal("invalid native note controls")
			}
			for i, change := range trace.Changes {
				if change.Tick-trace.Changes[0].Tick != int64(c.Edges[i]) || change.Powered != (i%2 == 0) {
					t.Fatal("invalid native note input edges")
				}
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			settings.WeatherCycle = false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			s := testSession("native-note")
			s.br = world.DefaultBlockRegistry
			s.heldPackets = nil
			loader := world.NewLoader(2, w, &dispenserPacketViewer{session: s})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			persistedPower := false
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				lookup := func(name string) world.Block {
					rid, ok := tx.World().BlockRegistry().StateToRuntimeID("minecraft:"+name, nil)
					if !ok {
						t.Fatalf("native note input block %s missing", name)
					}
					b, ok := tx.World().BlockRegistry().BlockByRuntimeID(rid)
					if !ok {
						t.Fatal("native note input runtime ID missing")
					}
					return b
				}
				tx.SetBlock(trace.Base.Side(cube.FaceDown).Side(cube.FaceDown), block.Stone{}, nil)
				tx.SetBlock(trace.Base.Side(cube.FaceDown), lookup(c.Below), nil)
				note := block.Note{Pitch: c.Pitch}
				if load {
					if !c.InitialPower {
						t.Fatal("native loaded note input control differs")
					}
					note = note.DecodeNBT(map[string]any{"id": "Music", "note": byte(c.Pitch), "adamant:java": map[string]any{"powered": c.StoredPowered}}).(block.Note)
					_, persistedPower = note.EncodeNBT()["adamant:java"]
				}
				if c.InitialPower {
					tx.SetBlock(trace.Source, block.RedstoneBlock{}, nil)
				}
				tx.SetBlock(trace.Base, note, nil)
				if c.SealedWater {
					for _, face := range []cube.Face{cube.FaceWest, cube.FaceEast, cube.FaceNorth, cube.FaceSouth} {
						tx.SetBlock(trace.Base.Side(cube.FaceUp).Side(face), block.Stone{}, nil)
					}
					tx.SetBlock(trace.Base.Side(cube.FaceUp).Side(cube.FaceUp), block.Stone{}, nil)
				}
				if c.Above == "water" {
					// The native setup is the static-water setblock command, not
					// a bucket placement. Preserve that entry point in the replay.
					tx.SetBlock(trace.Base.Side(cube.FaceUp), block.Water{Still: true, Depth: 8}, nil)
				} else {
					tx.SetBlock(trace.Base.Side(cube.FaceUp), lookup(c.Above), nil)
				}
			})
			byTick := map[int64][]v2193.BlockEventPacket{}
			if timed {
				for _, cp := range native.Checkpoints {
					for _, e := range cp.Events {
						if e.Position == trace.Base {
							wantPitch := c.Pitch
							if load {
								wantPitch = min(wantPitch, 24)
							}
							if e.Data != int32(wantPitch) {
								t.Fatal("native note event pitch differs")
							}
							byTick[cp.Tick] = append(byTick[cp.Tick], v2193.BlockEventPacket{Position: v2193.BlockPos{int32(e.Position[0]), int32(e.Position[1]), int32(e.Position[2])}, EventType: e.Type, EventData: e.Data})
						}
					}
				}
			}
			var got []proto.Packet
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native note clock differs")
					}
					for _, b := range []struct {
						Pos    cube.Pos
						ID     string
						States map[string]any
					}{
						{trace.Base, frame.Block.ID, frame.Block.States}, {trace.Source, frame.Source.ID, frame.Source.States},
					} {
						name, states := tx.Block(b.Pos).EncodeBlock()
						if name != b.ID {
							t.Fatalf("tick %d at %v block=%s, native=%s", frame.Tick, b.Pos, name, b.ID)
						}
						for key, want := range b.States {
							var value any = states[key]
							switch v := value.(type) {
							case uint8:
								value = v != 0
							case int32:
								value = float64(v)
							}
							if value != want {
								t.Fatalf("tick %d at %v %s differs", frame.Tick, b.Pos, key)
							}
						}
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							var source world.Block
							if change.Powered {
								source = block.RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, source, nil)
						}
					}
				})
				var events []v2193.BlockEventPacket
				for len(s.heldPackets) > 0 {
					p := s.testPacket()
					got = append(got, p)
					if e, ok := p.(*v2193.BlockEventPacket); ok {
						events = append(events, *e)
					}
				}
				if timed && !reflect.DeepEqual(events, byTick[frame.Tick]) {
					t.Fatalf("tick %d note events=%v, native=%v", frame.Tick, events, byTick[frame.Tick])
				}
				if t.Failed() {
					break
				}
			}
			if load && persistedPower {
				t.Error("note persisted power absent from the native actor save")
			}
			if !timed {
				var want []proto.Packet
				for _, p := range trace.Packets {
					switch {
					case p.Event != nil:
						want = append(want, p.Event)
					case p.Sound != nil:
						want = append(want, p.Sound)
					case p.Particle != nil:
						want = append(want, p.Particle)
					default:
						t.Fatal("invalid native note packet record")
					}
				}
				if !reflect.DeepEqual(got, want) {
					g, _ := json.Marshal(got)
					v, _ := json.Marshal(want)
					t.Fatalf("note packets differ\ngot: %s\nnative: %s", g, v)
				}
			}
		})
	}
}
