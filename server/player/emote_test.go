package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// TestPlayerShowsItsEmote checks that Emote shows the emote to the viewers
// of the position of the player, once each, with the player and the emote:
// a viewer near the player and the client of the player, which views it as
// a Bedrock session does, see it; a viewer far away does not.
func TestPlayerShowsItsEmote(t *testing.T) {
	w, near := clientTestWorld(t)
	far, self := clienttest.New(), clienttest.New()
	farLoader, selfLoader := world.NewLoader(2, w, far), world.NewLoader(2, w, self)
	t.Cleanup(func() {
		<-w.Exec(func(tx *world.Tx) {
			farLoader.Close(tx)
			selfLoader.Close(tx)
		})
	})
	<-w.Exec(func(tx *world.Tx) {
		farLoader.Move(tx, mgl64.Vec3{1000.5, 100, 1000.5})
		farLoader.Load(tx, 25)
		selfLoader.Move(tx, mgl64.Vec3{0.5, 100, 0.5})
		selfLoader.Load(tx, 25)
	})

	emote := uuid.New()
	withClientPlayer(t, w, self, func(_ *world.Tx, p *Player) {
		near.Reset()
		far.Reset()
		self.Reset()
		p.Emote(emote)

		for name, rec := range map[string]*clienttest.Recorder{"the viewer near": near, "the client of the player": self} {
			calls := rec.CallsTo("ViewEmote")
			if len(calls) != 1 {
				t.Errorf("%s was shown the emote %d times, want once", name, len(calls))
				continue
			}
			if calls[0].Args[0] != p || calls[0].Args[1] != emote {
				t.Errorf("%s was shown %v, want the emote %v of the player", name, calls[0].Args, emote)
			}
		}
		if calls := far.CallsTo("ViewEmote"); len(calls) != 0 {
			t.Errorf("the viewer far away was shown the emote %d times, want never", len(calls))
		}
	})
}

// TestPlayerShowsItsEmoteOnlyToViewers checks that Emote does not show the
// emote to the client of the player when it does not view the position of
// the player: unlike other broadcasts of the player, the client is not
// added to the viewers.
func TestPlayerShowsItsEmoteOnlyToViewers(t *testing.T) {
	w, near := clientTestWorld(t)
	self := clienttest.New()
	withClientPlayer(t, w, self, func(_ *world.Tx, p *Player) {
		near.Reset()
		self.Reset()
		p.Emote(uuid.New())
		if n := len(near.CallsTo("ViewEmote")); n != 1 {
			t.Errorf("the viewer near was shown the emote %d times, want once", n)
		}
		if n := len(self.CallsTo("ViewEmote")); n != 0 {
			t.Errorf("the client of the player, no viewer, was shown the emote %d times, want never", n)
		}
	})
}
