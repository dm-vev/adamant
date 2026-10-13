package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestVineStatesDecodeToVines(t *testing.T) {
	for bits := int32(0); bits < 16; bits++ {
		b, ok := world.BlockByName("minecraft:vine", map[string]any{"vine_direction_bits": bits})
		if !ok {
			t.Fatalf("vine_direction_bits=%d is not registered", bits)
		}
		v, ok := b.(Vines)
		if !ok {
			t.Fatalf("vine_direction_bits=%d decoded to %T", bits, b)
		}
		if _, props := v.EncodeBlock(); props["vine_direction_bits"] != bits {
			t.Fatalf("vine_direction_bits=%d re-encoded as %v", bits, props["vine_direction_bits"])
		}
	}
}
