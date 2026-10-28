// Package ice implements the ICE checks of one connection (RFC 8445) over
// a socket shared with every other connection: a controlled agent answers
// the checks of the peer and checks back the addresses they came from, as
// Bedrock Dedicated Server does; a controlling agent checks the candidates
// of the peer and nominates the first that answers. Neither gathers
// candidates nor opens sockets of its own.
//
// An Agent is a state machine without goroutines or I/O: the owner feeds
// it STUN messages and timer expiries with the current time. It is not safe
// for concurrent use.
package ice

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"hash"
	"net/netip"
	"time"

	"github.com/dm-vev/adamant/net/stun"
)

// Credentials are the ICE username fragment and password of one side.
type Credentials struct {
	Ufrag, Pwd string
}

// Config configures an Agent.
type Config struct {
	// Controlling makes the agent check and nominate; otherwise it answers.
	Controlling bool
	// Local and Remote are the credentials of both sides.
	Local, Remote Credentials
	// RemoteCandidates are the addresses of the peer's candidates, checked
	// by a controlling agent.
	RemoteCandidates []netip.AddrPort
}

// ErrTimeout is returned once the peer stopped proving it is there.
var ErrTimeout = errors.New("ice: peer stopped answering")

const (
	// checkInterval paces the retransmissions of a check.
	checkInterval = 100 * time.Millisecond
	maxCheckTries = 7
	// consentInterval paces the checks that keep a selected pair alive and
	// its NAT bindings open; consentTimeout drops a peer that went silent
	// (RFC 7675).
	consentInterval = 5 * time.Second
	consentTimeout  = 30 * time.Second
	// maxPairs bounds the addresses one agent tracks.
	maxPairs = 8
	// priority is the PRIORITY of the checks sent: a host candidate.
	priority = 126<<24 | 65535<<8 | 255
)

// pair is a remote address the agent checks or was checked from.
type pair struct {
	addr      netip.AddrPort
	succeeded bool
	// tx is the transaction of the check in flight, if any.
	tx        [12]byte
	checking  bool
	tries     int
	nextCheck time.Time
}

// Agent is the ICE state of one connection.
type Agent struct {
	cfg        Config
	send       func(b []byte, to netip.AddrPort)
	tieBreaker uint64
	localKey   hash.Hash
	remoteKey  hash.Hash
	username   string // the USERNAME of our checks: remote:local
	expected   string // the USERNAME of the peer's checks: local:remote
	pairs      []pair
	selected   netip.AddrPort
	haveSelect bool
	lastSeen   time.Time
	consentAt  time.Time
	failed     bool
	buf        []byte
	msg        stun.Message
}

// New returns an agent. send transmits a STUN message to an address; the
// slice is reused after it returns.
func New(cfg Config, send func(b []byte, to netip.AddrPort)) *Agent {
	a := &Agent{
		cfg:       cfg,
		send:      send,
		localKey:  hmac.New(sha1.New, []byte(cfg.Local.Pwd)),
		remoteKey: hmac.New(sha1.New, []byte(cfg.Remote.Pwd)),
		username:  cfg.Remote.Ufrag + ":" + cfg.Local.Ufrag,
		expected:  cfg.Local.Ufrag + ":" + cfg.Remote.Ufrag,
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	a.tieBreaker = binary.BigEndian.Uint64(b[:])
	return a
}

// Start begins the checks of a controlling agent and the consent timeout.
func (a *Agent) Start(now time.Time) {
	a.lastSeen = now
	if !a.cfg.Controlling {
		return
	}
	for _, c := range a.cfg.RemoteCandidates {
		if p := a.pair(c); p != nil {
			p.nextCheck = now
		}
	}
	a.HandleTimeout(now)
}

// Selected returns the address data goes to: the pair the controlling side
// nominated.
func (a *Agent) Selected() (netip.AddrPort, bool) { return a.selected, a.haveSelect }

// Validated reports whether addr sent an authenticated check or answered
// one, so that data from it may be accepted.
func (a *Agent) Validated(addr netip.AddrPort) bool {
	for i := range a.pairs {
		if a.pairs[i].addr == addr && a.pairs[i].succeeded {
			return true
		}
	}
	return false
}

// Alive records traffic from the peer authenticated by another layer, such
// as a DTLS record, which keeps consent like a check does.
func (a *Agent) Alive(now time.Time) { a.lastSeen = now }

// pair returns the pair of addr, adding it if there is room.
func (a *Agent) pair(addr netip.AddrPort) *pair {
	for i := range a.pairs {
		if a.pairs[i].addr == addr {
			return &a.pairs[i]
		}
	}
	if len(a.pairs) == maxPairs {
		return nil
	}
	a.pairs = append(a.pairs, pair{addr: addr})
	return &a.pairs[len(a.pairs)-1]
}

// Username returns the USERNAME the peer's checks carry, by which the
// owner finds the agent of a check.
func (a *Agent) Username() string { return a.expected }

// HandleSTUN processes a STUN message received from addr. It reports
// whether the message belonged to this agent and was authentic.
func (a *Agent) HandleSTUN(raw []byte, from netip.AddrPort, now time.Time) bool {
	if a.failed || stun.Parse(raw, &a.msg) != nil || !a.msg.CheckFingerprint() {
		return false
	}
	m := &a.msg
	switch m.Type {
	case stun.BindingRequest:
		user, ok := m.Get(stun.AttrUsername)
		if !ok || string(user) != a.expected || !m.CheckIntegrity(a.localKey) {
			return false
		}
		a.handleRequest(m, from, now)
		return true
	case stun.BindingSuccess, stun.BindingError:
		for i := range a.pairs {
			p := &a.pairs[i]
			if p.checking && p.tx == m.Transaction && p.addr == from {
				if !m.CheckIntegrity(a.remoteKey) {
					return false
				}
				p.checking = false
				p.nextCheck = time.Time{}
				a.lastSeen = now
				if m.Type == stun.BindingSuccess {
					p.succeeded = true
					if a.cfg.Controlling && !a.haveSelect {
						// Aggressive nomination: the check carried
						// USE-CANDIDATE.
						a.selected, a.haveSelect = p.addr, true
						a.consentAt = now.Add(consentInterval)
					}
				}
				return true
			}
		}
	}
	return false
}

func (a *Agent) handleRequest(m *stun.Message, from netip.AddrPort, now time.Time) {
	a.lastSeen = now
	if !a.cfg.Controlling && m.Has(stun.AttrICEControlled) || a.cfg.Controlling && m.Has(stun.AttrICEControlling) {
		// Both sides claim the same role: the peer must switch (RFC 8445
		// section 7.3.1.1). A server never takes the controlling role.
		a.reply(m, from, 487)
		return
	}
	a.reply(m, from, 0)
	p := a.pair(from)
	if p == nil {
		return
	}
	if !a.cfg.Controlling {
		// A check proves the address: the peer used our password.
		if !p.succeeded && !p.checking {
			// A triggered check back to the address (RFC 8445 section
			// 7.3.1.4), as a full agent does.
			p.nextCheck = now
			a.check(p, now)
		}
		p.succeeded = true
		if m.Has(stun.AttrUseCandidate) {
			a.selected, a.haveSelect = from, true
			a.consentAt = now.Add(consentInterval)
		}
	}
}

// reply answers a check with a success response, or an error response of
// code.
func (a *Agent) reply(m *stun.Message, to netip.AddrPort, code int) {
	typ := stun.BindingSuccess
	if code != 0 {
		typ = stun.BindingError
	}
	b := stun.NewBuilder(a.buf[:0], typ, m.Transaction)
	if code != 0 {
		b.AddErrorCode(code, "Role Conflict")
	} else {
		b.AddXORAddress(to)
	}
	b.AddIntegrity(a.localKey)
	b.AddFingerprint()
	a.buf = b.Bytes()
	a.send(a.buf, to)
}

// check sends a Binding request on a pair.
func (a *Agent) check(p *pair, now time.Time) {
	if !p.checking {
		p.checking = true
		p.tries = 0
		_, _ = rand.Read(p.tx[:])
	}
	p.tries++
	p.nextCheck = now.Add(checkInterval)
	b := stun.NewBuilder(a.buf[:0], stun.BindingRequest, p.tx)
	b.AddString(stun.AttrUsername, a.username)
	b.AddUint32(stun.AttrPriority, priority)
	if a.cfg.Controlling {
		b.AddUint64(stun.AttrICEControlling, a.tieBreaker)
		if !a.haveSelect {
			b.AddFlag(stun.AttrUseCandidate)
		}
	} else {
		b.AddUint64(stun.AttrICEControlled, a.tieBreaker)
	}
	b.AddIntegrity(a.remoteKey)
	b.AddFingerprint()
	a.buf = b.Bytes()
	a.send(a.buf, p.addr)
}

// Deadline returns when HandleTimeout must next be called.
func (a *Agent) Deadline() time.Time {
	if a.failed {
		return time.Time{}
	}
	d := a.lastSeen.Add(consentTimeout)
	if a.haveSelect && a.consentAt.Before(d) {
		d = a.consentAt
	}
	for i := range a.pairs {
		if p := &a.pairs[i]; !p.nextCheck.IsZero() && p.nextCheck.Before(d) {
			d = p.nextCheck
		}
	}
	return d
}

// HandleTimeout sends the checks that are due and fails the agent once the
// peer went silent.
func (a *Agent) HandleTimeout(now time.Time) error {
	if a.failed {
		return ErrTimeout
	}
	if now.Sub(a.lastSeen) >= consentTimeout {
		a.failed = true
		return ErrTimeout
	}
	for i := range a.pairs {
		p := &a.pairs[i]
		if p.nextCheck.IsZero() || now.Before(p.nextCheck) {
			continue
		}
		if p.checking && p.tries >= maxCheckTries {
			// The address does not answer.
			p.checking = false
			p.nextCheck = time.Time{}
			continue
		}
		a.check(p, now)
	}
	if a.haveSelect && !now.Before(a.consentAt) {
		// A consent check on the selected pair keeps its NAT binding open.
		a.consentAt = now.Add(consentInterval)
		if p := a.pair(a.selected); p != nil && !p.checking {
			a.check(p, now)
		}
	}
	return nil
}
