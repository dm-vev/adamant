package player

import (
	"reflect"
	"testing"
)

func TestCommandTagsPersistInPlayerJavaState(t *testing.T) {
	p := &Player{playerData: &playerData{wardenSpawnTracker: decodePlayerWardenTracker(nil)}}
	p.SetCommandTags([]string{"alpha", "beta"})
	if got := p.CommandTags(); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("tags after add = %v", got)
	}
	state := encodePlayerJavaState(p)
	if !reflect.DeepEqual(state["Tags"], []string{"alpha", "beta"}) {
		t.Fatalf("encoded player tags = %#v", state["Tags"])
	}
	p2 := &Player{playerData: &playerData{javaState: state, wardenSpawnTracker: decodePlayerWardenTracker(nil)}}
	p2.SetCommandTags([]string{"beta"})
	if got := p2.CommandTags(); !reflect.DeepEqual(got, []string{"beta"}) {
		t.Fatalf("tags after remove = %v", got)
	}
}
