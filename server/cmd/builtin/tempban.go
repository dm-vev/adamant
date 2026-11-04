package builtin

import (
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// tempBanCmd bans a player for a time, such as 90m, 12h, 7d or 1w2d, at
// most [Bans] MaxDuration. Java has the expiry in its file only; a command
// of its own keeps /ban's greedy reason unambiguous.
type tempBanCmd struct {
	admin
	srv      serverAdapter
	Name     string                    `cmd:"player"`
	Duration string                    `cmd:"duration"`
	Reason   cmd.Optional[cmd.Varargs] `cmd:"reason"`
}

// tempBanIPCmd bans an address, a network or the address of a player
// online for a time, as /ban-ip does for ever.
type tempBanIPCmd struct {
	admin
	srv      serverAdapter
	IP       string                    `cmd:"address"`
	Duration string                    `cmd:"duration"`
	Reason   cmd.Optional[cmd.Varargs] `cmd:"reason"`
}

func newTempBanCommand(srv serverAdapter) cmd.Command {
	return newCommand("tempban", nil, tempBanCmd{srv: srv})
}
func newTempBanIPCommand(srv serverAdapter) cmd.Command {
	return newCommand("tempban-ip", nil, tempBanIPCmd{srv: srv})
}

// Run bans the player for the time passed: "Banned Griefer until
// 2026-11-11 at 14:20:31 UTC: griefing"; over a ban, the ban now ends then.
func (c tempBanCmd) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	d, err := server.ParseBanDuration(c.Duration)
	if err != nil {
		o.Error(err)
		return
	}
	t, ok := addTarget(c.srv, o, c.Name)
	if !ok {
		return
	}
	reason := reasonArg(c.Reason)
	change, err := c.srv.Ban(t.Arg(), server.BanOptions{Reason: reason, By: c.srv.ActorOf(src), For: d})
	if err != nil {
		o.Error(err)
		return
	}
	until := banEnd(c.srv, t.Arg(), false)
	switch {
	case change == server.BanUpdated:
		o.Printf("The ban of %s now ends on %s.", t, until)
	case t.Pending:
		o.Printf(pendingAdded, t.Name, t.Name, "is banned until "+until)
	default:
		o.Print(bannedText("Banned "+t.String()+" until "+until, reason))
	}
}

// Run bans the address for the time passed, and names the players online
// it kicks, as /ban-ip does.
func (c tempBanIPCmd) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	d, err := server.ParseBanDuration(c.Duration)
	if err != nil {
		o.Error(err)
		return
	}
	key, err := c.srv.BanTarget(c.IP)
	if err != nil {
		o.Error(err)
		return
	}
	reason := reasonArg(c.Reason)
	affected := c.srv.PlayersAt(key)
	change, err := c.srv.BanAddress(key, server.BanOptions{Reason: reason, By: c.srv.ActorOf(src), For: d})
	if err != nil {
		o.Error(err)
		return
	}
	until := banEnd(c.srv, key, true)
	if change == server.BanUpdated {
		o.Printf("The ban of IP %s now ends on %s.", key, until)
		return
	}
	o.Print(bannedText("Banned IP "+key+" until "+until, reason))
	if len(affected) > 0 {
		o.Printf("This ban affects %d player(s): %s", len(affected), strings.Join(affected, ", "))
	}
}

// banEnd returns when the ban of the target passed ends, as [Bans]
// TimeFormat writes it.
func banEnd(srv serverAdapter, target string, ip bool) string {
	if b, ok := srv.BanOf(target, ip); ok && b.Until != "" {
		return b.Until
	}
	return "(Unknown)"
}
