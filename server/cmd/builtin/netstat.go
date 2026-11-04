package builtin

import (
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// netstatCommand shows the state of the connections of players: address,
// round trip and the queues on both sides, then what the client of each
// tells of its transport, such as the protocol and the congestion of SCTP.
type netstatCommand struct {
	admin
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
		for _, line := range st.Transport {
			o.Print(line)
		}
	}
}
