package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// benchmarkPlayer runs b with a player without a client: the text the
// player sends is built and rendered for Bedrock as for one with a client,
// and the packet is then dropped, so the benchmarks measure the text alone.
func benchmarkPlayer(b *testing.B, f func(p *Player)) {
	w := world.Config{Synchronous: true}.New()
	b.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 64, 0.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos})).(*Player)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			f(p)
		}
	})
}

// BenchmarkPlayerMessage measures Message with a plain string and with a
// status line of seven § codes.
func BenchmarkPlayerMessage(b *testing.B) {
	b.Run("plain", func(b *testing.B) {
		benchmarkPlayer(b, func(p *Player) { p.Message("Welcome to the server") })
	})
	b.Run("coded", func(b *testing.B) {
		benchmarkPlayer(b, func(p *Player) { p.Message("§6Coins: §e1200 §7| §aOnline: §f42 §7| §bPing: §f35ms") })
	})
}

// BenchmarkMessaget measures Messaget with the join message.
func BenchmarkMessaget(b *testing.B) {
	benchmarkPlayer(b, func(p *Player) { p.Messaget(chat.MessageJoin, "Steve") })
}
