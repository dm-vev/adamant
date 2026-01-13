package query

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

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
	// "MINECRAFTPE" when empty.
	GameID string
	// WhitelistEnabled indicates whether the server whitelist is enabled.
	WhitelistEnabled bool
}

type keyValue struct {
	key   string
	value string
}

var lastSnapshot atomic.Pointer[Data]

// queryInfoTTL matches the default QueryRegenerateEvent timeout in Lumi/Nukkit.
//
// Query clients may observe server information changing. Lumi/Nukkit keeps long/short responses stable for a short
// period to avoid rebuilding large payloads (player lists, plugin metadata) on every request. We mirror that behaviour
// for compatibility and performance.
const queryInfoTTL = 5 * time.Second

// timeNow is overridden in tests to make cache expiry deterministic.
var timeNow = time.Now

type cachedPayloads struct {
	host      string
	port      int
	expiresAt int64

	data  Data
	long  []byte
	short []byte
}

var (
	payloadCache     atomic.Pointer[cachedPayloads]
	payloadRefreshMu sync.Mutex
)

func invalidatePayloadCache() {
	payloadCache.Store(nil)
}

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
	// Detach provider-owned slices to avoid races if the provider mutates them after returning.
	data = cloneData(data)
	data.applyDefaults()
	storeSnapshot(data)
	return data
}

func collectPayload(host string, port int, long bool) []byte {
	host = canonicalHost(host)

	now := timeNow().UnixNano()
	if cached := payloadCache.Load(); cached != nil && cached.host == host && cached.port == port && now < cached.expiresAt {
		if long {
			return cached.long
		}
		return cached.short
	}

	payloadRefreshMu.Lock()
	defer payloadRefreshMu.Unlock()

	now = timeNow().UnixNano()
	if cached := payloadCache.Load(); cached != nil && cached.host == host && cached.port == port && now < cached.expiresAt {
		if long {
			return cached.long
		}
		return cached.short
	}

	data := collectData(host, port)

	updated := &cachedPayloads{
		host:      host,
		port:      port,
		expiresAt: now + int64(queryInfoTTL),
		data:      cloneData(data),
		long:      data.longPayload(),
		short:     data.shortPayload(),
	}
	payloadCache.Store(updated)

	if long {
		return updated.long
	}
	return updated.short
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
		d.GameID = "MINECRAFTPE"
	}
	if d.HostPort < 0 {
		d.HostPort = 0
	} else if d.HostPort > 65535 {
		d.HostPort = 65535
	}
}

// longKeyValues returns the ordered key/value pairs used by Lumi/Nukkit long query responses.
//
// Order is observable: Lumi uses a LinkedHashMap and serialises entries in insertion order.
func (d Data) longKeyValues() []keyValue {
	whitelist := "off"
	if d.WhitelistEnabled {
		whitelist = "on"
	}

	mapName := d.WorldName
	if mapName == "" {
		mapName = "unknown"
	}

	plugins := d.Engine
	if d.Plugins != "" {
		// Lumi prefixes plugin metadata with the engine label.
		plugins = plugins + ":" + d.Plugins
	}

	return []keyValue{
		{"hostname", d.HostName},
		{"gametype", d.GameType},
		{"game_id", d.GameID},
		{"version", d.Version},
		{"server_engine", d.Engine},
		{"plugins", plugins},
		{"map", mapName},
		{"numplayers", strconv.Itoa(d.PlayerCount)},
		{"maxplayers", strconv.Itoa(d.MaxPlayers)},
		{"whitelist", whitelist},
		{"hostip", d.HostIP},
		{"hostport", strconv.Itoa(d.HostPort)},
	}
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
		GameID:   "MINECRAFTPE",
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

// longPayload returns the wire payload for the long query response (excluding the packet header).
func (d Data) longPayload() []byte {
	var buf bytes.Buffer
	buf.Grow(512)

	buf.Write(querySplitNumPrefix[:])

	for _, kv := range d.longKeyValues() {
		buf.WriteString(kv.key)
		buf.WriteByte(0x00)
		buf.WriteString(kv.value)
		buf.WriteByte(0x00)
	}

	buf.Write(queryPlayerKey[:])
	for _, name := range d.PlayerNames {
		buf.WriteString(name)
		buf.WriteByte(0x00)
	}
	buf.WriteByte(0x00)

	return append([]byte(nil), buf.Bytes()...)
}

// shortPayload returns the wire payload for the short query response (excluding the packet header).
func (d Data) shortPayload() []byte {
	var buf bytes.Buffer
	buf.Grow(128)

	mapName := d.WorldName
	if mapName == "" {
		mapName = "unknown"
	}

	buf.WriteString(d.HostName)
	buf.WriteByte(0x00)
	buf.WriteString(d.GameType)
	buf.WriteByte(0x00)
	buf.WriteString(mapName)
	buf.WriteByte(0x00)
	buf.WriteString(strconv.Itoa(d.PlayerCount))
	buf.WriteByte(0x00)
	buf.WriteString(strconv.Itoa(d.MaxPlayers))
	buf.WriteByte(0x00)

	var port [2]byte
	binary.LittleEndian.PutUint16(port[:], uint16(d.HostPort))
	buf.Write(port[:])

	buf.WriteString(d.HostIP)
	buf.WriteByte(0x00)

	return append([]byte(nil), buf.Bytes()...)
}
