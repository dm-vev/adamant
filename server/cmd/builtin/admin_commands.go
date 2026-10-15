package builtin

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type adminPermission struct{ srv serverAdapter }

func (a adminPermission) Allow(src cmd.Source) bool { return operatorAllowed(a.srv, src) }

type opCmd struct {
	adminPermission
	srv  serverAdapter
	Name string `cmd:"player"`
}
type deopCmd struct {
	adminPermission
	srv  serverAdapter
	Name string `cmd:"player"`
}
type banCmd struct {
	adminPermission
	srv    serverAdapter
	Name   string                    `cmd:"player"`
	Reason cmd.Optional[cmd.Varargs] `cmd:"reason"`
}
type banIPCmd struct {
	adminPermission
	srv    serverAdapter
	IP     string                    `cmd:"address"`
	Reason cmd.Optional[cmd.Varargs] `cmd:"reason"`
}
type pardonCmd struct {
	adminPermission
	srv  serverAdapter
	Name string `cmd:"player"`
}
type pardonIPCmd struct {
	adminPermission
	srv serverAdapter
	IP  string `cmd:"address"`
}
type banListTarget string

func (banListTarget) Type() string                { return "banlisttarget" }
func (banListTarget) Options(cmd.Source) []string { return []string{"players", "ips"} }

type banListCmd struct {
	adminPermission
	srv  serverAdapter
	Type cmd.Optional[banListTarget] `cmd:"type"`
}
type saveAllCmd struct {
	adminPermission
	srv   serverAdapter
	Flush cmd.Optional[cmd.SubCommand] `cmd:"flush"`
}
type saveToggleCmd struct {
	adminPermission
	srv     serverAdapter
	enabled bool
}
type idleTimeoutCmd struct {
	adminPermission
	srv     serverAdapter
	Minutes int `cmd:"minutes"`
}
type transferCmd struct {
	adminPermission
	srv      serverAdapter
	Hostname string                     `cmd:"hostname"`
	Port     cmd.Optional[int]          `cmd:"port"`
	Targets  cmd.Optional[[]cmd.Target] `cmd:"players"`
}
type versionCmd struct{ srv serverAdapter }

func reasonArg(v cmd.Optional[cmd.Varargs]) string {
	if r, ok := v.Load(); ok {
		return strings.TrimSpace(string(r))
	}
	return ""
}
func newOpCommand(srv serverAdapter) cmd.Command {
	return cmd.New("op", "Grants operator status to a player.", nil, opCmd{adminPermission{srv}, srv, ""})
}
func newDeopCommand(srv serverAdapter) cmd.Command {
	return cmd.New("deop", "Revokes operator status from a player.", nil, deopCmd{adminPermission{srv}, srv, ""})
}
func newBanPlayerCommand(srv serverAdapter) cmd.Command {
	return cmd.New("ban", "Bans a player from the server.", nil, banCmd{adminPermission: adminPermission{srv}, srv: srv})
}
func newBanIPCommand(srv serverAdapter) cmd.Command {
	return cmd.New("ban-ip", "Bans an IP address from the server.", nil, banIPCmd{adminPermission: adminPermission{srv}, srv: srv})
}
func newPardonCommand(srv serverAdapter) cmd.Command {
	return cmd.New("pardon", "Removes a player ban.", nil, pardonCmd{adminPermission{srv}, srv, ""})
}
func newPardonIPCommand(srv serverAdapter) cmd.Command {
	return cmd.New("pardon-ip", "Removes an IP ban.", nil, pardonIPCmd{adminPermission{srv}, srv, ""})
}
func newBanListCommand(srv serverAdapter) cmd.Command {
	return cmd.New("banlist", "Lists banned players or IP addresses.", nil, banListCmd{adminPermission: adminPermission{srv}, srv: srv})
}
func newSaveAllCommand(srv serverAdapter) cmd.Command {
	return cmd.New("save-all", "Saves the world to disk.", nil, saveAllCmd{adminPermission: adminPermission{srv}, srv: srv})
}
func newSaveOffCommand(srv serverAdapter) cmd.Command {
	return cmd.New("save-off", "Disables automatic world saving.", nil, saveToggleCmd{adminPermission{srv}, srv, false})
}
func newSaveOnCommand(srv serverAdapter) cmd.Command {
	return cmd.New("save-on", "Enables automatic world saving.", nil, saveToggleCmd{adminPermission{srv}, srv, true})
}
func newIdleTimeoutCommand(srv serverAdapter) cmd.Command {
	return cmd.New("setidletimeout", "Sets the idle timeout in minutes.", nil, idleTimeoutCmd{adminPermission{srv}, srv, 0})
}
func newTransferCommand(srv serverAdapter) cmd.Command {
	return cmd.New("transfer", "Transfers a player to another server.", nil, transferCmd{adminPermission: adminPermission{srv}, srv: srv})
}
func newVersionCommand(srv serverAdapter) cmd.Command {
	return cmd.New("version", "Displays the server version.", nil, versionCmd{srv})
}

func (c opCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	ok, err := c.srv.Op(c.Name)
	if err != nil {
		o.Error(err)
	} else if !ok {
		o.Printf("%s is already an operator.", c.Name)
	} else {
		o.Printf("Made %s a server operator.", c.Name)
	}
}
func (c deopCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	ok, err := c.srv.Deop(c.Name)
	if err != nil {
		o.Error(err)
	} else if !ok {
		o.Printf("%s is not an operator.", c.Name)
	} else {
		o.Printf("Removed %s from the server operators.", c.Name)
	}
}
func (c banCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	added, err := c.srv.BanPlayer(c.Name, reasonArg(c.Reason))
	if err != nil {
		o.Error(err)
	} else if added {
		o.Printf("Banned %s.", c.Name)
	} else {
		o.Printf("%s is already banned.", c.Name)
	}
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
	ok, err := c.srv.PardonPlayer(c.Name)
	if err != nil {
		o.Error(err)
	} else if ok {
		o.Printf("Unbanned %s.", c.Name)
	} else {
		o.Printf("%s is not banned.", c.Name)
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
func (c saveAllCmd) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	if err := c.srv.SaveAll(tx); err != nil {
		o.Error(err)
		return
	}
	o.Print("Saved the world.")
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
