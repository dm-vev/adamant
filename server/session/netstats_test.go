package session

import (
	"fmt"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/sctp"
)

// oldNetstatTransport is the part of the netstat command that described the
// transport of a connection before the session did, with time.Since(x) as
// now.Sub(x): the oracle of TestTransportLines.
func oldNetstatTransport(o *cmd.Output, c netconn.Stats, now time.Time) {
	if c.Proxied {
		// A proxy carries the client, whose transport the proxy keeps.
		o.Printf("Protocol %d through a proxy, connected %v ago", c.Protocol, now.Sub(c.Connected).Round(time.Second))
		return
	}
	relay := ""
	if c.Relayed {
		relay = " through the TURN relay"
	}
	o.Printf("Protocol %d over NetherNet%s, connected %v ago", c.Protocol, relay, now.Sub(c.Connected).Round(time.Second))
	o.Printf("SCTP: RTT %v, RTO %v, cwnd %d B, in flight %d B, buffered %d B", c.SCTP.RTT.Round(time.Millisecond),
		c.SCTP.RTO.Round(time.Millisecond), c.SCTP.Cwnd, c.SCTP.Flight, c.SCTP.Buffered)
	o.Printf("Retransmitted chunks %d, timeouts %d", c.SCTP.Retransmits, c.SCTP.Timeouts)
	o.Printf("Sent %d batches (%d B), received %d (%d B), %d unreliable", c.MessagesSent, c.BytesSent, c.MessagesReceived, c.BytesReceived, c.UnreliableReceived)
	peer := c.SCTP.Peer
	o.Printf("Client SCTP: window %d B, streams %d/%d, extensions % x, parameters %x; DTLS 1.3 offered: %v",
		peer.Window, peer.OutStreams, peer.InStreams, peer.Extensions, peer.Params, c.DTLS13Offered)
}

// TestTransportLines holds the lines that describe the transport of a
// connection to what the netstat command printed for it before: for a
// proxied connection, and for NetherNet connections direct and through the
// TURN relay.
func TestTransportLines(t *testing.T) {
	now := time.Date(2026, 11, 3, 20, 0, 0, 0, time.UTC)
	nn := nethernet.Stats{
		Remote: netip.MustParseAddrPort("203.0.113.7:19132"),
		SCTP: sctp.Stats{
			RTT: 41*time.Millisecond + 600*time.Microsecond, RTO: 1200*time.Millisecond + 400*time.Microsecond,
			Cwnd: 48000, Flight: 3100, Buffered: 7400, Retransmits: 12, Timeouts: 1,
			Peer: sctp.PeerFeatures{Window: 1 << 20, OutStreams: 65535, InStreams: 1024, Extensions: []byte{0x40, 0xc0, 0x82}, Params: []uint16{0x8008, 0xc000}},
		},
		MessagesSent: 1500, BytesSent: 2_400_000, MessagesReceived: 900, BytesReceived: 120_000, UnreliableReceived: 33, DTLS13Offered: true,
	}
	relayed := nn
	relayed.Relayed, relayed.DTLS13Offered = true, false
	relayed.SCTP.Peer = sctp.PeerFeatures{}
	for name, c := range map[string]netconn.Stats{
		"proxied":  {Protocol: 975, Connected: now.Add(-90*time.Second - 400*time.Millisecond), Proxied: true},
		"direct":   {Stats: nn, Protocol: 975, Connected: now.Add(-3*time.Hour - 2*time.Second)},
		"relayed":  {Stats: relayed, Protocol: 975, Connected: now.Add(-1500 * time.Millisecond)},
		"just now": {Stats: nn, Protocol: 975, Connected: now},
	} {
		o := &cmd.Output{}
		oldNetstatTransport(o, c, now)
		var want []string
		for _, m := range o.Messages() {
			want = append(want, m.String())
		}
		if got := transportLines(c, now); !slices.Equal(got, want) {
			t.Errorf("%s: transportLines =\n%s\nwant\n%s", name, fmt.Sprint(got), fmt.Sprint(want))
		}
	}
}
