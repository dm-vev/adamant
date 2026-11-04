package builtin

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// gateTestAdapter is a server whose operators are the players of the UUIDs
// it holds.
type gateTestAdapter struct {
	serverAdapter
	ops map[uuid.UUID]bool
}

func (s gateTestAdapter) PlayerIsOperator(id uuid.UUID) bool { return s.ops[id] }

// gateSources are the sources TestCommandGatesToday asks every command
// about, by name.
type gateSources map[string]cmd.Source

// newGateSources registers the built-ins on a server of one operator and
// returns the sources of the test: a member and an operator online, the
// console, a command block, and /execute run by the member and by the
// operator.
func newGateSources(t *testing.T) gateSources {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	var member, op *player.Player
	hm := world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "Member", UUID: uuid.New()})
	ho := world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "Op", UUID: uuid.New()})
	if err := w.Do(func(tx *world.Tx) {
		member = tx.AddEntity(hm).(*player.Player)
		op = tx.AddEntity(ho).(*player.Player)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	Register(gateTestAdapter{ops: map[uuid.UUID]bool{op.UUID(): true}})
	return gateSources{
		"member":           member,
		"operator":         op,
		"console":          consoleTestSource{out: &cmd.Output{}},
		"command block":    cmd.NewBlockSource(cube.Pos{0, 64, 0}, w, ""),
		"execute member":   cmd.WithContext(member, cmd.ExecutionContext{Executor: member}),
		"execute operator": cmd.WithContext(op, cmd.ExecutionContext{Executor: op}),
	}
}

// builtinCommands returns the built-in commands registered, by name, each
// once, sorted.
func builtinCommands() []cmd.Command {
	seen := map[string]cmd.Command{}
	for _, c := range cmd.Commands() {
		seen[c.Name()] = c
	}
	out := make([]cmd.Command, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b cmd.Command) int { return strings.Compare(a.Name(), b.Name()) })
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

// openCommandsToday are the commands any player may run before the
// permission levels.
var openCommandsToday = []string{"about", "help", "list", "me", "msg", "random", "status", "teammsg", "trigger", "version"}

// TestCommandGatesToday pins who may run each built-in command: a player
// who is no operator runs ten, an operator all but /stop, which refuses
// every player, and every source that is no player, the console and a
// command block among them, all. /execute keeps the rights of the source
// that ran it, but for /stop, whose check does not unwrap it.
func TestCommandGatesToday(t *testing.T) {
	sources := newGateSources(t)
	commands := builtinCommands()
	if len(commands) != 75 {
		t.Errorf("%d built-in commands, want 75", len(commands))
	}
	all := make([]string, 0, len(commands))
	for _, c := range commands {
		all = append(all, c.Name())
	}
	without := func(names []string, name string) []string {
		return slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == name })
	}
	with := func(names []string, name string) []string {
		out := append(slices.Clone(names), name)
		slices.Sort(out)
		return out
	}
	for who, want := range map[string][]string{
		"member":           openCommandsToday,
		"operator":         without(all, "stop"),
		"console":          all,
		"command block":    all,
		"execute member":   with(openCommandsToday, "stop"),
		"execute operator": all,
	} {
		if got := runnableBy(t, commands, who, sources[who]); !slices.Equal(got, want) {
			t.Errorf("%s may run %v\nwant %v", who, got, want)
		}
	}
}
