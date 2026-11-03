package session

import (
	"fmt"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/server/client"
)

// NetStats returns the state of the connection of the session: its address,
// latency and queues, and the lines of transportLines if the connection
// tells its state.
func (s *Session) NetStats() client.NetStats {
	st := client.NetStats{Addr: s.conn.RemoteAddr(), Latency: s.conn.Latency()}
	s.packetsMu.Lock()
	st.Queued = len(s.pendingPackets) + len(s.heldPackets)
	s.packetsMu.Unlock()
	s.inboxMu.Lock()
	st.Inbox = len(s.inbox)
	s.inboxMu.Unlock()
	if c, ok := s.conn.(interface{ Stats() netconn.Stats }); ok {
		st.Transport = transportLines(c.Stats(), time.Now())
	}
	return st
}

// transportLines describes the transport of a connection of the state c at
// the time now: the protocol, and for a NetherNet connection the state of
// SCTP and the counts of its messages. A proxy carries the client of a
// proxied connection and keeps its transport, so only the protocol is told.
func transportLines(c netconn.Stats, now time.Time) []string {
	connected := now.Sub(c.Connected).Round(time.Second)
	if c.Proxied {
		return []string{fmt.Sprintf("Protocol %d through a proxy, connected %v ago", c.Protocol, connected)}
	}
	relay := ""
	if c.Relayed {
		relay = " through the TURN relay"
	}
	peer := c.SCTP.Peer
	return []string{
		fmt.Sprintf("Protocol %d over NetherNet%s, connected %v ago", c.Protocol, relay, connected),
		fmt.Sprintf("SCTP: RTT %v, RTO %v, cwnd %d B, in flight %d B, buffered %d B", c.SCTP.RTT.Round(time.Millisecond),
			c.SCTP.RTO.Round(time.Millisecond), c.SCTP.Cwnd, c.SCTP.Flight, c.SCTP.Buffered),
		fmt.Sprintf("Retransmitted chunks %d, timeouts %d", c.SCTP.Retransmits, c.SCTP.Timeouts),
		fmt.Sprintf("Sent %d batches (%d B), received %d (%d B), %d unreliable", c.MessagesSent, c.BytesSent, c.MessagesReceived, c.BytesReceived, c.UnreliableReceived),
		fmt.Sprintf("Client SCTP: window %d B, streams %d/%d, extensions % x, parameters %x; DTLS 1.3 offered: %v",
			peer.Window, peer.OutStreams, peer.InStreams, peer.Extensions, peer.Params, c.DTLS13Offered),
	}
}
