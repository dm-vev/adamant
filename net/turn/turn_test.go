package turn

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"net/netip"
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

// TestAllocationRetransmits sends a request again while the server is
// lost, and fails once it gave up.
func TestAllocationRetransmits(t *testing.T) {
	sends := 0
	c := New(Config{Server: serverAddr, Username: "u", Password: "p"}, func([]byte, netip.AddrPort) { sends++ })
	now := time.Unix(0, 0)
	c.Start(now)
	for i := 0; i < 20 && c.Err() == nil; i++ {
		now = c.Deadline()
		c.HandleTimeout(now)
	}
	if sends != maxSends || c.Err() == nil || c.Allocated() {
		t.Fatalf("%d sends, err %v", sends, c.Err())
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
