package query

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}

type packetRecorder struct {
	writes [][]byte
	addrs  []net.Addr
}

func (p *packetRecorder) ReadFrom([]byte) (int, net.Addr, error) {
	return 0, nil, errors.New("not implemented")
}

func (p *packetRecorder) WriteTo(b []byte, addr net.Addr) (int, error) {
	cp := append([]byte(nil), b...)
	p.writes = append(p.writes, cp)
	p.addrs = append(p.addrs, addr)
	return len(b), nil
}

func (p *packetRecorder) Close() error { return nil }

func (p *packetRecorder) LocalAddr() net.Addr { return &net.UDPAddr{} }

func (p *packetRecorder) SetDeadline(time.Time) error { return nil }

func (p *packetRecorder) SetReadDeadline(time.Time) error { return nil }

func (p *packetRecorder) SetWriteDeadline(time.Time) error { return nil }

func TestQueryResponsesMatchLumiFormat(t *testing.T) {
	lastSnapshot.Store(nil)
	payloadCache.Store(nil)
	RegisterProvider(nil)
	t.Cleanup(func() {
		RegisterProvider(nil)
		lastSnapshot.Store(nil)
		payloadCache.Store(nil)
	})

	expected := Data{
		HostName:         "Test Server",
		WorldName:        "Overworld",
		Engine:           "Adamant (integration)",
		Version:          "1.21.100",
		PlayerCount:      3,
		MaxPlayers:       25,
		Plugins:          "PluginA;PluginB",
		PlayerNames:      []string{"Alex", "Bob", "Steve"},
		GameType:         "SMP",
		GameID:           "MINECRAFTPE",
		WhitelistEnabled: true,
	}

	RegisterProvider(func(host string, port int) Data {
		data := expected
		data.HostIP = host
		data.HostPort = port
		return data
	})

	recorder := &packetRecorder{}
	host := "127.0.0.1"
	port := 19132

	pc := &packetConn{
		PacketConn: recorder,
		log:        nopLogger{},
		host:       host,
		port:       port,
		token:      [16]byte{0x01},
		lastToken:  [16]byte{0x01},
	}

	remote := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 12345}
	longInfo, shortInfo, err := doNukkitQuery(pc, remote, expected)
	if err != nil {
		t.Fatalf("nukkit query: %v", err)
	}

	checks := map[string]string{
		"hostname":      expected.HostName,
		"gametype":      expected.GameType,
		"game_id":       expected.GameID,
		"version":       expected.Version,
		"server_engine": expected.Engine,
		"map":           expected.WorldName,
		"numplayers":    strconv.Itoa(expected.PlayerCount),
		"maxplayers":    strconv.Itoa(expected.MaxPlayers),
		"whitelist":     "on",
		"hostip":        host,
		"hostport":      strconv.Itoa(port),
	}

	for key, want := range checks {
		got, ok := longInfo[key]
		if !ok {
			t.Fatalf("expected key %q to be present in query information", key)
		}
		if got != want {
			t.Fatalf("unexpected value for key %q: got %q, want %q", key, got, want)
		}
	}

	plugins, ok := longInfo["plugins"]
	if !ok {
		t.Fatalf("expected key %q to be present in query information", "plugins")
	}
	if plugins != expected.Engine+":"+expected.Plugins {
		t.Fatalf("unexpected plugins value: got %q, want %q", plugins, expected.Engine+":"+expected.Plugins)
	}

	if shortInfo.serverName != expected.HostName {
		t.Fatalf("unexpected short hostname: got %q, want %q", shortInfo.serverName, expected.HostName)
	}
	if shortInfo.gameType != expected.GameType {
		t.Fatalf("unexpected short gametype: got %q, want %q", shortInfo.gameType, expected.GameType)
	}
	if shortInfo.mapName != expected.WorldName {
		t.Fatalf("unexpected short map: got %q, want %q", shortInfo.mapName, expected.WorldName)
	}
	if shortInfo.numPlayers != expected.PlayerCount || shortInfo.maxPlayers != expected.MaxPlayers {
		t.Fatalf("unexpected short player counts: got %d/%d, want %d/%d",
			shortInfo.numPlayers, shortInfo.maxPlayers, expected.PlayerCount, expected.MaxPlayers)
	}
	if shortInfo.port != port {
		t.Fatalf("unexpected short port: got %d, want %d", shortInfo.port, port)
	}
	if shortInfo.hostIP != host {
		t.Fatalf("unexpected short hostip: got %q, want %q", shortInfo.hostIP, host)
	}
}

func TestHandleQueryRejectsInvalidToken(t *testing.T) {
	payloadCache.Store(nil)
	recorder := &packetRecorder{}
	pc := &packetConn{
		PacketConn: recorder,
		log:        nopLogger{},
		host:       "0.0.0.0",
		port:       19132,
		token:      [16]byte{0x01},
		lastToken:  [16]byte{0x01},
	}

	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 43210}

	payload := make([]byte, 0, 7+4+8)
	payload = append(payload, queryVersion[:]...)
	payload = append(payload, queryTypeInformation)
	var seq [4]byte
	binary.BigEndian.PutUint32(seq[:], 42)
	payload = append(payload, seq[:]...)
	payload = append(payload, 0xde, 0xad, 0xbe, 0xef)
	payload = append(payload, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)

	handled := pc.handleQuery(payload, addr)
	if !handled {
		t.Fatalf("expected query information request to be handled")
	}
	if len(recorder.writes) != 0 {
		t.Fatalf("expected no response write, got %d", len(recorder.writes))
	}
}

func TestQueryInfoIsCachedForTTL(t *testing.T) {
	lastSnapshot.Store(nil)
	payloadCache.Store(nil)
	RegisterProvider(nil)

	var currentNano atomic.Int64
	originalNow := timeNow
	timeNow = func() time.Time {
		return time.Unix(0, currentNano.Load())
	}
	t.Cleanup(func() {
		timeNow = originalNow
		RegisterProvider(nil)
		lastSnapshot.Store(nil)
		payloadCache.Store(nil)
	})

	var calls atomic.Int64
	RegisterProvider(func(host string, port int) Data {
		n := calls.Add(1)
		return Data{
			HostName:    "Test Server " + strconv.FormatInt(n, 10),
			WorldName:   "Overworld",
			PlayerCount: 1,
			MaxPlayers:  10,
			HostIP:      host,
			HostPort:    port,
		}
	})

	remote := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 12345}
	doQuery := func() (shortQueryInfo, error) {
		recorder := &packetRecorder{}
		conn := &packetConn{
			PacketConn: recorder,
			log:        nopLogger{},
			host:       "127.0.0.1",
			port:       19132,
			token:      [16]byte{0x01},
			lastToken:  [16]byte{0x01},
		}
		_, shortInfo, err := doNukkitQuery(conn, remote, Data{PlayerNames: nil})
		return shortInfo, err
	}

	shortInfo1, err := doQuery()
	if err != nil {
		t.Fatalf("nukkit query: %v", err)
	}
	if shortInfo1.serverName != "Test Server 1" {
		t.Fatalf("unexpected first server name: got %q, want %q", shortInfo1.serverName, "Test Server 1")
	}

	currentNano.Store(int64(time.Millisecond))
	shortInfo2, err := doQuery()
	if err != nil {
		t.Fatalf("nukkit query: %v", err)
	}
	if shortInfo2.serverName != "Test Server 1" {
		t.Fatalf("unexpected cached server name: got %q, want %q", shortInfo2.serverName, "Test Server 1")
	}

	currentNano.Store(int64(queryInfoTTL) + int64(time.Millisecond))
	shortInfo3, err := doQuery()
	if err != nil {
		t.Fatalf("nukkit query: %v", err)
	}
	if shortInfo3.serverName != "Test Server 2" {
		t.Fatalf("unexpected refreshed server name: got %q, want %q", shortInfo3.serverName, "Test Server 2")
	}

	if got := calls.Load(); got != 2 {
		t.Fatalf("unexpected provider call count: got %d, want %d", got, 2)
	}
}

func TestTokenDigestMatchesJavaInetAddressFormattingIPv6(t *testing.T) {
	pc := &packetConn{
		PacketConn: &packetRecorder{},
		log:        nopLogger{},
		host:       "0.0.0.0",
		port:       19132,
		token:      [16]byte{0x01},
		lastToken:  [16]byte{0x01},
	}

	addr := &net.UDPAddr{IP: net.ParseIP("::1"), Port: 12345}
	digest := pc.tokenDigest(addr, pc.token)

	hash := md5.New()
	_, _ = hash.Write([]byte("/0:0:0:0:0:0:0:1"))
	_, _ = hash.Write(pc.token[:])
	sum := hash.Sum(nil)

	if !bytes.Equal(digest[:], sum[:4]) {
		t.Fatalf("unexpected digest: got %x, want %x", digest, sum[:4])
	}
}

type shortQueryInfo struct {
	serverName string
	gameType   string
	mapName    string
	numPlayers int
	maxPlayers int
	port       int
	hostIP     string
}

func doNukkitQuery(conn *packetConn, remote net.Addr, expected Data) (map[string]string, shortQueryInfo, error) {
	recorder, ok := conn.PacketConn.(*packetRecorder)
	if !ok {
		return nil, shortQueryInfo{}, errors.New("packet recorder missing")
	}

	session := uint32(12345)

	handshakeReq := make([]byte, 7)
	copy(handshakeReq[:2], queryVersion[:])
	handshakeReq[2] = queryTypeHandshake
	binary.BigEndian.PutUint32(handshakeReq[3:7], session)
	if handled := conn.handleQuery(handshakeReq, remote); !handled {
		return nil, shortQueryInfo{}, errors.New("handshake request not handled")
	}

	if len(recorder.writes) != 1 {
		return nil, shortQueryInfo{}, errors.New("missing handshake response")
	}
	handshakeResp := recorder.writes[0]
	if len(handshakeResp) != 10 {
		return nil, shortQueryInfo{}, errors.New("unexpected handshake response length")
	}
	if handshakeResp[0] != queryTypeHandshake {
		return nil, shortQueryInfo{}, errors.New("unexpected handshake response type")
	}
	if binary.BigEndian.Uint32(handshakeResp[1:5]) != session {
		return nil, shortQueryInfo{}, errors.New("unexpected handshake session id")
	}
	token := append([]byte(nil), handshakeResp[5:9]...)

	var sequence [4]byte
	binary.BigEndian.PutUint32(sequence[:], session+1)

	infoReq := make([]byte, 0, 7+4+8)
	infoReq = append(infoReq, queryVersion[:]...)
	infoReq = append(infoReq, queryTypeInformation)
	infoReq = append(infoReq, sequence[:]...)
	infoReq = append(infoReq, token...)
	infoReq = append(infoReq, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)

	if handled := conn.handleQuery(infoReq, remote); !handled {
		return nil, shortQueryInfo{}, errors.New("long stats request not handled")
	}
	if len(recorder.writes) < 2 {
		return nil, shortQueryInfo{}, errors.New("missing long stats response")
	}
	infoResp := recorder.writes[1]
	if len(infoResp) < 5 {
		return nil, shortQueryInfo{}, errors.New("unexpected info response length")
	}
	if infoResp[0] != queryTypeInformation {
		return nil, shortQueryInfo{}, errors.New("unexpected info response type")
	}

	longKV, err := parseLongPayload(infoResp[5:], expected.PlayerNames)
	if err != nil {
		return nil, shortQueryInfo{}, err
	}

	binary.BigEndian.PutUint32(sequence[:], session+2)
	shortReq := make([]byte, 0, 7+4)
	shortReq = append(shortReq, queryVersion[:]...)
	shortReq = append(shortReq, queryTypeInformation)
	shortReq = append(shortReq, sequence[:]...)
	shortReq = append(shortReq, token...)
	if handled := conn.handleQuery(shortReq, remote); !handled {
		return nil, shortQueryInfo{}, errors.New("short stats request not handled")
	}

	if len(recorder.writes) < 3 {
		return nil, shortQueryInfo{}, errors.New("missing short stats response")
	}
	shortResp := recorder.writes[2]
	if len(shortResp) < 5 {
		return nil, shortQueryInfo{}, errors.New("unexpected short response length")
	}

	shortInfo, err := parseShortPayload(shortResp[5:])
	if err != nil {
		return nil, shortQueryInfo{}, err
	}
	return longKV, shortInfo, nil
}

func parseLongPayload(payload []byte, expectedPlayers []string) (map[string]string, error) {
	if !bytes.HasPrefix(payload, querySplitNumPrefix[:]) {
		return nil, errors.New("missing splitnum prefix")
	}

	playerIdx := bytes.Index(payload, queryPlayerKey[:])
	if playerIdx < 0 {
		return nil, errors.New("missing player section")
	}

	kvBytes := payload[len(querySplitNumPrefix):playerIdx]
	values, err := parseNullSeparatedPairs(kvBytes)
	if err != nil {
		return nil, err
	}

	playerBytes := payload[playerIdx+len(queryPlayerKey):]
	players := parseNullSeparatedStrings(playerBytes)
	if len(players) != len(expectedPlayers) {
		return nil, errors.New("unexpected player count")
	}
	return values, nil
}

func parseNullSeparatedPairs(b []byte) (map[string]string, error) {
	out := make(map[string]string)
	for len(b) > 0 {
		keyEnd := bytes.IndexByte(b, 0x00)
		if keyEnd < 0 {
			return nil, errors.New("unterminated key")
		}
		key := string(b[:keyEnd])
		b = b[keyEnd+1:]

		valEnd := bytes.IndexByte(b, 0x00)
		if valEnd < 0 {
			return nil, errors.New("unterminated value")
		}
		out[key] = string(b[:valEnd])
		b = b[valEnd+1:]
	}
	return out, nil
}

func parseNullSeparatedStrings(b []byte) []string {
	var out []string
	for len(b) > 0 {
		end := bytes.IndexByte(b, 0x00)
		if end < 0 {
			break
		}
		if end == 0 {
			return out
		}
		out = append(out, string(b[:end]))
		b = b[end+1:]
	}
	return out
}

func parseShortPayload(payload []byte) (shortQueryInfo, error) {
	readCString := func(b []byte) (string, []byte, error) {
		end := bytes.IndexByte(b, 0x00)
		if end < 0 {
			return "", nil, errors.New("unterminated string")
		}
		return string(b[:end]), b[end+1:], nil
	}

	var info shortQueryInfo
	var err error
	info.serverName, payload, err = readCString(payload)
	if err != nil {
		return shortQueryInfo{}, err
	}
	info.gameType, payload, err = readCString(payload)
	if err != nil {
		return shortQueryInfo{}, err
	}
	info.mapName, payload, err = readCString(payload)
	if err != nil {
		return shortQueryInfo{}, err
	}
	numPlayers, payload, err := readCString(payload)
	if err != nil {
		return shortQueryInfo{}, err
	}
	maxPlayers, payload, err := readCString(payload)
	if err != nil {
		return shortQueryInfo{}, err
	}
	info.numPlayers, err = strconv.Atoi(numPlayers)
	if err != nil {
		return shortQueryInfo{}, err
	}
	info.maxPlayers, err = strconv.Atoi(maxPlayers)
	if err != nil {
		return shortQueryInfo{}, err
	}
	if len(payload) < 2 {
		return shortQueryInfo{}, errors.New("missing port")
	}
	info.port = int(binary.LittleEndian.Uint16(payload[:2]))
	payload = payload[2:]
	info.hostIP, payload, err = readCString(payload)
	if err != nil {
		return shortQueryInfo{}, err
	}
	if len(payload) != 0 {
		return shortQueryInfo{}, errors.New("unexpected trailing data")
	}
	return info, nil
}
