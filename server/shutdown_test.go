package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"golang.org/x/text/language"
)

// shutdownText resolves to no key: it is shown as it is.
type shutdownText string

func (s shutdownText) Resolve(language.Tag) string { return string(s) }

// TestCloseDisconnectsWithShutdownMessage checks that closing the server
// disconnects the players online with the ShutdownMessage of its Config,
// and with chat.MessageServerDisconnect if none is set.
func TestCloseDisconnectsWithShutdownMessage(t *testing.T) {
	restart := chat.Translate(shutdownText("The server restarts."), 0, "The server restarts.")
	for name, tc := range map[string]struct {
		conf chat.Translation
		want chat.Translation
	}{
		"configured": {conf: restart, want: restart},
		"default":    {want: chat.MessageServerDisconnect},
	} {
		t.Run(name, func(t *testing.T) {
			srv := Config{
				Log:                     slog.New(slog.NewTextHandler(io.Discard, nil)),
				DisableResourceBuilding: true,
				DisableNether:           true,
				DisableEnd:              true,
				ShutdownMessage:         tc.conf,
				Generator:               func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
			}.New()
			closeWorlds(t, srv)

			rec := clienttest.New()
			handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}.New(player.Type, player.Config{
				Name: "Shutdown", Locale: language.English, Client: rec,
			})
			srv.players.add(&onlinePlayer{handle: handle, id: client.Identity{Name: "Shutdown", UUID: handle.UUID()}})
			<-srv.world.Exec(func(tx *world.Tx) { tx.AddEntity(handle) })

			srv.Listen()
			if err := srv.Close(); err != nil {
				t.Fatal(err)
			}
			calls := rec.CallsTo("Disconnect")
			if len(calls) != 1 {
				t.Fatalf("the client was disconnected %d times, want once", len(calls))
			}
			want := tc.want.F().Text(language.English)
			if got := calls[0].Args[0].(text.Text); !text.Equal(got, want) {
				t.Fatalf("the client was disconnected with %#v, want %#v", got, want)
			}
		})
	}
}
