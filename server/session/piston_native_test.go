package session

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type pistonPacketViewer struct {
	world.NopViewer
	session *Session
}

func (v *pistonPacketViewer) ViewBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	v.session.ViewBlockUpdate(pos, b, layer)
}
func (v *pistonPacketViewer) ViewSound(pos mgl64.Vec3, s world.Sound) { v.session.ViewSound(pos, s) }
func (v *pistonPacketViewer) ViewBlockAction(pos cube.Pos, a world.BlockAction) {
	v.session.ViewBlockAction(pos, a)
}

// Compare the real session publication path against packets received from BDS.
// Both block actor sequences and complete piston sound fields come from BDS.
func TestPistonUpdatesMatchNativePackets(t *testing.T) {
	for _, c := range []struct {
		name, file string
		actors     int
	}{
		{"single", "bedrock_1_26_51_piston_packets.json", 32},
		{"chain", "bedrock_1_26_51_piston_packets_chain.json", 48},
	} {
		t.Run(c.name, func(t *testing.T) { testPistonNativePackets(t, c.file, c.actors) })
	}
}

func testPistonNativePackets(t *testing.T, file string, actorCount int) {
	data, err := os.ReadFile("../block/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Sticky, Blocked bool
				Phase           int
				PayloadCount    int `json:"payload_count"`
			}
			Base, Source cube.Pos
			Front        []cube.Pos
			SetupTick    int64 `json:"setup_tick"`
			Changes      []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct{ Tick int64 }
		}
		Sounds      []map[string]any
		BlockActors []struct {
			Position cube.Pos
			NBTData  map[string]any
		} `json:"block_actors"`
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 8 || len(native.Sounds) != 8 || len(native.BlockActors) != actorCount {
		t.Fatal("incomplete native piston packet capture")
	}
	settings := (world.NopProvider{}).Settings()
	settings.CurrentTick = native.Scenarios[0].SetupTick - 1
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
	s := testSession("piston-packets")
	s.br = world.DefaultBlockRegistry
	s.packets = make(chan packet.Packet, 1024)
	l := world.NewLoader(2, w, &pistonPacketViewer{session: s})
	t.Cleanup(func() { <-w.Exec(func(tx *world.Tx) { l.Close(tx) }); _ = w.Close() })
	gotActors := map[cube.Pos][]map[string]any{}
	gotActorTicks := map[cube.Pos][]int{}
	gotSounds := map[cube.Pos][]map[string]any{}
	tick := -1
	collect := func() {
		s.flushBlockUpdates()
		for len(s.packets) > 0 {
			switch pk := (<-s.packets).(type) {
			case *packet.BlockActorData:
				if pk.NBTData["id"] != "PistonArm" && pk.NBTData["id"] != "MovingBlock" {
					continue
				}
				wire, err := nbt.MarshalEncoding(pk.NBTData, nbt.NetworkLittleEndian)
				if err != nil {
					t.Fatal(err)
				}
				var decoded map[string]any
				if err := nbt.UnmarshalEncoding(wire, &decoded, nbt.NetworkLittleEndian); err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(decoded)
				if err != nil {
					t.Fatal(err)
				}
				var normalized map[string]any
				if err := json.Unmarshal(encoded, &normalized); err != nil {
					t.Fatal(err)
				}
				pos := cube.Pos{int(pk.Position[0]), int(pk.Position[1]), int(pk.Position[2])}
				gotActors[pos] = append(gotActors[pos], normalized)
				gotActorTicks[pos] = append(gotActorTicks[pos], tick)
			case *packet.LevelSoundEvent:
				encoded, err := json.Marshal(pk)
				if err != nil {
					t.Fatal(err)
				}
				var normalized map[string]any
				if err := json.Unmarshal(encoded, &normalized); err != nil {
					t.Fatal(err)
				}
				pos := cube.Pos{int(pk.Position[0]), int(pk.Position[1]), int(pk.Position[2])}
				gotSounds[pos] = append(gotSounds[pos], normalized)
			}
		}
	}
	<-w.Exec(func(tx *world.Tx) {
		l.Move(tx, mgl64.Vec3{8, 65, 8})
		l.Load(tx, 25)
		for _, c := range native.Scenarios {
			count := max(1, c.Scenario.PayloadCount)
			if c.SetupTick != native.Scenarios[0].SetupTick || len(c.Samples) != 100 || len(c.Changes) != 2 || len(c.Front) != count+2 {
				t.Fatal("invalid native packet scenario")
			}
			tx.SetBlock(c.Base, block.Piston{Facing: cube.FaceEast, Sticky: c.Scenario.Sticky}, nil)
			var b world.Block = block.Stone{}
			if c.Scenario.Blocked {
				b = block.Obsidian{}
				count = 1
			}
			for _, pos := range c.Front[:count] {
				tx.SetBlock(pos, b, nil)
			}
		}
	})
	collect()
	for i := 0; i < 100; i++ {
		tick = i
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			for _, c := range native.Scenarios {
				if c.Samples[i].Tick != tx.CurrentTick() {
					t.Fatal("native packet replay clock mismatch")
				}
				for _, change := range c.Changes {
					if change.Tick == tx.CurrentTick() {
						var b world.Block = block.Air{}
						if change.Powered {
							b = block.RedstoneBlock{}
						}
						tx.SetBlock(c.Source, b, nil)
					}
				}
			}
		})
		collect()
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("sticky%t/blocked%t/phase%d", c.Scenario.Sticky, c.Scenario.Blocked, c.Scenario.Phase), func(t *testing.T) {
			var expectedActors []map[string]any
			for _, pk := range native.BlockActors {
				if pk.Position == c.Base && pk.NBTData["id"] == "PistonArm" {
					expectedActors = append(expectedActors, pk.NBTData)
				}
			}
			if !reflect.DeepEqual(gotActors[c.Base], expectedActors) {
				t.Errorf("PistonArm updates=%v, native %v", gotActors[c.Base], expectedActors)
			}
			for _, pos := range c.Front {
				var expected []map[string]any
				for _, pk := range native.BlockActors {
					if pk.Position == pos && pk.NBTData["id"] == "MovingBlock" {
						expected = append(expected, pk.NBTData)
					}
				}
				if !reflect.DeepEqual(gotActors[pos], expected) {
					t.Errorf("MovingBlock %v updates=%v, native %v", pos, gotActors[pos], expected)
				}
				for j, actor := range gotActors[pos] {
					state := float64(1)
					if actor["expanding"] == float64(0) {
						state = 3
					}
					for k, parent := range gotActors[c.Base] {
						if parent["State"] == state && gotActorTicks[pos][j] != gotActorTicks[c.Base][k]+1 {
							t.Errorf("MovingBlock publication tick=%d, parent start=%d; native first tick is one GT later", gotActorTicks[pos][j], gotActorTicks[c.Base][k])
						}
					}
				}
			}
			var expectedSounds []map[string]any
			for _, pk := range native.Sounds {
				pos := pk["Position"].([]any)
				if pos[0] == float64(c.Base[0]) && pos[1] == float64(c.Base[1]) && pos[2] == float64(c.Base[2]) {
					expectedSounds = append(expectedSounds, pk)
				}
			}
			if !reflect.DeepEqual(gotSounds[c.Base], expectedSounds) {
				t.Errorf("sound packets=%v, native %v", gotSounds[c.Base], expectedSounds)
			}
		})
	}
}
