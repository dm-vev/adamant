package builtin

import (
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// netstatCommand shows the state of the connections of players: address,
// protocol, round trip, the congestion of SCTP and the queues on both
// sides.
type netstatCommand struct {
	opGate
	Targets []cmd.Target `cmd:"target"`
}

func newNetstatCommand() cmd.Command {
	return newCommand("netstat", nil, netstatCommand{})
}

func (n netstatCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	players := playersFromTargets(n.Targets)
	if len(players) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	for _, p := range players {
		st, ok := p.NetStats()
		if !ok {
			o.Printf("%s: no connection", p.Name())
			continue
		}
		o.Printf("---- %s ----", p.Name())
		o.Printf("Address: %v, latency %v", st.Addr, st.Latency.Round(time.Millisecond))
		o.Printf("Queued for the client: %d packets; from the client: %d", st.Queued, st.Inbox)
		if !st.HasConn {
			continue
		}
		c := st.Conn
		relay := ""
		if c.Relayed {
			relay = " through the TURN relay"
		}
		o.Printf("Protocol %d over NetherNet%s, connected %v ago", c.Protocol, relay, time.Since(c.Connected).Round(time.Second))
		o.Printf("SCTP: RTT %v, RTO %v, cwnd %d B, in flight %d B, buffered %d B", c.SCTP.RTT.Round(time.Millisecond),
			c.SCTP.RTO.Round(time.Millisecond), c.SCTP.Cwnd, c.SCTP.Flight, c.SCTP.Buffered)
		o.Printf("Retransmitted chunks %d, timeouts %d", c.SCTP.Retransmits, c.SCTP.Timeouts)
		o.Printf("Sent %d batches (%d B), received %d (%d B)", c.MessagesSent, c.BytesSent, c.MessagesReceived, c.BytesReceived)
	}
}
