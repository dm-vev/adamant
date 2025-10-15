package query

import (
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	gophertunnelquery "github.com/sandertv/gophertunnel/query"
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

func TestQueryResponsesParseWithGophertunnel(t *testing.T) {
	lastSnapshot.Store(nil)
	RegisterProvider(nil)
	t.Cleanup(func() {
		RegisterProvider(nil)
		lastSnapshot.Store(nil)
	})

	expected := Data{
		HostName:         "Test Server",
		MOTD:             "Integration Test",
		GameMode:         "CREATIVE",
		Difficulty:       "HARD",
		WorldName:        "Overworld",
		Engine:           "Adamant (integration)",
		Version:          "1.21.100",
		PlayerCount:      3,
		MaxPlayers:       25,
		Plugins:          "PluginA; PluginB",
		PlayerNames:      []string{"Alex", "Bob", "Steve"},
		GameType:         "ADVENTURE",
		GameID:           "MINECRAFTPE",
		WhitelistEnabled: true,
	}

	RegisterProvider(func(host string, port int) Data {
		data := expected
		data.HostIP = host
		data.HostPort = port
		return data
	})

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen packet: %v", err)
	}
	defer conn.Close()

	addr := conn.LocalAddr().(*net.UDPAddr)
	host := addr.IP.String()
	if host == "" {
		host = "0.0.0.0"
	}

	pc := &packetConn{
		PacketConn: conn,
		log:        nopLogger{},
		host:       host,
		port:       addr.Port,
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, raddr, err := conn.ReadFrom(buf)
			if err != nil {
				if isClosedError(err) {
					done <- nil
				} else {
					done <- err
				}
				return
			}
			if pc.handleQuery(buf[:n], raddr) {
				continue
			}
		}
	}()

	information, err := gophertunnelquery.Do(addr.String())
	if err != nil {
		t.Fatalf("query do: %v", err)
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
		"hostport":      strconv.Itoa(addr.Port),
		"hostip":        host,
		"gamemode":      expected.GameMode,
		"difficulty":    expected.Difficulty,
		"motd":          expected.MOTD,
		"plugins":       expected.Plugins,
		"players":       strings.Join(expected.PlayerNames, ", "),
	}

	for key, want := range checks {
		got, ok := information[key]
		if !ok {
			t.Fatalf("expected key %q to be present in query information", key)
		}
		if got != want {
			t.Fatalf("unexpected value for key %q: got %q, want %q", key, got, want)
		}
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("close packet conn: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("listener failed: %v", err)
	}
}

func TestHandleQueryAcceptsASCIIChallengeTokens(t *testing.T) {
	recorder := &packetRecorder{}
	pc := &packetConn{
		PacketConn: recorder,
		log:        nopLogger{},
		host:       "0.0.0.0",
		port:       19132,
	}

	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 43210}

	pc.mu.Lock()
	pc.tokens = map[string]token{
		addr.String(): {
			value:  7654321,
			expiry: time.Now().Add(time.Minute),
		},
	}
	pc.mu.Unlock()

	payload := make([]byte, 0, 7+7+5)
	payload = append(payload, queryVersion[:]...)
	payload = append(payload, queryTypeInformation)
	seq := make([]byte, 4)
	binary.BigEndian.PutUint32(seq, 42)
	payload = append(payload, seq...)
	payload = append(payload, []byte("7654321")...)
	payload = append(payload, 0x00)
	payload = append(payload, 0xff, 0xff, 0xff, 0x01)

	handled := pc.handleQuery(payload, addr)
	if !handled {
		t.Fatalf("expected query information request to be handled")
	}
	if len(recorder.writes) != 1 {
		t.Fatalf("expected one response write, got %d", len(recorder.writes))
	}
}

func isClosedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return isClosedError(opErr.Err)
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}
