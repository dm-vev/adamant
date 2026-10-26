package item

import "testing"

func TestBundleCapacityAndNBTContents(t *testing.T) {
	bundle := Bundle{}
	bundleStack := NewStack(bundle, 1)
	input := NewStack(Emerald{}, 64)
	filled, remainder := bundle.Insert(bundleStack, input, 64)
	if !remainder.Empty() {
		t.Fatalf("remaining stack count = %d, want 0", remainder.Count())
	}
	if weight := bundle.Weight(filled); weight != 1 {
		t.Fatalf("bundle weight = %v, want 1", weight)
	}
	_, remainder = bundle.Insert(filled, NewStack(Emerald{}, 1), 1)
	if remainder.Count() != 1 {
		t.Fatalf("over-capacity insertion remainder = %d, want 1", remainder.Count())
	}

	restored, ok := bundle.DecodeNBT(filled.Item().(Bundle).EncodeNBT()).(Bundle)
	if !ok {
		t.Fatal("bundle NBT decoded to a non-bundle item")
	}
	contents := restored.Contents(NewStack(restored, 1))
	if len(contents.Items) != 1 || contents.Items[0].Item().(Emerald) != (Emerald{}) || contents.Items[0].Count() != 64 {
		t.Fatalf("restored bundle contents = %#v", contents)
	}
}

type testShulkerBox struct{}

func (testShulkerBox) EncodeItem() (string, int16) { return "minecraft:red_shulker_box", 0 }
func (testShulkerBox) MaxCount() int               { return 1 }

type testHive struct{ bees bool }

func (testHive) EncodeItem() (string, int16) { return "minecraft:beehive", 0 }
func (h testHive) HasBees() bool             { return h.bees }

// TestBundleWeightsFollowJava follows BundleContents: shulker boxes cannot go
// in a bundle, a hive with bees fills it and an empty hive weighs 1/64.
func TestBundleWeightsFollowJava(t *testing.T) {
	bundle := Bundle{}
	if _, left := bundle.Insert(NewStack(bundle, 1), NewStack(testShulkerBox{}, 1), 1); left.Count() != 1 {
		t.Fatal("a shulker box went into a bundle")
	}
	if filled, _ := bundle.Insert(NewStack(bundle, 1), NewStack(testHive{bees: true}, 1), 1); bundle.Weight(filled) != 1 {
		t.Fatalf("hive with bees weighs %v, want 1", bundle.Weight(filled))
	}
	if filled, _ := bundle.Insert(NewStack(bundle, 1), NewStack(testHive{}, 1), 1); bundle.Weight(filled) != 1.0/64 {
		t.Fatalf("empty hive weighs %v, want 1/64", bundle.Weight(filled))
	}
}

// TestBundleDropsWholeStacks follows BundleContents.Mutable.removeOne: a
// throw takes the whole first stack out.
func TestBundleDropsWholeStacks(t *testing.T) {
	bundle := Bundle{}
	filled, _ := bundle.Insert(NewStack(bundle, 1), NewStack(Emerald{}, 10), 10)
	_, removed, ok := bundle.removeOne(filled)
	if !ok || removed.Count() != 10 {
		t.Fatalf("one throw removed %v emeralds, want 10", removed.Count())
	}
}
