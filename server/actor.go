package server

import (
	"strings"

	"github.com/df-mc/dragonfly/server/audit"
)

// ActorKind is the kind of what does an admin action of the server.
type ActorKind string

const (
	// ActorServer is the server itself, as its schedules.
	ActorServer ActorKind = "server"
	// ActorConsole is the console of the server.
	ActorConsole ActorKind = "console"
	// ActorPlayer is a player online.
	ActorPlayer ActorKind = "player"
	// ActorRemote is a client of remote admin.
	ActorRemote ActorKind = "remote"
	// ActorCommandBlock is a command block or a command-block minecart.
	ActorCommandBlock ActorKind = "command_block"
	// ActorAPI is code that calls a method of the Server: the program that
	// embeds it, or a plugin.
	ActorAPI ActorKind = "api"
)

// Actor is what does an admin action of the server, such as a backup: its
// kind and the name it shows as, which records of the action keep.
type Actor struct {
	Kind ActorKind
	Name string
}

// String returns the name of a, or its kind if it has none.
func (a Actor) String() string {
	if a.Name != "" {
		return a.Name
	}
	if a.Kind == "" {
		return string(ActorAPI)
	}
	return string(a.Kind)
}

// auditActor returns a as the audit log records it; an actor of no kind is
// code calling the server.
func (a Actor) auditActor() audit.Actor {
	kind := audit.Kind(a.Kind)
	if kind == "" {
		kind = audit.KindAPI
	}
	r := audit.Actor{Kind: kind, Name: a.Name}
	if kind == audit.KindRemote {
		// The name of a remote client is "remote:" and the name of its
		// token.
		r.Token = strings.TrimPrefix(a.Name, "remote:")
	}
	return r
}
