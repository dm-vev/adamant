package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSignApplicatorsFollowJava follows SignBlock.useItemOn: dye and ink
// apply to the side the user faces only if it has text and the sign is not
// waxed.
func TestSignApplicatorsFollowJava(t *testing.T) {
	pos, user := cube.Pos{0, 64, 0}, mgl64.Vec3{0.5, 64.5, 5}
	written := Sign{Wood: OakWood(), Attach: StandingAttachment(0)}
	written.Front.Text, written.Back.Text = "hi", "hi"
	if _, ok := (Sign{Wood: OakWood()}).Dye(pos, user, item.ColourRed()); ok {
		t.Error("dyed a sign without text")
	}
	if _, ok := written.Dye(pos, user, item.ColourRed()); !ok {
		t.Error("could not dye a written sign")
	}
	waxed := written
	waxed.Waxed = true
	if _, ok := waxed.Dye(pos, user, item.ColourRed()); ok {
		t.Error("dyed a waxed sign")
	}
	if _, ok := waxed.Ink(pos, user, true); ok {
		t.Error("inked a waxed sign")
	}
	hanging := HangingSign{Wood: OakWood()}
	if _, ok := hanging.Ink(pos, user, true); ok {
		t.Error("inked a hanging sign without text")
	}
}
