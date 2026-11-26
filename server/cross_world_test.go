package server_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dm-vev/adamant/internal/faultcheck"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// deferredOutputSource wraps a Player to capture both synchronous and
// asynchronous command outputs without losing the channel wrapper when
// cmd.Later runs.
type deferredOutputSource struct {
	*player.Player
	out      chan<- *cmd.Output
	deferred *atomic.Int32
}

func (s deferredOutputSource) SendCommandOutput(o *cmd.Output) { s.out <- o }

func (s deferredOutputSource) Defer() func() {
	if s.deferred != nil {
		s.deferred.Add(1)
	}
	return func() {}
}

// consoleTestSource is a non-entity command source (like the server console)
// implementing cmd.Deferrer so tests can await async outputs cleanly.
type consoleTestSource struct {
	pos mgl64.Vec3
	out chan<- *cmd.Output
}

func (s consoleTestSource) Name() string                    { return "Console" }
func (s consoleTestSource) Position() mgl64.Vec3            { return s.pos }
func (s consoleTestSource) PermissionLevel() cmd.Level      { return cmd.LevelOwner }
func (s consoleTestSource) SendCommandOutput(o *cmd.Output) { s.out <- o }
func (s consoleTestSource) Defer() func()                   { return func() {} }

// testPlayersCmd exercises cmd.Players + cmd.ForEach + cmd.Tally.
type testPlayersCmd struct {
	Targets cmd.Players `cmd:"targets"`
	onEach  func(tx *world.Tx, t cmd.Target) int
	onThen  func(src cmd.Source, o *cmd.Output, tx *world.Tx, rs []cmd.Result[int])
}

func (c testPlayersCmd) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	cmd.ForEach(src, o, tx, c.Targets,
		func(tx *world.Tx, t cmd.Target) int {
			if c.onEach != nil {
				return c.onEach(tx, t)
			}
			return 1
		},
		func(src cmd.Source, o *cmd.Output, tx *world.Tx, rs []cmd.Result[int]) {
			if c.onThen != nil {
				c.onThen(src, o, tx, rs)
				return
			}
			names := make([]string, len(rs))
			for i, r := range rs {
				names[i] = r.Name
			}
			cmd.TallyOf(rs).Feedback(o, cmd.ErrNoPlayerFound,
				func(who string) string { return "HIT:" + who },
				func(n string) string { return fmt.Sprintf("HITS:%s:%s", n, strings.Join(names, ",")) },
			)
		},
	)
}

// testEntitiesCmd exercises cmd.Entities + cmd.ForEach + cmd.Tally.
type testEntitiesCmd struct {
	Targets cmd.Entities `cmd:"targets"`
}

func (c testEntitiesCmd) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	cmd.ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, _ cmd.Target) int { return 1 },
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			names := make([]string, len(rs))
			for i, r := range rs {
				names[i] = r.Name
			}
			cmd.TallyOf(rs).Feedback(o, cmd.ErrNoEntityFound,
				func(who string) string { return "ENT:" + who },
				func(n string) string { return fmt.Sprintf("ENTS:%s:%s", n, strings.Join(names, ",")) },
			)
		},
	)
}

// testNestedCmd exercises a two-stage nested cmd.ForEach (like tp <targets> <dest>).
type testNestedCmd struct {
	Targets cmd.Players   `cmd:"targets"`
	Dest    cmd.OnePlayer `cmd:"dest"`
}

func (c testNestedCmd) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	cmd.ForEach(src, o, tx, c.Dest,
		func(tx *world.Tx, _ cmd.Target) string { return tx.World().Name() },
		func(src cmd.Source, o *cmd.Output, tx *world.Tx, dests []cmd.Result[string]) {
			if len(dests) == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			destName, destWorld := dests[0].Name, dests[0].Value
			cmd.ForEach(src, o, tx, c.Targets,
				func(_ *world.Tx, _ cmd.Target) int { return 1 },
				func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
					o.Printf("NESTED:%d->%s(%s)", len(rs), destName, destWorld)
					o.SetResult(len(rs))
				},
			)
		},
	)
}

func enableOwnerWaitPanic(t *testing.T) {
	t.Helper()
	prev := world.PanicOnOwnerWait
	world.PanicOnOwnerWait = true
	t.Cleanup(func() { world.PanicOnOwnerWait = prev })
}

func execPlayerCmd(t *testing.T, h *world.EntityHandle, c cmd.Command, args string) *cmd.Output {
	t.Helper()
	out := make(chan *cmd.Output, 1)
	player.Do(h, func(tx *world.Tx, p *player.Player) {
		c.Execute(args, deferredOutputSource{Player: p, out: out}, tx)
	})
	select {
	case o := <-out:
		return o
	case <-time.After(5 * time.Second):
		t.Fatalf("command %q timed out", args)
		return nil
	}
}

func outputStrings(o *cmd.Output) []string {
	msgs := o.Messages()
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.String()
	}
	return out
}

func outputErrors(o *cmd.Output) []string {
	errs := o.Errors()
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}

// TestForEachLocalIsSynchronous verifies the synchronous fast path: when all
// online players are in the caller's world, output arrives before Execute
// returns and no task is scheduled on the Nether.
func TestForEachLocalIsSynchronous(t *testing.T) {
	enableOwnerWaitPanic(t)
	srv, hs := newTestWorlds(t, nil, true, []string{"Alice", "Bob"}, nil)
	t.Cleanup(func() { _ = srv.Close() })

	// Block the Nether owner completely: if ForEach tried to touch the Nether,
	// the command could not finish while blocked.
	netherBlocked := make(chan struct{})
	unblockNether := make(chan struct{})
	srv.Nether().Do(func(*world.Tx) {
		close(netherBlocked)
		<-unblockNether
	})
	<-netherBlocked
	t.Cleanup(func() { close(unblockNether) })

	c := cmd.New("ping", "", nil, testPlayersCmd{})
	var deferred atomic.Int32
	var syncLen int
	out := make(chan *cmd.Output, 1)
	done := make(chan struct{})
	player.Do(hs[0], func(tx *world.Tx, p *player.Player) {
		c.Execute("@a", deferredOutputSource{Player: p, out: out, deferred: &deferred}, tx)
		syncLen = len(out)
		close(done)
	})
	<-done

	if syncLen != 1 {
		t.Fatalf("output was not delivered synchronously before Execute returned (len=%d)", syncLen)
	}
	if deferred.Load() != 0 {
		t.Fatalf("Defer() called %d times on synchronous fast path, want 0", deferred.Load())
	}
	o := <-out
	if got := outputStrings(o); !slices.Equal(got, []string{"HITS:2:Alice,Bob"}) || o.Result() != 2 {
		t.Fatalf("output = %v (res=%d), want [HITS:2:Alice,Bob] (res=2)", got, o.Result())
	}
}

// TestCrossWorldSelectors exercises unbounded, bounded, named, UUID, world-
// limited, nearest-ignores-dimension, limit-order, random, nested ForEach,
// remote panic recovery, and stand-in source behavior across three worlds.
func TestCrossWorldSelectors(t *testing.T) {
	enableOwnerWaitPanic(t)
	tw := newThreeWorlds(t, nil)
	t.Cleanup(func() { _ = tw.srv.Close() })

	playersCmd := cmd.New("xplayers", "", nil, testPlayersCmd{})
	entitiesCmd := cmd.New("xentities", "", nil, testEntitiesCmd{})

	// Position players deterministically:
	// Alice (overworld) at (100, 64, 0), Bob (Nether) at (5, 64, 0), Carol (End) at (50, 64, 0).
	for _, tc := range []struct {
		h   *world.EntityHandle
		pos mgl64.Vec3
	}{
		{tw.alice, mgl64.Vec3{100, 64, 0}},
		{tw.bob, mgl64.Vec3{5, 64, 0}},
		{tw.carol, mgl64.Vec3{50, 64, 0}},
	} {
		if _, err := player.Call(context.Background(), tc.h, func(_ *world.Tx, p *player.Player) (struct{}, error) {
			p.Teleport(tc.pos)
			return struct{}{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("AllPlayersInJoinOrder", func(t *testing.T) {
		o := execPlayerCmd(t, tw.alice, playersCmd, "@a")
		if got := outputStrings(o); !slices.Equal(got, []string{"HITS:3:Alice,Bob,Carol"}) || o.Result() != 3 {
			t.Fatalf("output = %v errs=%v res=%d", got, outputErrors(o), o.Result())
		}
	})

	t.Run("NameReachesOtherWorld", func(t *testing.T) {
		o := execPlayerCmd(t, tw.alice, playersCmd, "Bob")
		if got := outputStrings(o); !slices.Equal(got, []string{"HIT:Bob"}) || o.Result() != 1 {
			t.Fatalf("from Alice: output = %v errs=%v", got, outputErrors(o))
		}
		// Also from a non-entity console source in the overworld.
		out := make(chan *cmd.Output, 1)
		tw.srv.World().Do(func(tx *world.Tx) {
			playersCmd.Execute("Carol", consoleTestSource{out: out}, tx)
		})
		o = <-out
		if got := outputStrings(o); !slices.Equal(got, []string{"HIT:Carol"}) || o.Result() != 1 {
			t.Fatalf("from Console: output = %v errs=%v", got, outputErrors(o))
		}
	})

	t.Run("WorldLimitedStaysLocal", func(t *testing.T) {
		o := execPlayerCmd(t, tw.alice, playersCmd, "@a[distance=..1000]")
		if got := outputStrings(o); !slices.Equal(got, []string{"HIT:Alice"}) {
			t.Fatalf("overworld distance selector = %v, want [HIT:Alice]", got)
		}
		// With ExecutionContext pointing to Nether, distance selector searches Nether only.
		out := make(chan *cmd.Output, 1)
		tw.srv.World().Do(func(tx *world.Tx) {
			src := cmd.WithContext(consoleTestSource{out: out}, cmd.ExecutionContext{
				World:    tw.srv.Nether(),
				Position: mgl64.Vec3{0, 64, 0},
			})
			playersCmd.Execute("@a[distance=..1000]", src, tx)
		})
		o = <-out
		if got := outputStrings(o); !slices.Equal(got, []string{"HIT:Bob"}) {
			t.Fatalf("nether context distance selector = %v, want [HIT:Bob]", got)
		}
	})

	t.Run("NearestIgnoresDimension", func(t *testing.T) {
		// Origin (0, 64, 0): Bob in Nether (dist 5) < Carol in End (dist 50) < Alice in Overworld (dist 100).
		out := make(chan *cmd.Output, 1)
		tw.srv.World().Do(func(tx *world.Tx) {
			playersCmd.Execute("@p", consoleTestSource{pos: mgl64.Vec3{0, 64, 0}, out: out}, tx)
		})
		o := <-out
		if got := outputStrings(o); !slices.Equal(got, []string{"HIT:Bob"}) {
			t.Fatalf("@p from (0,64,0) = %v, want [HIT:Bob]", got)
		}
	})

	t.Run("LimitFillsInOrderAndEntitiesAcrossWorlds", func(t *testing.T) {
		o := execPlayerCmd(t, tw.alice, playersCmd, "@a[limit=2]")
		if got := outputStrings(o); !slices.Equal(got, []string{"HITS:2:Alice,Bob"}) {
			t.Fatalf("@a[limit=2] = %v, want [HITS:2:Alice,Bob]", got)
		}

		// Spawn one named pig in each dimension.
		var endPigUUID string
		for _, tc := range []struct {
			w    *world.World
			name string
		}{
			{tw.srv.World(), "OverworldPig"},
			{tw.srv.Nether(), "NetherPig"},
			{tw.srv.End(), "EndPig"},
		} {
			id, err := world.Call(context.Background(), tc.w, func(tx *world.Tx) (string, error) {
				h, err := entity.NewFromStructureNBT(map[string]any{"id": "minecraft:pig"}, world.EntitySpawnOpts{
					Position:       mgl64.Vec3{10, 64, 10},
					SpawnFinalized: true,
				})
				if err != nil {
					return "", err
				}
				e := tx.AddEntity(h)
				if namer, ok := e.(interface{ SetNameTag(string) }); ok {
					namer.SetNameTag(tc.name)
				}
				return e.H().UUID().String(), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "EndPig" {
				endPigUUID = id
			}
		}

		o = execPlayerCmd(t, tw.alice, entitiesCmd, "@e[type=pig]")
		if got := outputStrings(o); !slices.Equal(got, []string{"ENTS:3:OverworldPig,NetherPig,EndPig"}) {
			t.Fatalf("@e[type=pig] = %v, want [ENTS:3:OverworldPig,NetherPig,EndPig]", got)
		}
		o = execPlayerCmd(t, tw.alice, entitiesCmd, "@e[limit=1,type=pig]")
		if got := outputStrings(o); !slices.Equal(got, []string{"ENT:OverworldPig"}) {
			t.Fatalf("@e[limit=1,type=pig] = %v, want [ENT:OverworldPig]", got)
		}
		// Entity UUID lookup across worlds reaches the End pig.
		o = execPlayerCmd(t, tw.alice, entitiesCmd, endPigUUID)
		if got := outputStrings(o); !slices.Equal(got, []string{"ENT:EndPig"}) {
			t.Fatalf("UUID lookup = %v, want [ENT:EndPig]", got)
		}
	})

	t.Run("RandomAcrossWorlds", func(t *testing.T) {
		seen := map[string]bool{}
		for range 200 {
			o := execPlayerCmd(t, tw.alice, playersCmd, "@r")
			for _, m := range outputStrings(o) {
				seen[m] = true
			}
		}
		for _, want := range []string{"HIT:Alice", "HIT:Bob", "HIT:Carol"} {
			if !seen[want] {
				t.Errorf("@r over 200 runs never selected %s (seen: %v)", want, seen)
			}
		}
	})

	t.Run("NestedForEach", func(t *testing.T) {
		nested := cmd.New("xnested", "", nil, testNestedCmd{})
		o := execPlayerCmd(t, tw.alice, nested, "@a Bob")
		want := fmt.Sprintf("NESTED:3->Bob(%s)", tw.srv.Nether().Name())
		if got := outputStrings(o); !slices.Equal(got, []string{want}) || o.Result() != 3 {
			t.Fatalf("nested = %v errs=%v res=%d, want [%s]", got, outputErrors(o), o.Result(), want)
		}
	})

	t.Run("RemotePanicProducesError", func(t *testing.T) {
		panicCmd := cmd.New("xpanic", "", nil, testPlayersCmd{
			onEach: func(_ *world.Tx, t cmd.Target) int {
				if nt, ok := t.(cmd.NamedTarget); ok && nt.Name() == "Bob" {
					panic(faultcheck.Expected{Why: "boom in nether"})
				}
				return 1
			},
		})
		o := execPlayerCmd(t, tw.alice, panicCmd, "@a")
		errs := outputErrors(o)
		if !slices.Contains(errs, "An unexpected error occurred trying to execute that command") {
			t.Fatalf("expected unexpected error message, got errs=%v msgs=%v", errs, outputStrings(o))
		}
		if got := outputStrings(o); !slices.Equal(got, []string{"HITS:2:Alice,Carol"}) {
			t.Fatalf("expected Alice and Carol results alongside panic error, got %v", got)
		}
	})

	t.Run("StandInSourceWhenCallerMoves", func(t *testing.T) {
		netherBlocked := make(chan struct{})
		unblockNether := make(chan struct{})
		tw.srv.Nether().Do(func(*world.Tx) {
			close(netherBlocked)
			<-unblockNether
		})
		<-netherBlocked

		thenSrcName := make(chan string, 1)
		standInCmd := cmd.New("xstandin", "", nil, testPlayersCmd{
			onThen: func(src cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
				name := ""
				if nt, ok := src.(cmd.NamedTarget); ok {
					name = nt.Name()
				}
				thenSrcName <- name
				o.Printf("DONE:%d", len(rs))
			},
		})

		// Run as real *player.Player (Alice) while Nether is paused, then move
		// Alice to the End before Nether finishes so then gets a stand-in source.
		cmdStarted := make(chan struct{})
		player.Do(tw.alice, func(tx *world.Tx, p *player.Player) {
			standInCmd.Execute("@a", p, tx)
			close(cmdStarted)
		})
		<-cmdStarted

		// Move Alice out of the overworld before unblocking Nether.
		moved := make(chan struct{})
		player.Do(tw.alice, func(tx *world.Tx, p *player.Player) {
			p.TeleportToWorld(tx, tw.srv.End(), mgl64.Vec3{0, 64, 0})
			close(moved)
		})
		<-moved
		// Wait until Alice is actually in the End.
		deadline := time.Now().Add(5 * time.Second)
		for tw.alice.World() != tw.srv.End() && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}

		close(unblockNether)
		select {
		case got := <-thenSrcName:
			if got != "Alice" {
				t.Fatalf("stand-in source Name() = %q, want Alice", got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("stand-in then callback did not run")
		}
	})
}

// TestTargetLeavesMidCommand verifies that if a remote target disconnects
// after being selected/grouped and before its effect runs, it is dropped
// silently with no error.
func TestTargetLeavesMidCommand(t *testing.T) {
	enableOwnerWaitPanic(t)
	tw := newTwoWorlds(t, nil)
	t.Cleanup(func() { _ = tw.srv.Close() })

	// Hold the Nether owner so ForEach groups Bob while online, then Bob
	// disconnects before ForEach's Nether task runs.
	inNether := make(chan struct{})
	disconnectBob := make(chan struct{})
	tw.srv.Nether().Do(func(tx *world.Tx) {
		close(inNether)
		<-disconnectBob
		if e, ok := tw.bob.Entity(tx); ok {
			e.(*player.Player).Disconnect("left mid-command")
		}
	})
	<-inNether

	c := cmd.New("xleave", "", nil, testPlayersCmd{})
	out := make(chan *cmd.Output, 1)
	started := make(chan struct{})
	player.Do(tw.alice, func(tx *world.Tx, p *player.Player) {
		c.Execute("@a", deferredOutputSource{Player: p, out: out}, tx)
		close(started)
	})
	<-started
	close(disconnectBob)

	select {
	case o := <-out:
		if len(outputErrors(o)) != 0 {
			t.Fatalf("unexpected errors when target left: %v", outputErrors(o))
		}
		if got := outputStrings(o); !slices.Equal(got, []string{"HIT:Alice"}) || o.Result() != 1 {
			t.Fatalf("output = %v (res=%d), want [HIT:Alice] (res=1)", got, o.Result())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command did not finish after target disconnected")
	}
}

type warnCaptureHandler struct {
	mu      sync.Mutex
	records []string
}

func (h *warnCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *warnCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		var sb strings.Builder
		sb.WriteString(r.Message)
		r.Attrs(func(a slog.Attr) bool {
			sb.WriteString(" ")
			sb.WriteString(a.Key)
			sb.WriteString("=")
			sb.WriteString(a.Value.String())
			return true
		})
		h.mu.Lock()
		h.records = append(h.records, sb.String())
		h.mu.Unlock()
	}
	return nil
}
func (h *warnCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *warnCaptureHandler) WithGroup(string) slog.Handler      { return h }

// TestWorldTimeout verifies that when a remote world does not answer within
// the cross-world timeout, pending tasks are cancelled (so the remote target
// never receives the effect once unblocked), a Warn is logged naming the world,
// and the output contains local results plus the timeout error line.
func TestWorldTimeout(t *testing.T) {
	enableOwnerWaitPanic(t)
	cmd.SetCrossWorldTimeout(150 * time.Millisecond)
	t.Cleanup(func() { cmd.SetCrossWorldTimeout(0) })

	warnLog := &warnCaptureHandler{}
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(warnLog))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	tw := newTwoWorlds(t, nil)
	t.Cleanup(func() { _ = tw.srv.Close() })

	netherBlocked := make(chan struct{})
	unblockNether := make(chan struct{})
	tw.srv.Nether().Do(func(*world.Tx) {
		close(netherBlocked)
		<-unblockNether
	})
	<-netherBlocked

	var bobHits atomic.Int32
	c := cmd.New("xtimeout", "", nil, testPlayersCmd{
		onEach: func(_ *world.Tx, target cmd.Target) int {
			if nt, ok := target.(cmd.NamedTarget); ok && nt.Name() == "Bob" {
				bobHits.Add(1)
			}
			return 1
		},
	})

	o := execPlayerCmd(t, tw.alice, c, "@a")
	if got := outputStrings(o); !slices.Equal(got, []string{"HIT:Alice"}) {
		t.Fatalf("output messages = %v, want [HIT:Alice]", got)
	}
	if errs := outputErrors(o); !slices.Contains(errs, "Could not reach 1 targets in time") {
		t.Fatalf("output errors = %v, want timeout error", errs)
	}

	// Unblock Nether and flush its queue: Bob's cancelled task must never run.
	close(unblockNether)
	if _, err := world.Call(context.Background(), tw.srv.Nether(), func(*world.Tx) (struct{}, error) {
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if bobHits.Load() != 0 {
		t.Fatalf("cancelled Nether task still ran on Bob (%d times)", bobHits.Load())
	}

	warnLog.mu.Lock()
	warns := slices.Clone(warnLog.records)
	warnLog.mu.Unlock()
	netherName := tw.srv.Nether().Name()
	foundWarn := slices.ContainsFunc(warns, func(s string) bool {
		return strings.Contains(s, netherName)
	})
	if !foundWarn {
		t.Fatalf("expected Warn log mentioning Nether world %q, got %v", netherName, warns)
	}
}

const crossWorldDeadlockEnv = "ADAMANT_CROSS_WORLD_DEADLOCK_HELPER"

// TestCrossWorldCommandsDoNotDeadlock runs unbounded and bounded cross-world
// commands concurrently from Alice, Bob and Carol in all three worlds across
// 200 rounds released together by a barrier inside their transactions.
func TestCrossWorldCommandsDoNotDeadlock(t *testing.T) {
	if os.Getenv(crossWorldDeadlockEnv) != "" {
		t.Skip("in the subprocess")
	}
	c := exec.Command(os.Args[0], "-test.run=^TestCrossWorldDeadlockHelper$", "-test.v")
	c.Env = append(os.Environ(), crossWorldDeadlockEnv+"=1")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("cross-world deadlock test failed: %v\n%s", err, out)
	}
}

// TestCrossWorldDeadlockHelper is the subprocess of TestCrossWorldCommandsDoNotDeadlock.
func TestCrossWorldDeadlockHelper(t *testing.T) {
	if os.Getenv(crossWorldDeadlockEnv) == "" {
		t.Skip("only run by TestCrossWorldCommandsDoNotDeadlock")
	}
	world.PanicOnOwnerWait = true
	tw := newThreeWorlds(t, nil)

	playersCmd := cmd.New("xdl", "", nil, testPlayersCmd{})
	nestedCmd := cmd.New("xdlnest", "", nil, testNestedCmd{})

	const rounds = 200
	outputs := make(chan *cmd.Output, rounds*3*2)

	for range rounds {
		var arrived sync.WaitGroup
		arrived.Add(3)
		for _, h := range []*world.EntityHandle{tw.alice, tw.bob, tw.carol} {
			player.Do(h, func(tx *world.Tx, p *player.Player) {
				arrived.Done()
				arrived.Wait()
				src := deferredOutputSource{Player: p, out: outputs}
				playersCmd.Execute("@a", src, tx)
				nestedCmd.Execute("@a @r", src, tx)
			})
		}
		for range 6 {
			select {
			case o := <-outputs:
				if o.ErrorCount() != 0 {
					fmt.Printf("ERROR: %v\n", outputErrors(o))
					os.Exit(2)
				}
			case <-time.After(10 * time.Second):
				fmt.Println("DEADLOCK: cross-world command did not return within 10s")
				os.Exit(3)
			}
		}
	}
	if err := tw.srv.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestCrossWorldRace hammers unbounded and bounded cross-world commands from
// all three worlds while a background goroutine transfers Bob between worlds.
func TestCrossWorldRace(t *testing.T) {
	enableOwnerWaitPanic(t)
	tw := newThreeWorlds(t, nil)
	t.Cleanup(func() { _ = tw.srv.Close() })

	playersCmd := cmd.New("xrace", "", nil, testPlayersCmd{})
	nestedCmd := cmd.New("xracenest", "", nil, testNestedCmd{})

	stopMover := make(chan struct{})
	moverDone := make(chan struct{})
	go func() {
		defer close(moverDone)
		dests := []*world.World{tw.srv.World(), tw.srv.Nether(), tw.srv.End()}
		i := 0
		for {
			select {
			case <-stopMover:
				return
			default:
				dest := dests[i%len(dests)]
				i++
				player.Do(tw.bob, func(tx *world.Tx, p *player.Player) {
					p.TeleportToWorld(tx, dest, mgl64.Vec3{0, 64, 0})
				})
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	const rounds = 50
	outputs := make(chan *cmd.Output, rounds*3*2)
	for range rounds {
		for _, h := range []*world.EntityHandle{tw.alice, tw.bob, tw.carol} {
			player.Do(h, func(tx *world.Tx, p *player.Player) {
				src := deferredOutputSource{Player: p, out: outputs}
				playersCmd.Execute("@a", src, tx)
				nestedCmd.Execute("@a @r", src, tx)
			})
		}
		for range 6 {
			select {
			case o := <-outputs:
				if o.ErrorCount() != 0 {
					t.Fatalf("unexpected error under concurrent transfers: %v", outputErrors(o))
				}
			case <-time.After(10 * time.Second):
				t.Fatal("cross-world command timed out under race load")
			}
		}
	}
	close(stopMover)
	<-moverDone
}
