package server_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/cmd/builtin"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// opWaitSource is a command Source with owner permissions and reference-counted
// deferral so end-to-end tests can await arbitrarily nested async commands
// (such as /execute as @a run tag @s ...).
type opWaitSource struct {
	name    string
	pos     mgl64.Vec3
	mu      sync.Mutex
	outputs []*cmd.Output
	pending int
	closed  bool
	done    chan struct{}
}

func newOpWaitSource(p *player.Player) *opWaitSource {
	return &opWaitSource{name: p.Name(), pos: p.Position(), done: make(chan struct{})}
}

func (s *opWaitSource) Name() string               { return s.name }
func (s *opWaitSource) Position() mgl64.Vec3       { return s.pos }
func (*opWaitSource) PermissionLevel() cmd.Level   { return cmd.LevelOwner }

func (s *opWaitSource) SendCommandOutput(o *cmd.Output) {
	s.mu.Lock()
	s.outputs = append(s.outputs, o)
	s.mu.Unlock()
}

func (s *opWaitSource) Defer() func() {
	s.mu.Lock()
	s.pending++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.pending--
			finished := s.pending == 0 && !s.closed
			if finished {
				s.closed = true
			}
			s.mu.Unlock()
			if finished {
				close(s.done)
			}
		})
	}
}

func execBuiltin(t *testing.T, h *world.EntityHandle, name, args string) []*cmd.Output {
	t.Helper()
	c, ok := cmd.ByAlias(name)
	if !ok {
		t.Fatalf("command %q not registered", name)
	}
	var src *opWaitSource
	launched := make(chan struct{})
	player.Do(h, func(tx *world.Tx, p *player.Player) {
		src = newOpWaitSource(p)
		ctxSrc := cmd.WithContext(src, cmd.ExecutionContext{
			Executor:       p,
			ExecutorHandle: p.H(),
			Position:       p.Position(),
			Rotation:       p.Rotation(),
			World:          tx.World(),
			Anchor:         cmd.Feet,
		})
		c.Execute(args, ctxSrc, tx)
		close(launched)
	})
	select {
	case <-launched:
	case <-time.After(5 * time.Second):
		t.Fatalf("command %s %s failed to start", name, args)
	}
	src.mu.Lock()
	p := src.pending
	src.mu.Unlock()
	if p > 0 {
		select {
		case <-src.done:
		case <-time.After(5 * time.Second):
			t.Fatalf("command %s %s timed out waiting for deferred completion", name, args)
		}
	}
	src.mu.Lock()
	defer src.mu.Unlock()
	return append([]*cmd.Output(nil), src.outputs...)
}

func assertNoErrors(t *testing.T, label string, outs []*cmd.Output) {
	t.Helper()
	for _, o := range outs {
		if o.ErrorCount() > 0 {
			t.Fatalf("%s returned errors: %v", label, outputErrors(o))
		}
	}
}

func countItem(t *testing.T, h *world.EntityHandle, want item.Stack) int {
	t.Helper()
	n, err := player.Call(context.Background(), h, func(_ *world.Tx, p *player.Player) (int, error) {
		total := 0
		for _, st := range p.Inventory().Slots() {
			if st.Item() == want.Item() {
				total += st.Count()
			}
		}
		return total, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCrossWorldGiveAndPlayerCommands(t *testing.T) {
	enableOwnerWaitPanic(t)
	tw := newThreeWorlds(t, nil)
	t.Cleanup(func() { _ = tw.srv.Close() })
	builtin.Register(tw.srv)

	outs := execBuiltin(t, tw.alice, "give", "@a minecraft:diamond 2")
	assertNoErrors(t, "give @a", outs)
	if outs[0].Result() != 3 {
		t.Fatalf("give @a result = %d, want 3", outs[0].Result())
	}
	for _, h := range []*world.EntityHandle{tw.alice, tw.bob, tw.carol} {
		if got := countItem(t, h, item.NewStack(item.Diamond{}, 1)); got != 2 {
			t.Fatalf("diamond count = %d, want 2", got)
		}
	}

	assertNoErrors(t, "give Bob", execBuiltin(t, tw.alice, "give", "Bob minecraft:apple 3"))
	if got := countItem(t, tw.bob, item.NewStack(item.Apple{}, 1)); got != 3 {
		t.Fatalf("Bob apple count = %d, want 3", got)
	}

	assertNoErrors(t, "gamemode creative @a", execBuiltin(t, tw.alice, "gamemode", "creative @a"))
	assertNoErrors(t, "effect give @a", execBuiltin(t, tw.alice, "effect", "give @a speed 10"))
	assertNoErrors(t, "tag @a add cross", execBuiltin(t, tw.alice, "tag", "@a add cross"))
	assertNoErrors(t, "xp add @a 4 levels", execBuiltin(t, tw.alice, "xp", "add @a 4 levels"))

	for _, h := range []*world.EntityHandle{tw.alice, tw.bob, tw.carol} {
		_, err := player.Call(context.Background(), h, func(_ *world.Tx, p *player.Player) (struct{}, error) {
			if p.GameMode() != world.GameModeCreative {
				t.Errorf("%s gamemode = %v, want creative", p.Name(), p.GameMode())
			}
			if _, ok := p.Effect(effect.Speed); !ok {
				t.Errorf("%s missing speed effect", p.Name())
			}
			if !slices.Contains(p.CommandTags(), "cross") {
				t.Errorf("%s missing tag cross", p.Name())
			}
			if p.ExperienceLevel() < 4 {
				t.Errorf("%s xp level = %d, want >= 4", p.Name(), p.ExperienceLevel())
			}
			return struct{}{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCrossWorldTeleportAndRide(t *testing.T) {
	enableOwnerWaitPanic(t)
	tw := newThreeWorlds(t, nil)
	t.Cleanup(func() { _ = tw.srv.Close() })
	builtin.Register(tw.srv)

	// Cross-dimension ride should fail cleanly without blocking.
	rideOuts := execBuiltin(t, tw.alice, "ride", "Alice mount Bob")
	if len(rideOuts) == 0 || rideOuts[0].ErrorCount() == 0 {
		t.Fatalf("expected error for cross-dimension ride, got %v", rideOuts)
	}

	// /tp Bob 10 64 10 from Alice (Overworld) moves Bob from Nether into Overworld.
	assertNoErrors(t, "tp Bob 10 64 10", execBuiltin(t, tw.alice, "tp", "Bob 10 64 10"))
	if tw.bob.World() != tw.srv.World() {
		t.Fatalf("Bob world = %p, want Overworld %p", tw.bob.World(), tw.srv.World())
	}
	pos, err := player.Call(context.Background(), tw.bob, func(_ *world.Tx, p *player.Player) (mgl64.Vec3, error) {
		return p.Position(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if pos != (mgl64.Vec3{10, 64, 10}) {
		t.Fatalf("Bob pos = %v, want (10, 64, 10)", pos)
	}

	// /tp @a Carol moves Alice and Bob (Overworld) into Carol's world (End).
	assertNoErrors(t, "tp @a Carol", execBuiltin(t, tw.alice, "tp", "@a Carol"))
	for _, h := range []*world.EntityHandle{tw.alice, tw.bob, tw.carol} {
		if h.World() != tw.srv.End() {
			t.Fatalf("expected player in End (%p) after tp @a Carol, got %p", tw.srv.End(), h.World())
		}
	}
}

func TestCrossWorldExecuteAndStore(t *testing.T) {
	enableOwnerWaitPanic(t)
	tw := newThreeWorlds(t, nil)
	t.Cleanup(func() { _ = tw.srv.Close() })
	builtin.Register(tw.srv)

	assertNoErrors(t, "execute as @a", execBuiltin(t, tw.alice, "execute", "as @a run tag @s add exec_seen"))
	for _, h := range []*world.EntityHandle{tw.alice, tw.bob, tw.carol} {
		has, err := player.Call(context.Background(), h, func(_ *world.Tx, p *player.Player) (bool, error) {
			return slices.Contains(p.CommandTags(), "exec_seen"), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !has {
			t.Fatalf("player missing exec_seen tag")
		}
	}

	assertNoErrors(t, "execute in nether setblock", execBuiltin(t, tw.alice, "execute", "in minecraft:the_nether run setblock 4 64 4 minecraft:gold_block"))
	<-tw.srv.Nether().Exec(func(tx *world.Tx) {
		name, _ := tx.Block(cube.Pos{4, 64, 4}).EncodeBlock()
		if name != "minecraft:gold_block" {
			t.Fatalf("nether block at (4,64,4) = %s, want minecraft:gold_block", name)
		}
	})

	assertNoErrors(t, "scoreboard add", execBuiltin(t, tw.alice, "scoreboard", "objectives add pts dummy"))
	assertNoErrors(t, "execute store", execBuiltin(t, tw.alice, "execute", "store result score Total pts run give @a minecraft:apple 1"))
	if got, ok := tw.srv.World().Scoreboard().Score("pts", "Total"); !ok || got != 3 {
		t.Fatalf("stored score Total pts = (%d, %v), want (3, true)", got, ok)
	}
}

func TestCrossWorldScoreboardAndCommandBlock(t *testing.T) {
	enableOwnerWaitPanic(t)
	tw := newThreeWorlds(t, nil)
	t.Cleanup(func() { _ = tw.srv.Close() })
	builtin.Register(tw.srv)

	assertNoErrors(t, "scoreboard add", execBuiltin(t, tw.alice, "scoreboard", "objectives add kills dummy"))
	assertNoErrors(t, "scoreboard set @a", execBuiltin(t, tw.alice, "scoreboard", "players set @a kills 7"))
	m := tw.srv.World().Scoreboard()
	for _, name := range []string{"Alice", "Bob", "Carol"} {
		if v, ok := m.Score("kills", name); !ok || v != 7 {
			t.Fatalf("score kills for %s = (%d, %v), want (7, true)", name, v, ok)
		}
	}

	assertNoErrors(t, "team add", execBuiltin(t, tw.alice, "team", "add red Red"))
	assertNoErrors(t, "team join @a", execBuiltin(t, tw.alice, "team", "join red @a"))
	for _, name := range []string{"Alice", "Bob", "Carol"} {
		if team, ok := m.TeamFor(name); !ok || team.Name != "red" {
			t.Fatalf("team for %s = (%v, %v), want red", name, team.Name, ok)
		}
	}

	// Command block deferred output across all three worlds.
	giveCmd, ok := cmd.ByAlias("give")
	if !ok {
		t.Fatal("give command not registered")
	}
	src := cmd.NewBlockSource(cube.Pos{0, 64, 0}, tw.srv.World(), "@")
	deferredDone := make(chan struct{})
	var gotResult int
	var gotText string
	src.OnDeferred(func(o *cmd.Output, text string) {
		if o != nil {
			gotResult = o.Result()
		}
		gotText = text
		close(deferredDone)
	})
	<-tw.srv.World().Exec(func(tx *world.Tx) {
		giveCmd.Execute("@a minecraft:emerald 1", src, tx)
	})
	if !src.Deferred() {
		t.Fatal("expected command block /give @a across worlds to defer")
	}
	select {
	case <-deferredDone:
	case <-time.After(5 * time.Second):
		t.Fatal("command block deferred callback timed out")
	}
	if gotResult != 3 || !strings.Contains(gotText, "Alice") || !strings.Contains(gotText, "Bob") || !strings.Contains(gotText, "Carol") {
		t.Fatalf("command block deferred result=%d text=%q, want result=3 with all three players", gotResult, gotText)
	}
	for _, h := range []*world.EntityHandle{tw.alice, tw.bob, tw.carol} {
		if got := countItem(t, h, item.NewStack(item.Emerald{}, 1)); got != 1 {
			t.Fatalf("emerald count = %d, want 1", got)
		}
	}
}
