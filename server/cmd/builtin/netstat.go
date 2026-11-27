package builtin

import (
	"time"

	"github.com/dm-vev/adamant/server/client"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
)

// netstatCommand shows the state of the connections of players: address,
// round trip and the queues on both sides, then what the client of each
// tells of its transport, such as the protocol and the congestion of SCTP.
type netstatCommand struct {
	admin
	Targets cmd.Players `cmd:"target"`
}

func newNetstatCommand() cmd.Command {
	return newCommand("netstat", nil, netstatCommand{})
}

type netstatResult struct {
	isPlayer  bool
	connected bool
	stats     client.NetStats
}

func (n netstatCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	cmd.ForEach(src, o, tx, n.Targets,
		func(_ *world.Tx, t cmd.Target) netstatResult {
			p, ok := t.(*player.Player)
			if !ok {
				return netstatResult{}
			}
			st, connected := p.NetStats()
			return netstatResult{isPlayer: true, connected: connected, stats: st}
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[netstatResult]) {
			count := 0
			for _, r := range rs {
				if !r.Value.isPlayer {
					continue
				}
				count++
				if !r.Value.connected {
					o.Printf("%s: no connection", r.Name)
					continue
				}
				st := r.Value.stats
				o.Printf("---- %s ----", r.Name)
				o.Printf("Address: %v, latency %v", st.Addr, st.Latency.Round(time.Millisecond))
				o.Printf("Queued for the client: %d packets; from the client: %d", st.Queued, st.Inbox)
				for _, line := range st.Transport {
					o.Print(line)
				}
			}
			if count == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(count)
		},
	)
}
