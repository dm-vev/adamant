package structure

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

func TestStructureActorSourceBytesSurviveNativeSerialization(t *testing.T) {
	state := world.BlockState{Name: "minecraft:structure_block", Properties: map[string]any{"mode": "data"}}
	payload, err := BlockEntityPayload(state, [3]int32{257, 70, -257}, map[string]any{
		"id": "minecraft:structure_block", "author": "source-author", "strict": int8(1), "showair": int8(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := world.DefaultBlockRegistry
	registry.Finalize()
	placement := Block{State: state}
	native, err := placement.ResolveNative(registry)
	if err != nil {
		t.Fatal(err)
	}
	base, ok := registry.BlockByRuntimeID(native.Block)
	if !ok {
		t.Fatal("resolved structure actor has no native block")
	}
	actor := world.DecodeNBT(base.(world.NBTer), payload, registry).(world.NBTer)
	encoded, err := nbt.MarshalEncoding(actor.EncodeNBT(), nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := nbt.UnmarshalEncoding(encoded, &persisted, nbt.LittleEndian); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"author": "source-author", "strict": uint8(1), "showair": uint8(0)}
	if got := persisted["adamant:java"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("source byte metadata corrupted native actor stream: got=%v want=%v", got, want)
	}
	// The chunk encoder writes the position of every block entity.
	delete(payload, "x")
	delete(payload, "y")
	delete(payload, "z")
	if !reflect.DeepEqual(persisted, payload) {
		t.Fatalf("native actor retained transient decoder data or lost fields: got=%v want=%v", persisted, payload)
	}
}
