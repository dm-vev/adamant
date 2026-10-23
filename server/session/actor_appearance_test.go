package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The expectations below are what BDS 1.26.52.3 sends in AddActor for mobs
// summoned with /summon, or with the minecraft:entity_born event for babies.

func TestActorMetadataMatchesBDS(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()
	viewer := testSession("actor-metadata")
	viewer.br = world.DefaultBlockRegistry

	cases := []struct {
		name  string
		nbt   map[string]any
		want  map[uint32]any
		flags []uint8
		props []int32
	}{
		{name: "baby pig", nbt: map[string]any{"id": "minecraft:pig", "Age": int32(-24000), "variant": "minecraft:warm", "sound_variant": "minecraft:big"},
			want:  map[uint32]any{protocol.EntityDataKeyWidth: float32(0.9), protocol.EntityDataKeyHeight: float32(0.9), protocol.EntityDataKeyScale: float32(0.5), protocol.EntityDataKeyVariant: int32(0)},
			flags: []uint8{protocol.EntityDataFlagBaby, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagWalk}, props: []int32{1, 1}},
		{name: "baby zombie", nbt: map[string]any{"id": "minecraft:zombie", "IsBaby": byte(1)},
			want: map[uint32]any{protocol.EntityDataKeyWidth: float32(0.98), protocol.EntityDataKeyHeight: float32(1.96), protocol.EntityDataKeyScale: float32(0.5)}},
		{name: "wither skeleton", nbt: map[string]any{"id": "minecraft:wither_skeleton"},
			want:  map[uint32]any{protocol.EntityDataKeyWidth: float32(0.72), protocol.EntityDataKeyHeight: float32(2.01), protocol.EntityDataKeyScale: float32(1.2), protocol.EntityDataKeyVariant: int32(1)},
			flags: []uint8{protocol.EntityDataFlagFireImmune}},
		{name: "elder guardian", nbt: map[string]any{"id": "minecraft:elder_guardian"}, flags: []uint8{protocol.EntityDataFlagElder}},
		{name: "boat", nbt: map[string]any{"id": "minecraft:boat"}, flags: []uint8{protocol.EntityDataFlagStackable, protocol.EntityDataFlagCollidable},
			want: map[uint32]any{protocol.EntityDataKeyStructuralIntegrity: int32(0), protocol.EntityDataKeyControllingSeatIndex: byte(0)}},
		{name: "armor stand", nbt: map[string]any{"id": "minecraft:armor_stand"}, want: map[uint32]any{protocol.EntityDataKeyHurt: int32(0)},
			flags: []uint8{protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHiddenWhenInvisible}},
		{name: "tabby cat", nbt: map[string]any{"id": "minecraft:cat", "variant": "minecraft:tabby", "Tame": byte(1), "Sitting": byte(1), "CollarColor": byte(11)},
			want:  map[uint32]any{protocol.EntityDataKeyVariant: int32(8), protocol.EntityDataKeyColorIndex: byte(11), protocol.EntityDataKeyScale: float32(0.8)},
			flags: []uint8{protocol.EntityDataFlagTamed, protocol.EntityDataFlagSitting}, props: []int32{0}},
		{name: "striped wolf", nbt: map[string]any{"id": "minecraft:wolf", "variant": "minecraft:striped", "sound_variant": "minecraft:angry"},
			want: map[uint32]any{protocol.EntityDataKeyVariant: int32(7)}, props: []int32{4}},
		{name: "librarian", nbt: map[string]any{"id": "minecraft:villager", "VillagerData": map[string]any{"profession": "minecraft:librarian", "type": "minecraft:desert", "level": int32(3)}},
			want: map[uint32]any{protocol.EntityDataKeyVariant: int32(5), protocol.EntityDataKeyMarkVariant: int32(1), protocol.EntityDataKeyTradeTier: int32(2)}},
		{name: "large slime", nbt: map[string]any{"id": "minecraft:slime", "Size": int32(3)},
			want: map[uint32]any{protocol.EntityDataKeyVariant: int32(4), protocol.EntityDataKeyWidth: float32(2.08), protocol.EntityDataKeyHeight: float32(2.08)}},
		{name: "medium magma cube", nbt: map[string]any{"id": "minecraft:magma_cube", "Size": int32(1)},
			want: map[uint32]any{protocol.EntityDataKeyVariant: int32(2), protocol.EntityDataKeyWidth: float32(1.04), protocol.EntityDataKeyHeight: float32(1.02)}},
		{name: "clownfish", nbt: map[string]any{"id": "minecraft:tropical_fish", "Variant": int32(1 << 16)},
			want: map[uint32]any{protocol.EntityDataKeyVariant: int32(0), protocol.EntityDataKeyMarkVariant: int32(0), protocol.EntityDataKeyColorIndex: byte(1), protocol.EntityDataKeyColorTwoIndex: byte(0), protocol.EntityDataKeyScale: float32(1.3)}},
		{name: "wild axolotl", nbt: map[string]any{"id": "minecraft:axolotl", "Variant": int32(1)},
			want: map[uint32]any{protocol.EntityDataKeyVariant: int32(3)}},
		{name: "large salmon", nbt: map[string]any{"id": "minecraft:salmon", "Variant": int32(2)},
			want: map[uint32]any{protocol.EntityDataKeyVariant: int32(0), protocol.EntityDataKeyScale: float32(1.5)}},
		{name: "rabbit", nbt: map[string]any{"id": "minecraft:rabbit", "RabbitType": int32(4)},
			want: map[uint32]any{protocol.EntityDataKeyVariant: int32(4), protocol.EntityDataKeyWidth: float32(0.81666666), protocol.EntityDataKeyScale: float32(0.6)}},
		{name: "trader llama", nbt: map[string]any{"id": "minecraft:trader_llama"}, want: map[uint32]any{protocol.EntityDataKeyMarkVariant: int32(1)}},
		{name: "cold chicken", nbt: map[string]any{"id": "minecraft:chicken", "variant": "minecraft:cold"}, props: []int32{2, 0}},
		{name: "tadpole", nbt: map[string]any{"id": "minecraft:tadpole"}, flags: []uint8{protocol.EntityDataFlagBaby}},
		{name: "happy ghast", nbt: map[string]any{"id": "minecraft:happy_ghast"}, props: []int32{1}},
	}
	<-w.Exec(func(tx *world.Tx) {
		for _, tc := range cases {
			handle, err := entity.NewFromStructureNBT(tc.nbt, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}})
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			e := tx.AddEntity(handle)
			m := viewer.parseEntityMetadata(e)
			for key, want := range tc.want {
				if got := m[key]; got != want {
					t.Errorf("%s: metadata %d = %v (%T), want %v (%T)", tc.name, key, got, got, want, want)
				}
			}
			for _, flag := range tc.flags {
				key := uint32(protocol.EntityDataKeyFlags)
				if flag >= 64 {
					key, flag = protocol.EntityDataKeyFlagsTwo, flag-64
				}
				if !m.Flag(key, flag) {
					t.Errorf("%s: flag %d not set", tc.name, flag)
				}
			}
			if tc.props != nil {
				props := entityProperties(e).IntegerProperties
				if len(props) != len(tc.props) {
					t.Fatalf("%s: properties %v, want values %v", tc.name, props, tc.props)
				}
				for i, want := range tc.props {
					if props[i].Index != uint32(i) || props[i].Value != want {
						t.Errorf("%s: property %d = %+v, want %d", tc.name, i, props[i], want)
					}
				}
			}
		}
	})
}

func TestEndermanCarriesBlockOnClient(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()
	viewer := testSession("enderman-metadata")
	viewer.br = world.DefaultBlockRegistry
	<-w.Exec(func(tx *world.Tx) {
		e := tx.AddEntity(entity.NewEnderman(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}})).(*entity.Enderman)
		if got, want := viewer.parseEntityMetadata(e)[protocol.EntityDataKeyCarryBlockRuntimeID], int32(networkBlockID(viewer.br, block.Air{})); got != want {
			t.Fatalf("empty-handed enderman carries %v, want air %v", got, want)
		}
		e.CarryBlock(block.Grass{})
		if got, want := viewer.parseEntityMetadata(e)[protocol.EntityDataKeyCarryBlockRuntimeID], int32(networkBlockID(viewer.br, block.Grass{})); got != want {
			t.Fatalf("enderman carries %v, want grass %v", got, want)
		}
	})
}
