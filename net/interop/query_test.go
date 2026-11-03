package interop

import (
	"net/netip"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/query"
	"github.com/dm-vev/adamant/net/nethernet"
	gtquery "github.com/sandertv/gophertunnel/query"
)

// TestQueryWithGophertunnel queries a NetherNet listener that hands its
// query datagrams to the query responder, as the server does, with the
// query client of gophertunnel: the challenge of the handshake and the
// full statistics go as the UT3 query protocol has them.
func TestQueryWithGophertunnel(t *testing.T) {
	query.RegisterProvider(func(host string, port int) query.Data {
		return query.Data{HostName: "Adamant test", WorldName: "world", PlayerCount: 2, MaxPlayers: 20, PlayerNames: []string{"Alex", "Steve"}}
	})
	t.Cleanup(func() { query.RegisterProvider(nil) })
	query.SetEnabled(true)
	var responder *query.Responder
	l, err := nethernet.ListenConfig{
		AllowAnonymous: true,
		Datagram: func(b []byte, from netip.AddrPort, reply func([]byte)) {
			responder.Answer(b, from, reply)
		},
	}.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if responder, err = query.NewResponder(addrPort(l.Addr()), nopLogger{}); err != nil {
		t.Fatal(err)
	}
	info, err := gtquery.Do(l.Addr().String())
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for key, want := range map[string]string{"hostname": "Adamant test", "map": "world", "numplayers": "2", "maxplayers": "20", "players": "Alex, Steve", "server_engine": "Adamant", "plugins": "Adamant"} {
		if info[key] != want {
			t.Errorf("%s = %q, want %q (all: %v)", key, info[key], want, info)
		}
	}
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
