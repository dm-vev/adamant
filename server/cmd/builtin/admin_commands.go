package builtin

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type opCmd struct {
	admin
	srv  serverAdapter
	Name string `cmd:"player"`
}
type deopCmd struct {
	admin
	srv  serverAdapter
	Name string `cmd:"player"`
}
type banCmd struct {
	admin
	srv    serverAdapter
	Name   string                    `cmd:"player"`
	Reason cmd.Optional[cmd.Varargs] `cmd:"reason"`
}
type banIPCmd struct {
	admin
	srv    serverAdapter
	IP     string                    `cmd:"address"`
	Reason cmd.Optional[cmd.Varargs] `cmd:"reason"`
}
type pardonCmd struct {
	admin
	srv  serverAdapter
	Name string `cmd:"player"`
}
type pardonIPCmd struct {
	admin
	srv serverAdapter
	IP  string `cmd:"address"`
}
type banListTarget string

func (banListTarget) Type() string                { return "banlisttarget" }
func (banListTarget) Options(cmd.Source) []string { return []string{"players", "ips"} }

type banListCmd struct {
	admin
	srv  serverAdapter
	Type cmd.Optional[banListTarget] `cmd:"type"`
}
type saveAllCmd struct {
	owner
	srv   serverAdapter
	Flush cmd.Optional[cmd.SubCommand] `cmd:"flush"`
}
type saveToggleCmd struct {
	owner
	srv     serverAdapter
	enabled bool
}
type idleTimeoutCmd struct {
	admin
	srv     serverAdapter
	Minutes int `cmd:"minutes"`
}
type transferCmd struct {
	admin
	srv      serverAdapter
	Hostname string                     `cmd:"hostname"`
	Port     cmd.Optional[int]          `cmd:"port"`
	Targets  cmd.Optional[[]cmd.Target] `cmd:"players"`
}

// versionCmd is open to every player, as it has always been on this
// server. Java's needs cmd.LevelGameMaster on a dedicated server, which the
// server may set by the name of the command.
type versionCmd struct{ srv serverAdapter }

func reasonArg(v cmd.Optional[cmd.Varargs]) string {
	if r, ok := v.Load(); ok {
		return strings.TrimSpace(string(r))
	}
	return ""
}
func newOpCommand(srv serverAdapter) cmd.Command {
	return newCommand("op", nil, opCmd{admin{}, srv, ""})
}
func newDeopCommand(srv serverAdapter) cmd.Command {
	return newCommand("deop", nil, deopCmd{admin{}, srv, ""})
}
func newBanPlayerCommand(srv serverAdapter) cmd.Command {
	return newCommand("ban", nil, banCmd{srv: srv})
}
func newBanIPCommand(srv serverAdapter) cmd.Command {
	return newCommand("ban-ip", nil, banIPCmd{srv: srv})
}
func newPardonCommand(srv serverAdapter) cmd.Command {
	return newCommand("pardon", nil, pardonCmd{admin{}, srv, ""})
}
func newPardonIPCommand(srv serverAdapter) cmd.Command {
	return newCommand("pardon-ip", nil, pardonIPCmd{admin{}, srv, ""})
}
func newBanListCommand(srv serverAdapter) cmd.Command {
	return newCommand("banlist", nil, banListCmd{srv: srv})
}
func newSaveAllCommand(srv serverAdapter) cmd.Command {
	return newCommand("save-all", nil, saveAllCmd{srv: srv})
}
func newSaveOffCommand(srv serverAdapter) cmd.Command {
	return newCommand("save-off", nil, saveToggleCmd{owner{}, srv, false})
}
func newSaveOnCommand(srv serverAdapter) cmd.Command {
	return newCommand("save-on", nil, saveToggleCmd{owner{}, srv, true})
}
func newIdleTimeoutCommand(srv serverAdapter) cmd.Command {
	return newCommand("setidletimeout", nil, idleTimeoutCmd{admin{}, srv, 0})
}
func newTransferCommand(srv serverAdapter) cmd.Command {
	return newCommand("transfer", nil, transferCmd{srv: srv})
}
func newVersionCommand(srv serverAdapter) cmd.Command {
	return newCommand("version", nil, versionCmd{srv})
}

func (c opCmd) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	t, ok := addTarget(c.srv, o, c.Name)
	if !ok {
		return
	}
	if own := cmd.SourceLevel(src); c.srv.OpLevel() > own {
		// No one gives a level above their own: an admin of level 3 makes
		// no operators of level 4.
		o.Errorf("You can't give a level above your own (%v).", own)
		return
	}
	added, err := c.srv.Op(t.Arg())
	switch {
	case err != nil:
		o.Error(err)
	case !added:
		o.Printf("%s is already an operator.", t)
	case t.Pending:
		o.Printf(pendingAdded, t.Name, t.Name, "becomes an operator")
	default:
		o.Printf("Made %s a server operator.", t)
	}
}
func (c deopCmd) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	t, ok := removeTarget(c.srv, o, server.Operators, c.Name)
	if !ok {
		return
	}
	if level, ok := c.srv.OperatorLevel(t.Arg()); ok {
		if own := cmd.SourceLevel(src); level > own {
			o.Errorf("You can't remove an operator above your own level (%v).", own)
			return
		}
	}
	removed, err := c.srv.Deop(t.Arg())
	if err != nil {
		o.Error(err)
	} else if !removed {
		o.Printf("%s is not an operator.", t)
	} else {
		o.Printf("Removed %s from the server operators.", t)
	}
}
func (c banCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	t, ok := addTarget(c.srv, o, c.Name)
	if !ok {
		return
	}
	added, err := c.srv.BanPlayer(t.Arg(), reasonArg(c.Reason))
	switch {
	case err != nil:
		o.Error(err)
	case !added:
		o.Printf("%s is already banned.", t)
	case t.Pending:
		o.Printf(pendingAdded, t.Name, t.Name, "is banned")
	default:
		o.Printf("Banned %s.", t)
	}
}

// pendingAdded is what a command that adds a name no account is known by
// to a list of a server keyed by XUID prints: the name, the name, and what
// the account that joins with it becomes.
const pendingAdded = "%s has not joined this server: the first Xbox account that joins as %s %s. Name the account with xuid:<XUID> instead."

// addTarget resolves the player an admin names to add to a list, and
// prints the error if it cannot.
func addTarget(srv serverAdapter, o *cmd.Output, name string) (server.Target, bool) {
	t, err := srv.ResolvePlayer(name)
	if err != nil {
		o.Error(err)
		return t, false
	}
	return t, true
}

// removeTarget resolves the entry of a list an admin names to remove, and
// prints the error if it cannot.
func removeTarget(srv serverAdapter, o *cmd.Output, list server.List, name string) (server.Target, bool) {
	t, err := srv.ResolveEntry(list, name)
	if err != nil {
		o.Error(err)
		return t, false
	}
	return t, true
}
func (c banIPCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	added, err := c.srv.BanIP(c.IP, reasonArg(c.Reason))
	if err != nil {
		o.Error(err)
	} else if added {
		o.Printf("Banned IP address %s.", c.IP)
	} else {
		o.Printf("IP address %s is already banned.", c.IP)
	}
}
func (c pardonCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	t, ok := removeTarget(c.srv, o, server.Banned, c.Name)
	if !ok {
		return
	}
	removed, err := c.srv.PardonPlayer(t.Arg())
	if err != nil {
		o.Error(err)
	} else if removed {
		o.Printf("Unbanned %s.", t)
	} else {
		o.Printf("%s is not banned.", t)
	}
}
func (c pardonIPCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	ok, err := c.srv.PardonIP(c.IP)
	if err != nil {
		o.Error(err)
	} else if ok {
		o.Printf("Unbanned IP address %s.", c.IP)
	} else {
		o.Printf("IP address %s is not banned.", c.IP)
	}
}
func (c banListCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	ips := false
	if t, ok := c.Type.Load(); ok {
		ips = strings.EqualFold(string(t), "ips")
	}
	entries, err := c.srv.BanEntries(ips)
	if err != nil {
		o.Error(err)
		return
	}
	label := "player"
	if ips {
		label = "IP"
	}
	o.Printf("There are %d banned %s(s).", len(entries), label)
	if len(entries) > 0 {
		o.Print(strings.Join(entries, ", "))
	}
}

// Run saves every dimension off the command's world, whose owner it would
// otherwise have to wait for, and tells the source once they were saved.
func (c saveAllCmd) Run(src cmd.Source, _ *cmd.Output, tx *world.Tx) {
	send := outputLater(src)
	c.srv.SaveAllFrom(tx, func(tx *world.Tx, err error) {
		o := &cmd.Output{}
		if err != nil {
			o.Error(err)
		} else {
			o.Print("Saved the world.")
		}
		send(tx, o)
	})
}

// outputLater returns a function that sends output to src from a later
// transaction, or from none, once the transaction of the command ended. A
// source that is an entity, as a player, is valid only in the transaction of
// the command: it is found again by its handle, wherever it is then.
func outputLater(src cmd.Source) func(tx *world.Tx, o *cmd.Output) {
	base := src
unwrap:
	for {
		switch s := base.(type) {
		case cmd.ContextSource:
			base = s.Source
		case *storeSource:
			base = s.Source
		default:
			break unwrap
		}
	}
	e, ok := base.(world.Entity)
	if !ok {
		return func(_ *world.Tx, o *cmd.Output) { base.SendCommandOutput(o) }
	}
	h := e.H()
	sendTo := func(e world.Entity, o *cmd.Output) {
		if s, ok := e.(cmd.Source); ok {
			s.SendCommandOutput(o)
		}
	}
	return func(tx *world.Tx, o *cmd.Output) {
		if tx != nil {
			if e, ok := h.Entity(tx); ok {
				sendTo(e, o)
				return
			}
		}
		h.Do(func(_ *world.Tx, e world.Entity) { sendTo(e, o) })
	}
}
func (c saveToggleCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	c.srv.SaveEnabled(c.enabled)
	if c.enabled {
		o.Print("Automatic saving is now enabled.")
	} else {
		o.Print("Automatic saving is now disabled.")
	}
}
func (c idleTimeoutCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if c.Minutes < 0 {
		o.Errort(cmd.MessageParameterInvalid, fmt.Sprint(c.Minutes))
		return
	}
	c.srv.SetIdleTimeout(c.Minutes)
	o.Printf("Set the idle timeout to %d minutes.", c.Minutes)
}
func (c transferCmd) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	port := 25565
	if value, ok := c.Port.Load(); ok {
		port = value
	}
	if port < 1 || port > 65535 {
		o.Errort(cmd.MessageParameterInvalid, port)
		return
	}
	targets, ok := c.Targets.Load()
	if !ok {
		p, isPlayer := src.(*player.Player)
		if !isPlayer {
			o.Errort(cmd.MessageNoTargets)
			return
		}
		targets = []cmd.Target{p}
	}
	players := playersFromTargets(targets)
	if len(players) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	address := net.JoinHostPort(c.Hostname, strconv.Itoa(port))
	for _, p := range players {
		if err := p.Transfer(address); err != nil {
			o.Error(err)
			return
		}
	}
	o.Printf("Transferred %s to %s:%d.", joinNames(players), c.Hostname, port)
}
func (c versionCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) { o.Print(c.srv.Version()) }
