package server

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/query"
	"github.com/df-mc/dragonfly/server/version"
)

// TestQueryDataFromHost holds the answers to queries, read from the Host, to
// those the server gave when the query read the Server: over a server with
// two players, with the default status and with one that sets every field.
func TestQueryDataFromHost(t *testing.T) {
	for name, provider := range map[string]StatusProvider{"default": nil, "custom": customStatus{}} {
		t.Run(name, func(t *testing.T) {
			jt := newJoinTest(t, player.NopProvider{}, Config{Name: "Query Test", MaxPlayers: 10, StatusProvider: provider})
			jt.start()
			jt.join(t, joinLogin("Bob", ""))
			jt.join(t, joinLogin("Alice", ""))

			got, want := queryData(jt.srv, "127.0.0.1", 19132), oldQueryData(jt.srv, "127.0.0.1", 19132)
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
// with the fields of the neutral Status in place of netconn's.
func oldQueryData(srv *Server, host string, port int) query.Data {
	playerCount := srv.PlayerCount()
	maxPlayers := srv.MaxPlayerCount()
	status := srv.conf.StatusProvider.ServerStatus(playerCount, maxPlayers)
	worldName := ""
	if srv.world != nil {
		worldName = srv.world.Name()
	}
	modeName := defaultGameModeName(srv.world)
	pluginString := ""
	if query.PluginListingEnabled() {
		pluginString = strings.Join(plugins(), ";")
	}
	difficulty := "NORMAL"

	playerNames := srv.players.names()
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
		GameType:         defaultGameType(srv.world),
		WhitelistEnabled: srv.WhitelistEnabled(),
	}
}

// customStatus is a StatusProvider that sets every field.
type customStatus struct{}

func (customStatus) ServerStatus(playerCount, maxPlayers int) Status {
	return Status{Name: "Name", SubName: "Sub", PlayerCount: playerCount + 1, MaxPlayers: maxPlayers + 2}
}
