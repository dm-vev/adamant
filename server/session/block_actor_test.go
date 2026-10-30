package session

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
)

// TestClientActorNBTMatchesBDS checks the block actor clients are sent: the
// NBT BDS sends, with the block's position and BlockEntityVersion, and none
// of the adamant: state the server keeps for itself.
func TestClientActorNBTMatchesBDS(t *testing.T) {
	note := block.Note{Pitch: 3, Powered: true}
	got := clientActorNBT(note.EncodeNBT(), cube.Pos{1, -2, 3})
	want := map[string]any{"id": "Music", "note": byte(3), "BlockEntityVersion": int32(0), "x": int32(1), "y": int32(-2), "z": int32(3)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("client actor = %#v, want %#v", got, want)
	}
	if _, kept := note.EncodeNBT()["adamant:java"]; kept {
		t.Fatal("note saved transient power history absent from the native Music actor")
	}
	if note.DecodeNBT(note.EncodeNBT()).(block.Note).Powered {
		t.Fatal("loaded note retained transient power history")
	}
}
