package server_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/cmd/builtin"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// netherProvider is a player.Provider that loads the players of the UUIDs
// it holds in the Nether, and every other player nowhere, so that the server
// spawns it in its default world.
type netherProvider struct{ nether map[uuid.UUID]bool }

func (p netherProvider) Save(uuid.UUID, player.Config, *world.World) error { return nil }
func (p netherProvider) Close() error                                      { return nil }
func (p netherProvider) Load(id uuid.UUID, dim func(world.Dimension) *world.World) (player.Config, *world.World, error) {
	if !p.nether[id] {
		return player.Config{}, nil, errors.New("no data")
	}
	return player.Config{}, dim(world.Nether), nil
}

// outputSource is the player of a command that records its output.
type outputSource struct {
	*player.Player
	out chan<- *cmd.Output
}

func (s outputSource) SendCommandOutput(o *cmd.Output) { s.out <- o }

// twoWorlds is a server of an Overworld and a Nether, which generate no
// terrain, with Alice in the Overworld and Bob in the Nether.
type twoWorlds struct {
	srv        *server.Server
	alice, bob *world.EntityHandle
}

// newTwoWorlds starts a twoWorlds and joins Alice and Bob. The server logs
// to log, if not nil.
func newTwoWorlds(t *testing.T, log slog.Handler) *twoWorlds {
	t.Helper()
	l := clienttest.NewListener()
	login := func(name string) *clienttest.Join {
		return &clienttest.Join{L: client.Login{
			Identity: client.Identity{Name: name, UUID: uuid.New()},
			Locale:   language.English,
			Addr:     &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132},
		}}
	}
	alice, bob := login("Alice"), login("Bob")
	if log == nil {
		log = slog.NewTextHandler(io.Discard, nil)
	}
	conf := server.Config{
		Log:            slog.New(log),
		PlayerProvider: netherProvider{nether: map[uuid.UUID]bool{bob.L.Identity.UUID: true}},
		DisableEnd:     true,
		Generator:      func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
		Listeners:      []func(server.Config, server.Host) (server.Listener, error){func(server.Config, server.Host) (server.Listener, error) { return l, nil }},
	}
	tw := &twoWorlds{srv: conf.New()}
	tw.srv.Listen()
	spawned := make(chan *world.EntityHandle, 2)
	go func() {
		for p := range tw.srv.Accept() {
			spawned <- p.H()
		}
	}()
	for _, j := range []*clienttest.Join{alice, bob} {
		l.Send(j)
		select {
		case h := <-spawned:
			if j == alice {
				tw.alice = h
			} else {
				tw.bob = h
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s did not spawn", j.L.Identity.Name)
		}
	}
	return tw
}

// listHelperEnv runs TestListHelper in a subprocess of TestListDoesNotWaitOnOtherWorlds.
const listHelperEnv = "ADAMANT_LIST_HELPER"

// TestListDoesNotWaitOnOtherWorlds runs /list from Alice in the Overworld and
// Bob in the Nether at once, both inside their transactions: if /list waited
// for the owner of the other world, each would wait for the other forever.
// It runs in a subprocess, which a deadlock leaves unable to close its
// worlds: the subprocess exits with status 3 if neither /list returns within
// 3 seconds.
func TestListDoesNotWaitOnOtherWorlds(t *testing.T) {
	if os.Getenv(listHelperEnv) != "" {
		t.Skip("in the subprocess")
	}
	c := exec.Command(os.Args[0], "-test.run=^TestListHelper$", "-test.v")
	c.Env = append(os.Environ(), listHelperEnv+"=1")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("/list from two worlds at once: %v\n%s", err, out)
	}
	if n := strings.Count(string(out), "LIST There are 2/"); n != 2 {
		t.Errorf("/list printed the count of 2 players %d times, want twice:\n%s", n, out)
	}
	if n := strings.Count(string(out), "LIST Alice, Bob\n"); n != 2 {
		t.Errorf("/list printed \"Alice, Bob\" %d times, want twice:\n%s", n, out)
	}
}

// TestListHelper is the subprocess of TestListDoesNotWaitOnOtherWorlds.
func TestListHelper(t *testing.T) {
	if os.Getenv(listHelperEnv) == "" {
		t.Skip("only run by TestListDoesNotWaitOnOtherWorlds")
	}
	tw := newTwoWorlds(t, nil)
	builtin.Register(tw.srv)
	list, ok := cmd.ByAlias("list")
	if !ok {
		t.Fatal("/list is not registered")
	}

	// Both transactions start before either runs /list.
	var arrived sync.WaitGroup
	arrived.Add(2)
	outputs := make(chan *cmd.Output, 2)
	for _, h := range []*world.EntityHandle{tw.alice, tw.bob} {
		player.Do(h, func(tx *world.Tx, p *player.Player) {
			arrived.Done()
			arrived.Wait()
			list.Execute("", outputSource{Player: p, out: outputs}, tx)
		})
	}
	for range 2 {
		select {
		case o := <-outputs:
			for _, m := range o.Messages() {
				fmt.Println("LIST " + m.String())
			}
			for _, e := range o.Errors() {
				fmt.Println("LIST error: " + e.Error())
			}
		case <-time.After(3 * time.Second):
			fmt.Println("DEADLOCK: /list did not return within 3s")
			os.Exit(3)
		}
	}
	if err := tw.srv.Close(); err != nil {
		t.Fatal(err)
	}
}

// warnings is a slog.Handler that records the messages of the warnings
// logged.
type warnings struct {
	mu   sync.Mutex
	msgs *[]string
}

func (w *warnings) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelWarn }
func (w *warnings) Handle(_ context.Context, r slog.Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	*w.msgs = append(*w.msgs, r.Message)
	return nil
}
func (w *warnings) WithAttrs([]slog.Attr) slog.Handler { return w }
func (w *warnings) WithGroup(string) slog.Handler      { return w }

func (w *warnings) count(prefix string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, m := range *w.msgs {
		if strings.HasPrefix(m, prefix) {
			n++
		}
	}
	return n
}

// TestPlayersWithTxYieldsItsWorldOnly checks that Players(tx) from the
// Overworld yields Alice, there, and skips Bob, in the Nether, with a warning
// the first time only; and that Players(nil) yields both.
func TestPlayersWithTxYieldsItsWorldOnly(t *testing.T) {
	log := &warnings{msgs: new([]string)}
	tw := newTwoWorlds(t, log)
	defer tw.srv.Close()

	for range 2 {
		names, err := player.Call(context.Background(), tw.alice, func(tx *world.Tx, _ *player.Player) ([]string, error) {
			var names []string
			for p := range tw.srv.Players(tx) {
				names = append(names, p.Name())
			}
			return names, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(names) != 1 || names[0] != "Alice" {
			t.Errorf("Players(tx) in the Overworld yielded %v, want Alice only", names)
		}
	}
	if n := log.count("Server.Players(tx) yields the players of the world of tx only"); n != 1 {
		t.Errorf("Players(tx) skipping Bob twice warned %d times, want once", n)
	}

	var names []string
	for p := range tw.srv.Players(nil) {
		names = append(names, p.Name())
	}
	if len(names) != 2 || names[0] != "Alice" || names[1] != "Bob" {
		t.Errorf("Players(nil) yielded %v, want Alice and Bob, in the order they joined", names)
	}
}

// TestOnlineNeverWaits checks that Online and Players(tx), from inside a
// transaction of the Overworld, return at once while the owner of the Nether,
// where Bob is, is blocked.
func TestOnlineNeverWaits(t *testing.T) {
	tw := newTwoWorlds(t, nil)
	defer tw.srv.Close()

	release, blocked := make(chan struct{}), make(chan struct{})
	tw.srv.Nether().Do(func(*world.Tx) {
		close(blocked)
		<-release
	})
	defer close(release)
	<-blocked

	type result struct {
		online  []server.OnlinePlayer
		players int
	}
	done := make(chan result, 1)
	player.Do(tw.alice, func(tx *world.Tx, _ *player.Player) {
		r := result{online: tw.srv.Online()}
		for range tw.srv.Players(tx) {
			r.players++
		}
		done <- r
	})
	select {
	case r := <-done:
		if len(r.online) != 2 || r.online[0].Name != "Alice" || r.online[1].Name != "Bob" || r.online[1].Handle != tw.bob {
			t.Errorf("Online() = %+v, want Alice and Bob, in the order they joined, with their handles", r.online)
		}
		if r.players != 1 {
			t.Errorf("Players(tx) in the Overworld yielded %d players, want Alice only", r.players)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Online or Players(tx) waited for the owner of the Nether")
	}
}
