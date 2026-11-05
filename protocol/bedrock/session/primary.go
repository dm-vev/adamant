package session

import (
	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// The primary protocol, [Network] PrimaryProtocol, is the protocol the
// session speaks: the packets and fields it uses and the packets of a
// client it acts on. It is no newer than the model, the codecs the session
// writes with. A field of the model the primary has not is left at its zero
// value, which stands for the behaviour of the primary, and the clients of
// other protocols get what the session writes converted to their own.
//
// The session uses the features of a protocol only once its primary is that
// protocol or a newer one. Those of 2225 (1.26.60), and what each primary
// does with them:
//
//   - ServerboundStonecutterSetRecipe, ServerboundCursorItemDrag,
//     ServerboundMatchmakingCancel, ServerboundRegisterAudioContent and
//     SetPassengerOfBlock from a client: a primary of 2225 ignores them, as
//     the game models no recipe chosen at a stonecutter, no drag of the
//     cursor, no matchmaking, no audio content and no entity seated on a
//     block; an older one does not know them and logs them as it logs every
//     packet it does not know.
//   - The Hand of an Animate: the game tells no hand of a swing, so it is the
//     main hand, 0, for every primary.
//   - PassengerOfBlock of AddActor and AddPlayer, SetPassengerOfBlock: the
//     game seats no entity on a block, so they are never sent.
//   - The cloud height and rendering of a custom dimension: zero for every
//     primary, as the step up from 2193 gives them, until BDS of 2225 shows
//     its own (open question Q2 of the design).
//   - The PlayFab ID of a player list entry is no feature: 2225 moved it out
//     of the skin, and the session fills both for every primary.
//
// The skins the session writes tell the version of the primary as the
// engine version of their geometry.

// protocol2225 is the number of protocol 2225, whose features the session
// uses once its primary is 2225 or newer.
const protocol2225 = 2225

// primary returns the protocol the session speaks.
func (s *Session) primary() *versions.Protocol {
	if s.conf.Primary != nil {
		return s.conf.Primary
	}
	return versions.NewestPrimary()
}

// speaks reports whether the primary protocol of the session is protocol n
// or a newer one, so that the session uses the features of n.
func (s *Session) speaks(n int32) bool { return s.primary().Number >= n }

// registerPrimaryHandlers registers the handlers of the packets of clients
// that the primary protocol of the session knows past those of 2193.
func (s *Session) registerPrimaryHandlers() {
	if !s.speaks(protocol2225) {
		return
	}
	for _, id := range []uint32{
		mc.IDServerboundStonecutterSetRecipe,
		mc.IDServerboundCursorItemDrag,
		mc.IDServerboundMatchmakingCancel,
		mc.IDServerboundRegisterAudioContent,
		mc.IDSetPassengerOfBlock,
	} {
		s.handlers[id] = nil
	}
}
