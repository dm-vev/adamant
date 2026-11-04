package block

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestCommandBlockEdited(t *testing.T) {
	b := CommandBlock{Facing: cube.FaceEast, Mode: CommandBlockRepeat, Powered: true, LastOutput: "out", SuccessCount: 2}
	e := CommandBlockEdit{Mode: CommandBlockChain, Conditional: true, NeedsRedstone: true, Command: "say", Name: "n", LastOutput: "ignored", TrackOutput: true, TickDelay: 7, ExecuteOnFirstTick: true}
	got := b.Edited(e)
	want := CommandBlock{Facing: cube.FaceEast, Mode: CommandBlockChain, Conditional: true, NeedsRedstone: true, Powered: true, Command: "say", CustomName: "n", LastOutput: "out", TrackOutput: true, SuccessCount: 2, TickDelay: 7, ExecuteOnFirstTick: true}
	if got != want {
		t.Fatalf("Edited = %#v, want %#v", got, want)
	}
	e.TrackOutput = false
	if got := b.Edited(e); got.LastOutput != "" {
		t.Fatalf("an edit that does not track the output kept %q", got.LastOutput)
	}
}

func TestCommandBlockEditValidText(t *testing.T) {
	long := strings.Repeat("a", MaxCommandBlockText+1)
	if !(CommandBlockEdit{Command: long[1:], Name: long[1:], LastOutput: long[1:]}).ValidText() {
		t.Fatal("texts of MaxCommandBlockText bytes are not valid")
	}
	for _, e := range []CommandBlockEdit{{Command: long}, {Name: long}, {LastOutput: long}} {
		if e.ValidText() {
			t.Fatalf("%d bytes are valid", len(long))
		}
	}
}
