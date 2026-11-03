package bedrockadapter

import (
	"io"
	"log/slog"
	"net"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/query"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// TestQueryDataFromHost holds the answers to queries, read from the Host, to
// those the server gave when the query read the Server: over a server with
// two players, with the default status and with one that sets every field.
func TestQueryDataFromHost(t *testing.T) {
	for name, provider := range map[string]server.StatusProvider{"default": nil, "custom": customStatus{}} {
		t.Run(name, func(t *testing.T) {
			conf := server.Config{Name: "Query Test", MaxPlayers: 10, StatusProvider: provider}
			srv := newQueryServer(t, conf, "Bob", "Alice")

			got, want := queryData(srv, "127.0.0.1", 19132), oldQueryData(srv, provider, "127.0.0.1", 19132)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("query data = %+v, want %+v", got, want)
			}
			if got.PlayerCount != 2 || !reflect.DeepEqual(got.PlayerNames, []string{"Alice", "Bob"}) {
				t.Errorf("query data lists %d players %v, want Alice and Bob", got.PlayerCount, got.PlayerNames)
			}
		})
	}
}

// oldQueryData is buildQueryData as it was when the query read the Server,
// with the fields of the neutral Status in place of netconn's, and the
// methods of the Server in place of the fields it read: the default
// provider showed Config.Name, which the Server's Status shows. provider is
// the StatusProvider of the Config of srv.
func oldQueryData(srv *server.Server, provider server.StatusProvider, host string, port int) query.Data {
	playerCount := srv.PlayerCount()
	maxPlayers := srv.MaxPlayerCount()
	status := server.Status{Name: "Query Test", PlayerCount: playerCount, MaxPlayers: maxPlayers}
	if provider != nil {
		status = provider.ServerStatus(playerCount, maxPlayers)
	}
	worldName := ""
	if srv.World() != nil {
		worldName = srv.World().Name()
	}
	modeName := defaultGameModeName(srv.World())
	pluginString := ""
	if query.PluginListingEnabled() {
		pluginString = strings.Join(plugins(), ";")
	}
	difficulty := "NORMAL"

	playerNames := srv.PlayerNames()
	sort.Strings(playerNames)

	return query.Data{
		HostName:         status.Name,
		MOTD:             status.SubName,
		GameMode:         modeName,
		Difficulty:       difficulty,
		WorldName:        worldName,
		PlayerCount:      playerCount,
		MaxPlayers:       status.MaxPlayers,
		HostIP:           host,
		HostPort:         port,
		Plugins:          pluginString,
		PlayerNames:      playerNames,
		Version:          version.Minecraft,
		GameID:           "MINECRAFTPE",
		GameType:         defaultGameType(srv.World()),
		WhitelistEnabled: srv.WhitelistEnabled(),
	}
}

// newQueryServer starts a server of conf whose only listener is one of
// clienttest, with worlds that generate no terrain, and joins players of
// the names passed to it.
func newQueryServer(t *testing.T, conf server.Config, names ...string) *server.Server {
	t.Helper()
	l := clienttest.NewListener()
	conf.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	conf.PlayerProvider = player.NopProvider{}
	conf.DisableNether, conf.DisableEnd = true, true
	conf.Generator = func(world.Dimension, int64) world.Generator { return world.NopGenerator{} }
	conf.Listeners = []func(server.Config, server.Host) (server.Listener, error){func(server.Config, server.Host) (server.Listener, error) { return l, nil }}
	srv := conf.New()
	t.Cleanup(func() { _ = srv.Close() })
	spawned := make(chan struct{}, len(names))
	srv.Listen()
	go func() {
		for range srv.Accept() {
			spawned <- struct{}{}
		}
	}()
	for _, name := range names {
		l.Send(&clienttest.Join{L: client.Login{
			Identity: client.Identity{Name: name, UUID: uuid.New()},
			Locale:   language.English,
			Addr:     &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132},
		}})
		select {
		case <-spawned:
		case <-time.After(5 * time.Second):
			t.Fatalf("the server did not spawn %s", name)
		}
	}
	return srv
}

// customStatus is a StatusProvider that sets every field.
type customStatus struct{}

func (customStatus) ServerStatus(playerCount, maxPlayers int) server.Status {
	return server.Status{Name: "Name", SubName: "Sub", PlayerCount: playerCount + 1, MaxPlayers: maxPlayers + 2}
}
