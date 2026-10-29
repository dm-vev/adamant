package interop

import (
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/turn"
	pionturn "github.com/pion/turn/v5"
)

// TestTURNClientWithPionServer allocates a relay on the TURN server of
// pion with long-term credentials, permits a peer, and has data go both
// ways between the peer and the client through the relay.
func TestTURNClientWithPionServer(t *testing.T) {
	serverSock, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := pionturn.NewServer(pionturn.ServerConfig{
		Realm: "adamant",
		AuthHandler: func(ra *pionturn.RequestAttributes) (string, []byte, bool) {
			return ra.Username, pionturn.GenerateAuthKey("user", "adamant", "secret"), ra.Username == "user"
		},
		PacketConnConfigs: []pionturn.PacketConnConfig{{
			PacketConn:            serverSock,
			RelayAddressGenerator: &pionturn.RelayAddressGeneratorStatic{RelayAddress: net.ParseIP("127.0.0.1"), Address: "127.0.0.1"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	sock := listenLoopback(t)
	peer := listenLoopback(t)
	var mu sync.Mutex
	c := turn.New(turn.Config{Server: addrPort(serverSock.LocalAddr()), Username: "user", Password: "secret"}, func(b []byte, to netip.AddrPort) {
		_, _ = sock.WriteToUDPAddrPort(b, to)
	})
	received := make(chan string, 4)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		buf := make([]byte, 2048)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = sock.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			n, from, err := sock.ReadFromUDPAddrPort(buf)
			now := time.Now()
			mu.Lock()
			if err == nil && from == c.Server() {
				if p, d, ok := c.HandleDatagram(buf[:n], now); ok {
					received <- p.String() + " " + string(d)
				}
			}
			if d := c.Deadline(); !d.IsZero() && !now.Before(d) {
				c.HandleTimeout(now)
			}
			mu.Unlock()
		}
	}()
	mu.Lock()
	c.Start(time.Now())
	mu.Unlock()
	wait := func(cond func() bool, what string) {
		t.Helper()
		for end := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			mu.Lock()
			ok, err := cond(), c.Err()
			mu.Unlock()
			if ok {
				return
			}
			if err != nil || time.Now().After(end) {
				t.Fatalf("%s: %v", what, err)
			}
		}
	}
	wait(c.Allocated, "allocate")
	mu.Lock()
	relayed, mapped := c.Relayed(), c.Mapped()
	c.Permit(addrPort(peer.LocalAddr()).Addr(), time.Now())
	mu.Unlock()
	if !relayed.IsValid() || mapped != addrPort(sock.LocalAddr()) {
		t.Fatalf("relayed %v, mapped %v, socket %v", relayed, mapped, sock.LocalAddr())
	}
	wait(func() bool { return c.Permitted(addrPort(peer.LocalAddr()).Addr(), time.Now()) }, "permit")

	if _, err := peer.WriteToUDPAddrPort([]byte("to the relay"), relayed); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if got != addrPort(peer.LocalAddr()).String()+" to the relay" {
			t.Fatalf("received %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("data of the peer did not come through the relay")
	}
	mu.Lock()
	err = c.Send(addrPort(peer.LocalAddr()), []byte("from the client"))
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	_ = peer.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, from, err := peer.ReadFromUDPAddrPort(buf)
	if err != nil || string(buf[:n]) != "from the client" || from != relayed {
		t.Fatalf("peer read %q from %v, %v", buf[:n], from, err)
	}
}
