package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestJavaNBTReadsTheRegistryOfTheWorld decodes Java entity NBT in a world whose registry is the default
// registry without zombies and cod. The mobs are made of the types of the world's registry, as the registry
// config's functions are called through it, and not of the types of entity.DefaultRegistry: a zombie or a cod
// is not made at all.
func TestJavaNBTReadsTheRegistryOfTheWorld(t *testing.T) {
	t.Parallel()
	var types []world.EntityType
	for _, typ := range entity.DefaultRegistry.Types() {
		if id := typ.EncodeEntity(); id != "minecraft:zombie" && id != "minecraft:cod" {
			types = append(types, typ)
		}
	}
	w := world.Config{Entities: entity.DefaultRegistry.Config().New(types), Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()

	<-w.Exec(func(tx *world.Tx) {
		reg := tx.World().EntityRegistry()
		conf := reg.Config()
		opts := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 70, 0.5}}
		if _, ok := reg.Lookup("minecraft:zombie"); ok {
			t.Fatal("the registry of the world has zombies")
		}

		if h := conf.SpawnMobFromNBT(reg, "minecraft:zombie", opts, map[string]any{}); h != nil {
			t.Errorf("SpawnMobFromNBT made a %s, a type the world does not register", h.Type().EncodeEntity())
		}
		if h := conf.NewFromBucket(reg, opts, "minecraft:cod", map[string]any{}); h != nil {
			t.Errorf("NewFromBucket made a %s, a type the world does not register", h.Type().EncodeEntity())
		}
		if h, err := entity.NewFromStructureNBTOf(reg, map[string]any{"id": "minecraft:zombie"}, opts); err == nil {
			t.Errorf("NewFromStructureNBTOf made a %s, a type the world does not register", h.Type().EncodeEntity())
		}

		if h := conf.SpawnMobFromNBT(reg, "minecraft:pig", opts, map[string]any{}); h == nil || h.Type() != entity.PigType {
			t.Errorf("SpawnMobFromNBT of a pig = %v, want a pig", h)
		}
		if h := conf.NewFromBucket(reg, opts, "minecraft:salmon", map[string]any{}); h == nil || h.Type() != entity.SalmonType {
			t.Errorf("NewFromBucket of a salmon = %v, want a salmon", h)
		}

		// The registry passed decides, not the one the config came from: the default registry has zombies.
		if h := conf.SpawnMobFromNBT(entity.DefaultRegistry, "minecraft:zombie", opts, map[string]any{}); h == nil || h.Type() != entity.ZombieType {
			t.Errorf("SpawnMobFromNBT of a zombie in the default registry = %v, want a zombie", h)
		}
		if h, err := entity.NewFromStructureNBT(map[string]any{"id": "minecraft:zombie"}, opts); err != nil || h.Type() != entity.ZombieType {
			t.Errorf("NewFromStructureNBT of a zombie = %v, %v, want a zombie of the default registry", h, err)
		}
	})
}
