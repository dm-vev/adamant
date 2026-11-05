package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"testing"

	"github.com/df-mc/dragonfly/server/logging"
	"github.com/dm-vev/adamant/net/bedrock/login"
)

// refusedConn is a Conn whose login holds no valid UUID.
type refusedConn struct{ Conn }

func (refusedConn) IdentityData() login.Identity { return login.Identity{DisplayName: "Nobody"} }
func (refusedConn) RemoteAddr() net.Addr {
	return net.TCPAddrFromAddrPort(netip.MustParseAddrPort("192.0.2.1:19132"))
}

// oneConn is a ConnListener that accepts one connection.
type oneConn struct {
	conn Conn
	done bool
}

func (l *oneConn) Accept() (Conn, error) {
	if l.done {
		return nil, errors.New("closed")
	}
	l.done = true
	return l.conn, nil
}
func (*oneConn) Disconnect(Conn, string) error { return nil }
func (*oneConn) Close() error                  { return nil }

// TestComponentsGetTheirLoggers checks that a listener of sessions logs as
// the session component: with it at debug and the other components at
// error, the debug line of a login it refuses shows, as the session
// component.
func TestComponentsGetTheirLoggers(t *testing.T) {
	var out bytes.Buffer
	set := logging.New(logging.Options{Format: logging.JSON, Output: &out, Level: slog.LevelError,
		Levels: map[string]slog.Level{logging.Session: slog.LevelDebug}})
	l := Listen(&oneConn{conn: refusedConn{}}, ListenConfig{Log: set.Logger(logging.Network)})
	if _, err := l.Accept(); err == nil {
		t.Fatal("Accept returned a Join for a login with no UUID")
	}
	var rec struct{ Component, Msg string }
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &rec); err != nil {
		t.Fatalf("logged %q, want one JSON line: %v", out.String(), err)
	}
	if rec.Component != logging.Session || rec.Msg != "login refused: no valid UUID" {
		t.Fatalf("logged %+v, want the refused login as the %s component", rec, logging.Session)
	}
}
