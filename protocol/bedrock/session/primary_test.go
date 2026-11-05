package session

import (
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/server/player/skin"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// TestPrimaryGatesFeatures checks the features of 2225 are off with a primary
// of 2193, and on with a primary of 2225, which none is too: the packets of
// 2225's clients the game has no use for are ignored rather than unknown,
// and skins tell the version of the primary.
func TestPrimaryGatesFeatures(t *testing.T) {
	ids := []uint32{mc.IDServerboundStonecutterSetRecipe, mc.IDServerboundCursorItemDrag, mc.IDServerboundMatchmakingCancel,
		mc.IDServerboundRegisterAudioContent, mc.IDSetPassengerOfBlock}
	for _, c := range []struct {
		primary *versions.Protocol
		speaks  bool
		engine  string
	}{
		{nil, true, "1.26.60"},
		{oldProtocol(), false, "1.26.50"},
		{versions.Model(), true, "1.26.60"},
	} {
		s := testSession("Primary")
		s.conf.Primary = c.primary
		s.registerHandlers()
		if s.speaks(protocol2225) != c.speaks {
			t.Errorf("primary %v: speaks 2225 %v", c.primary, s.speaks(protocol2225))
		}
		for _, id := range ids {
			if _, known := s.handlers[id]; known != c.speaks {
				t.Errorf("primary %v: packet %d known %v", c.primary, id, known)
			}
		}
		entry := s.newListEntry(skin.New(64, 32)).Packet.(*mc.PlayerListPacket).Entries[0]
		if got := string(entry.Skin.GeometryDataEngineVersion); got != c.engine {
			t.Errorf("primary %v: skins of the engine of %s", c.primary, got)
		}
	}
}
