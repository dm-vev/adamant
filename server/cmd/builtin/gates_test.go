package builtin

import (
	"context"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// gateTestAdapter is a server whose operators are the players of the UUIDs
// it holds, at the levels it holds, and whose command blocks run at
// cmd.LevelGameMaster.
type gateTestAdapter struct {
	serverAdapter
	ops map[uuid.UUID]cmd.Level
}

func (s gateTestAdapter) PlayerIsOperator(id uuid.UUID) bool {
	_, ok := s.ops[id]
	return ok
}
func (s gateTestAdapter) PlayerLevel(id uuid.UUID) cmd.Level { return s.ops[id] }
func (gateTestAdapter) CommandLevel(_ string, declared cmd.Level) cmd.Level {
	return declared
}
func (gateTestAdapter) CommandBlockLevel() cmd.Level { return cmd.LevelGameMaster }

// gateSources are the sources TestCommandGates asks every command about,
// by name.
type gateSources map[string]cmd.Source

// newGateSources registers the built-ins on a server of three operators,
// of the levels 2, 3 and 4, and returns the sources of the test: a member
// and the operators online, the console, a command block, and /execute run
// by the member and by the operator of level 4.
func newGateSources(t *testing.T) gateSources {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	names := []string{"Member", "GameMaster", "Admin", "Owner"}
	players := make([]*player.Player, len(names))
	if err := w.Do(func(tx *world.Tx) {
		for i, name := range names {
			h := world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: name, UUID: uuid.New()})
			players[i] = tx.AddEntity(h).(*player.Player)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	member, gm, adm, op := players[0], players[1], players[2], players[3]
	Register(gateTestAdapter{ops: map[uuid.UUID]cmd.Level{
		gm.UUID(): cmd.LevelGameMaster, adm.UUID(): cmd.LevelAdmin, op.UUID(): cmd.LevelOwner,
	}})
	t.Cleanup(func() { cmd.SetPermissions(nil) })
	stored := func(src cmd.Source) cmd.Source { return &storeSource{Source: src, store: func(bool, int) {}} }
	return gateSources{
		"member":           member,
		"operator 2":       gm,
		"operator 3":       adm,
		"operator 4":       op,
		"console":          consoleTestSource{out: &cmd.Output{}},
		"command block":    cmd.NewBlockSource(cube.Pos{0, 64, 0}, w, ""),
		"execute member":   cmd.WithContext(member, cmd.ExecutionContext{Executor: member}),
		"execute operator": cmd.WithContext(op, cmd.ExecutionContext{Executor: op}),
		"store member":     stored(cmd.WithContext(member, cmd.ExecutionContext{Executor: member})),
	}
}

// builtinNames are the names of the 76 built-in commands, sorted.
var builtinNames = []string{
	"about", "attribute", "ban", "ban-ip", "banlist", "bossbar", "clear", "clone", "damage", "data",
	"defaultgamemode", "deop", "difficulty", "effect", "enchant", "execute", "experience", "fill", "fillbiome",
	"forceload", "gamemode", "gamerule", "gc", "give", "help", "item", "kick", "kill", "list", "locate", "loot",
	"me", "msg", "netstat", "op", "pardon", "pardon-ip", "particle", "permission", "place", "playsound", "random", "recipe",
	"return", "ride", "rotate", "save-all", "save-off", "save-on", "say", "scoreboard", "seed", "setblock",
	"setidletimeout", "setworldspawn", "spawnpoint", "spectate", "spreadplayers", "status", "stop", "stopsound",
	"summon", "tag", "team", "teammsg", "tellraw", "tick", "time", "title", "tp", "transfer", "trigger", "version",
	"weather", "whitelist", "worldborder",
}

// builtinCommands returns the built-in commands, in the order of
// builtinNames, and fails for any that is not registered.
func builtinCommands(t *testing.T) []cmd.Command {
	t.Helper()
	out := make([]cmd.Command, 0, len(builtinNames))
	for _, name := range builtinNames {
		c, ok := cmd.ByAlias(name)
		if !ok || c.Name() != name {
			t.Fatalf("/%s is not registered", name)
		}
		out = append(out, c)
	}
	return out
}

// runnableBy returns the names of the commands of which src may run at
// least one runnable, and fails if src may run some runnables of a command
// but not others.
func runnableBy(t *testing.T, commands []cmd.Command, who string, src cmd.Source) []string {
	t.Helper()
	var names []string
	for _, c := range commands {
		n := len(c.Runnables(src))
		switch {
		case n == 0:
		case n != len(c.Runnables(consoleTestSource{out: &cmd.Output{}})):
			t.Errorf("%s may run %d of the runnables of /%s: a partial command", who, n, c.Name())
			names = append(names, c.Name())
		default:
			names = append(names, c.Name())
		}
	}
	return names
}

// Commands by the level Java 26.3 requires of them, as the built-ins
// declare it.
var (
	openCommands  = []string{"about", "help", "list", "me", "msg", "random", "status", "teammsg", "trigger"}
	adminCommands = []string{"ban", "ban-ip", "banlist", "deop", "kick", "netstat", "op", "pardon", "pardon-ip",
		"permission", "setidletimeout", "tick", "transfer", "whitelist"}
	ownerCommands = []string{"gc", "save-all", "save-off", "save-on", "stop"}
)

// TestCommandGates checks who may run each built-in command, by the level
// the command needs: Java 26.3's. A player who is no operator runs the nine
// open commands; an operator, the commands of its level and below, /stop
// at level 4; the console, all; a command block, those of level 2.
// /execute, also with store, keeps the level of the source that ran it.
func TestCommandGates(t *testing.T) {
	sources := newGateSources(t)
	commands := builtinCommands(t)
	without := func(names []string, drop ...[]string) []string {
		return slices.DeleteFunc(slices.Clone(names), func(n string) bool {
			for _, d := range drop {
				if slices.Contains(d, n) {
					return true
				}
			}
			return false
		})
	}
	for who, want := range map[string][]string{
		"member":           openCommands,
		"operator 2":       without(builtinNames, adminCommands, ownerCommands),
		"operator 3":       without(builtinNames, ownerCommands),
		"operator 4":       builtinNames,
		"console":          builtinNames,
		"command block":    without(builtinNames, adminCommands, ownerCommands),
		"execute member":   openCommands,
		"execute operator": builtinNames,
		"store member":     openCommands,
	} {
		if got := runnableBy(t, commands, who, sources[who]); !slices.Equal(got, want) {
			t.Errorf("%s may run %v\nwant %v", who, got, want)
		}
	}
}

// TestDeniedCommandIsUnknown checks that a source below the level of a
// command is told the command does not exist, as Java tells it.
func TestDeniedCommandIsUnknown(t *testing.T) {
	sources := newGateSources(t)
	c, _ := cmd.ByAlias("ban")
	out := &cmd.Output{}
	src := cmd.NewBlockSource(cube.Pos{}, nil, "")
	c.Execute("Steve", src, nil)
	*out = *src.Output()
	if errs := out.Errors(); len(errs) != 1 || errs[0].Error() != cmd.MessageUnknown.F("ban").Error() {
		t.Errorf("/ban from a command block: %v, want %q", errs, cmd.MessageUnknown.F("ban").Error())
	}
	_ = sources
}
