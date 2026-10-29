package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
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
			want:  map[uint32]any{v2193.EntityDataKeyWidth: float32(0.9), v2193.EntityDataKeyHeight: float32(0.9), v2193.EntityDataKeyScale: float32(0.5), v2193.EntityDataKeyVariant: int32(0)},
			flags: []uint8{v2193.EntityDataFlagBaby, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagWalk}, props: []int32{1, 1}},
		{name: "baby zombie", nbt: map[string]any{"id": "minecraft:zombie", "IsBaby": byte(1)},
			want: map[uint32]any{v2193.EntityDataKeyWidth: float32(0.98), v2193.EntityDataKeyHeight: float32(1.96), v2193.EntityDataKeyScale: float32(0.5)}},
		{name: "wither skeleton", nbt: map[string]any{"id": "minecraft:wither_skeleton"},
			want:  map[uint32]any{v2193.EntityDataKeyWidth: float32(0.72), v2193.EntityDataKeyHeight: float32(2.01), v2193.EntityDataKeyScale: float32(1.2), v2193.EntityDataKeyVariant: int32(1)},
			flags: []uint8{v2193.EntityDataFlagFireImmune}},
		{name: "elder guardian", nbt: map[string]any{"id": "minecraft:elder_guardian"}, flags: []uint8{v2193.EntityDataFlagElder}},
		{name: "boat", nbt: map[string]any{"id": "minecraft:boat"}, flags: []uint8{v2193.EntityDataFlagStackable, v2193.EntityDataFlagCollidable},
			want: map[uint32]any{v2193.EntityDataKeyStructuralIntegrity: int32(0), v2193.EntityDataKeyControllingSeatIndex: byte(0)}},
		{name: "armor stand", nbt: map[string]any{"id": "minecraft:armor_stand"}, want: map[uint32]any{v2193.EntityDataKeyHurt: int32(0)},
			flags: []uint8{v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHiddenWhenInvisible}},
		{name: "tabby cat", nbt: map[string]any{"id": "minecraft:cat", "variant": "minecraft:tabby", "Tame": byte(1), "Sitting": byte(1), "CollarColor": byte(11)},
			want:  map[uint32]any{v2193.EntityDataKeyVariant: int32(8), v2193.EntityDataKeyColorIndex: byte(11), v2193.EntityDataKeyScale: float32(0.8)},
			flags: []uint8{v2193.EntityDataFlagTamed, v2193.EntityDataFlagSitting}, props: []int32{0}},
		{name: "striped wolf", nbt: map[string]any{"id": "minecraft:wolf", "variant": "minecraft:striped", "sound_variant": "minecraft:angry"},
			want: map[uint32]any{v2193.EntityDataKeyVariant: int32(7)}, props: []int32{4}},
		{name: "librarian", nbt: map[string]any{"id": "minecraft:villager", "VillagerData": map[string]any{"profession": "minecraft:librarian", "type": "minecraft:desert", "level": int32(3)}},
			want: map[uint32]any{v2193.EntityDataKeyVariant: int32(5), v2193.EntityDataKeyMarkVariant: int32(1), v2193.EntityDataKeyTradeTier: int32(2)}},
		{name: "large slime", nbt: map[string]any{"id": "minecraft:slime", "Size": int32(3)},
			want: map[uint32]any{v2193.EntityDataKeyVariant: int32(4), v2193.EntityDataKeyWidth: float32(2.08), v2193.EntityDataKeyHeight: float32(2.08)}},
		{name: "medium magma cube", nbt: map[string]any{"id": "minecraft:magma_cube", "Size": int32(1)},
			want: map[uint32]any{v2193.EntityDataKeyVariant: int32(2), v2193.EntityDataKeyWidth: float32(1.04), v2193.EntityDataKeyHeight: float32(1.02)}},
		{name: "clownfish", nbt: map[string]any{"id": "minecraft:tropical_fish", "Variant": int32(1 << 16)},
			want: map[uint32]any{v2193.EntityDataKeyVariant: int32(0), v2193.EntityDataKeyMarkVariant: int32(0), v2193.EntityDataKeyColorIndex: byte(1), v2193.EntityDataKeyColorTwoIndex: byte(0), v2193.EntityDataKeyScale: float32(1.3)}},
		{name: "wild axolotl", nbt: map[string]any{"id": "minecraft:axolotl", "Variant": int32(1)},
			want: map[uint32]any{v2193.EntityDataKeyVariant: int32(3)}},
		{name: "large salmon", nbt: map[string]any{"id": "minecraft:salmon", "Variant": int32(2)},
			want: map[uint32]any{v2193.EntityDataKeyVariant: int32(0), v2193.EntityDataKeyScale: float32(1.5)}},
		{name: "rabbit", nbt: map[string]any{"id": "minecraft:rabbit", "RabbitType": int32(4)},
			want: map[uint32]any{v2193.EntityDataKeyVariant: int32(4), v2193.EntityDataKeyWidth: float32(0.81666666), v2193.EntityDataKeyScale: float32(0.6)}},
		{name: "trader llama", nbt: map[string]any{"id": "minecraft:trader_llama"}, want: map[uint32]any{v2193.EntityDataKeyMarkVariant: int32(1)}},
		{name: "cold chicken", nbt: map[string]any{"id": "minecraft:chicken", "variant": "minecraft:cold"}, props: []int32{2, 0}},
		{name: "tadpole", nbt: map[string]any{"id": "minecraft:tadpole"}, flags: []uint8{v2193.EntityDataFlagBaby}},
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
				key := uint32(v2193.EntityDataKeyFlags)
				if flag >= 64 {
					key, flag = v2193.EntityDataKeyFlagsTwo, flag-64
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
		if got, want := viewer.parseEntityMetadata(e)[v2193.EntityDataKeyCarryBlockRuntimeID], int32(networkBlockID(viewer.br, block.Air{})); got != want {
			t.Fatalf("empty-handed enderman carries %v, want air %v", got, want)
		}
		e.CarryBlock(block.Grass{})
		if got, want := viewer.parseEntityMetadata(e)[v2193.EntityDataKeyCarryBlockRuntimeID], int32(networkBlockID(viewer.br, block.Grass{})); got != want {
			t.Fatalf("enderman carries %v, want grass %v", got, want)
		}
	})
}

// TestVindicatorAngryWhileAttacking checks that a vindicator is angry on the
// client only while it attacks, as BDS flags it when it acquires a target:
// Bedrock vindicators raise their axe only then.
func TestVindicatorAngryWhileAttacking(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()
	w.SetDifficulty(world.DifficultyNormal)
	viewer := testSession("vindicator-metadata")
	viewer.br = world.DefaultBlockRegistry
	var vindicator *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -16; x <= 16; x++ {
			for z := -16; z <= 16; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
		e := tx.AddEntity(entity.NewVindicator(world.EntitySpawnOpts{Position: mgl64.Vec3{6.5, 65, 0.5}}))
		vindicator = e.H()
		if viewer.parseEntityMetadata(e).Flag(v2193.EntityDataKeyFlags, v2193.EntityDataFlagAngry) {
			t.Error("idle vindicator is angry")
		}
		tx.AddEntity(entity.NewVillager(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 65, 0.5}}))
	})
	angry := false
	for range 200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := vindicator.Entity(tx)
			if v := e.(*entity.Vindicator); v.Aggressive() {
				angry = viewer.parseEntityMetadata(v).Flag(v2193.EntityDataKeyFlags, v2193.EntityDataFlagAngry)
			}
		})
		if angry {
			break
		}
	}
	if !angry {
		t.Fatal("attacking vindicator is not angry on the client")
	}
}
