package session_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type barrierPlacementPhaseMarker struct {
	Index    int    `json:"index"`
	Tick     int64  `json:"tick"`
	GameTick int64  `json:"game_tick"`
	Stage    string `json:"stage"`
}

type barrierPlacementClockPacket struct {
	barrierPlacementWirePacket
	SubChunk *packet.UpdateSubChunkBlocks `json:"subchunk_update"`
	Sequence uint64                       `json:"packet_sequence"`
}

type barrierPlacementNativeState struct {
	ID     string
	States map[string]any
}

func placementPacketState(t *testing.T, b barrierPlacementNativeState) string {
	t.Helper()
	states := map[string]any{}
	for k, v := range b.States {
		switch n := v.(type) {
		case uint8:
			v = n != 0
		case int32:
			v = float64(n)
		}
		states[k] = v
	}
	if d, ok := states["minecraft:cardinal_direction"].(string); ok {
		if v, exists := states["direction"]; exists {
			if v != map[string]float64{"south": 0, "west": 1, "north": 2, "east": 3}[d] {
				t.Fatal("native phase direction aliases disagree")
			}
			delete(states, "direction")
		}
	}
	b.States = states
	data, _ := json.Marshal(b)
	return string(data)
}

func TestBarrierPlacementPacketsRespectNativeScriptPhase(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprintf("split%t", split), func(t *testing.T) { testBarrierPlacementPackets(t, true, split, true) })
	}
}

func validatePlacementPhaseClock(t *testing.T, transcript []barrierPlacementClockPacket) {
	t.Helper()
	last, markers := uint64(0), 0
	for _, p := range transcript {
		if p.Sequence <= last {
			t.Fatal("native placement clock sequence is not monotonic")
		}
		last = p.Sequence
		if p.Clock != nil {
			markers++
		}
	}
	if markers != 2126 {
		t.Fatal("native placement marker stream has a missing tail")
	}
}

func scopedPlacementPhasePackets(transcript []barrierPlacementClockPacket, index int, setup int64, positions []cube.Pos) []packet.Packet {
	inFocus := func(pos cube.Pos) bool {
		for _, cell := range positions {
			if pos == cell {
				return true
			}
		}
		return false
	}
	var want []packet.Packet
	active := false
	for _, p := range transcript {
		if p.Clock != nil {
			active = p.Clock.Index == index && p.Clock.Tick > setup+12
			if active {
				data, _ := json.Marshal(p.Clock)
				want = append(want, &packet.Text{Message: string(data)})
			}
			continue
		}
		if !active {
			continue
		}
		if p.Update != nil && p.Update.Layer == 0 && inFocus(cube.Pos{int(p.Update.Position[0]), int(p.Update.Position[1]), int(p.Update.Position[2])}) {
			want = append(want, p.Update)
		}
		if p.Sound != nil && inFocus(cube.PosFromVec3(mgl64.Vec3{float64(p.Sound.Position[0]), float64(p.Sound.Position[1]), float64(p.Sound.Position[2])})) {
			want = append(want, p.Sound)
		}
		if p.SubChunk != nil {
			for _, u := range p.SubChunk.Blocks {
				if inFocus(cube.Pos{int(u.BlockPos[0]), int(u.BlockPos[1]), int(u.BlockPos[2])}) {
					want = append(want, &packet.UpdateBlock{Position: u.BlockPos, NewBlockRuntimeID: u.BlockRuntimeID, Flags: u.Flags})
				}
			}
		}
	}
	return want
}
