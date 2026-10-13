package entity

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestCommandTagsRoundTripThroughEntityNBT(t *testing.T) {
	var data world.EntityData
	CowType.DecodeNBT(map[string]any{}, &data)
	cow := &Cow{Ent: &Ent{data: &data}}
	cow.SetCommandTags([]string{"alpha", "beta"})
	if got := cow.CommandTags(); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("tags after add = %v", got)
	}
	got := CowType.EncodeNBT(&data)
	state := ReadNativeMobJavaState(got)
	if !reflect.DeepEqual(state["Tags"], []string{"alpha", "beta"}) {
		t.Fatalf("encoded Java tags = %#v", state["Tags"])
	}
	var decoded world.EntityData
	decoded.NativeNBT = got
	CowType.DecodeNBT(got, &decoded)
	reloaded := &Cow{Ent: &Ent{data: &decoded}}
	if tags := reloaded.CommandTags(); !reflect.DeepEqual(tags, []string{"alpha", "beta"}) {
		t.Fatalf("reloaded tags = %v", tags)
	}
	reloaded.SetCommandTags([]string{"beta"})
	if tags := reloaded.CommandTags(); !reflect.DeepEqual(tags, []string{"beta"}) {
		t.Fatalf("tags after remove = %v", tags)
	}
}
