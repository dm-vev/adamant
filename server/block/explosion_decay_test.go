package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type decayEntityType struct{ id string }

func (t decayEntityType) Open(_ *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return decayEntity{h: h}
}
func (t decayEntityType) EncodeEntity() string                      { return t.id }
func (decayEntityType) BBox(world.Entity) cube.BBox                 { return cube.BBox{} }
func (decayEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (decayEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }
func (decayEntityConfig) Apply(*world.EntityData)                   {}
func (e decayEntity) H() *world.EntityHandle                        { return e.h }
func (decayEntity) Position() mgl64.Vec3                            { return mgl64.Vec3{} }
func (decayEntity) Rotation() cube.Rotation                         { return cube.Rotation{} }
func (decayEntity) Close() error                                    { return nil }

type decayEntityConfig struct{}
type decayEntity struct{ h *world.EntityHandle }

func TestExplosionDropDecayFollowsSourceGamerule(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	source := func(id string) world.ExplosionSource {
		h := world.EntitySpawnOpts{}.New(decayEntityType{id}, decayEntityConfig{})
		return world.EntityExplosionSource{Entity: decayEntity{h: h}}
	}
	cases := []struct {
		name string
		src  world.ExplosionSource
		rule string
		def  bool
	}{
		{"tnt", source("minecraft:tnt"), "tnt_explosion_drop_decay", false},
		{"tnt minecart", source("minecraft:tnt_minecart"), "tnt_explosion_drop_decay", false},
		{"end crystal", source("minecraft:ender_crystal"), "block_explosion_drop_decay", true},
		{"creeper", source("minecraft:creeper"), "mob_explosion_drop_decay", true},
		{"bed", world.BlockExplosionSource{}, "block_explosion_drop_decay", true},
	}
	for _, c := range cases {
		if got := explosionDropDecay(w, c.src); got != c.def {
			t.Errorf("%s: default decay %v, want %v", c.name, got, c.def)
		}
		if err := w.SetGameRule(c.rule, !c.def); err != nil {
			t.Fatal(err)
		}
		if got := explosionDropDecay(w, c.src); got != !c.def {
			t.Errorf("%s: decay with %s=%v is %v", c.name, c.rule, !c.def, got)
		}
		if err := w.SetGameRule(c.rule, c.def); err != nil {
			t.Fatal(err)
		}
	}
}
