package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

func TestZombieVillagerTemplateSpawnMatchesJava263(t *testing.T) {
	for _, fixture := range []string{"java263_zombie_template_spawn.json", "java263_zombie_template_live_spawn.json"} {
		data, err := os.ReadFile("testdata/" + fixture)
		if err != nil {
			t.Fatal(err)
		}
		var cases []struct {
			Template   string `json:"template"`
			Input      string `json:"input_binary"`
			Difficulty int    `json:"difficulty"`
			drownedSourceSpawnCase
		}
		if err := json.Unmarshal(data, &cases); err != nil {
			t.Fatal(err)
		}
		for _, source := range cases {
			t.Run(fmt.Sprintf("%s/level_%s/difficulty_%d/halloween_%t", source.Template, source.LevelSeed, source.Difficulty, source.Halloween), func(t *testing.T) {
				levelSeed, err := strconv.ParseInt(source.LevelSeed, 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				ownSeed, err := strconv.ParseInt(source.OwnSeed, 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				level, own := vanilla.NewXoroshiroRandom(levelSeed), vanilla.NewLegacyRandom(ownSeed)
				base, ok := world.DifficultyByID(source.Difficulty)
				if !ok {
					t.Fatalf("Source difficulty %d", source.Difficulty)
				}
				if fixture == "java263_zombie_template_spawn.json" {
					// The first capture used a NORMAL Mob.level with independently
					// varied DifficultyInstance; the live capture varies both.
					base = world.DifficultyNormal
				}
				context := world.GenerationContext{Difficulty: world.LocalDifficulty{Base: base, Effective: 2 + 2*source.Multiplier}, Halloween: source.Halloween}
				input, want := sourceSpawnNBT(t, source.Input), sourceSpawnNBT(t, source.NBT)
				vehicle, err := prepareZombieVillagerTemplateSpawn(input, &level, &own, context, source.Biome)
				if err != nil {
					t.Fatal(err)
				}
				if got, expected := templateSpawnEquipment(t, input["equipment"]), templateSpawnEquipment(t, want["equipment"]); !reflect.DeepEqual(got, expected) {
					t.Errorf("equipment=%#v, Source=%#v", got, expected)
				}
				for _, key := range []string{"Health", "VillagerData", "VillagerDataFinalized", "CanPickUpLoot", "LeftHanded", "drop_chances"} {
					if !reflect.DeepEqual(input[key], want[key]) {
						t.Errorf("%s=%#v, Source=%#v", key, input[key], want[key])
					}
				}
				for _, key := range []string{"IsBaby", "CanBreakDoors"} {
					if got, expected := templateSpawnBool(input[key]), templateSpawnBool(want[key]); got != expected {
						t.Errorf("%s=%t, Source=%t", key, got, expected)
					}
				}
				if got, expected := spawnAttributesByID(input["attributes"]), spawnAttributesByID(want["attributes"]); !reflect.DeepEqual(got, expected) {
					t.Errorf("attributes=%#v, Source=%#v", got, expected)
				}
				if len(source.Children) == 0 {
					if vehicle != nil {
						t.Errorf("unexpected jockey vehicle=%#v", vehicle)
					}
				} else {
					if len(source.Children) != 1 {
						t.Fatalf("Source emitted %d jockey entities", len(source.Children))
					}
					if vehicle == nil {
						t.Fatal("Source chicken jockey was not prepared")
					}
					expected := sourceSpawnNBT(t, source.Children[0].NBT)
					if vehicle["id"] != source.Children[0].ID {
						t.Errorf("vehicle id=%v, Source=%s", vehicle["id"], source.Children[0].ID)
					}
					for _, key := range []string{"Health", "variant", "sound_variant", "IsChickenJockey", "LeftHanded"} {
						if !reflect.DeepEqual(vehicle[key], expected[key]) {
							t.Errorf("vehicle %s=%#v, Source=%#v", key, vehicle[key], expected[key])
						}
					}
					if got, expected := spawnAttributesByID(vehicle["attributes"]), spawnAttributesByID(expected["attributes"]); !reflect.DeepEqual(got, expected) {
						t.Errorf("vehicle attributes=%#v, Source=%#v", got, expected)
					}
				}
				for _, continuation := range []struct {
					name     string
					stream   *vanilla.Random
					expected string
				}{{"region", &level, source.NextLevel}, {"entity", &own, source.NextOwn}} {
					expected, err := strconv.ParseInt(continuation.expected, 10, 64)
					if err != nil {
						t.Fatal(err)
					}
					if got := continuation.stream.NextLong(); got != expected {
						t.Errorf("%s continuation=%d, Source=%d", continuation.name, got, expected)
					}
				}
			})
		}
	}
}
