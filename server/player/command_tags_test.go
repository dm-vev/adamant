package player

import (
	"reflect"
	"testing"
)

func TestCommandTagsPersistInPlayerExtraState(t *testing.T) {
	t.Parallel()
	p := &Player{playerData: &playerData{wardenSpawnTracker: decodePlayerWardenTracker(nil)}}
	p.SetCommandTags([]string{"alpha", "beta"})
	if got := p.CommandTags(); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("tags after add = %v", got)
	}
	state := encodePlayerExtraState(p)
	if !reflect.DeepEqual(state["Tags"], []string{"alpha", "beta"}) {
		t.Fatalf("encoded player tags = %#v", state["Tags"])
	}
	p2 := &Player{playerData: &playerData{extraState: state, wardenSpawnTracker: decodePlayerWardenTracker(nil)}}
	p2.SetCommandTags([]string{"beta"})
	if got := p2.CommandTags(); !reflect.DeepEqual(got, []string{"beta"}) {
		t.Fatalf("tags after remove = %v", got)
	}
}
