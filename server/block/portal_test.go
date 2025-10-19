package block

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestPortalRuntimeIDs(t *testing.T) {
	tests := []struct {
		axis cube.Axis
		prop string
	}{
		{cube.X, "x"},
		{cube.Z, "z"},
	}
	for _, tt := range tests {
		t.Run(tt.prop, func(t *testing.T) {
			p := Portal{Axis: tt.axis}
			rid := world.BlockRuntimeID(p)

			b, ok := world.BlockByName("minecraft:portal", map[string]any{"portal_axis": tt.prop})
			if !ok {
				t.Fatal("portal block not registered")
			}
			if reflect.TypeOf(b) != reflect.TypeOf(p) {
				t.Fatalf("BlockByName returned %T, want %T", b, p)
			}
			if got := world.BlockRuntimeID(b); got != rid {
				t.Fatalf("portal %s runtime ID = %d, want %d", tt.prop, rid, got)
			}
			byRID, ok := world.BlockByRuntimeID(rid)
			if !ok {
				t.Fatalf("BlockByRuntimeID(%d) not found", rid)
			}
			if reflect.TypeOf(byRID) != reflect.TypeOf(p) {
				t.Fatalf("BlockByRuntimeID returned %T, want %T", byRID, p)
			}
			name, props, found := chunk.RuntimeIDToState(rid)
			if !found {
				t.Fatalf("chunk.RuntimeIDToState(%d) not found", rid)
			}
			if name != "minecraft:portal" || props["portal_axis"] != tt.prop {
				t.Fatalf("RuntimeIDToState(%d) = %s %+v", rid, name, props)
			}
			entry := chunk.BlockPaletteEncoding.EncodeBlockState(rid)
			if entry.Name != "minecraft:portal" || entry.State["portal_axis"] != tt.prop {
				t.Fatalf("EncodeBlockState(%d) = %s %+v", rid, entry.Name, entry.State)
			}
		})
	}
}
