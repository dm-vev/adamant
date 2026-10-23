package entity

import (
	"context"
	"encoding/json"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
	"math"
	"os"
	"testing"
)

func TestSourceMountTemplateStateSurvivesMCDB(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_passive_template_authored_spawn.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ID       string `json:"id"`
		Template string `json:"template"`
		NBT      string `json:"nbt_binary"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"minecraft:horse", "minecraft:camel"} {
		t.Run(kind, func(t *testing.T) {
			var source map[string]any
			for _, candidate := range cases {
				if candidate.ID == kind && candidate.Template != "biome-boundary/"+kind {
					source = sourceSpawnNBT(t, candidate.NBT)
					source["id"] = kind
					break
				}
			}
			if source == nil {
				t.Fatalf("Source fixture for %s missing", kind)
			}
			path := t.TempDir()
			open := func() *world.World {
				db, err := mcdb.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				return world.Config{Provider: db, Entities: DefaultRegistry, Synchronous: true}.New()
			}
			w := open()
			pos := mgl64.Vec3{.5, 65, .5}
			handle, err := NewFromStructureNBT(source, world.EntitySpawnOpts{Position: pos, SpawnReason: "structure", SpawnFinalized: true})
			if err != nil {
				t.Fatal(err)
			}
			check := func(e world.Entity) {
				t.Helper()
				mount := mountFromEntity(e)
				if mount == nil {
					t.Fatalf("entity=%T", e)
				}
				if got, want := mount.AgeTicks(), int(nbtconv.Int32(source, "Age")); got != want {
					t.Errorf("age=%d, want %d", got, want)
				}
				if kind == "minecraft:horse" && mount.b().variant != int(nbtconv.Int32(source, "Variant")) {
					t.Errorf("variant=%d", mount.b().variant)
				}
				if kind == "minecraft:camel" && mount.b().lastPoseTick != nbtconv.Int64(source, "LastPoseTick") {
					t.Errorf("LastPoseTick=%d", mount.b().lastPoseTick)
				}
				if got, ok := source["Health"].(float32); ok && mount.Health() != float64(got) {
					t.Errorf("health=%g, Source=%g", mount.Health(), got)
				}
				if kind == "minecraft:horse" {
					attributes := spawnAttributesByID(source["attributes"])
					for _, pair := range []struct {
						id  string
						got float64
					}{{"minecraft:max_health", mount.MaxHealth()}, {"minecraft:movement_speed", mount.Speed()}, {"minecraft:jump_strength", mount.b().jumpStrength}} {
						attribute := attributes[pair.id]
						expected := sourceTemplateAttributeValue(attribute["base"].(float64), attribute["modifiers"])
						if math.Abs(pair.got-expected) > 1e-7 {
							t.Errorf("%s=%g, Source=%g", pair.id, pair.got, expected)
						}
					}
				}
			}
			if err := w.Do(func(tx *world.Tx) { check(tx.AddEntity(handle)) }).Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
			w.Save()
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			w = open()
			t.Cleanup(func() { _ = w.Close() })
			if err := w.Do(func(tx *world.Tx) {
				tx.Block(cube.PosFromVec3(pos))
				for e := range tx.Entities() {
					if e.H().UniqueID() == handle.UniqueID() {
						check(e)
						return
					}
				}
				t.Error("mount missing after MCDB reopen")
			}).Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
