package turn

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/stun"
)

// fakeServer is a TURN server of a test: it asks for the credentials,
// grants an allocation and permissions, takes Send indications and makes
// the first Refresh fail with a stale nonce.
type fakeServer struct {
	t        *testing.T
	nonce    string
	out      [][]byte
	sent     []string
	refreshN int
	// permissions counts the CreatePermission requests granted by peer.
	permissions map[netip.Addr]int
}

var (
	serverAddr  = netip.MustParseAddrPort("203.0.113.1:3478")
	relayedAddr = netip.MustParseAddrPort("203.0.113.1:50000")
	mappedAddr  = netip.MustParseAddrPort("198.51.100.7:40000")
	peerAddr    = netip.MustParseAddrPort("192.0.2.9:6000")
)

func (s *fakeServer) key() []byte {
	k := md5.Sum([]byte("user:realm:pass"))
	return k[:]
}

// handle answers a datagram of the client.
func (s *fakeServer) handle(b []byte) {
	var m stun.Message
	if err := stun.Parse(b, &m); err != nil {
		s.t.Fatalf("client sent no STUN: %v", err)
	}
	if class(m.Type) == classIndication {
		if method(m.Type) == methodSend {
			v, _ := m.Get(attrXORPeerAddress)
			d, _ := m.Get(attrData)
			peer, _ := decodeXORAddress(v, m.Transaction)
			s.sent = append(s.sent, peer.String()+" "+string(d))
		}
		return
	}
	reply := func(cls uint16, add func(b *stun.Builder), signed bool) {
		b := stun.NewBuilder(nil, msgType(method(m.Type), cls), m.Transaction)
		add(&b)
		if signed {
			b.AddIntegrity(hmac.New(sha1.New, s.key()))
		}
		s.out = append(s.out, b.Bytes())
	}
	if !m.HasIntegrity() {
		reply(classError, func(b *stun.Builder) {
			b.AddErrorCode(401, "Unauthorized")
			b.AddString(attrRealm, "realm")
			b.AddString(attrNonce, s.nonce)
		}, false)
		return
	}
	if !m.CheckIntegrity(hmac.New(sha1.New, s.key())) {
		s.t.Fatal("request not signed with the long-term key")
	}
	if n, _ := m.Get(attrNonce); string(n) != s.nonce {
		reply(classError, func(b *stun.Builder) { b.AddErrorCode(438, "Stale Nonce"); b.AddString(attrNonce, s.nonce) }, false)
		return
	}
	switch method(m.Type) {
	case methodAllocate:
		reply(classSuccess, func(b *stun.Builder) {
			b.Add(attrXORRelayedAddress, xorAddress(relayedAddr, m.Transaction))
			b.Add(attrXORMappedAddress, xorAddress(mappedAddr, m.Transaction))
			b.AddUint32(attrLifetime, 600)
		}, true)
	case methodCreatePermission:
		v, _ := m.Get(attrXORPeerAddress)
		peer, _ := decodeXORAddress(v, m.Transaction)
		if s.permissions == nil {
			s.permissions = make(map[netip.Addr]int)
		}
		s.permissions[peer.Addr()]++
		reply(classSuccess, func(*stun.Builder) {}, true)
	case methodRefresh:
		if s.refreshN++; s.refreshN == 1 {
			s.nonce = "nonce-2"
			reply(classError, func(b *stun.Builder) { b.AddErrorCode(438, "Stale Nonce"); b.AddString(attrNonce, s.nonce) }, false)
			return
		}
		reply(classSuccess, func(b *stun.Builder) { b.AddUint32(attrLifetime, 600) }, true)
	}
}

// exchange delivers what the client sent to the server and back until
// both are quiet.
func exchange(t *testing.T, s *fakeServer, c *Client, toServer *[][]byte, now time.Time) (data []string) {
	for len(*toServer) > 0 || len(s.out) > 0 {
		for len(*toServer) > 0 {
			b := (*toServer)[0]
			*toServer = (*toServer)[1:]
			s.handle(b)
		}
		for len(s.out) > 0 {
			b := s.out[0]
			s.out = s.out[1:]
			if peer, d, ok := c.HandleDatagram(b, now); ok {
				data = append(data, peer.String()+" "+string(d))
			}
		}
	}
	return data
}

func TestAllocation(t *testing.T) {
	s := &fakeServer{t: t, nonce: "nonce-1"}
	var toServer [][]byte
	c := New(Config{Server: serverAddr, Username: "user", Password: "pass"}, func(b []byte, to netip.AddrPort) {
		if to != serverAddr {
			t.Fatalf("sent to %v", to)
		}
		toServer = append(toServer, append([]byte(nil), b...))
	})
	now := time.Unix(1000, 0)
	c.Start(now)
	exchange(t, s, c, &toServer, now)
	if !c.Allocated() || c.Relayed() != relayedAddr || c.Mapped() != mappedAddr {
		t.Fatalf("allocated %v, relayed %v, mapped %v, err %v", c.Allocated(), c.Relayed(), c.Mapped(), c.Err())
	}

	// A peer not permitted is not heard.
	data := func(from netip.AddrPort, d string) []byte {
		var tx [12]byte
		b := stun.NewBuilder(nil, msgType(methodData, classIndication), tx)
		b.Add(attrXORPeerAddress, xorAddress(from, tx))
		b.Add(attrData, []byte(d))
		return b.Bytes()
	}
	if _, _, ok := c.HandleDatagram(data(peerAddr, "early"), now); ok {
		t.Fatal("data of a peer not permitted handed over")
	}
	c.Permit(peerAddr.Addr(), now)
	exchange(t, s, c, &toServer, now)
	if !c.Permitted(peerAddr.Addr(), now) {
		t.Fatal("permission not granted")
	}
	if peer, d, ok := c.HandleDatagram(data(peerAddr, "hello"), now); !ok || peer != peerAddr || string(d) != "hello" {
		t.Fatalf("data %v %q %v", peer, d, ok)
	}
	if err := c.Send(peerAddr, []byte("back")); err != nil {
		t.Fatal(err)
	}
	exchange(t, s, c, &toServer, now)
	if len(s.sent) != 1 || s.sent[0] != peerAddr.String()+" back" {
		t.Fatalf("server relayed %q", s.sent)
	}

	// The refresh before the lifetime ends takes a stale nonce in its
	// stride; the permission in use is renewed too.
	later := now.Add(9*time.Minute + time.Second)
	for at := c.Deadline(); !at.After(later); at = c.Deadline() {
		c.HandleTimeout(at)
		exchange(t, s, c, &toServer, at)
	}
	if s.refreshN != 2 || !c.Allocated() || c.Err() != nil {
		t.Fatalf("refreshes %d, allocated %v, err %v", s.refreshN, c.Allocated(), c.Err())
	}
	if !c.Permitted(peerAddr.Addr(), later.Add(3*time.Minute)) {
		t.Fatal("permission not renewed")
	}
}

// allocate returns a client allocated on s at now, and the datagrams it
// sends the server.
func allocate(t *testing.T, s *fakeServer, now time.Time) (*Client, *[][]byte) {
	toServer := new([][]byte)
	c := New(Config{Server: serverAddr, Username: "user", Password: "pass"}, func(b []byte, _ netip.AddrPort) {
		*toServer = append(*toServer, append([]byte(nil), b...))
	})
	c.Start(now)
	exchange(t, s, c, toServer, now)
	if !c.Allocated() {
		t.Fatalf("not allocated: %v", c.Err())
	}
	return c, toServer
}

// run handles the deadlines of the client and exchanges what it sends
// with s until past end.
func run(t *testing.T, s *fakeServer, c *Client, toServer *[][]byte, end time.Time) {
	for at := c.Deadline(); !at.After(end); at = c.Deadline() {
		c.HandleTimeout(at)
		exchange(t, s, c, toServer, at)
	}
}

// TestPermissionReleased renews the permission of a peer while it is
// wanted and lets that of a released one lapse on the server.
func TestPermissionReleased(t *testing.T) {
	s := &fakeServer{t: t, nonce: "nonce-1"}
	now := time.Unix(1000, 0)
	c, toServer := allocate(t, s, now)
	kept, released := netip.MustParseAddr("192.0.2.9"), netip.MustParseAddr("192.0.2.10")
	c.Permit(kept, now)
	c.Permit(released, now)
	exchange(t, s, c, toServer, now)
	c.Release(released)
	// The permission released lasts until it lapses on the server, past
	// the renewal of the one kept.
	renewed := now.Add(permitLifetime - permitRefresh)
	run(t, s, c, toServer, renewed)
	if !c.Permitted(released, renewed) {
		t.Fatal("released permission forgotten before it lapsed")
	}
	later := now.Add(2 * permitLifetime)
	run(t, s, c, toServer, later)
	// Asked for at once, and renewed a minute before it runs out twice.
	if !c.Permitted(kept, later) || s.permissions[kept] != 3 {
		t.Fatalf("permission of a wanted peer: %d requests in ten minutes, want 3", s.permissions[kept])
	}
	if c.Permitted(released, now.Add(permitLifetime)) || s.permissions[released] != 1 {
		t.Fatalf("permission of a released peer renewed: %d requests", s.permissions[released])
	}
	// One wanted again is asked for anew.
	c.Permit(released, later)
	exchange(t, s, c, toServer, later)
	if !c.Permitted(released, later) {
		t.Fatal("permission of a peer wanted again not granted")
	}
}

// TestPermitBound grants permissions to as many peers as a listener may
// ask for, 256 connections opening with 8 addresses each and as many
// open, and refuses those past the bound until one is released.
func TestPermitBound(t *testing.T) {
	s := &fakeServer{t: t, nonce: "nonce-1"}
	now := time.Unix(1000, 0)
	c, toServer := allocate(t, s, now)
	const bound = 4096
	peer := func(i int) netip.Addr { return netip.AddrFrom4([4]byte{10, 0, byte(i >> 8), byte(i)}) }
	for i := range bound + 1 {
		c.Permit(peer(i), now)
	}
	exchange(t, s, c, toServer, now)
	for i := range bound {
		if !c.Permitted(peer(i), now) {
			t.Fatalf("peer %d of %d not permitted", i, bound)
		}
	}
	if c.Permitted(peer(bound), now) {
		t.Fatal("peer past the bound permitted")
	}
	c.Release(peer(0))
	c.Permit(peer(bound), now)
	exchange(t, s, c, toServer, now)
	if !c.Permitted(peer(bound), now) {
		t.Fatal("peer not permitted in the room of a released one")
	}
}

// TestPermitBeforeAllocation asks for the permissions wanted before the
// allocation was made once it is, but for those released since.
func TestPermitBeforeAllocation(t *testing.T) {
	s := &fakeServer{t: t, nonce: "nonce-1"}
	var toServer [][]byte
	c := New(Config{Server: serverAddr, Username: "user", Password: "pass"}, func(b []byte, _ netip.AddrPort) {
		toServer = append(toServer, append([]byte(nil), b...))
	})
	now := time.Unix(1000, 0)
	early, allocating, released := netip.MustParseAddr("192.0.2.9"), netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("192.0.2.11")
	c.Permit(early, now)
	c.Permit(released, now)
	c.Start(now)
	c.Permit(allocating, now)
	c.Release(released)
	exchange(t, s, c, &toServer, now)
	if !c.Permitted(early, now) || !c.Permitted(allocating, now) {
		t.Fatalf("permitted before the allocation %v, while it was made %v", c.Permitted(early, now), c.Permitted(allocating, now))
	}
	if s.permissions[released] != 0 {
		t.Fatal("permission released before the allocation asked for")
	}
}

// TestAllocationMalformedRelayedAddress fails an allocation whose success
// carries a relayed address it cannot read.
func TestAllocationMalformedRelayedAddress(t *testing.T) {
	var sent []byte
	c := New(Config{Server: serverAddr}, func(b []byte, _ netip.AddrPort) { sent = append([]byte(nil), b...) })
	now := time.Unix(0, 0)
	c.Start(now)
	var m stun.Message
	if err := stun.Parse(sent, &m); err != nil {
		t.Fatal(err)
	}
	b := stun.NewBuilder(nil, msgType(methodAllocate, classSuccess), m.Transaction)
	b.Add(attrXORRelayedAddress, []byte{0, 1, 2})
	c.HandleDatagram(b.Bytes(), now)
	if c.Allocated() || c.Err() == nil {
		t.Fatalf("allocated %v, err %v", c.Allocated(), c.Err())
	}
}

// TestPermitAskedOnce asks for a permission once while the request is
// out, however often the peer is wanted or the timers run.
func TestPermitAskedOnce(t *testing.T) {
	s := &fakeServer{t: t, nonce: "nonce-1"}
	now := time.Unix(1000, 0)
	c, toServer := allocate(t, s, now)
	peer := peerAddr.Addr()
	c.Permit(peer, now)
	c.Permit(peer, now)
	c.HandleTimeout(now)
	exchange(t, s, c, toServer, now)
	if !c.Permitted(peer, now) || s.permissions[peer] != 1 {
		t.Fatalf("permitted %v after %d requests, want 1", c.Permitted(peer, now), s.permissions[peer])
	}
}

// TestAllocationRetransmits sends a request again while the server is
// lost, half a second after the first send and twice as long after each
// other, as RFC 8489 has it, and fails once it gave up.
func TestAllocationRetransmits(t *testing.T) {
	start := time.Unix(0, 0)
	now := start
	var sends []time.Duration
	c := New(Config{Server: serverAddr, Username: "u", Password: "p"}, func([]byte, netip.AddrPort) { sends = append(sends, now.Sub(start)) })
	c.Start(now)
	for i := 0; i < 20 && c.Err() == nil; i++ {
		now = c.Deadline()
		c.HandleTimeout(now)
	}
	want := []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond, 3500 * time.Millisecond,
		7500 * time.Millisecond, 15500 * time.Millisecond, 31500 * time.Millisecond}
	if !slices.Equal(sends, want) || now.Sub(start) != 63500*time.Millisecond || c.Err() == nil || c.Allocated() {
		t.Fatalf("sent at %v, failed at %v with %v", sends, now.Sub(start), c.Err())
	}
}

func FuzzHandleDatagram(f *testing.F) {
	f.Add([]byte{0, 1, 0, 0, 0x21, 0x12, 0xa4, 0x42, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
	f.Fuzz(func(t *testing.T, b []byte) {
		c := New(Config{Server: serverAddr}, func([]byte, netip.AddrPort) {})
		c.Start(time.Unix(0, 0))
		c.HandleDatagram(b, time.Unix(0, 0))
		c.state = stateAllocated
		c.permits[peerAddr.Addr()] = time.Unix(100, 0)
		c.HandleDatagram(b, time.Unix(0, 0))
	})
}
