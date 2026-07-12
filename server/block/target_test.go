package block

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTargetSignal(t *testing.T) {
	pos := cube.Pos{2, 3, 4}
	tests := []struct {
		name string
		hit  mgl64.Vec3
		face cube.Face
		want uint8
	}{
		{"centre top", mgl64.Vec3{2.5, 4, 4.5}, cube.FaceUp, 15},
		{"midway top", mgl64.Vec3{2.75, 4, 4.5}, cube.FaceUp, 8},
		{"edge top", mgl64.Vec3{3, 4, 4.5}, cube.FaceUp, 1},
		{"centre east", mgl64.Vec3{3, 3.5, 4.5}, cube.FaceEast, 15},
		{"edge north", mgl64.Vec3{3, 4, 4}, cube.FaceNorth, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := targetSignal(pos, test.hit, test.face); got != test.want {
				t.Fatalf("targetSignal() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestTargetNBT(t *testing.T) {
	target := Target{Signal: 12}
	encoded := target.EncodeNBT()
	if encoded["id"] != "Target" || encoded["OutputSignal"] != int32(12) {
		t.Fatalf("EncodeNBT() = %#v", encoded)
	}
	decoded := (Target{}).DecodeNBT(encoded).(Target)
	if decoded.Signal != 12 {
		t.Fatalf("DecodeNBT().Signal = %d, want 12", decoded.Signal)
	}
	if got := (Target{}).DecodeNBT(map[string]any{"OutputSignal": int32(20)}).(Target).Signal; got != 15 {
		t.Fatalf("DecodeNBT() did not clamp signal: got %d, want 15", got)
	}
}

func TestTargetResetDelay(t *testing.T) {
	for identifier, want := range map[string]time.Duration{
		"minecraft:arrow":          time.Second,
		"minecraft:thrown_trident": time.Second,
		"minecraft:snowball":       400 * time.Millisecond,
	} {
		if got := targetResetDelay(identifier); got != want {
			t.Errorf("targetResetDelay(%q) = %s, want %s", identifier, got, want)
		}
	}
}

func TestTargetInterfacesAndEncoding(t *testing.T) {
	var target any = Target{Signal: 15}
	if _, ok := target.(world.NBTer); !ok {
		t.Fatal("Target does not implement world.NBTer")
	}
	if _, ok := target.(world.RedstonePowerSource); !ok {
		t.Fatal("Target does not implement world.RedstonePowerSource")
	}
	if _, ok := target.(world.ScheduledTicker); !ok {
		t.Fatal("Target does not implement world.ScheduledTicker")
	}
	name, states := target.(Target).EncodeBlock()
	if name != "minecraft:target" || states != nil {
		t.Fatalf("EncodeBlock() = %q, %#v", name, states)
	}
	source := target.(world.RedstonePowerSource)
	if got := source.RedstonePower(cube.Pos{}, nil, cube.FaceUp); got != 15 {
		t.Fatalf("RedstonePower() = %d, want 15", got)
	}
}
