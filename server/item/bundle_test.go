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
