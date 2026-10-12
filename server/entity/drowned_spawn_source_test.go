package entity

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

type drownedSourceSpawnCase struct {
	LevelSeed  string  `json:"level_seed"`
	OwnSeed    string  `json:"own_seed"`
	Biome      string  `json:"biome"`
	Halloween  bool    `json:"halloween"`
	Multiplier float32 `json:"special_multiplier"`
	Baby       bool    `json:"baby"`
	Health     float64 `json:"health"`
	MaxHealth  float64 `json:"max_health"`
	Vehicle    string  `json:"vehicle"`
	NBT        string  `json:"nbt_binary"`
	NextLevel  string  `json:"next_level_long"`
	NextOwn    string  `json:"next_own_long"`
	Children   []struct {
		ID  string `json:"id"`
		NBT string `json:"nbt_binary"`
	} `json:"children"`
}

func sourceSpawnNBT(t *testing.T, encoded string) map[string]any {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = nbt.UnmarshalEncoding(data, &value, nbt.BigEndian); err != nil {
		t.Fatal(err)
	}
	return value
}

func spawnAttributesByID(value any) map[string]map[string]any {
	out := make(map[string]map[string]any)
	if entries, ok := value.([]any); ok {
		for _, entry := range entries {
			attribute := entry.(map[string]any)
			out[attribute["id"].(string)] = attribute
		}
	}
	return out
}

func TestDrownedStructureSpawnMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_drowned_spawn.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []drownedSourceSpawnCase
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	w := world.Config{Entities: DefaultRegistry, Synchronous: true}.New()
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, source := range cases {
		t.Run(fmt.Sprintf("halloween_%t/level_%s/own_%s/special_%g", source.Halloween, source.LevelSeed, source.OwnSeed, source.Multiplier), func(t *testing.T) {
			levelSeed, err := strconv.ParseInt(source.LevelSeed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			ownSeed, err := strconv.ParseInt(source.OwnSeed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			level, own := java26.NewXoroshiroRandom(levelSeed), java26.NewLegacyRandom(ownSeed)
			context := world.GenerationContext{Difficulty: world.LocalDifficulty{Effective: 2 + 2*source.Multiplier}, Halloween: source.Halloween}
			prepared, vehicle, err := PrepareDrownedStructureSpawn(&level, &own, context, source.Biome)
			if err != nil {
				t.Fatal(err)
			}
			want := sourceSpawnNBT(t, source.NBT)
			for _, key := range []string{"PersistenceRequired", "IsBaby", "CanPickUpLoot", "CanBreakDoors", "LeftHanded", "Health", "equipment", "drop_chances"} {
				if !reflect.DeepEqual(prepared[key], want[key]) {
					t.Errorf("%s=%#v, Source=%#v", key, prepared[key], want[key])
				}
			}
			if got, expected := spawnAttributesByID(prepared["attributes"]), spawnAttributesByID(want["attributes"]); !reflect.DeepEqual(got, expected) {
				t.Errorf("attributes=%#v, Source=%#v", got, expected)
			}
			wantLevel, err := strconv.ParseInt(source.NextLevel, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			wantOwn, err := strconv.ParseInt(source.NextOwn, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := level.NextLong(); got != wantLevel {
				t.Errorf("level RNG=%d, Source=%d", got, wantLevel)
			}
			if got := own.NextLong(); got != wantOwn {
				t.Errorf("entity RNG=%d, Source=%d", got, wantOwn)
			}
			if source.Vehicle == "" {
				if vehicle != nil {
					t.Errorf("unexpected native jockey vehicle=%v", vehicle["id"])
				}
			} else {
				if vehicle == nil || vehicle["id"] != source.Vehicle {
					t.Fatalf("vehicle=%v, Source=%s", vehicle, source.Vehicle)
				}
				child := sourceSpawnNBT(t, source.Children[0].NBT)
				for _, key := range []string{"Health", "variant", "LeftHanded", "sound_variant", "IsChickenJockey", "Brain"} {
					if source.Vehicle == "minecraft:chicken" && key == "Brain" {
						continue
					}
					if !reflect.DeepEqual(vehicle[key], child[key]) {
						t.Errorf("vehicle %s=%#v, Source=%#v", key, vehicle[key], child[key])
					}
				}
				if !reflect.DeepEqual(spawnAttributesByID(vehicle["attributes"]), spawnAttributesByID(child["attributes"])) {
					t.Errorf("vehicle attributes=%#v, Source=%#v", vehicle["attributes"], child["attributes"])
				}
			}
			handle, err := NewFromJavaNBT(prepared, world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}, SpawnReason: "structure", SpawnFinalized: true, EntityRandom: &own})
			if err != nil {
				t.Fatal(err)
			}
			job := w.Do(func(tx *world.Tx) {
				mob := tx.AddEntity(handle).(*Drowned)
				if wantHealth, wantMax := float64(want["Health"].(float32)), float64(float32(source.MaxHealth)); mob.Health() != wantHealth || mob.MaxHealth() != wantMax || mob.Baby() != source.Baby {
					t.Errorf("registered drowned health=%g/%g baby=%t, Source=%g/%g baby=%t", mob.Health(), mob.MaxHealth(), mob.Baby(), wantHealth, wantMax, source.Baby)
				}
				main, off := mob.HeldItems()
				actualEquipment, _ := prepared["equipment"].(map[string]any)
				for _, slot := range []struct {
					name  string
					stack item.Stack
				}{{"mainhand", main}, {"offhand", off}} {
					got, err := item.StackToJavaNBT(slot.stack)
					if err != nil {
						t.Error(err)
						continue
					}
					wantStack, _ := actualEquipment[slot.name].(map[string]any)
					if !reflect.DeepEqual(got, wantStack) {
						t.Errorf("registered %s=%#v, Source=%#v", slot.name, got, wantStack)
					}
				}
				tx.RemoveEntity(mob)
			})
			<-job.Done()
			if err = job.Err(); err != nil {
				t.Fatal(err)
			}
			if err = handle.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
