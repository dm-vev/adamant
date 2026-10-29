// Package turn is the client side of TURN over UDP (RFC 8656), without I/O
// of its own: a Client allocates a relayed address on a TURN server with
// the long-term credentials the server hands out, keeps the allocation
// and the permissions of its peers alive, and wraps what goes to a peer in
// Send indications and unwraps the Data indications of what comes from
// one. The allocation also tells the address the server saw the client
// at, a server-reflexive candidate of its own.
package turn

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"net/netip"
	"time"

	"github.com/dm-vev/adamant/net/stun"
)

// Methods of TURN.
const (
	methodAllocate         = 0x003
	methodRefresh          = 0x004
	methodSend             = 0x006
	methodData             = 0x007
	methodCreatePermission = 0x008
)

// Classes of a STUN message.
const (
	classRequest    = 0x000
	classIndication = 0x010
	classSuccess    = 0x100
	classError      = 0x110
)

// Attributes of TURN.
const (
	attrLifetime           = 0x000D
	attrXORPeerAddress     = 0x0012
	attrData               = 0x0013
	attrRealm              = 0x0014
	attrNonce              = 0x0015
	attrXORRelayedAddress  = 0x0016
	attrRequestedTransport = 0x0019
	attrXORMappedAddress   = 0x0020
)

// msgType returns the type of a message of a method and a class: the bits
// of the class sit between those of the method.
func msgType(method, class uint16) stun.Type {
	return stun.Type(method&0x000f | (method&0x0070)<<1 | (method&0x0f80)<<2 | class)
}

// method returns the method of a message type and class its class.
func method(t stun.Type) uint16 {
	v := uint16(t)
	return v&0x000f | (v&0x00e0)>>1 | (v&0x3e00)>>2
}

func class(t stun.Type) uint16 { return uint16(t) & 0x0110 }

// Config configures a Client.
type Config struct {
	// Server is the address of the TURN server.
	Server netip.AddrPort
	// Username and Password are the long-term credentials the server
	// takes.
	Username, Password string
	// Lifetime is the lifetime asked for the allocation. Zero means the
	// ten minutes of the server's default.
	Lifetime time.Duration
}

// States of a Client.
const (
	stateNew = iota
	stateAllocating
	stateAllocated
	stateClosed
)

// Errors of a Client.
var (
	ErrNotAllocated = errors.New("turn: no allocation")
	errFailed       = errors.New("turn: allocation failed")
)

// Client is an allocation on a TURN server. Its methods are not safe for
// concurrent use: the caller serializes them, as it does for the other
// layers of the module.
type Client struct {
	cfg  Config
	send func(b []byte, to netip.AddrPort)

	state   int
	err     error
	realm   string
	nonce   string
	key     []byte
	relayed netip.AddrPort
	mapped  netip.AddrPort
	expires time.Time

	// pending holds the requests waiting for a response, by transaction.
	pending map[[12]byte]*transaction
	// permits holds when the permission of each peer address runs out.
	permits map[netip.Addr]time.Time
	// asked holds the peer addresses a CreatePermission is out for.
	asked map[netip.Addr]bool

	msg stun.Message
}

// transaction is a request waiting for its response.
type transaction struct {
	method  uint16
	peer    netip.Addr
	raw     []byte
	sent    int
	next    time.Time
	retried bool
}

// Retransmission of requests over UDP, as RFC 8489 section 6.2.1 sets it:
// an initial RTO of half a second doubling, seven sends in all.
const (
	rto      = 500 * time.Millisecond
	maxSends = 7
)

// permitLifetime is how long a permission lasts on the server, and
// permitRefresh how long before that the client renews one in use.
const (
	permitLifetime = 5 * time.Minute
	permitRefresh  = time.Minute
)

// New returns a Client that sends datagrams to the server with send.
func New(cfg Config, send func(b []byte, to netip.AddrPort)) *Client {
	return &Client{
		cfg:     cfg,
		send:    send,
		pending: make(map[[12]byte]*transaction),
		permits: make(map[netip.Addr]time.Time),
		asked:   make(map[netip.Addr]bool),
	}
}

// Start asks the server for an allocation.
func (c *Client) Start(now time.Time) {
	if c.state != stateNew {
		return
	}
	c.state = stateAllocating
	c.request(methodAllocate, netip.Addr{}, now)
}

// Allocated reports whether the client holds an allocation.
func (c *Client) Allocated() bool { return c.state == stateAllocated }

// Relayed returns the relayed address of the allocation, where peers send
// to reach the client.
func (c *Client) Relayed() netip.AddrPort { return c.relayed }

// Mapped returns the address the server saw the client at.
func (c *Client) Mapped() netip.AddrPort { return c.mapped }

// Err returns why the allocation failed, or nil.
func (c *Client) Err() error { return c.err }

// Server returns the address of the server: datagrams from it go to
// HandleDatagram.
func (c *Client) Server() netip.AddrPort { return c.cfg.Server }

// Permit lets peer send to the client through the relay, and the client
// send to it. Permissions are per IP address and kept alive while used.
func (c *Client) Permit(peer netip.Addr, now time.Time) {
	peer = peer.Unmap()
	if c.state != stateAllocated || c.asked[peer] {
		return
	}
	if until, ok := c.permits[peer]; ok && now.Add(permitRefresh).Before(until) {
		return
	}
	if len(c.asked)+len(c.permits) >= maxPermits {
		return
	}
	c.asked[peer] = true
	c.request(methodCreatePermission, peer, now)
}

// maxPermits bounds the peers of an allocation.
const maxPermits = 256

// Permitted reports whether peer may send through the relay.
func (c *Client) Permitted(peer netip.Addr, now time.Time) bool {
	until, ok := c.permits[peer.Unmap()]
	return ok && now.Before(until)
}

// Send sends data to peer through the relay, which takes it once the peer
// was permitted.
func (c *Client) Send(peer netip.AddrPort, data []byte) error {
	if c.state != stateAllocated {
		return ErrNotAllocated
	}
	var tx [12]byte
	_, _ = rand.Read(tx[:])
	b := stun.NewBuilder(nil, msgType(methodSend, classIndication), tx)
	b.Add(attrXORPeerAddress, xorAddress(peer, tx))
	b.Add(attrData, data)
	c.send(b.Bytes(), c.cfg.Server)
	return nil
}

// HandleDatagram handles a datagram from the server. A Data indication of
// a permitted peer returns the peer and what it sent.
func (c *Client) HandleDatagram(b []byte, now time.Time) (peer netip.AddrPort, data []byte, ok bool) {
	if c.state == stateNew || c.state == stateClosed || stun.Parse(b, &c.msg) != nil {
		return netip.AddrPort{}, nil, false
	}
	m := &c.msg
	if class(m.Type) == classIndication {
		if method(m.Type) != methodData {
			return netip.AddrPort{}, nil, false
		}
		v, has := m.Get(attrXORPeerAddress)
		d, hasData := m.Get(attrData)
		if !has || !hasData {
			return netip.AddrPort{}, nil, false
		}
		peer, valid := decodeXORAddress(v, m.Transaction)
		if !valid || !c.Permitted(peer.Addr(), now) {
			return netip.AddrPort{}, nil, false
		}
		return peer, d, true
	}
	t := c.pending[m.Transaction]
	if t == nil || method(m.Type) != t.method {
		return netip.AddrPort{}, nil, false
	}
	// Responses to requests with the credentials must carry them, but for
	// those asking for the credentials or a fresh nonce, which cannot.
	if c.key != nil && !m.CheckIntegrity(c.mac()) {
		if code := errorCode(m); class(m.Type) != classError || code != 401 && code != 438 {
			return netip.AddrPort{}, nil, false
		}
	}
	delete(c.pending, m.Transaction)
	switch class(m.Type) {
	case classSuccess:
		c.succeeded(t, now)
	case classError:
		c.failed(t, now)
	}
	return netip.AddrPort{}, nil, false
}

// succeeded handles the success of a request.
func (c *Client) succeeded(t *transaction, now time.Time) {
	m := &c.msg
	switch t.method {
	case methodAllocate:
		v, ok := m.Get(attrXORRelayedAddress)
		relayed, valid := decodeXORAddress(v, m.Transaction)
		if !ok || !valid {
			c.fail(errors.New("turn: allocation without a relayed address"))
			return
		}
		c.relayed = relayed
		if v, ok := m.Get(attrXORMappedAddress); ok {
			c.mapped, _ = decodeXORAddress(v, m.Transaction)
		}
		c.state = stateAllocated
		c.expires = now.Add(c.lifetime(m))
	case methodRefresh:
		c.expires = now.Add(c.lifetime(m))
	case methodCreatePermission:
		delete(c.asked, t.peer)
		c.permits[t.peer] = now.Add(permitLifetime)
	}
}

// lifetime returns the lifetime a response grants, the default if none.
func (c *Client) lifetime(m *stun.Message) time.Duration {
	if s, ok := m.Uint32(attrLifetime); ok {
		return time.Duration(s) * time.Second
	}
	return 10 * time.Minute
}

// failed handles an error response: one that asks for the credentials or
// a fresh nonce is retried once with them.
func (c *Client) failed(t *transaction, now time.Time) {
	m := &c.msg
	code := errorCode(m)
	if (code == 401 || code == 438) && !t.retried {
		realm, hasRealm := m.Get(attrRealm)
		nonce, hasNonce := m.Get(attrNonce)
		if hasNonce && (hasRealm || c.realm != "") {
			if hasRealm {
				c.realm = string(realm)
			}
			c.nonce = string(nonce)
			key := md5.Sum([]byte(c.cfg.Username + ":" + c.realm + ":" + c.cfg.Password))
			c.key = key[:]
			c.retry(t, now)
			return
		}
	}
	switch t.method {
	case methodAllocate:
		c.fail(fmt.Errorf("%w: error %d", errFailed, code))
	case methodRefresh:
		c.fail(fmt.Errorf("turn: refresh failed with error %d", code))
	case methodCreatePermission:
		delete(c.asked, t.peer)
	}
}

// errorCode returns the code of the ERROR-CODE attribute of m, 0 if none.
func errorCode(m *stun.Message) int {
	v, ok := m.Get(stun.AttrErrorCode)
	if !ok || len(v) < 4 {
		return 0
	}
	return int(v[2]&7)*100 + int(v[3])
}

// retry sends a request again with the credentials and the nonce now
// known.
func (c *Client) retry(t *transaction, now time.Time) {
	n := c.request(t.method, t.peer, now)
	n.retried = true
}

// request sends a request of a method, for a peer for CreatePermission,
// and keeps it until its response.
func (c *Client) request(m uint16, peer netip.Addr, now time.Time) *transaction {
	var tx [12]byte
	_, _ = rand.Read(tx[:])
	b := stun.NewBuilder(nil, msgType(m, classRequest), tx)
	switch m {
	case methodAllocate:
		// UDP, the only transport of relays the game uses.
		b.AddUint32(attrRequestedTransport, 17<<24)
		if c.cfg.Lifetime > 0 {
			b.AddUint32(attrLifetime, uint32(c.cfg.Lifetime/time.Second))
		}
	case methodRefresh:
		if c.state == stateClosed {
			b.AddUint32(attrLifetime, 0)
		}
	case methodCreatePermission:
		b.Add(attrXORPeerAddress, xorAddress(netip.AddrPortFrom(peer, 0), tx))
	}
	if c.key != nil {
		b.AddString(stun.AttrUsername, c.cfg.Username)
		b.AddString(attrRealm, c.realm)
		b.AddString(attrNonce, c.nonce)
		b.AddIntegrity(c.mac())
	}
	t := &transaction{method: m, peer: peer, raw: b.Bytes(), sent: 1, next: now.Add(rto)}
	c.pending[tx] = t
	c.send(t.raw, c.cfg.Server)
	return t
}

// mac returns the HMAC-SHA1 keyed with the long-term key.
func (c *Client) mac() hash.Hash { return hmac.New(sha1.New, c.key) }

// fail ends the allocation with err.
func (c *Client) fail(err error) {
	if c.err == nil {
		c.err = err
	}
	c.state = stateClosed
	clear(c.pending)
}

// Deadline returns when HandleTimeout must next be called.
func (c *Client) Deadline() time.Time {
	var d time.Time
	earlier := func(t time.Time) {
		if !t.IsZero() && (d.IsZero() || t.Before(d)) {
			d = t
		}
	}
	for _, t := range c.pending {
		earlier(t.next)
	}
	if c.state == stateAllocated {
		earlier(c.refreshAt())
		for _, until := range c.permits {
			earlier(until.Add(-permitRefresh))
		}
	}
	return d
}

// refreshAt is when the allocation is refreshed: a minute before it runs
// out, or halfway through a short lifetime.
func (c *Client) refreshAt() time.Time {
	return c.expires.Add(-min(time.Minute, c.cfg.lifetimeOr(10*time.Minute)/2))
}

func (cfg Config) lifetimeOr(d time.Duration) time.Duration {
	if cfg.Lifetime > 0 {
		return cfg.Lifetime
	}
	return d
}

// HandleTimeout retransmits requests, refreshes the allocation and renews
// the permissions due.
func (c *Client) HandleTimeout(now time.Time) {
	for tx, t := range c.pending {
		if now.Before(t.next) {
			continue
		}
		if t.sent == maxSends {
			delete(c.pending, tx)
			switch t.method {
			case methodAllocate, methodRefresh:
				c.fail(errors.New("turn: the server does not answer"))
				return
			case methodCreatePermission:
				delete(c.asked, t.peer)
			}
			continue
		}
		t.sent++
		t.next = now.Add(rto << (t.sent - 1))
		c.send(t.raw, c.cfg.Server)
	}
	if c.state != stateAllocated {
		return
	}
	if !now.Before(c.refreshAt()) && !c.refreshing() {
		c.request(methodRefresh, netip.Addr{}, now)
	}
	for peer, until := range c.permits {
		if !now.Before(until) {
			delete(c.permits, peer)
		} else if !now.Before(until.Add(-permitRefresh)) && !c.asked[peer] {
			c.asked[peer] = true
			c.request(methodCreatePermission, peer, now)
		}
	}
}

// refreshing reports whether a Refresh is out.
func (c *Client) refreshing() bool {
	for _, t := range c.pending {
		if t.method == methodRefresh {
			return true
		}
	}
	return false
}

// Keepalive sends the server a Binding indication, which needs no answer:
// a NAT in front of the client forgets a mapping it sees no traffic on in
// a minute or two, sooner than any refresh comes.
func (c *Client) Keepalive() {
	if c.state == stateNew || c.state == stateClosed {
		return
	}
	var tx [12]byte
	_, _ = rand.Read(tx[:])
	b := stun.NewBuilder(nil, stun.BindingIndication, tx)
	b.AddFingerprint()
	c.send(b.Bytes(), c.cfg.Server)
}

// Close gives the allocation back to the server.
func (c *Client) Close(now time.Time) {
	if c.state != stateAllocated {
		c.state = stateClosed
		return
	}
	c.state = stateClosed
	c.request(methodRefresh, netip.Addr{}, now)
	clear(c.pending)
}

// xorAddress encodes an address as XOR-PEER-ADDRESS and the other XOR
// addresses carry it.
func xorAddress(addr netip.AddrPort, tx [12]byte) []byte {
	ip := addr.Addr().Unmap()
	port := addr.Port() ^ uint16(magicCookie>>16)
	if ip.Is4() {
		v := make([]byte, 8)
		v[1] = 1
		binary.BigEndian.PutUint16(v[2:], port)
		a := ip.As4()
		binary.BigEndian.PutUint32(v[4:], binary.BigEndian.Uint32(a[:])^magicCookie)
		return v
	}
	v := make([]byte, 20)
	v[1] = 2
	binary.BigEndian.PutUint16(v[2:], port)
	var key [16]byte
	binary.BigEndian.PutUint32(key[:], magicCookie)
	copy(key[4:], tx[:])
	a := ip.As16()
	for i := range a {
		v[4+i] = a[i] ^ key[i]
	}
	return v
}

// decodeXORAddress decodes an address xorAddress encodes.
func decodeXORAddress(v []byte, tx [12]byte) (netip.AddrPort, bool) {
	if len(v) < 4 {
		return netip.AddrPort{}, false
	}
	port := binary.BigEndian.Uint16(v[2:]) ^ uint16(magicCookie>>16)
	switch {
	case v[1] == 1 && len(v) == 8:
		var ip [4]byte
		binary.BigEndian.PutUint32(ip[:], binary.BigEndian.Uint32(v[4:])^magicCookie)
		return netip.AddrPortFrom(netip.AddrFrom4(ip), port), true
	case v[1] == 2 && len(v) == 20:
		var key, ip [16]byte
		binary.BigEndian.PutUint32(key[:], magicCookie)
		copy(key[4:], tx[:])
		for i := range ip {
			ip[i] = v[4+i] ^ key[i]
		}
		return netip.AddrPortFrom(netip.AddrFrom16(ip), port), true
	}
	return netip.AddrPort{}, false
}

const magicCookie = 0x2112A442
