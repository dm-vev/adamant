package session

import (
	"context"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMountMetadataUnchanged holds the actor data of mounts to the values
// the session sent when MountState held Bedrock's horse flags and strength
// modifier, frozen from the commit before.
func TestMountMetadataUnchanged(t *testing.T) {
	tamedFlag := int64(1) << mc.EntityDataFlagTamed
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true}.New()
	defer w.Close()
	for _, c := range []struct {
		name string
		nbt  map[string]any
		want mc.Metadata
	}{
		{"tamed llama", map[string]any{"id": "minecraft:llama", "Tame": byte(1), "Strength": int32(3)},
			mc.Metadata{0x0: tamedFlag, 0x1a: byte(0), 0x2c: byte(0xc), 0x2d: int32(16), 0x2e: int32(3), 0x4b: int32(3), 0x4c: int32(5), 0x5c: int64(0)}},
		{"tamed trader llama", map[string]any{"id": "minecraft:trader_llama", "Tame": byte(1), "Strength": int32(2)},
			mc.Metadata{0x0: tamedFlag, 0x1a: byte(0), 0x2c: byte(0xc), 0x2d: int32(16), 0x2e: int32(3), 0x4b: int32(2), 0x4c: int32(5), 0x5c: int64(0)}},
		{"wild llama", map[string]any{"id": "minecraft:llama", "Strength": int32(4)},
			mc.Metadata{0x0: int64(0), 0x1a: byte(0), 0x4b: int32(4), 0x4c: int32(5), 0x5c: int64(0)}},
		{"eating horse", map[string]any{"id": "minecraft:horse", "EatingHaystack": byte(1)},
			mc.Metadata{0x0: int64(0), 0x10: int64(32), 0x1a: byte(0), 0x5c: int64(0)}},
		{"tamed saddled horse", map[string]any{"id": "minecraft:horse", "Tame": byte(1), "Saddled": byte(1)},
			mc.Metadata{0x0: int64(52776826568960), 0x10: int64(0), 0x1a: byte(0), 0x2c: byte(0xc), 0x2d: int32(2), 0x2e: int32(0), 0x5c: int64(0)}},
		{"tamed camel", map[string]any{"id": "minecraft:camel", "Tame": byte(1)},
			mc.Metadata{0x0: tamedFlag, 0x1a: byte(0), 0x2c: byte(0xc), 0x2d: int32(5), 0x2e: int32(0), 0x5c: int64(0)}},
	} {
		c.nbt["Health"] = float32(20)
		h, err := entity.NewFromStructureNBT(c.nbt, world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 65, 8.5}, SpawnReason: "structure", SpawnFinalized: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Do(func(tx *world.Tx) {
			m := mc.NewMetadata()
			addMountMetadata(tx.AddEntity(h).(interface{ MountState() entity.MountState }).MountState(), m)
			if !reflect.DeepEqual(m, c.want) {
				t.Errorf("%s: actor data %#v, want %#v", c.name, m, c.want)
			}
		}).Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	// A horse that eats and stands sends both bits: 32 and 128.
	m := mc.NewMetadata()
	addMountMetadata(entity.MountState{Rearing: true, Eating: true, Standing: true}, m)
	if got := m[mc.EntityDataKeyHorseFlags]; got != int64(160) {
		t.Errorf("horse flags of an eating, standing horse = %v, want 160", got)
	}
}
