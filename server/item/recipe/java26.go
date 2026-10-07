package recipe

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

//go:embed java26_recipe_index.json
var java26RecipeIndex []byte

type java26Recipe struct {
	ID          string                     `json:"id"`
	Type        string                     `json:"type"`
	Result      java26RecipeResult         `json:"result"`
	Ingredients []json.RawMessage          `json:"ingredients"`
	Pattern     java26RecipePattern        `json:"pattern"`
	Key         map[string]json.RawMessage `json:"key"`
	Template    json.RawMessage            `json:"template"`
	Base        json.RawMessage            `json:"base"`
	Addition    json.RawMessage            `json:"addition"`
}

// java26RecipePattern represents the recipe-type union: shaped recipes encode
// rows, while smithing trims encode a trim-pattern resource identifier.
type java26RecipePattern struct {
	Rows       []string
	Identifier string
}

func (p *java26RecipePattern) UnmarshalJSON(raw []byte) error {
	for _, b := range raw {
		if b <= ' ' {
			continue
		}
		switch b {
		case '[':
			return json.Unmarshal(raw, &p.Rows)
		case '"':
			return json.Unmarshal(raw, &p.Identifier)
		case 'n':
			return nil
		default:
			return fmt.Errorf("unexpected Java recipe pattern form %q", raw)
		}
	}
	return nil
}

type java26RecipeResult struct {
	ID    string `json:"id"`
	Count int    `json:"count"`
}

var (
	java26RecipesOnce     sync.Once
	java26Recipes         []java26Recipe
	java26RecipesByResult map[string][]java26Recipe
)

func loadJava26Recipes() {
	if err := json.Unmarshal(java26RecipeIndex, &java26Recipes); err != nil {
		panic(err)
	}
	java26RecipesByResult = make(map[string][]java26Recipe)
	for _, r := range java26Recipes {
		if r.ID == "" || r.Result.ID == "" {
			continue
		}
		java26RecipesByResult[r.Result.ID] = append(java26RecipesByResult[r.Result.ID], r)
	}
}

// JavaRecipeIDs returns Java recipe resource keys whose actual 26.3 recipe
// definitions match the Go recipe sent to the client.
func JavaRecipeIDs(r Recipe) []string {
	if multi, ok := r.(Multi); ok {
		return multi.JavaIDs()
	}
	if trim, ok := r.(SmithingTrim); ok {
		java26RecipesOnce.Do(loadJava26Recipes)
		var ids []string
		for _, candidate := range java26Recipes {
			if candidate.Type == "minecraft:smithing_trim" && java26RecipeMatches(candidate, trim) {
				ids = append(ids, candidate.ID)
			}
		}
		sort.Strings(ids)
		return ids
	}
	outputs := r.Output()
	if len(outputs) != 1 || outputs[0].Empty() {
		return nil
	}
	output := outputs[0]
	encoded, ok := output.Item().(interface{ EncodeItem() (string, int16) })
	if !ok {
		return nil
	}
	name, _ := encoded.EncodeItem()
	java26RecipesOnce.Do(loadJava26Recipes)
	var ids []string
	for _, candidate := range java26RecipesByResult[name] {
		count := candidate.Result.Count
		if count == 0 {
			count = 1
		}
		if count == output.Count() && java26RecipeMatches(candidate, r) {
			ids = append(ids, candidate.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func java26MultiRecipeIDsByUUID(id string) []string {
	if recipeIDs, ok := java26MultiRecipeIDMap[id]; ok {
		return append([]string(nil), recipeIDs...)
	}
	if id != "b5c5d105-75a2-4076-af2b-923ea2bf4bf0" {
		return nil
	}
	java26RecipesOnce.Do(loadJava26Recipes)
	ids := make([]string, 0, 16)
	for _, candidate := range java26Recipes {
		if candidate.Type == "minecraft:crafting_special_bannerduplicate" {
			ids = append(ids, candidate.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

var java26MultiRecipeIDMap = map[string][]string{
	"00000000-0000-0000-0000-000000000001": {"minecraft:repair_item"},
	"00000000-0000-0000-0000-000000000002": {
		"minecraft:firework_rocket",
		"minecraft:firework_star",
		"minecraft:firework_star_fade",
	},
	"442d85ed-8272-4543-a6f1-418f90ded05d": {"minecraft:map_cloning"},
	"85939755-ba10-4d9d-a4cc-efb7a8e943c4": {"minecraft:map_cloning"},
	"8b36268c-1829-483c-a0f1-993b7156a8f2": {"minecraft:map_extending"},
	"d392b075-4ba1-40ae-8789-af868d56f6ce": {"minecraft:map_extending"},
	"d1ca6b84-338e-4f2f-9c6b-76cc8b4bd98d": {"minecraft:book_cloning"},
	"685a742a-c42e-4a4e-88ea-5eb83fc98e5b": {"minecraft:shield_decoration"},
}

func java26RecipeMatches(j java26Recipe, r Recipe) bool {
	switch r := r.(type) {
	case Shaped:
		if j.Type != "minecraft:crafting_shaped" || r.Block() != "crafting_table" {
			return false
		}
		shape := r.Shape()
		inputs := r.Input()
		pattern := j.Pattern.Rows
		if len(pattern) != shape.Height() || len(inputs) != shape.Width()*shape.Height() {
			return false
		}
		for y, row := range pattern {
			for x := range shape.Width() {
				var alternatives []string
				if x < len(row) && row[x] != ' ' {
					var ok bool
					alternatives, ok = java26Alternatives(j.Key[string(row[x])])
					if !ok {
						return false
					}
				}
				if !java26IngredientMatches(inputs[y*shape.Width()+x], alternatives) {
					return false
				}
			}
			if len(row) > shape.Width() && strings.TrimSpace(row[shape.Width():]) != "" {
				return false
			}
		}
		return true
	case Shapeless, UserDataShapeless:
		if j.Type != "minecraft:crafting_shapeless" || r.Block() != "crafting_table" {
			return false
		}
		return java26UnorderedInputsMatch(j.Ingredients, r.Input())
	case SmithingTransform:
		if j.Type != "minecraft:smithing_transform" || r.Block() != "smithing_table" {
			return false
		}
		inputs := r.Input()
		return len(inputs) == 3 && java26IngredientMatchesRaw(inputs[0], j.Base) &&
			java26IngredientMatchesRaw(inputs[1], j.Addition) && java26IngredientMatchesRaw(inputs[2], j.Template)
	case SmithingTrim:
		if j.Type != "minecraft:smithing_trim" || r.Block() != "smithing_table" {
			return false
		}
		inputs := r.Input()
		return len(inputs) == 3 && java26IngredientMatchesRaw(inputs[0], j.Base) &&
			java26IngredientMatchesRaw(inputs[1], j.Addition) && java26IngredientMatchesRaw(inputs[2], j.Template)
	default:
		return false
	}
}


func java26UnorderedInputsMatch(rawInputs []json.RawMessage, inputs []Item) bool {
	if len(rawInputs) != len(inputs) {
		return false
	}
	alternatives := make([][]string, len(rawInputs))
	for i, raw := range rawInputs {
		var ok bool
		alternatives[i], ok = java26Alternatives(raw)
		if !ok {
			return false
		}
	}
	used := make([]bool, len(inputs))
	var assign func(int) bool
	assign = func(index int) bool {
		if index == len(alternatives) {
			return true
		}
		for i, in := range inputs {
			if !used[i] && java26IngredientMatches(in, alternatives[index]) {
				used[i] = true
				if assign(index + 1) {
					return true
				}
				used[i] = false
			}
		}
		return false
	}
	return assign(0)
}

func java26IngredientMatchesRaw(in Item, raw json.RawMessage) bool {
	alternatives, ok := java26Alternatives(raw)
	return ok && java26IngredientMatches(in, alternatives)
}

func java26Alternatives(raw json.RawMessage) ([]string, bool) {
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		return []string{name}, name != ""
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err == nil && len(names) != 0 {
		return names, true
	}
	var ingredient struct {
		Item string `json:"item"`
		Tag  string `json:"tag"`
	}
	if err := json.Unmarshal(raw, &ingredient); err != nil {
		return nil, false
	}
	if ingredient.Item != "" {
		return []string{ingredient.Item}, true
	}
	if ingredient.Tag != "" {
		return []string{"#" + ingredient.Tag}, true
	}
	return nil, false
}

func java26IngredientMatches(in Item, alternatives []string) bool {
	if in == nil || in.Empty() {
		return len(alternatives) == 0
	}
	if in.Count() != 1 {
		return false
	}
	if tag, ok := in.(ItemTag); ok {
		for _, alternative := range alternatives {
			if alternative == "#"+tag.Tag() {
				return true
			}
			if tag.Tag() == "minecraft:trimmable_armors" && alternative == "#minecraft:trimmable_armor" {
				return true
			}
		}
		return false
	}
	stack, ok := in.(item.Stack)
	if !ok || stack.Empty() {
		return false
	}
	encoded, ok := stack.Item().(interface{ EncodeItem() (string, int16) })
	if !ok {
		return false
	}
	name, meta := encoded.EncodeItem()
	for _, alternative := range alternatives {
		if name == alternative && meta == 0 {
			return true
		}
		if sourceItem, ok := world.ItemByName(alternative, 0); ok {
			sourceName, sourceMeta := sourceItem.EncodeItem()
			if name == sourceName && meta == sourceMeta {
				return true
			}
		}
	}
	return false
}
