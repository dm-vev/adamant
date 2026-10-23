package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func templateSpawnEquipment(t *testing.T, value any) map[string]any {
	t.Helper()
	equipment, _ := value.(map[string]any)
	if len(equipment) == 0 {
		return nil
	}
	out := make(map[string]any, len(equipment))
	for slot, raw := range equipment {
		stack, err := item.StackFromStructureNBT(raw.(map[string]any))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := item.StackToStructureNBT(stack)
		if err != nil {
			t.Fatal(err)
		}
		out[slot] = encoded
	}
	return out
}

type netherTemplateSpawnCase struct {
	ID                string  `json:"id"`
	Template          string  `json:"template"`
	LevelSeed         string  `json:"level_seed"`
	OwnSeed           string  `json:"own_seed"`
	Difficulty        int     `json:"difficulty"`
	SpecialMultiplier float32 `json:"special_multiplier"`
	PosX              int32   `json:"pos_x"`
	PosY              int32   `json:"pos_y"`
	PosZ              int32   `json:"pos_z"`
	Dimension         string  `json:"dimension"`
	TemplateNBT       string  `json:"template_nbt_binary"`
	NBT               string  `json:"nbt_binary"`
	NextLevel         string  `json:"next_level_long"`
	NextOwn           string  `json:"next_own_long"`
}

func TestNetherTemplateSpawnMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_nether_template_spawn.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []netherTemplateSpawnCase
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, source := range cases {
		t.Run(fmt.Sprintf("%s/%s/level_%s/difficulty_%d", source.ID, source.Template, source.LevelSeed, source.Difficulty), func(t *testing.T) {
			levelSeed, err := strconv.ParseInt(source.LevelSeed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			ownSeed, err := strconv.ParseInt(source.OwnSeed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			levelRandom, entityRandom := vanilla.NewXoroshiroRandom(levelSeed), vanilla.NewLegacyRandom(ownSeed)
			context := world.GenerationContext{Difficulty: world.LocalDifficulty{Effective: 2 + 2*source.SpecialMultiplier}}
			input := sourceSpawnNBT(t, source.TemplateNBT)
			if input["id"] != source.ID {
				t.Fatalf("template id=%v, fixture id=%s", input["id"], source.ID)
			}
			want := sourceSpawnNBT(t, source.NBT)
			if err := prepareNetherTemplateSpawn(input, &levelRandom, &entityRandom, context, [3]int32{source.PosX, source.PosY, source.PosZ}, source.Dimension); err != nil {
				t.Fatal(err)
			}
			if got, expected := templateSpawnEquipment(t, input["equipment"]), templateSpawnEquipment(t, want["equipment"]); !reflect.DeepEqual(got, expected) {
				t.Errorf("equipment=%#v, Source=%#v", got, expected)
			}
			if !reflect.DeepEqual(input["LeftHanded"], want["LeftHanded"]) {
				t.Errorf("LeftHanded=%v, Source=%v", input["LeftHanded"], want["LeftHanded"])
			}
			if source.ID == "minecraft:hoglin" {
				if !reflect.DeepEqual(input["Age"], want["Age"]) {
					t.Errorf("Age=%#v, Source=%#v", input["Age"], want["Age"])
				}
			} else if got, expected := nbtByteBool(input["IsBaby"]), nbtByteBool(want["IsBaby"]); got != expected {
				t.Errorf("IsBaby=%t, Source=%t", got, expected)
			}
			if source.ID == "minecraft:piglin" || source.ID == "minecraft:piglin_brute" {
				if !reflect.DeepEqual(input["Brain"], want["Brain"]) {
					t.Errorf("Brain=%#v, Source=%#v", input["Brain"], want["Brain"])
				}
			}
			gotAttributes, wantAttributes := spawnAttributesByID(input["attributes"]), spawnAttributesByID(want["attributes"])
			if !reflect.DeepEqual(gotAttributes["minecraft:follow_range"], wantAttributes["minecraft:follow_range"]) {
				t.Errorf("follow_range=%#v, Source=%#v", gotAttributes["minecraft:follow_range"], wantAttributes["minecraft:follow_range"])
			}
			wantLevel, err := strconv.ParseInt(source.NextLevel, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			wantOwn, err := strconv.ParseInt(source.NextOwn, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := levelRandom.NextLong(); got != wantLevel {
				t.Errorf("level RNG=%d, Source=%d", got, wantLevel)
			}
			if got := entityRandom.NextLong(); got != wantOwn {
				t.Errorf("entity RNG=%d, Source=%d", got, wantOwn)
			}
		})
	}
}

func nbtByteBool(value any) bool {
	switch value := value.(type) {
	case byte:
		return value != 0
	case bool:
		return value
	default:
		return false
	}
}
