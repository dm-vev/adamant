package item

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Bundle is a container item with up to one item-equivalent of contents.
type Bundle struct {
	contents []Stack
	selected int
	Colour   Colour
	Dyed     bool
}

// MaxCount returns the Java bundle stack limit.
func (Bundle) MaxCount() int { return 1 }

// Requirements returns the items required to use the bundle.
func (Bundle) Requirements() []Stack { return nil }

// Release requires no separate action after the bundle's use ticks.
func (Bundle) Release(Releaser, *world.Tx, *UseContext, time.Duration) {}

// UseTick throws one item from the selected stack at the Java bundle-use cadence.
func (b Bundle) UseTick(tx *world.Tx, user User, stack Stack, ticks int) Stack {
	if ticks != 1 && (ticks <= 10 || ticks%2 != 0) {
		return stack
	}
	dropper, ok := user.(interface{ Drop(Stack) int })
	if !ok {
		return stack
	}
	updated, removed, ok := b.removeOne(stack)
	if !ok || dropper.Drop(removed) == 0 {
		return stack
	}
	tx.PlaySound(user.Position(), sound.Custom{Name: "item.bundle.remove_one", Volume: 0.8, Pitch: 1})
	return updated
}

// UseDurationTicks returns the Java bundle use duration.
func (Bundle) UseDurationTicks() int { return 200 }

// EncodeItem returns the Bedrock item identifier.
func (b Bundle) EncodeItem() (string, int16) {
	if b.Dyed {
		return "minecraft:" + b.Colour.String() + "_bundle", 0
	}
	return "minecraft:bundle", 0
}

// BundleContents stores the item stacks carried by a bundle and the currently selected stack.
type BundleContents struct {
	Items    []Stack
	Selected int
}

func (Bundle) Contents(stack Stack) BundleContents {
	bundle, ok := stack.Item().(Bundle)
	if !ok || len(bundle.contents) == 0 {
		return BundleContents{Selected: -1}
	}
	return BundleContents{Items: slices.Clone(bundle.contents), Selected: bundle.selected}
}

// WithContents returns a stack containing the supplied immutable contents.
func (Bundle) WithContents(stack Stack, contents BundleContents) Stack {
	contents.Items = slices.Clone(contents.Items)
	if len(contents.Items) == 0 {
		contents.Selected = -1
	} else if contents.Selected < -1 {
		contents.Selected = -1
	} else if contents.Selected >= len(contents.Items) {
		contents.Selected = len(contents.Items) - 1
	}
	bundle, ok := stack.Item().(Bundle)
	if !ok {
		bundle = Bundle{}
	}
	bundle.contents, bundle.selected = contents.Items, contents.Selected
	return stack.WithItem(bundle)
}

// Weight returns the bundle fullness as a fraction from zero through one.
func (b Bundle) Weight(stack Stack) float64 {
	contents := b.Contents(stack)
	var weight float64
	for _, content := range contents.Items {
		weight += b.itemWeight(content) * float64(content.Count())
	}
	return math.Min(weight, 1)
}

// Insert adds as many items as fit and returns the updated bundle and the unconsumed input.
func (b Bundle) Insert(bundle, incoming Stack, count int) (Stack, Stack) {
	if count <= 0 || incoming.Empty() {
		return bundle, incoming
	}
	count = min(count, incoming.Count())
	contents := b.Contents(bundle)
	candidate := incoming.Grow(count - incoming.Count())
	itemWeight := b.itemWeight(candidate)
	if itemWeight <= 0 {
		return bundle, incoming
	}
	space := 1 - b.Weight(bundle)
	toAdd := min(count, int(math.Floor((space+1e-9)/itemWeight)))
	if toAdd <= 0 {
		return bundle, incoming
	}
	candidate = candidate.Grow(toAdd - candidate.Count())
	mergeIndex := -1
	for i := range contents.Items {
		if contents.Items[i].Comparable(candidate) && contents.Items[i].Count()+candidate.Count() <= contents.Items[i].MaxCount() {
			mergeIndex = i
			break
		}
	}
	if mergeIndex >= 0 {
		merged := contents.Items[mergeIndex].Grow(candidate.Count())
		contents.Items = slices.Delete(contents.Items, mergeIndex, mergeIndex+1)
		contents.Items = append([]Stack{merged}, contents.Items...)
		if contents.Selected >= 0 {
			if contents.Selected == mergeIndex {
				contents.Selected = 0
			} else {
				if contents.Selected > mergeIndex {
					contents.Selected--
				}
				contents.Selected++
			}
		}
	} else {
		contents.Items = append([]Stack{candidate}, contents.Items...)
		if contents.Selected >= 0 {
			contents.Selected++
		}
	}
	return b.WithContents(bundle, contents), incoming.Grow(-toAdd)
}

// removeOne ports BundleContents.Mutable.removeOne: the whole selected stack,
// or the first if none is selected, leaves the bundle.
func (b Bundle) removeOne(bundle Stack) (Stack, Stack, bool) {
	contents := b.Contents(bundle)
	if contents.Selected >= 0 && contents.Selected < len(contents.Items) {
		return b.RemoveSelectedCount(bundle, contents.Items[contents.Selected].Count())
	}
	if len(contents.Items) == 0 {
		return bundle, Stack{}, false
	}
	return b.RemoveFirstCount(bundle, contents.Items[0].Count())
}

func (b Bundle) RemoveSelected(bundle Stack) (Stack, Stack, bool) {
	return b.RemoveFirstCount(bundle, 1)
}

// RemoveFirstCount removes up to count items from the first bundle stack.
func (b Bundle) RemoveFirstCount(bundle Stack, count int) (Stack, Stack, bool) {
	contents := b.Contents(bundle)
	if len(contents.Items) == 0 || count <= 0 {
		return bundle, Stack{}, false
	}
	selected := contents.Items[0]
	count = min(count, selected.Count())
	removed := selected.Grow(count - selected.Count())
	if count == selected.Count() {
		contents.Items = slices.Delete(contents.Items, 0, 1)
	} else {
		contents.Items[0] = selected.Grow(-count)
	}
	contents.Selected = -1
	return b.WithContents(bundle, contents), removed, true
}

// RemoveSelectedCount removes up to count items from the selected bundle stack.
func (b Bundle) RemoveSelectedCount(bundle Stack, count int) (Stack, Stack, bool) {
	contents := b.Contents(bundle)
	if len(contents.Items) == 0 || contents.Selected < 0 || contents.Selected >= len(contents.Items) || count <= 0 {
		return bundle, Stack{}, false
	}
	selected := contents.Items[contents.Selected]
	count = min(count, selected.Count())
	removed := selected.Grow(count - selected.Count())
	if count == selected.Count() {
		contents.Items = slices.Delete(contents.Items, contents.Selected, contents.Selected+1)
	} else {
		contents.Items[contents.Selected] = selected.Grow(-count)
	}
	contents.Selected = -1
	return b.WithContents(bundle, contents), removed, true
}

// Select changes the selected contained stack by the requested offset.
func (b Bundle) Select(bundle Stack, offset int) Stack {
	contents := b.Contents(bundle)
	if len(contents.Items) == 0 {
		return bundle
	}
	if contents.Selected < 0 {
		contents.Selected = 0
	} else {
		contents.Selected = (contents.Selected + offset) % len(contents.Items)
		if contents.Selected < 0 {
			contents.Selected += len(contents.Items)
		}
	}
	return b.WithContents(bundle, contents)
}

// itemWeight ports BundleContents.getWeight: a nested bundle weighs 1/16 more
// than its contents, a hive with bees fills the bundle, and other items weigh
// one over their stack size. Items that cannot fit inside container items,
// shulker boxes, weigh nothing so none fit.
func (b Bundle) itemWeight(stack Stack) float64 {
	if stack.Empty() || !canFitInsideContainerItems(stack.Item()) {
		return 0
	}
	if nested, ok := stack.Item().(Bundle); ok {
		return 1.0/16 + nested.Weight(stack)
	}
	if hive, ok := stack.Item().(interface{ HasBees() bool }); ok && hive.HasBees() {
		return 1
	}
	return 1 / float64(stack.MaxCount())
}

// canFitInsideContainerItems ports Item.canFitInsideContainerItems: every
// item but shulker boxes, as BlockItem decides.
func canFitInsideContainerItems(it world.Item) bool {
	name, _ := it.EncodeItem()
	return !strings.HasSuffix(name, "shulker_box")
}

// EncodeNBT preserves nested item stacks across save/load.
func (b Bundle) EncodeNBT() map[string]any {
	items := make([]any, 0, len(b.contents))
	for _, stack := range b.contents {
		items = append(items, encodeBundleStack(stack))
	}
	return map[string]any{"Items": items}
}

// DecodeNBT restores nested stacks from bundle item data.
func (b Bundle) DecodeNBT(data map[string]any) any {
	contents := BundleContents{Selected: -1}
	registry := world.BlockRegistryFromNBT(data)
	add := func(raw any) {
		encoded, ok := raw.(map[string]any)
		if !ok {
			return
		}
		if stack := decodeBundleStack(encoded, registry); !stack.Empty() {
			contents.Items = append(contents.Items, stack)
		}
	}
	switch items := data["Items"].(type) {
	case []any:
		for _, raw := range items {
			add(raw)
		}
	case []map[string]any:
		for _, encoded := range items {
			add(encoded)
		}
	}
	if len(contents.Items) == 0 {
		contents.Selected = -1
	}
	b.contents, b.selected = contents.Items, contents.Selected
	return b

}
func encodeBundleStack(stack Stack) map[string]any {
	return WriteNBT(stack, true)
}

func decodeBundleStack(data map[string]any, registry world.BlockRegistry) Stack {
	return ReadNBT(data, nil, registry)
}
