package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPreparedStructureMobsMatchJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_structure_mob_spawn.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ID string `json:"id"`
		drownedSourceSpawnCase
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, source := range cases {
		t.Run(fmt.Sprintf("%s/level_%s/special_%g", source.ID, source.LevelSeed, source.Multiplier), func(t *testing.T) {
			levelSeed, err := strconv.ParseInt(source.LevelSeed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			ownSeed, err := strconv.ParseInt(source.OwnSeed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			level, own := java26.NewXoroshiroRandom(levelSeed), java26.NewLegacyRandom(ownSeed)
			context := world.GenerationContext{Difficulty: world.LocalDifficulty{Effective: 2 + 2*source.Multiplier}}
			prepared, err := PrepareStructureMobSpawn(source.ID, &level, &own, context)
			if err != nil {
				t.Fatal(err)
			}
			want := sourceSpawnNBT(t, source.NBT)
			for _, key := range []string{"Health", "LeftHanded", "CanJoinRaid", "equipment"} {
				if !reflect.DeepEqual(prepared[key], want[key]) {
					t.Errorf("%s=%#v, Source=%#v", key, prepared[key], want[key])
				}
			}
			if got, expected := spawnAttributesByID(prepared["attributes"]), spawnAttributesByID(want["attributes"]); !reflect.DeepEqual(got, expected) {
				t.Errorf("attributes=%#v, Source=%#v", got, expected)
			}
			for _, continuation := range []struct {
				name     string
				stream   *java26.Random
				expected string
			}{{"region", &level, source.NextLevel}, {"entity", &own, source.NextOwn}} {
				wantNext, err := strconv.ParseInt(continuation.expected, 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				if got := continuation.stream.NextLong(); got != wantNext {
					t.Errorf("%s continuation=%d, Source=%d", continuation.name, got, wantNext)
				}
			}
		})
	}
}

func TestSourceVindicatorEquipmentSurvivesNativeMCDB(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_structure_mob_spawn.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ID string `json:"id"`
		drownedSourceSpawnCase
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	var source map[string]any
	for _, candidate := range cases {
		if candidate.ID == "minecraft:vindicator" && candidate.LevelSeed == "0" && candidate.Multiplier > 0 {
			source = sourceSpawnNBT(t, candidate.NBT)
			source["id"] = candidate.ID
			break
		}
	}
	if source == nil {
		t.Fatal("Source enchanted Vindicator case missing")
	}
	expected, err := item.StackFromJavaNBT(source["equipment"].(map[string]any)["mainhand"].(map[string]any))
	if err != nil {
		t.Fatal(err)
	}
	want, err := item.StackToJavaNBT(expected)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	open := func() *world.World {
		db, err := mcdb.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Provider: db, Entities: DefaultRegistry, Synchronous: true}.New()
	}
	w := open()
	t.Cleanup(func() { _ = w.Close() })
	pos := mgl64.Vec3{.5, 65, .5}
	handle, err := NewFromJavaNBT(source, world.EntitySpawnOpts{Position: pos, SpawnReason: "structure", SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	var held item.Stack
	if err := w.Do(func(tx *world.Tx) {
		held, _ = tx.AddEntity(handle).(*Vindicator).HeldItems()
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	check := func(stack item.Stack) {
		t.Helper()
		got, err := item.StackToJavaNBT(stack)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("actual Source Vindicator equipment changed: %#v, Source %#v", got, want)
		}
	}
	check(held)
	w.Save()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = open()
	held = item.Stack{}
	if err := w.Do(func(tx *world.Tx) {
		tx.Block(cube.PosFromVec3(pos))
		for e := range tx.Entities() {
			if e.H().UniqueID() == handle.UniqueID() {
				held, _ = e.(*Vindicator).HeldItems()
				break
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	check(held)
}
