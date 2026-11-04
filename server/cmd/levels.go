package cmd

import (
	"fmt"
	"sync/atomic"

	"github.com/google/uuid"
)

// Level is a permission level of commands, as Java Edition numbers them:
// a source runs a command if its level is at least the level the command
// needs.
type Level uint8

const (
	// LevelAll is the level of anyone: help, list, msg, me and the like.
	LevelAll Level = iota
	// LevelModerator is Java's level 1, which no built-in command needs.
	LevelModerator
	// LevelGameMaster runs the commands of the game, such as /give, /tp
	// and /gamemode, and edits command and structure blocks.
	LevelGameMaster
	// LevelAdmin moderates: /ban, /kick, /op, /whitelist and the like.
	LevelAdmin
	// LevelOwner runs the server itself: /stop, /save-all and the like.
	LevelOwner
)

// MaxLevel is the highest level, LevelOwner.
const MaxLevel = LevelOwner

// String returns the level as a number, as config.toml and ops.json write
// it.
func (l Level) String() string { return fmt.Sprint(uint8(l)) }

// Requirer is implemented by a Runnable that needs a level to run. A
// Runnable that does not implement it needs LevelAll.
type Requirer interface {
	// RequiredLevel returns the level the Runnable declares it needs, which
	// the server may override by the name of its command.
	RequiredLevel() Level
}

// Leveled is implemented by a Source with a level of its own: a player,
// whose level the server knows, the console, a remote client, a command
// block. A Source that is not Leveled has LevelOwner, the trust of the
// code that embeds the server and runs commands with a source of its own.
type Leveled interface {
	// PermissionLevel returns the level of the Source now.
	PermissionLevel() Level
}

// Wrapper is implemented by a Source that runs a command on behalf of
// another, such as a ContextSource of /execute: it has the level of the
// Source it wraps.
type Wrapper interface {
	// Unwrap returns the Source wrapped.
	Unwrap() Source
}

// Permissions is what the server installs with SetPermissions: the level of
// a player online, the level a command needs after the overrides of the
// server, and the level command blocks run commands at.
type Permissions interface {
	// PlayerLevel returns the level of the player online of the UUID
	// passed, LevelAll for a UUID of no player online.
	PlayerLevel(id uuid.UUID) Level
	// CommandLevel returns the level a runnable of the command of the name
	// passed needs, which declared it needs the level passed.
	CommandLevel(command string, declared Level) Level
	// CommandBlockLevel returns the level of command blocks and of
	// command-block minecarts.
	CommandBlockLevel() Level
}

var permissions atomic.Pointer[Permissions]

// SetPermissions installs the permissions of the server. Until it is
// called, players have LevelAll, command blocks LevelGameMaster, and every
// command needs the level it declares.
func SetPermissions(p Permissions) {
	permissions.Store(&p)
}

// installed returns the permissions SetPermissions installed, if any.
func installed() (Permissions, bool) {
	p := permissions.Load()
	if p == nil || *p == nil {
		return nil, false
	}
	return *p, true
}

// PlayerLevel returns the level of the player online of the UUID passed,
// as the server says, LevelAll until it installs its permissions.
func PlayerLevel(id uuid.UUID) Level {
	if p, ok := installed(); ok {
		return p.PlayerLevel(id)
	}
	return LevelAll
}

// HasLevel reports whether the player online of the UUID passed has the
// level passed at least. IsOperator reports whether it is an operator, of
// any level.
func HasLevel(id uuid.UUID, l Level) bool {
	return PlayerLevel(id) >= l
}

// CommandBlockLevel returns the level command blocks run commands at:
// LevelGameMaster, as Java fixes it, until the server installs its
// permissions.
func CommandBlockLevel() Level {
	if p, ok := installed(); ok {
		return p.CommandBlockLevel()
	}
	return LevelGameMaster
}

// SourceLevel returns the level of src: that of the Source it wraps for a
// Wrapper, its own for a Leveled source, and LevelOwner otherwise.
func SourceLevel(src Source) Level {
	for {
		w, ok := src.(Wrapper)
		if !ok {
			break
		}
		src = w.Unwrap()
	}
	if l, ok := src.(Leveled); ok {
		return l.PermissionLevel()
	}
	return LevelOwner
}

// RequiredLevel returns the level a source needs to run the runnable r of
// the command c: the level r declares, if it is a Requirer, as the server
// overrides it by the name of c.
func RequiredLevel(c Command, r Runnable) Level {
	declared := LevelAll
	if req, ok := r.(Requirer); ok {
		declared = req.RequiredLevel()
	}
	if p, ok := installed(); ok {
		return p.CommandLevel(c.name, declared)
	}
	return declared
}

// allows reports whether src may run the runnable r of the command c: it
// has the level r needs, and r allows it if r is an Allower.
func (cmd Command) allows(src Source, r Runnable) bool {
	if SourceLevel(src) < RequiredLevel(cmd, r) {
		return false
	}
	if a, ok := r.(Allower); ok && !a.Allow(src) {
		return false
	}
	return true
}
