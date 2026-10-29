package lan

import (
	"bytes"
	"context"
	"net/netip"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/nethernet"
)

func TestPacketsRoundTrip(t *testing.T) {
	for _, pk := range []Packet{
		&Request{},
		&Response{ApplicationData: []byte{7, 1, 2, 3, 0xff}},
		&Message{RecipientID: 1<<63 + 5, Data: "CONNECTREQUEST 42 v=0..."},
	} {
		b := Marshal(pk, 99)
		got, sender, err := Unmarshal(b)
		if err != nil || sender != 99 || !reflect.DeepEqual(got, pk) {
			t.Fatalf("%T: %+v from %d, %v", pk, got, sender, err)
		}
	}
}

func TestUnmarshalRefusesBrokenPackets(t *testing.T) {
	good := Marshal(&Message{RecipientID: 1, Data: "x"}, 2)
	tampered := bytes.Clone(good)
	tampered[len(tampered)-1] ^= 1
	forged := bytes.Clone(good)
	forged[0] ^= 1
	for name, b := range map[string][]byte{
		"empty": nil, "short": good[:40], "unaligned": good[:len(good)-1],
		"tampered": tampered, "forged HMAC": forged,
		"no payload": encrypt(nil),
	} {
		if _, _, err := Unmarshal(b); err == nil {
			t.Fatalf("%s: unmarshalled", name)
		}
	}
}

func TestServerDataRoundTrip(t *testing.T) {
	d := ServerData{ServerName: "Adamant", Protocol: 2193, Version: "1.26.50", LevelName: "world",
		GameType: GameTypeCreative, PlayerCount: 3, MaxPlayerCount: -1, Hardcore: true,
		AcceptsOnlineAuth: true, AcceptsSelfSignedAuth: true, Nonce: "n", ConnectionType: ConnectionTypeLAN}
	b, _ := d.MarshalBinary()
	var got ServerData
	if err := got.UnmarshalBinary(b); err != nil || got != d {
		t.Fatalf("%+v, %v", got, err)
	}
	for i := range b {
		if err := got.UnmarshalBinary(b[:i]); err == nil {
			t.Fatalf("read %d of %d bytes", i, len(b))
		}
	}
}

func FuzzUnmarshal(f *testing.F) {
	f.Add(Marshal(&Request{}, 1))
	f.Add(Marshal(&Response{ApplicationData: []byte("data")}, 2))
	f.Add(Marshal(&Message{RecipientID: 3, Data: "CANDIDATEADD 1 x"}, 4))
	f.Fuzz(func(t *testing.T, b []byte) {
		pk, sender, err := Unmarshal(b)
		if err != nil {
			return
		}
		again, sender2, err := Unmarshal(Marshal(pk, sender))
		if err != nil || sender2 != sender || !reflect.DeepEqual(again, pk) {
			t.Fatalf("%+v does not round trip: %+v, %v", pk, again, err)
		}
	})
}

func FuzzServerData(f *testing.F) {
	b, _ := ServerData{ServerName: "s", Version: "v", LevelName: "l", Nonce: "n"}.MarshalBinary()
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		var d ServerData
		if d.UnmarshalBinary(b) != nil {
			return
		}
		again, _ := d.MarshalBinary()
		var d2 ServerData
		if err := d2.UnmarshalBinary(again); err != nil || d2 != d {
			t.Fatalf("%+v does not round trip", d)
		}
	})
}

func listen(t *testing.T, data func() ServerData) *Listener {
	t.Helper()
	l, err := Config{Addr: "127.0.0.1:0", Data: data}.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func addr(l *Listener) netip.AddrPort { return netip.MustParseAddrPort(l.Addr().String()) }

// TestJoinOverLAN finds a server on the network and joins it as the game
// does: the client asks for the data of the server, then signals its offer
// over LAN messages to the NetherNet listener serving them.
func TestJoinOverLAN(t *testing.T) {
	nn, err := nethernet.ListenConfig{AllowAnonymous: true}.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer nn.Close()
	srv := listen(t, func() ServerData {
		return ServerData{ServerName: "Adamant", LevelName: "world", ConnectionType: ConnectionTypeLAN}
	})
	go func() { _ = nn.Serve(srv) }()
	client := listen(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for len(client.Servers()) == 0 {
		if err := client.Discover(addr(srv)); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("the server did not answer")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if d := client.Servers()[srv.NetworkID()]; d.ServerName != "Adamant" {
		t.Fatalf("servers %+v", client.Servers())
	}
	c, err := nethernet.Dialer{}.DialSignaling(ctx, client, strconv.FormatUint(srv.NetworkID(), 10))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	conn, err := nn.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	accepted := conn.(*nethernet.Conn)
	if err := c.Send([]byte("over the LAN")); err != nil {
		t.Fatal(err)
	}
	if msg, err := accepted.ReadMessage(); err != nil || string(msg) != "over the LAN" {
		t.Fatalf("read %q, %v", msg, err)
	}
}
