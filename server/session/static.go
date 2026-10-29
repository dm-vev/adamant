package session

import (
	"reflect"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// staticPacket is a packet that is the same for every player, such as the
// recipes: a connection may encode and compress it once for all of them.
// It must not change once written.
type staticPacket struct{ packet.Packet }

// StaticPacket marks the packet as static.
func (*staticPacket) StaticPacket() {}

// recipeData is what sendRecipes sends and what a session keeps of it,
// the same for every session of a block registry, so made once.
type recipeData struct {
	// count is the number of recipes registered when it was made.
	count   int
	packet  *staticPacket
	recipes map[uint32]recipe.Recipe
	javaIDs map[string][]string
}

var recipeCache struct {
	sync.Mutex
	data map[world.BlockRegistry]*recipeData
}

// recipesFor returns the recipe data of a block registry, made again once
// more recipes were registered.
func recipesFor(br world.BlockRegistry) *recipeData {
	all := recipe.Recipes()
	if !reflect.TypeOf(br).Comparable() {
		return newRecipeData(br, all)
	}
	recipeCache.Lock()
	defer recipeCache.Unlock()
	if d := recipeCache.data[br]; d != nil && d.count == len(all) {
		return d
	}
	d := newRecipeData(br, all)
	if recipeCache.data == nil {
		recipeCache.data = make(map[world.BlockRegistry]*recipeData)
	}
	recipeCache.data[br] = d
	return d
}

func newRecipeData(br world.BlockRegistry, all []recipe.Recipe) *recipeData {
	d := &recipeData{count: len(all), recipes: make(map[uint32]recipe.Recipe, len(all)), javaIDs: make(map[string][]string)}
	var (
		shapedRecipes            []protocol.ShapedRecipe
		shapelessRecipes         []protocol.ShapelessRecipe
		userDataShapelessRecipes []protocol.UserDataShapelessRecipe
		multiRecipes             []protocol.MultiRecipe
		smithingTransformRecipes []protocol.SmithingTransformRecipe
		smithingTrimRecipes      []protocol.SmithingTrimRecipe
		potionRecipes            []protocol.PotionRecipe
		potionContainerChange    []protocol.PotionContainerChangeRecipe
	)

	for index, i := range all {
		networkID := uint32(index) + 1
		d.recipes[networkID] = i

		switch i := i.(type) {
		case recipe.Shapeless:
			shapelessRecipes = append(shapelessRecipes, protocol.ShapelessRecipe{
				RecipeID:        d.registerJavaID(i),
				Priority:        int32(i.Priority()),
				Input:           stacksToIngredientItems(br, i.Input()),
				Output:          stacksToRecipeStacks(br, i.Output()),
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			})
		case recipe.UserDataShapeless:
			userDataShapelessRecipes = append(userDataShapelessRecipes, protocol.UserDataShapelessRecipe{ShapelessRecipe: protocol.ShapelessRecipe{
				RecipeID:        d.registerJavaID(i),
				Priority:        int32(i.Priority()),
				Input:           stacksToIngredientItems(br, i.Input()),
				Output:          stacksToRecipeStacks(br, i.Output()),
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			}})
		case recipe.Multi:
			d.registerJavaMultiID(i)
			multiRecipes = append(multiRecipes, protocol.MultiRecipe{
				UUID:            i.UUID(),
				RecipeNetworkID: networkID,
			})
		case recipe.Shaped:
			shapedRecipes = append(shapedRecipes, protocol.ShapedRecipe{
				RecipeID:        d.registerJavaID(i),
				Priority:        int32(i.Priority()),
				Width:           int32(i.Shape().Width()),
				Height:          int32(i.Shape().Height()),
				Input:           stacksToIngredientItems(br, i.Input()),
				Output:          stacksToRecipeStacks(br, i.Output()),
				Block:           i.Block(),
				AssumeSymmetry:  true,
				RecipeNetworkID: networkID,
			})
		case recipe.SmithingTransform:
			input, output := stacksToIngredientItems(br, i.Input()), stacksToRecipeStacks(br, i.Output())
			smithingTransformRecipes = append(smithingTransformRecipes, protocol.SmithingTransformRecipe{
				RecipeID:        d.registerJavaID(i),
				Base:            input[0],
				Addition:        input[1],
				Template:        input[2],
				Result:          output[0],
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			})
		case recipe.SmithingTrim:
			input := stacksToIngredientItems(br, i.Input())
			smithingTrimRecipes = append(smithingTrimRecipes, protocol.SmithingTrimRecipe{
				RecipeID:        d.registerJavaID(i),
				Base:            input[0],
				Addition:        input[1],
				Template:        input[2],
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			})
		case recipe.Potion:
			inputRuntimeID, inputMeta, _ := world.ItemRuntimeID(i.Input()[0].(item.Stack).Item())
			reagentRuntimeID, reagentMeta, _ := world.ItemRuntimeID(i.Input()[1].(item.Stack).Item())
			outputRuntimeID, outputMeta, _ := world.ItemRuntimeID(i.Output()[0].Item())

			potionRecipes = append(potionRecipes, protocol.PotionRecipe{
				InputPotionID:        inputRuntimeID,
				InputPotionMetadata:  int32(inputMeta),
				ReagentItemID:        reagentRuntimeID,
				ReagentItemMetadata:  int32(reagentMeta),
				OutputPotionID:       outputRuntimeID,
				OutputPotionMetadata: int32(outputMeta),
			})

		case recipe.PotionContainerChange:
			inputRuntimeID, _, _ := world.ItemRuntimeID(i.Input()[0].(item.Stack).Item())
			reagentRuntimeID, _, _ := world.ItemRuntimeID(i.Input()[1].(item.Stack).Item())
			outputRuntimeID, _, _ := world.ItemRuntimeID(i.Output()[0].Item())

			potionContainerChange = append(potionContainerChange, protocol.PotionContainerChangeRecipe{
				InputItemID:   inputRuntimeID,
				ReagentItemID: reagentRuntimeID,
				OutputItemID:  outputRuntimeID,
			})
		}
	}
	d.packet = &staticPacket{&packet.CraftingData{
		ShapedRecipes:                shapedRecipes,
		ShapelessRecipes:             shapelessRecipes,
		MultiRecipes:                 multiRecipes,
		UserDataShapelessRecipes:     userDataShapelessRecipes,
		SmithingTransformRecipes:     smithingTransformRecipes,
		SmithingTrimRecipes:          smithingTrimRecipes,
		PotionRecipes:                potionRecipes,
		PotionContainerChangeRecipes: potionContainerChange,
		ClearRecipes:                 true,
	}}
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

// creativeData is the CreativeContent of a block registry.
type creativeData struct {
	// groups and items are the numbers of creative groups and items
	// registered when it was made.
	groups, items int
	packet        *staticPacket
}

var creativeCache struct {
	sync.Mutex
	data map[world.BlockRegistry]*creativeData
}

// creativePacket returns the CreativeContent of a block registry, made
// again once more creative groups or items were registered.
func creativePacket(br world.BlockRegistry) packet.Packet {
	groups, items := creative.Groups(), creative.Items()
	if !reflect.TypeOf(br).Comparable() {
		return creativeContent(br, groups, items)
	}
	creativeCache.Lock()
	defer creativeCache.Unlock()
	if d := creativeCache.data[br]; d != nil && d.groups == len(groups) && d.items == len(items) {
		return d.packet
	}
	d := &creativeData{groups: len(groups), items: len(items), packet: &staticPacket{creativeContent(br, groups, items)}}
	if creativeCache.data == nil {
		creativeCache.data = make(map[world.BlockRegistry]*creativeData)
	}
	creativeCache.data[br] = d
	return d.packet
}

// creativeContent returns the creative groups and the items in them as
// protocol item stacks.
func creativeContent(br world.BlockRegistry, groups []creative.Group, items []creative.Item) *packet.CreativeContent {
	pk := &packet.CreativeContent{Groups: make([]protocol.CreativeGroup, 0, len(groups)), Items: make([]protocol.CreativeItem, 0, len(items))}
	for _, group := range groups {
		pk.Groups = append(pk.Groups, protocol.CreativeGroup{
			Category: group.Category.Uint8(),
			Name:     group.Name,
			Icon:     deleteDamage(stackFromItem(br, group.Icon)),
		})
	}
	for index, i := range items {
		group := slices.IndexFunc(groups, func(group creative.Group) bool {
			return group.Name == i.Group
		})
		if group < 0 {
			continue
		}
		pk.Items = append(pk.Items, protocol.CreativeItem{
			CreativeItemNetworkID: uint32(index) + 1,
			Item:                  deleteDamage(stackFromItem(br, i.Stack)),
			GroupIndex:            uint32(group),
		})
	}
	return pk
}
