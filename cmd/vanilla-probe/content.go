package main

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/nbt"
)

// The dumps of content below name items by their identifiers rather than
// their runtime IDs, which differ from protocol to protocol, so that what
// two versions of BDS send compares, and what the adapter sends compares
// with what BDS does. They are checks, not data the binary embeds.

// creativeItem is an item of the creative inventory.
type creativeItem struct {
	Group    int    `json:"group"`
	Category uint8  `json:"category"`
	Name     string `json:"group_name,omitempty"`
	Item     string `json:"item"`
	Meta     uint32 `json:"meta,omitempty"`
	Count    uint16 `json:"count"`
	NBT      bool   `json:"nbt,omitempty"`
}

// creativeDump returns the items of the creative inventory, in its order.
func creativeDump(pk *mc.CreativeContentPacket, names map[int32]string) []creativeItem {
	out := make([]creativeItem, 0, len(pk.Items))
	for _, it := range pk.Items {
		entry := creativeItem{Group: int(it.GroupIndex), Item: itemName(names, it.Item.NetworkID), Meta: it.Item.MetadataValue, Count: it.Item.Count, NBT: len(it.Item.NBTData) > 0}
		if g := int(it.GroupIndex); g < len(pk.Groups) {
			entry.Category, entry.Name = pk.Groups[g].Category, pk.Groups[g].Name
		}
		out = append(out, entry)
	}
	return out
}

// recipe is a recipe of the crafting data: its kind, its ID and the block
// it is made at, and its inputs and outputs as "count name:meta", "count
// #tag" or "count ?expression".
type recipe struct {
	Kind   string   `json:"kind"`
	ID     string   `json:"id,omitempty"`
	Block  string   `json:"block,omitempty"`
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

// craftingDump returns the recipes of the crafting data, sorted.
func craftingDump(pk *mc.CraftingDataPacket, names map[int32]string) []recipe {
	var out []recipe
	in := func(l ...mc.ItemDescriptorCount) []string {
		s := make([]string, len(l))
		for i, d := range l {
			s[i] = descriptor(d)
		}
		return s
	}
	res := func(l ...mc.ItemStack) []string {
		s := make([]string, len(l))
		for i, it := range l {
			s[i] = fmt.Sprintf("%d %s:%d", it.Count, itemName(names, it.NetworkID), it.MetadataValue)
		}
		return s
	}
	shaped := func(kind string, r mc.ShapedRecipe) recipe {
		return recipe{Kind: fmt.Sprintf("%s %dx%d", kind, r.Width, r.Height), ID: r.RecipeID, Block: r.Block, Input: in(r.Input...), Output: res(r.Output...)}
	}
	shapeless := func(kind string, r mc.ShapelessRecipe) recipe {
		return recipe{Kind: kind, ID: r.RecipeID, Block: r.Block, Input: in(r.Input...), Output: res(r.Output...)}
	}
	for _, r := range pk.ShapedRecipes {
		out = append(out, shaped("shaped", r))
	}
	for _, r := range pk.ShapedChemistryRecipes {
		out = append(out, shaped("shaped chemistry", r.ShapedRecipe))
	}
	for _, r := range pk.ShapelessRecipes {
		out = append(out, shapeless("shapeless", r))
	}
	for _, r := range pk.UserDataShapelessRecipes {
		out = append(out, shapeless("shapeless user data", r.ShapelessRecipe))
	}
	for _, r := range pk.ShapelessChemistryRecipes {
		out = append(out, shapeless("shapeless chemistry", r.ShapelessRecipe))
	}
	for _, r := range pk.SmithingTransformRecipes {
		out = append(out, recipe{Kind: "smithing transform", ID: r.RecipeID, Block: r.Block, Input: in(r.Template, r.Base, r.Addition), Output: res(r.Result)})
	}
	for _, r := range pk.SmithingTrimRecipes {
		out = append(out, recipe{Kind: "smithing trim", ID: r.RecipeID, Block: r.Block, Input: in(r.Template, r.Base, r.Addition)})
	}
	for _, r := range pk.MultiRecipes {
		out = append(out, recipe{Kind: "multi", ID: fmt.Sprintf("%x", r.UUID)})
	}
	for _, r := range pk.PotionRecipes {
		out = append(out, recipe{Kind: "potion", Input: []string{
			fmt.Sprintf("%s:%d", itemName(names, r.InputPotionID), r.InputPotionMetadata),
			fmt.Sprintf("%s:%d", itemName(names, r.ReagentItemID), r.ReagentItemMetadata),
		}, Output: []string{fmt.Sprintf("%s:%d", itemName(names, r.OutputPotionID), r.OutputPotionMetadata)}})
	}
	for _, r := range pk.PotionContainerChangeRecipes {
		out = append(out, recipe{Kind: "potion container", Input: []string{itemName(names, r.InputItemID), itemName(names, r.ReagentItemID)},
			Output: []string{itemName(names, r.OutputItemID)}})
	}
	slices.SortFunc(out, func(a, b recipe) int { return strings.Compare(a.key(), b.key()) })
	return out
}

// key returns all of a recipe as one string, by which recipes sort.
func (r recipe) key() string {
	return strings.Join([]string{r.Kind, r.ID, r.Block, strings.Join(r.Input, ","), strings.Join(r.Output, ",")}, "\x00")
}

// descriptor returns an input of a recipe as a recipe dump holds it.
func descriptor(d mc.ItemDescriptorCount) string {
	switch v := d.Descriptor.(type) {
	case *mc.DefaultItemDescriptor:
		return fmt.Sprintf("%d %s:%d", d.Count, v.Name, v.MetadataValue)
	case *mc.ItemTagItemDescriptor:
		return fmt.Sprintf("%d #%s", d.Count, v.Tag)
	case *mc.MoLangItemDescriptor:
		return fmt.Sprintf("%d ?%s", d.Count, v.Expression)
	case nil, *mc.InvalidItemDescriptor:
		return fmt.Sprintf("%d none", d.Count)
	}
	return fmt.Sprintf("%d %T", d.Count, d.Descriptor)
}

// itemName returns the identifier of an item by its runtime ID in the
// registry, or the ID in brackets for one it has not.
func itemName(names map[int32]string, id int32) string {
	if id == 0 {
		return "air"
	}
	if name, ok := names[id]; ok {
		return name
	}
	return fmt.Sprintf("[%d]", id)
}

// itemNames returns the identifiers of the items of a registry by their
// runtime IDs.
func itemNames(items []mc.ItemEntry) map[int32]string {
	names := make(map[int32]string, len(items))
	for _, it := range items {
		names[int32(it.RuntimeID)] = it.Name
	}
	return names
}

// entityDump returns the identifiers of the entity types of an
// AvailableActorIdentifiers, in its order.
func entityDump(pk *mc.AvailableActorIdentifiersPacket) ([]string, error) {
	var list map[string]any
	if err := nbt.NewDecoderWithEncoding(bytes.NewReader(pk.SerialisedEntityIdentifiers), nbt.NetworkLittleEndian).Decode(&list); err != nil {
		return nil, fmt.Errorf("entity identifiers: %w", err)
	}
	entries, _ := list["idlist"].([]any)
	out := make([]string, 0, len(entries))
	for i, e := range entries {
		id, _ := e.(map[string]any)["id"].(string)
		if id == "" {
			return nil, fmt.Errorf("entity identifier %d has no id", i)
		}
		out = append(out, id)
	}
	return out, nil
}
