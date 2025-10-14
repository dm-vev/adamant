package query

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Data summarises the information returned by the query responder. The
// structure is intentionally high level so that the server package can supply
// values without being aware of the exact key/value pairs that are sent over
// the wire.
type Data struct {
	// HostName is the public server name.
	HostName string
	// MOTD is the optional secondary server name shown in some clients.
	MOTD string
	// GameMode represents the default game mode of the primary world.
	GameMode string
	// Difficulty is the textual representation of the server difficulty.
	Difficulty string
	// WorldName holds the name of the primary world exposed by the server.
	WorldName string
	// Engine identifies the software that powers the server. When empty the
	// package falls back to the compiled engineLabel.
	Engine string
	// Version represents the protocol version string advertised to clients.
	Version string
	// PlayerCount reports the amount of online players.
	PlayerCount int
	// MaxPlayers is the configured player capacity.
	MaxPlayers int
	// HostIP is the textual representation of the listening IP address.
	HostIP string
	// HostPort is the listening port number.
	HostPort int
	// Plugins contains a semi-colon separated description of active plugins.
	Plugins string
	// PlayerNames lists the names of online players in sorted order.
	PlayerNames []string
	// GameType describes the type of game. Defaults to "SMP" when empty.
	GameType string
	// GameID is the identifier of the title shown to clients. Defaults to
	// "MINECRAFT" when empty.
	GameID string
	// WhitelistEnabled indicates whether the server whitelist is enabled.
	WhitelistEnabled bool
}

type keyValue struct {
	key   string
	value string
}

var lastSnapshot atomic.Pointer[Data]

// collectData retrieves the latest state, normalises it and updates the cached
// snapshot. When no provider is registered the latest cached snapshot is used
// instead. If no snapshot exists yet, sane defaults are emitted.
func collectData(host string, port int) Data {
	provider := loadProvider()
	if provider == nil {
		if snap, ok := loadSnapshot(); ok {
			snap.HostIP = canonicalHost(host)
			snap.HostPort = port
			snap.applyDefaults()
			return snap
		}
		return defaultData(host, port)
	}
	data := provider(canonicalHost(host), port)
	data.applyDefaults()
	storeSnapshot(data)
	return data
}

// canonicalHost returns the textual representation of the listening host or a
// safe default when it cannot be determined.
func canonicalHost(host string) string {
	if host == "" {
		return "0.0.0.0"
	}
	return host
}

// applyDefaults ensures that required fields are initialised before the data is
// serialised into key/value pairs.
func (d *Data) applyDefaults() {
	if d.HostIP == "" {
		d.HostIP = "0.0.0.0"
	}
	if d.Engine == "" {
		d.Engine = engineLabel
	}
	if d.Version == "" {
		d.Version = protocol.CurrentVersion
	}
	if d.GameType == "" {
		d.GameType = "SMP"
	}
	if d.GameID == "" {
		d.GameID = "MINECRAFT"
	}
	d.HostPort = int(uint16(d.HostPort))
}

// keyValues converts Data into the ordered key/value pairs required by the
// query protocol.
func (d Data) keyValues() []keyValue {
	whitelist := "off"
	if d.WhitelistEnabled {
		whitelist = "on"
	}
	values := []keyValue{
		{"hostname", d.HostName},
		{"gametype", d.GameType},
		{"game_id", d.GameID},
		{"version", d.Version},
		{"server_engine", d.Engine},
	}
	if d.WorldName != "" {
		values = append(values, keyValue{"map", d.WorldName})
	}
	values = append(values,
		keyValue{"numplayers", strconv.Itoa(d.PlayerCount)},
		keyValue{"maxplayers", strconv.Itoa(d.MaxPlayers)},
		keyValue{"whitelist", whitelist},
		keyValue{"hostport", strconv.Itoa(d.HostPort)},
		keyValue{"hostip", d.HostIP},
	)
	if d.GameMode != "" {
		values = append(values, keyValue{"gamemode", d.GameMode})
	}
	if d.Difficulty != "" {
		values = append(values, keyValue{"difficulty", d.Difficulty})
	}
	if d.MOTD != "" {
		values = append(values, keyValue{"motd", d.MOTD})
	}
	if d.Plugins != "" {
		values = append(values, keyValue{"plugins", d.Plugins})
	} else {
		values = append(values, keyValue{"plugins", ""})
	}
	if len(d.PlayerNames) > 0 {
		values = append(values, keyValue{"players", strings.Join(d.PlayerNames, ", ")})
	}
	return values
}

// defaultData returns the fallback query response when neither a provider nor a
// cached snapshot is available.
func defaultData(host string, port int) Data {
	data := Data{
		HostName: "Minecraft Server",
		Engine:   engineLabel,
		Version:  protocol.CurrentVersion,
		HostIP:   canonicalHost(host),
		HostPort: port,
		GameType: "SMP",
		GameID:   "MINECRAFT",
	}
	storeSnapshot(data)
	return data
}

// storeSnapshot copies the provided data into the snapshot cache.
func storeSnapshot(data Data) {
	cp := cloneData(data)
	lastSnapshot.Store(&cp)
}

// loadSnapshot retrieves the cached snapshot if present.
func loadSnapshot() (Data, bool) {
	snap := lastSnapshot.Load()
	if snap == nil {
		return Data{}, false
	}
	return cloneData(*snap), true
}

// cloneData deep-copies the Data structure so that cached snapshots remain
// immutable.
func cloneData(data Data) Data {
	cp := data
	if data.PlayerNames != nil {
		cp.PlayerNames = append([]string(nil), data.PlayerNames...)
	}
	return cp
}
