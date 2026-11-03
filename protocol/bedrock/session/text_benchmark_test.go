package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/text"
)

// BenchmarkSessionSendTip measures what a tip costs the session beyond its
// packet: the string a player is given read as text and rendered back, for a
// plain string and for a status line of seven § codes. Nop drops the packet.
func BenchmarkSessionSendTip(b *testing.B) {
	for name, s := range map[string]string{
		"plain": "Welcome to the server",
		"coded": "§6Coins: §e1200 §7| §aOnline: §f42 §7| §bPing: §f35ms",
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				Nop.SendTip(text.ParseLegacy(s))
			}
		})
	}
}
