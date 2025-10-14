package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/go-raknet"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

const (
	queryTypeHandshake   = 0x09
	queryTypeInformation = 0x00
)

var (
	querySplitNum  = [...]byte{'S', 'P', 'L', 'I', 'T', 'N', 'U', 'M', 0x00}
	queryPlayerKey = [...]byte{0x00, 0x01, 'p', 'l', 'a', 'y', 'e', 'r', '_', 0x00, 0x00}
	queryVersion   = [...]byte{0xfe, 0xfd}

	queryServer       atomic.Pointer[Server]
	lastQuerySnapshot atomic.Pointer[queryData]
	engineLabel       = buildEngineLabel()
)

func buildEngineLabel() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "Adamant"
	}
	version := info.Main.Version
	if version == "" {
		version = "dev"
	}
	return fmt.Sprintf("Adamant (%s)", version)
}

// registerQueryServer exposes the server instance to the Bedrock query listener.
func registerQueryServer(s *Server) {
	queryServer.Store(s)
}

func init() {
	// Override the default RakNet network with a query-aware implementation.
	minecraft.RegisterNetwork("raknet", func(l *slog.Logger) minecraft.Network {
		return queryRakNet{log: l}
	})
}

type queryRakNet struct {
	log *slog.Logger
}

func (r queryRakNet) DialContext(ctx context.Context, address string) (net.Conn, error) {
	return raknet.Dialer{ErrorLog: r.log.With("net origin", "raknet")}.DialContext(ctx, address)
}

func (r queryRakNet) PingContext(ctx context.Context, address string) ([]byte, error) {
	return raknet.Dialer{ErrorLog: r.log.With("net origin", "raknet")}.PingContext(ctx, address)
}

func (r queryRakNet) Listen(address string) (minecraft.NetworkListener, error) {
	lc := raknet.ListenConfig{
		ErrorLog: r.log.With("net origin", "raknet"),
		UpstreamPacketListener: &queryPacketListener{
			log: r.log.With("net origin", "raknet"),
		},
	}
	return lc.Listen(address)
}

type queryPacketListener struct {
	log *slog.Logger
}

func (l *queryPacketListener) ListenPacket(network, address string) (net.PacketConn, error) {
	conn, err := net.ListenPacket(network, address)
	if err != nil {
		return nil, err
	}
	local, _ := net.ResolveUDPAddr(network, conn.LocalAddr().String())
	host := ""
	if local != nil && local.IP != nil {
		host = local.IP.String()
		if host == "" || local.IP.IsUnspecified() {
			host = "0.0.0.0"
		}
	}
	port := 0
	if local != nil {
		port = local.Port
	}
	return &queryPacketConn{
		PacketConn: conn,
		log:        l.log,
		host:       host,
		port:       port,
		tokens:     make(map[string]queryToken),
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

type queryPacketConn struct {
	net.PacketConn

	log  *slog.Logger
	host string
	port int

	mu     sync.Mutex
	tokens map[string]queryToken
	rng    *rand.Rand
}

type queryToken struct {
	value  int32
	expiry time.Time
}

func (c *queryPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(p)
		if err != nil || n == 0 {
			return n, addr, err
		}
		if c.handleQuery(p[:n], addr) {
			continue
		}
		return n, addr, nil
	}
}

func (c *queryPacketConn) handleQuery(b []byte, addr net.Addr) bool {
	if len(b) < 7 || b[0] != queryVersion[0] || b[1] != queryVersion[1] {
		return false
	}
	reqType := b[2]
	sequence := int32(binary.BigEndian.Uint32(b[3:7]))
	switch reqType {
	case queryTypeHandshake:
		token := c.newToken(addr.String())
		c.writeHandshake(addr, sequence, token)
		return true
	case queryTypeInformation:
		if len(b) < 15 {
			return true
		}
		if !c.validateToken(addr.String(), int32(binary.BigEndian.Uint32(b[7:11]))) {
			return true
		}
		c.writeInfo(addr, sequence)
		return true
	default:
		return false
	}
}

func (c *queryPacketConn) newToken(addr string) int32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	value := int32(c.rng.Int31())
	c.tokens[addr] = queryToken{
		value:  value,
		expiry: time.Now().Add(30 * time.Second),
	}
	return value
}

func (c *queryPacketConn) validateToken(addr string, value int32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	token, ok := c.tokens[addr]
	if !ok || time.Now().After(token.expiry) || token.value != value {
		delete(c.tokens, addr)
		return false
	}
	return true
}

func (c *queryPacketConn) writeHandshake(addr net.Addr, sequence, token int32) {
	buf := bytes.NewBuffer(make([]byte, 0, 1+4+12))
	buf.WriteByte(queryTypeHandshake)
	_ = binary.Write(buf, binary.BigEndian, sequence)

	tokenStr := strconv.FormatInt(int64(token), 10)
	if len(tokenStr) > 12 {
		tokenStr = tokenStr[:12]
	}
	buf.WriteString(tokenStr)
	if padding := 12 - len(tokenStr); padding > 0 {
		buf.Write(make([]byte, padding))
	}
	if _, err := c.PacketConn.WriteTo(buf.Bytes(), addr); err != nil {
		c.log.Debug("query handshake write failed", "err", err, "raddr", addr.String())
	}
}

func (c *queryPacketConn) writeInfo(addr net.Addr, sequence int32) {
	data := collectQueryData(c.host, c.port)

	buf := bytes.NewBuffer(make([]byte, 0, 256))
	buf.WriteByte(queryTypeInformation)
	_ = binary.Write(buf, binary.BigEndian, sequence)
	buf.Write(querySplitNum[:])
	buf.WriteByte(0x80)
	buf.WriteByte(0x00)

	for _, kv := range data.kvPairs {
		buf.WriteString(kv.key)
		buf.WriteByte(0x00)
		buf.WriteString(kv.value)
		buf.WriteByte(0x00)
	}
	buf.WriteByte(0x00)
	buf.Write(queryPlayerKey[:])
	for _, name := range data.players {
		buf.WriteString(name)
		buf.WriteByte(0x00)
	}
	buf.WriteByte(0x00)

	if _, err := c.PacketConn.WriteTo(buf.Bytes(), addr); err != nil {
		c.log.Debug("query info write failed", "err", err, "raddr", addr.String())
	}
}

type queryPair struct {
	key   string
	value string
}

type queryData struct {
	kvPairs []queryPair
	players []string
}

func cloneQueryData(data queryData) queryData {
	cp := queryData{
		kvPairs: make([]queryPair, len(data.kvPairs)),
		players: append([]string(nil), data.players...),
	}
	copy(cp.kvPairs, data.kvPairs)
	return cp
}

func storeQuerySnapshot(data queryData) {
	cp := cloneQueryData(data)
	lastQuerySnapshot.Store(&cp)
}

func loadQuerySnapshot() (queryData, bool) {
	snap := lastQuerySnapshot.Load()
	if snap == nil {
		return queryData{}, false
	}
	return cloneQueryData(*snap), true
}

func setPair(pairs []queryPair, key, value string) []queryPair {
	for i := range pairs {
		if pairs[i].key == key {
			pairs[i].value = value
			return pairs
		}
	}
	return append(pairs, queryPair{key: key, value: value})
}

func collectQueryData(host string, port int) queryData {
	if host == "" {
		host = "0.0.0.0"
	}
	srv := queryServer.Load()
	if srv == nil {
		if snap, ok := loadQuerySnapshot(); ok {
			snap.kvPairs = setPair(snap.kvPairs, "hostport", strconv.Itoa(port))
			snap.kvPairs = setPair(snap.kvPairs, "hostip", host)
			return snap
		}
		return queryData{
			kvPairs: []queryPair{
				{"hostname", "Minecraft Server"},
				{"gametype", "SMP"},
				{"game_id", "MINECRAFT"},
				{"version", protocol.CurrentVersion},
				{"server_engine", engineLabel},
				{"numplayers", "0"},
				{"maxplayers", "0"},
				{"whitelist", "off"},
				{"hostport", strconv.Itoa(port)},
				{"hostip", host},
				{"plugins", ""},
			},
			players: nil,
		}
	}

	playerCount := srv.PlayerCount()
	maxPlayers := srv.MaxPlayerCount()
	status := srv.conf.StatusProvider.ServerStatus(playerCount, maxPlayers)
	worldName := ""
	if srv.world != nil {
		worldName = srv.world.Name()
	}
	modeName := defaultGameModeName(srv)
	pluginString := strings.Join(srv.plugins(), "; ")
	if pluginString == "" {
		pluginString = "Adamant"
	}
	difficulty := "NORMAL"

	srv.pmu.RLock()
	playerNames := make([]string, 0, len(srv.p))
	for _, p := range srv.p {
		playerNames = append(playerNames, p.name)
	}
	srv.pmu.RUnlock()
	sort.Strings(playerNames)

	kv := []queryPair{
		{"hostname", status.ServerName},
		{"gametype", "SMP"},
		{"game_id", "MINECRAFT"},
		{"version", protocol.CurrentVersion},
		{"server_engine", engineLabel},
		{"map", worldName},
		{"numplayers", strconv.Itoa(playerCount)},
		{"maxplayers", strconv.Itoa(status.MaxPlayers)},
		{"whitelist", "off"},
		{"hostport", strconv.Itoa(port)},
		{"hostip", host},
		{"gamemode", modeName},
		{"difficulty", difficulty},
		{"motd", status.ServerSubName},
		{"plugins", pluginString},
		{"players", strings.Join(playerNames, ", ")},
	}
	data := queryData{kvPairs: kv, players: playerNames}
	storeQuerySnapshot(data)
	return data
}

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

func (srv *Server) plugins() []string {
	// TODO: Wire up plugin discovery once an explicit plugin system is available.
	return nil
}
