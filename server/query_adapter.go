package server

import (
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server/query"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world"
)

// queryData assembles the Data structure consumed by the query package from
// the server host. It collects the dynamic server state while keeping the
// query implementation agnostic of the Server internals.
func queryData(h Host, host string, port int) query.Data {
	playerNames := h.PlayerNames()
	sort.Strings(playerNames)
	status := h.Status()
	w := h.World()
	worldName := ""
	if w != nil {
		worldName = w.Name()
	}
	modeName := defaultGameModeName(w)
	pluginString := ""
	if query.PluginListingEnabled() {
		pluginString = strings.Join(plugins(), ";")
	}
	difficulty := "NORMAL"

	return query.Data{
		HostName:         status.Name,
		MOTD:             status.SubName,
		GameMode:         modeName,
		Difficulty:       difficulty,
		WorldName:        worldName,
		PlayerCount:      len(playerNames),
		MaxPlayers:       status.MaxPlayers,
		HostIP:           host,
		HostPort:         port,
		Plugins:          pluginString,
		PlayerNames:      playerNames,
		Version:          version.Minecraft,
		GameID:           "MINECRAFTPE",
		GameType:         defaultGameType(w),
		WhitelistEnabled: h.WhitelistEnabled(),
	}
}

// defaultGameModeName translates the configured default game mode into the
// textual representation required by query clients.
func defaultGameModeName(w *world.World) string {
	if w == nil {
		return "SURVIVAL"
	}
	if id, ok := world.GameModeID(w.DefaultGameMode()); ok {
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
func defaultGameType(w *world.World) string {
	if w == nil {
		return "SMP"
	}
	if id, ok := world.GameModeID(w.DefaultGameMode()); ok && id == 1 {
		return "CMP"
	}
	return "SMP"
}

// plugins returns the names of active plugins. The function remains in place so
// that the query adapter can be wired into a future plugin system.
func plugins() []string {
	// TODO: Wire up plugin discovery once an explicit plugin system is available.
	return nil
}
