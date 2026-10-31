package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type bellSupportNativeBlock struct {
	ID     string
	States map[string]any
}

func bellSupportInputBlock(t *testing.T, tx *world.Tx, b bellSupportNativeBlock) world.Block {
	t.Helper()
	switch b.ID {
	case "minecraft:air":
		return block.Air{}
	case "minecraft:stone":
		return block.Stone{}
	case "minecraft:glass":
		return block.Glass{}
	case "minecraft:bell":
		properties := map[string]any{"attachment": b.States["attachment"], "direction": int32(b.States["direction"].(float64)), "toggle_bit": uint8(0)}
		if b.States["toggle_bit"].(bool) {
			properties["toggle_bit"] = uint8(1)
		}
		if bell, ok := tx.World().BlockRegistry().BlockByName(b.ID, properties); ok {
			return bell
		}
	}
	t.Fatalf("unknown native bell support input: %+v", b)
	return nil
}

func compareBellSupportBlock(t *testing.T, tx *world.Tx, tick int64, pos cube.Pos, want bellSupportNativeBlock) {
	t.Helper()
	name, states := tx.Block(pos).EncodeBlock()
	got := map[string]any{}
	for k, v := range states {
		switch value := v.(type) {
		case uint8:
			v = value != 0
		case int32:
			v = float64(value)
		}
		got[k] = v
	}
	if name == "minecraft:stone" {
		got["stone_type"] = "stone"
	}
	g, _ := json.Marshal(got)
	v, _ := json.Marshal(want.States)
	if name != want.ID || !bytes.Equal(g, v) {
		t.Fatalf("tick %d at %v state=%s/%s, native=%s/%s", tick, pos, name, g, want.ID, v)
	}
}

func TestBellSupportStatesMatchBDS(t *testing.T) { testBellSupportsNative(t, false, false, false) }

func testBellSupportsNative(t *testing.T, packets, split, backlog bool) {
	t.Helper()
	file := "bedrock_1_26_51_bell_supports.json"
	if packets {
		file = "bedrock_1_26_51_bell_support_packets.json"
	}
	data, err := os.ReadFile("../block/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Attachment, Action string
				Direction, Phase   int
			}
			Base      cube.Pos
			SetupTick int64 `json:"setup_tick"`
			Changes   []struct {
				Tick   int64
				Writes []struct {
					Position      cube.Pos
					Before, After bellSupportNativeBlock
				}
			}
			Samples []struct {
				Tick  int64
				Cells []struct {
					Position cube.Pos
					Block    bellSupportNativeBlock
				}
			}
			Packets []struct {
				Update *packet.UpdateBlock    `json:"block_update"`
				Actor  *packet.BlockActorData `json:"block_actor"`
				Sound  *packet.LevelSoundEvent
				Event  *packet.LevelEvent `json:"level_event"`
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 128 {
		t.Fatal("incomplete native bell support corpus")
	}
	packetCount := 0
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		packetCount += len(trace.Packets)
		t.Run(fmt.Sprintf("%s/%s/direction%d/phase%d", c.Attachment, c.Action, c.Direction, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 64 || len(trace.Changes) != 1 || trace.Changes[0].Tick%2 != int64(c.Phase) {
				t.Fatal("invalid native bell support controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = trace.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			s := testSession("native-bell-support")
			s.br = world.DefaultBlockRegistry
			s.packets = make(chan packet.Packet, 256)
			loader := world.NewLoader(2, w, &bellPacketViewer{session: s, split: split})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			_ = w.SetGameRule("block_drops", false)
			var got []packet.Packet
			position := protocol.BlockPos{int32(trace.Base[0]), int32(trace.Base[1]), int32(trace.Base[2])}
			drain := func() {
				s.flushBlockUpdates()
				for len(s.packets) > 0 {
					p := <-s.packets
					switch v := p.(type) {
					case *packet.UpdateBlock:
						if v.Position == position && v.Layer == 0 {
							got = append(got, p)
						}
					case *packet.BlockActorData:
						if v.Position == position {
							got = append(got, p)
						}
					case *packet.LevelSoundEvent:
						if v.Position == vec64To32(trace.Base.Vec3()) {
							got = append(got, p)
						}
					case *packet.LevelEvent:
						if v.Position == vec64To32(trace.Base.Vec3()) {
							got = append(got, p)
						}
					}
				}
			}
			collect := func() {
				for rounds := 0; ; rounds++ {
					drain()
					if len(s.blockUpdates) == 0 {
						return
					}
					if rounds >= 8 {
						t.Fatal("bell notifications did not drain through the block backlog")
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				if len(trace.Samples[0].Cells) != 6 {
					t.Fatal("missing native bell support cells")
				}
				for _, cell := range trace.Samples[0].Cells {
					if cell.Position != trace.Base {
						tx.SetBlock(cell.Position, bellSupportInputBlock(t, tx, cell.Block), nil)
					}
				}
				tx.SetBlock(trace.Base, bellSupportInputBlock(t, tx, trace.Samples[0].Cells[0].Block), nil)
			})
			collect()
			for _, f := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick || len(f.Cells) != 6 {
						t.Fatal("native bell support clock/cell count differs")
					}
					for _, cell := range f.Cells {
						compareBellSupportBlock(t, tx, f.Tick, cell.Position, cell.Block)
					}
					for _, change := range trace.Changes {
						if change.Tick == f.Tick {
							if backlog {
								for i := 0; i < 400; i++ {
									s.enqueueBlockUpdate(cube.Pos{i, 80, 0}, block.Stone{}, 0)
								}
							}
							for _, write := range change.Writes {
								compareBellSupportBlock(t, tx, f.Tick, write.Position, write.Before)
								tx.SetBlock(write.Position, bellSupportInputBlock(t, tx, write.After), nil)
								compareBellSupportBlock(t, tx, f.Tick, write.Position, write.After)
							}
						}
					}
				})
				collect()
				if t.Failed() {
					break
				}
			}
			if packets {
				var want []packet.Packet
				for _, p := range trace.Packets {
					switch {
					case p.Update != nil:
						want = append(want, p.Update)
					case p.Actor != nil:
						want = append(want, p.Actor)
					case p.Sound != nil:
						want = append(want, p.Sound)
					case p.Event != nil:
						want = append(want, p.Event)
					default:
						t.Fatal("unknown native bell support packet")
					}
				}
				g, _ := json.Marshal(got)
				v, _ := json.Marshal(want)
				if !bytes.Equal(g, v) {
					t.Fatalf("native bell support packets differ\ngot: %s\nnative: %s", g, v)
				}
			}
		})
	}
	if packets && packetCount != 416 {
		t.Fatal("native bell support packet count changed")
	}
}
