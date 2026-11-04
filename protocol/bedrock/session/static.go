package session

import (
	"slices"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/google/uuid"
)

// staticPacket is a packet that is the same for every player, such as the
// recipes: a connection may encode and compress it once for all of them.
// It must not change once written.
type staticPacket struct{ proto.Packet }

// StaticPacket marks the packet as static.
func (*staticPacket) StaticPacket() {}

// Unwrap returns the packet, which a connection that converts packets
// converts for its protocol.
func (p *staticPacket) Unwrap() proto.Packet { return p.Packet }

// recipeData is what sendRecipes sends and what a session keeps of it,
// the same for every session of a protocol and a block registry, so made
// once.
type recipeData struct {
	// count is the number of recipes registered when it was made.
	count   int
	packet  *staticPacket
	recipes map[uint32]recipe.Recipe
	javaIDs map[string][]string
}

// recipeData returns the recipe data of the tables, made again once more
// recipes were registered.
func (w *wireTables) recipeData() *recipeData {
	all := recipe.Recipes()
	w.recipesMu.Lock()
	defer w.recipesMu.Unlock()
	if d := w.recipes; d != nil && d.count == len(all) {
		return d
	}
	w.recipes = newRecipeData(w, all)
	return w.recipes
}

func newRecipeData(w *wireTables, all []recipe.Recipe) *recipeData {
	d := &recipeData{count: len(all), recipes: make(map[uint32]recipe.Recipe, len(all)), javaIDs: make(map[string][]string)}
	var (
		shapedRecipes            []v2193.ShapedRecipe
		shapelessRecipes         []v2193.ShapelessRecipe
		userDataShapelessRecipes []v2193.UserDataShapelessRecipe
		multiRecipes             []v2193.MultiRecipe
		smithingTransformRecipes []v2193.SmithingTransformRecipe
		smithingTrimRecipes      []v2193.SmithingTrimRecipe
		potionRecipes            []v2193.PotionRecipe
		potionContainerChange    []v2193.PotionContainerChangeRecipe
	)

	for index, i := range all {
		networkID := uint32(index) + 1
		d.recipes[networkID] = i

		switch i := i.(type) {
		case recipe.Shapeless:
			shapelessRecipes = append(shapelessRecipes, v2193.ShapelessRecipe{
				RecipeID:        d.registerJavaID(i),
				Priority:        int32(i.Priority()),
				Input:           stacksToIngredientItems(i.Input()),
				Output:          stacksToRecipeStacks(w, i.Output()),
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			})
		case recipe.UserDataShapeless:
			userDataShapelessRecipes = append(userDataShapelessRecipes, v2193.UserDataShapelessRecipe{ShapelessRecipe: v2193.ShapelessRecipe{
				RecipeID:        d.registerJavaID(i),
				Priority:        int32(i.Priority()),
				Input:           stacksToIngredientItems(i.Input()),
				Output:          stacksToRecipeStacks(w, i.Output()),
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			}})
		case recipe.Multi:
			d.registerJavaMultiID(i)
			multiRecipes = append(multiRecipes, v2193.MultiRecipe{
				UUID:            i.UUID(),
				RecipeNetworkID: networkID,
			})
		case recipe.Shaped:
			shapedRecipes = append(shapedRecipes, v2193.ShapedRecipe{
				RecipeID:        d.registerJavaID(i),
				Priority:        int32(i.Priority()),
				Width:           int32(i.Shape().Width()),
				Height:          int32(i.Shape().Height()),
				Input:           stacksToIngredientItems(i.Input()),
				Output:          stacksToRecipeStacks(w, i.Output()),
				Block:           i.Block(),
				AssumeSymmetry:  true,
				RecipeNetworkID: networkID,
			})
		case recipe.SmithingTransform:
			input, output := stacksToIngredientItems(i.Input()), stacksToRecipeStacks(w, i.Output())
			smithingTransformRecipes = append(smithingTransformRecipes, v2193.SmithingTransformRecipe{
				RecipeID:        d.registerJavaID(i),
				Base:            input[0],
				Addition:        input[1],
				Template:        input[2],
				Result:          output[0],
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			})
		case recipe.SmithingTrim:
			input := stacksToIngredientItems(i.Input())
			smithingTrimRecipes = append(smithingTrimRecipes, v2193.SmithingTrimRecipe{
				RecipeID:        d.registerJavaID(i),
				Base:            input[0],
				Addition:        input[1],
				Template:        input[2],
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			})
		case recipe.Potion:
			inputRuntimeID, inputMeta, _ := w.itemRuntimeID(i.Input()[0].(item.Stack).Item())
			reagentRuntimeID, reagentMeta, _ := w.itemRuntimeID(i.Input()[1].(item.Stack).Item())
			outputRuntimeID, outputMeta, _ := w.itemRuntimeID(i.Output()[0].Item())

			potionRecipes = append(potionRecipes, v2193.PotionRecipe{
				InputPotionID:        inputRuntimeID,
				InputPotionMetadata:  int32(inputMeta),
				ReagentItemID:        reagentRuntimeID,
				ReagentItemMetadata:  int32(reagentMeta),
				OutputPotionID:       outputRuntimeID,
				OutputPotionMetadata: int32(outputMeta),
			})

		case recipe.PotionContainerChange:
			inputRuntimeID, _, _ := w.itemRuntimeID(i.Input()[0].(item.Stack).Item())
			reagentRuntimeID, _, _ := w.itemRuntimeID(i.Input()[1].(item.Stack).Item())
			outputRuntimeID, _, _ := w.itemRuntimeID(i.Output()[0].Item())

			potionContainerChange = append(potionContainerChange, v2193.PotionContainerChangeRecipe{
				InputItemID:   inputRuntimeID,
				ReagentItemID: reagentRuntimeID,
				OutputItemID:  outputRuntimeID,
			})
		}
	}
	recipes := v2193.CraftingDataPacket{
		ShapedRecipes:                shapedRecipes,
		ShapelessRecipes:             shapelessRecipes,
		MultiRecipes:                 multiRecipes,
		UserDataShapelessRecipes:     userDataShapelessRecipes,
		SmithingTransformRecipes:     smithingTransformRecipes,
		SmithingTrimRecipes:          smithingTrimRecipes,
		PotionRecipes:                potionRecipes,
		PotionContainerChangeRecipes: potionContainerChange,
		ClearRecipes:                 true,
	}
	d.packet = &staticPacket{&recipes}
	return d
}

// registerJavaID returns the ID a recipe goes by: its Java ID when it has
// one, else a new UUID; each of its Java IDs names it.
func (d *recipeData) registerJavaID(r recipe.Recipe) string {
	javaIDs := recipe.JavaRecipeIDs(r)
	id := uuid.New().String()
	if len(javaIDs) == 1 {
		id = javaIDs[0]
	}
	for _, javaID := range javaIDs {
		d.javaIDs[javaID] = append(d.javaIDs[javaID], id)
	}
	return id
}

// registerJavaMultiID names a multi recipe, which goes by its UUID, by
// each of its Java IDs.
func (d *recipeData) registerJavaMultiID(r recipe.Multi) {
	packetID := r.UUID().String()
	for _, javaID := range recipe.JavaRecipeIDs(r) {
		d.javaIDs[javaID] = append(d.javaIDs[javaID], packetID)
	}
}

// creativeData is the CreativeContent of the tables of a protocol and a
// block registry.
type creativeData struct {
	// groups and items are the numbers of creative groups and items
	// registered when it was made.
	groups, items int
	packet        *staticPacket
}

// creativePacket returns the CreativeContent of the tables, made again
// once more creative groups or items were registered.
func (w *wireTables) creativePacket() proto.Packet {
	groups, items := creative.Groups(), creative.Items()
	w.creativeMu.Lock()
	defer w.creativeMu.Unlock()
	if d := w.creative; d != nil && d.groups == len(groups) && d.items == len(items) {
		return d.packet
	}
	w.creative = &creativeData{groups: len(groups), items: len(items), packet: &staticPacket{creativeContent(w, groups, items)}}
	return w.creative.packet
}

// creativeContent returns the creative groups and the items in them as
// protocol item stacks.
func creativeContent(w *wireTables, groups []creative.Group, items []creative.Item) *v2193.CreativeContentPacket {
	pk := &v2193.CreativeContentPacket{Groups: make([]v2193.CreativeGroup, 0, len(groups)), Items: make([]v2193.CreativeItem, 0, len(items))}
	for _, group := range groups {
		pk.Groups = append(pk.Groups, v2193.CreativeGroup{
			Category: bedrockCreativeCategory(group.Category),
			Name:     group.Name,
			Icon:     deleteDamage(stackFromItem(w, group.Icon)),
		})
	}
	for index, i := range items {
		group := slices.IndexFunc(groups, func(group creative.Group) bool {
			return group.Name == i.Group
		})
		if group < 0 {
			continue
		}
		pk.Items = append(pk.Items, v2193.CreativeItem{
			CreativeItemNetworkID: uint32(index) + 1,
			Item:                  deleteDamage(stackFromItem(w, i.Stack)),
			GroupIndex:            uint32(group),
		})
	}
	return pk
}
