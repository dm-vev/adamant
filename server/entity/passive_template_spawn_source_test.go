package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
)

func TestPassiveTemplateSpawnMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_passive_template_authored_spawn.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ID         string `json:"id"`
		Template   string `json:"template"`
		Input      string `json:"input_binary"`
		NBT        string `json:"nbt_binary"`
		LevelSeed  string `json:"level_seed"`
		OwnSeed    string `json:"own_seed"`
		Difficulty int    `json:"difficulty"`
		Biome      string `json:"biome"`
		MoonPhase  int    `json:"moon_phase"`
		BlackCats  bool   `json:"cats_spawn_as_black"`
		GameTime   string `json:"game_time"`
		NextLevel  string `json:"next_level_long"`
		NextOwn    string `json:"next_own_long"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, source := range cases {
		t.Run(fmt.Sprintf("%s/%s/level_%s/%s/moon%d/hut%t", source.ID, source.Template, source.LevelSeed, source.Biome, source.MoonPhase, source.BlackCats), func(t *testing.T) {
			levelSeed, err := strconv.ParseInt(source.LevelSeed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			ownSeed, err := strconv.ParseInt(source.OwnSeed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			gameTime, err := strconv.ParseInt(source.GameTime, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			base, ok := world.DifficultyByID(source.Difficulty)
			if !ok {
				t.Fatal(source.Difficulty)
			}
			context := world.GenerationContext{Difficulty: world.LocalDifficulty{Base: base}, GameTime: gameTime, MoonBrightness: [...]float32{1, .75, .5, .25, 0, .25, .5, .75}[source.MoonPhase]}
			level, own := java26.NewXoroshiroRandom(levelSeed), java26.NewLegacyRandom(ownSeed)
			input, want := sourceSpawnNBT(t, source.Input), sourceSpawnNBT(t, source.NBT)
			if err := preparePassiveTemplateSpawn(input, &level, &own, context, source.Biome, source.BlackCats); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"Health", "Age", "variant", "sound_variant", "Color", "Variant", "LastPoseTick", "VillagerData", "VillagerDataFinalized", "LeftHanded"} {
				if _, present := want[key]; !present {
					continue
				}
				if !reflect.DeepEqual(input[key], want[key]) {
					t.Errorf("%s=%#v, Source=%#v", key, input[key], want[key])
				}
			}
			if got, expected := spawnAttributesByID(input["attributes"]), spawnAttributesByID(want["attributes"]); !reflect.DeepEqual(got, expected) {
				t.Errorf("attributes=%#v, Source=%#v", got, expected)
			}
			for _, continuation := range []struct {
				name   string
				stream *java26.Random
				value  string
			}{{"region", &level, source.NextLevel}, {"entity", &own, source.NextOwn}} {
				expected, err := strconv.ParseInt(continuation.value, 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				if got := continuation.stream.NextLong(); got != expected {
					t.Errorf("%s RNG=%d, Source=%d", continuation.name, got, expected)
				}
			}
		})
	}
}
