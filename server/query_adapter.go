package server

import (
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server/query"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// registerQueryServer exposes the Server instance to the Bedrock query listener.
func registerQueryServer(srv *Server) {
	query.RegisterProvider(func(host string, port int) query.Data {
		return srv.buildQueryData(host, port)
	})
}

// buildQueryData assembles the Data structure consumed by the query package.
// It collects the dynamic server state while keeping the query implementation
// agnostic of the Server internals.
func (srv *Server) buildQueryData(host string, port int) query.Data {
	playerCount := srv.PlayerCount()
	maxPlayers := srv.MaxPlayerCount()
	status := srv.conf.StatusProvider.ServerStatus(playerCount, maxPlayers)
	worldName := ""
	if srv.world != nil {
		worldName = srv.world.Name()
	}
	modeName := defaultGameModeName(srv)
	pluginString := ""
	if query.PluginListingEnabled() {
		pluginString = strings.Join(srv.plugins(), ";")
	}
	difficulty := "NORMAL"

	srv.pmu.RLock()
	playerNames := make([]string, 0, len(srv.p))
	for _, p := range srv.p {
		playerNames = append(playerNames, p.name)
	}
	srv.pmu.RUnlock()
	sort.Strings(playerNames)

	return query.Data{
		HostName:         status.ServerName,
		MOTD:             status.ServerSubName,
		GameMode:         modeName,
		Difficulty:       difficulty,
		WorldName:        worldName,
		PlayerCount:      playerCount,
		MaxPlayers:       status.MaxPlayers,
		HostIP:           host,
		HostPort:         port,
		Plugins:          pluginString,
		PlayerNames:      playerNames,
		Version:          protocol.CurrentVersion,
		GameID:           "MINECRAFTPE",
		GameType:         defaultGameType(srv),
		WhitelistEnabled: srv.WhitelistEnabled(),
	}
}

// defaultGameModeName translates the configured default game mode into the
// textual representation required by query clients.
func defaultGameModeName(srv *Server) string {
	if srv == nil || srv.world == nil {
		return "SURVIVAL"
	}
	if id, ok := world.GameModeID(srv.world.DefaultGameMode()); ok {
		switch id {
		case 0:
			return "SURVIVAL"
		case 1:
			return "CREATIVE"
		case 2:
			return "ADVENTURE"
		case 3:
			return "SPECTATOR"
		}
	}
	return "SURVIVAL"
}

// defaultGameType returns the Nukkit-style game type value.
//
// Lumi reports "CMP" when the default game mode is creative and "SMP" otherwise.
func defaultGameType(srv *Server) string {
	if srv == nil || srv.world == nil {
		return "SMP"
	}
	if id, ok := world.GameModeID(srv.world.DefaultGameMode()); ok && id == 1 {
		return "CMP"
	}
	return "SMP"
}

// plugins returns the names of active plugins. The function remains in place so
// that the query adapter can be wired into a future plugin system.
func (srv *Server) plugins() []string {
	// TODO: Wire up plugin discovery once an explicit plugin system is available.
	return nil
}
