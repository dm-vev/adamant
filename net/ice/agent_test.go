package ice

import (
	"bytes"
	"net/netip"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/stun"
)

type wire struct {
	from, to netip.AddrPort
	b        []byte
}

// pairNetwork connects a controlling client agent with addresses
// clientAddrs to a controlled server agent at serverAddr.
type pairNetwork struct {
	now      time.Time
	queue    []wire
	client   *Agent
	server   *Agent
	srvAddr  netip.AddrPort
	cliAddrs []netip.AddrPort
	// reachable decides which client addresses the server can reach.
	reachable func(netip.AddrPort) bool
}

func newPairNetwork(t *testing.T) *pairNetwork {
	n := &pairNetwork{
		now:      time.Unix(1_700_000_000, 0),
		srvAddr:  netip.MustParseAddrPort("203.0.113.5:19132"),
		cliAddrs: []netip.AddrPort{netip.MustParseAddrPort("10.0.0.2:5000"), netip.MustParseAddrPort("198.51.100.7:40000")},
	}
	server := Credentials{Ufrag: "srvf", Pwd: "server-password-0123456789"}
	client := Credentials{Ufrag: "clif", Pwd: "client-password-0123456789"}
	n.server = New(Config{Local: server, Remote: client}, func(b []byte, to netip.AddrPort) {
		n.queue = append(n.queue, wire{n.srvAddr, to, bytes.Clone(b)})
	})
	// The client sends from its first address it has a route from; the
	// second one is where the server sees it after NAT.
	n.client = New(Config{Controlling: true, Local: client, Remote: server, RemoteCandidates: []netip.AddrPort{n.srvAddr}}, func(b []byte, to netip.AddrPort) {
		n.queue = append(n.queue, wire{n.cliAddrs[1], to, bytes.Clone(b)})
	})
	return n
}

func (n *pairNetwork) run(limit time.Duration) {
	end := n.now.Add(limit)
	for n.now.Before(end) {
		q := n.queue
		n.queue = nil
		for _, w := range q {
			if w.to == n.srvAddr {
				n.server.HandleSTUN(w.b, w.from, n.now)
			} else if n.reachable == nil || n.reachable(w.to) {
				n.client.HandleSTUN(w.b, w.from, n.now)
			}
		}
		if len(n.queue) > 0 {
			n.now = n.now.Add(time.Millisecond)
			continue
		}
		next := n.client.Deadline()
		if d := n.server.Deadline(); d.Before(next) {
			next = d
		}
		if !next.After(n.now) {
			next = n.now.Add(time.Millisecond)
		}
		if next.After(end) {
			return
		}
		n.now = next
		_ = n.client.HandleTimeout(n.now)
		_ = n.server.HandleTimeout(n.now)
	}
}

func TestNominationAndTriggeredCheck(t *testing.T) {
	n := newPairNetwork(t)
	n.server.Start(n.now)
	n.client.Start(n.now)
	n.run(time.Second)
	sel, ok := n.client.Selected()
	if !ok || sel != n.srvAddr {
		t.Fatalf("client selected %v %v", sel, ok)
	}
	sel, ok = n.server.Selected()
	if !ok || sel != n.cliAddrs[1] {
		t.Fatalf("server selected %v %v, want the client's mapped address", sel, ok)
	}
	if !n.server.Validated(n.cliAddrs[1]) || n.server.Validated(n.cliAddrs[0]) {
		t.Fatal("server validated the wrong addresses")
	}
	// The triggered check of the server reached the client and got its
	// answer.
	if p := n.server.pair(n.cliAddrs[1]); p.checking || !p.succeeded {
		t.Fatalf("server check: checking %v, succeeded %v", p.checking, p.succeeded)
	}
}

func TestConsentKeepsAndLosesThePeer(t *testing.T) {
	n := newPairNetwork(t)
	n.server.Start(n.now)
	n.client.Start(n.now)
	n.run(2 * time.Minute)
	if err := n.server.HandleTimeout(n.now); err != nil {
		t.Fatalf("consent lost while both sides answer: %v", err)
	}
	// The client goes silent: the server drops it after the timeout.
	n.client = New(Config{Controlling: true, Local: Credentials{"x", "y"}, Remote: Credentials{"z", "w"}}, func([]byte, netip.AddrPort) {})
	n.run(consentTimeout + 10*time.Second)
	if err := n.server.HandleTimeout(n.now); err != ErrTimeout {
		t.Fatalf("server error %v after the peer went silent", err)
	}
}

func TestRejectsUnauthenticatedChecks(t *testing.T) {
	n := newPairNetwork(t)
	from := netip.MustParseAddrPort("192.0.2.9:1234")
	forge := func(user, pwd string) []byte {
		b := stun.NewBuilder(nil, stun.BindingRequest, [12]byte{1})
		b.AddString(stun.AttrUsername, user)
		b.AddUint64(stun.AttrICEControlling, 1)
		key := New(Config{Local: Credentials{Pwd: pwd}}, nil).localKey
		b.AddIntegrity(key)
		b.AddFingerprint()
		return b.Bytes()
	}
	if n.server.HandleSTUN(forge("srvf:clif", "wrong"), from, n.now) {
		t.Fatal("accepted a check with the wrong password")
	}
	if n.server.HandleSTUN(forge("srvf:other", "server-password-0123456789"), from, n.now) {
		t.Fatal("accepted a check for another connection")
	}
	if n.server.Validated(from) || len(n.queue) != 0 {
		t.Fatal("answered or validated a forged check")
	}
	if !n.server.HandleSTUN(forge("srvf:clif", "server-password-0123456789"), from, n.now) {
		t.Fatal("rejected an authentic check")
	}
}

func TestRoleConflictAnswered(t *testing.T) {
	n := newPairNetwork(t)
	b := stun.NewBuilder(nil, stun.BindingRequest, [12]byte{2})
	b.AddString(stun.AttrUsername, "srvf:clif")
	b.AddUint64(stun.AttrICEControlled, 1)
	b.AddIntegrity(n.server.localKey)
	b.AddFingerprint()
	from := netip.MustParseAddrPort("192.0.2.9:1234")
	n.server.HandleSTUN(b.Bytes(), from, n.now)
	if len(n.queue) != 1 {
		t.Fatalf("%d replies", len(n.queue))
	}
	var m stun.Message
	if err := stun.Parse(n.queue[0].b, &m); err != nil || m.Type != stun.BindingError {
		t.Fatalf("reply %v %v", m.Type, err)
	}
	if n.server.Validated(from) {
		t.Fatal("validated a peer in conflict")
	}
}
