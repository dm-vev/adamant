package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestSkullRetainsCapturedNativeAnimation(t *testing.T) {
	// BDS 1.26.52.3 build51798919: a dragon head beside a redstone block,
	// and an unpowered control, saved from a ticking area without clients.
	for _, tc := range []struct {
		name    string
		powered byte
		ticks   int32
	}{
		{"powered", 1, 115}, {"unpowered", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := map[string]any{"id": "Skull", "SkullType": byte(255), "Rotation": float32(0), "DoingAnimation": tc.powered, "MouthTickCount": tc.ticks, "BlockEntityVersion": int32(0)}
			restored := (Skull{Type: DragonHead()}).DecodeNBT(captured).(Skull)
			if restored.Powered != (tc.powered != 0) {
				t.Fatal("native DoingAnimation did not restore powered state")
			}
			saved := restored.EncodeNBT()
			if saved["DoingAnimation"] != tc.powered || saved["MouthTickCount"] != tc.ticks {
				t.Fatalf("native animation fields lost on save: %#v", saved)
			}
			if saved["MouthMoving"] != tc.powered {
				t.Fatalf("native MouthMoving field missing or incorrect: %#v", saved["MouthMoving"])
			}
		})
	}
}

func TestSkullRedstoneAnimationTick(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()

	pos := cube.Pos{0, 0, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos, Skull{Type: DragonHead()}, nil)
		tx.SetBlock(pos.Side(cube.FaceEast), RedstoneBlock{}, nil)
		skull := tx.Block(pos).(Skull)
		skull.Tick(0, pos, tx)
		got := tx.Block(pos).(Skull)
		if !got.Powered || got.AnimationTicks != 1 {
			t.Fatalf("powered dragon head = %#v, want powered with one animation tick", got)
		}
		tx.SetBlock(pos.Side(cube.FaceEast), Air{}, nil)
		got.Tick(1, pos, tx)
		got = tx.Block(pos).(Skull)
		if got.Powered || got.AnimationTicks != 1 {
			t.Fatalf("unpowered dragon head = %#v, want stopped at one animation tick", got)
		}
		if nbt := got.EncodeNBT(); nbt["MouthMoving"] != byte(0) {
			t.Fatalf("unpowered MouthMoving = %#v, want 0", nbt["MouthMoving"])
		}
	})
}
