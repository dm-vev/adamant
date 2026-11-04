package server

import (
	"fmt"
	"net"
	"strings"

	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// AuditConfig is how a server keeps its audit log, as [Audit] of
// config.toml sets it. See package server/audit.
type AuditConfig struct {
	// File is the path of the audit log.
	File string
	// MaxSize is the size the file is rotated at, in bytes; zero or less
	// never rotates.
	MaxSize int64
	// Keep is how many rotated files are kept.
	Keep int
	// MinLevel records the commands that need this level or more. Remote
	// requests, list changes and backups are recorded at any level.
	MinLevel cmd.Level
	// RecordAddresses records the addresses of players and remote clients.
	RecordAddresses bool
}

// openAudit opens the audit log of the Config, if it has one. A log that
// fails to open is an error of the log, and the server runs without it.
func (srv *Server) openAudit() {
	c := srv.conf.Audit
	if c == nil {
		return
	}
	l, err := audit.Open(audit.Config{
		File: c.File, MaxSize: c.MaxSize, Keep: c.Keep, RecordAddresses: c.RecordAddresses,
		Log: logging.Sub(srv.conf.Log, logging.Commands),
	})
	if err != nil {
		srv.conf.Log.Error("Open the audit log: " + err.Error())
		return
	}
	srv.audit = l
}

// closeAudit writes the records left and closes the audit log, if open.
func (srv *Server) closeAudit() {
	if err := srv.audit.Close(); err != nil {
		srv.conf.Log.Error("Close the audit log: " + err.Error())
	}
}

// Audit writes r to the audit log of the server, if it keeps one, without
// waiting: code that changes the lists or runs admin actions of its own,
// as a plugin will, records them here, with the actor of kind
// audit.KindAPI.
func (srv *Server) Audit(r audit.Record) {
	if srv == nil {
		return
	}
	srv.audit.Record(r)
}

// AuditDropped returns how many records the audit log dropped, as its
// writer was behind.
func (srv *Server) AuditDropped() uint64 {
	if srv == nil {
		return 0
	}
	return srv.audit.Dropped()
}

// ObserveCommand records a command a source ran, or was denied, if it
// needs [Audit] MinLevel or more, and shows the feedback of a console or a
// remote command to the operators online, as [Permissions]
// BroadcastConsoleToOps and BroadcastRemoteToOps say, with the game rule
// log_admin_commands on. builtin.Register installs it as the observer of
// package cmd: it runs on the goroutine of the command and never waits.
func (srv *Server) ObserveCommand(e cmd.Execution) {
	if srv == nil {
		return
	}
	actor := srv.actorOf(e.Source)
	srv.broadcastFeedback(e, actor)
	if srv.audit == nil || (actor.Kind != audit.KindRemote && e.Level < srv.conf.Audit.MinLevel) {
		return
	}
	r := audit.Record{
		Actor:   actor,
		Where:   whereOf(e.Source),
		Action:  e.Command.Name(),
		Command: strings.TrimSpace("/" + e.Command.Name() + " " + e.Args),
		Result:  audit.ResultOK,
	}
	switch {
	case e.Denied:
		r.Result = audit.ResultDenied
	case e.Output.ErrorCount() > 0:
		r.Result = audit.ResultError
	}
	for _, m := range e.Output.Messages() {
		r.Output = append(r.Output, m.String())
	}
	for _, err := range e.Output.Errors() {
		r.Output = append(r.Output, err.Error())
	}
	srv.audit.Record(r)
}

// broadcastFeedback shows the messages of a command of the console or of
// a remote client to the operators online, as Java's
// broadcast-console-to-ops and broadcast-rcon-to-ops do.
func (srv *Server) broadcastFeedback(e cmd.Execution, actor audit.Actor) {
	perms := srv.permissionConfig()
	var name string
	switch {
	case actor.Kind == audit.KindConsole && perms.BroadcastConsoleToOps:
		name = "Console"
	case actor.Kind == audit.KindRemote && perms.BroadcastRemoteToOps:
		name = actor.String()
	default:
		return
	}
	if e.Output.MessageCount() == 0 || srv.world == nil || !srv.world.GameRuleBool("log_admin_commands") {
		return
	}
	for _, m := range e.Output.Messages() {
		srv.AdminFeedback(e.Source, name, m.String())
	}
}

// AdminFeedback shows the operators online, of every world, but the player
// that is src, a message of a command src ran, as "[name: message]". It
// sends them from a goroutine of its own, so that it never waits for a
// world, and reports true. builtin.Register installs it in package cmd.
func (srv *Server) AdminFeedback(src cmd.Source, name, message string) bool {
	if srv == nil {
		return false
	}
	var from uuid.UUID
	if p, ok := unwrapSource(src).(*player.Player); ok {
		from = p.UUID()
	}
	var ops []*world.EntityHandle
	for _, p := range srv.players.snapshot() {
		if p.handle.UUID() != from && srv.operatorOf(p.id) {
			ops = append(ops, p.handle)
		}
	}
	if len(ops) > 0 {
		go func() {
			for _, h := range ops {
				player.Do(h, func(_ *world.Tx, p *player.Player) { p.Messaget(chat.MessageAdminCommand, name, message) })
			}
		}()
	}
	return true
}

// unwrapSource returns the source a source runs commands on behalf of, as
// /execute wraps it, or the source itself.
func unwrapSource(src cmd.Source) cmd.Source {
	for {
		w, ok := src.(cmd.Wrapper)
		if !ok {
			return src
		}
		src = w.Unwrap()
	}
}

// actorOf returns the actor of the audit log a source of commands is: a
// player online, a command block, a source that says which it is (the
// console, a remote client), or else code calling the server.
func (srv *Server) actorOf(src cmd.Source) audit.Actor {
	src = unwrapSource(src)
	level := int(cmd.SourceLevel(src))
	switch s := src.(type) {
	case *player.Player:
		a := audit.Actor{Kind: audit.KindPlayer, Name: s.Name(), Level: level}
		if p, ok := srv.players.player(s.UUID()); ok {
			a.Key = srv.identityKeyOf(p.id)
		}
		if addr := s.Addr(); addr != nil {
			a.Address = hostOf(addr)
		}
		return a
	case *cmd.BlockSource:
		return audit.Actor{Kind: audit.KindCommandBlock, Name: s.Name(), Level: level}
	case interface{ AuditActor() audit.Actor }:
		return s.AuditActor()
	}
	a := audit.Actor{Kind: audit.KindAPI, Level: level}
	if n, ok := src.(interface{ Name() string }); ok {
		a.Name = n.Name()
	}
	return a
}

// identityKeyOf returns the key of the lists of the player of the identity
// passed, as text: "xuid:<XUID>" on a server keyed by XUID, the name
// normalised otherwise.
func (srv *Server) identityKeyOf(id client.Identity) string {
	if srv.IdentityKey() == identity.XUID {
		if k, ok := accountKey(id); ok {
			return k.String()
		}
		return ""
	}
	if k, err := identity.OfName(id.Name); err == nil {
		return k.String()
	}
	return ""
}

// whereOf returns where a source in a world is, nil for one in none.
func whereOf(src cmd.Source) *audit.Where {
	var w *world.World
	switch s := unwrapSource(src).(type) {
	case *player.Player:
		if tx := s.Tx(); tx != nil {
			w = tx.World()
		}
	case *cmd.BlockSource:
		w = s.World()
	}
	if w == nil {
		return nil
	}
	pos := unwrapSource(src).Position()
	return &audit.Where{Dimension: strings.ToLower(fmt.Sprint(w.Dimension())), Pos: [3]float64{pos[0], pos[1], pos[2]}}
}

// hostOf returns the host of an address, without its port.
func hostOf(addr net.Addr) string {
	s := addr.String()
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	return s
}
