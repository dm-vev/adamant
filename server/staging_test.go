package server_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/cmd/builtin"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/playerdb"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"golang.org/x/text/language"
)

// unloadHelperEnv runs TestUnloadDupeHelper in a subprocess of
// TestCrashAfterUnloadHasNoDupe, with the directory of its world and
// players.
const unloadHelperEnv = "ADAMANT_UNLOAD_DUPE_HELPER"

// farChest is where the helper's Alice puts her diamonds: far enough from
// her that nothing views its chunk, which unloads.
var farChest = cube.Pos{4008, 100, 8}

// runCrashHelper runs the test helper of the name passed in a subprocess,
// with env set to dir, until it prints line, then kills it with SIGKILL, as
// a crash would. It returns what the helper printed.
func runCrashHelper(t *testing.T, helper, env, dir, line string) []string {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^"+helper+"$", "-test.v")
	c.Env = append(os.Environ(), env+"="+dir)
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c.Stderr = c.Stdout
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	reached := make(chan struct{})
	var lines []string
	go func() {
		s := bufio.NewScanner(out)
		for s.Scan() {
			lines = append(lines, s.Text())
			if s.Text() == line {
				close(reached)
				_, _ = io.Copy(io.Discard, out)
				return
			}
		}
	}()
	select {
	case <-reached:
	case <-time.After(60 * time.Second):
		_ = c.Process.Kill()
		_ = c.Wait()
		t.Fatalf("the helper did not print %s within 60s:\n%s", line, strings.Join(lines, "\n"))
	}
	// The crash.
	if err := c.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = c.Wait()
	return lines
}

// TestCrashAfterUnloadHasNoDupe crashes a server just after the chunk of a
// chest unloaded. Its helper runs a server saving to a world and a players
// database: Alice takes 10 diamonds and /save-all saves her with them. She
// then puts them in a chest far from her, whose chunk unloads as nothing
// views it, and the helper waits for the saver to write it. This test then
// kills the helper with SIGKILL, opens what it left on disk and counts the
// diamonds in the chest and in Alice's record: there are 10, with Alice, as
// at /save-all. The chunk unloaded after the checkpoint waits for the next
// one, as the record of Alice does.
func TestCrashAfterUnloadHasNoDupe(t *testing.T) {
	if os.Getenv(unloadHelperEnv) != "" {
		t.Skip("in the subprocess")
	}
	dir := t.TempDir()
	lines := runCrashHelper(t, "TestUnloadDupeHelper", unloadHelperEnv, dir, "UNLOADED")
	for _, l := range lines {
		if strings.HasPrefix(l, "chunk ") {
			t.Log("helper: " + l)
		}
	}

	world.DefaultBlockRegistry.Finalize()
	db, err := mcdb.Config{Log: slog.New(slog.DiscardHandler)}.Open(filepath.Join(dir, "world"))
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Generator: world.NopGenerator{}, ReadOnly: true, Synchronous: true, Log: slog.New(slog.DiscardHandler)}.New()
	defer w.Close()
	inChest := 0
	<-w.Exec(func(tx *world.Tx) {
		if chest, ok := tx.Block(farChest).(block.Chest); ok {
			inChest = diamonds(chest.Inventory(tx, farChest))
		}
	})
	players, err := playerdb.NewProvider(filepath.Join(dir, "players"))
	if err != nil {
		t.Fatal(err)
	}
	defer players.Close()
	alice, _, err := players.Load(identity.OfflineUUID(dupeAlice.Name), func(world.Dimension) *world.World { return w })
	if err != nil {
		t.Fatalf("load Alice: %v", err)
	}
	withAlice := diamonds(alice.Inventory)
	t.Logf("after the crash: %d diamonds in the chest, %d with Alice", inChest, withAlice)
	if inChest+withAlice != 10 {
		t.Errorf("after the crash there are %d diamonds in the chest and %d with Alice: %d, want the 10 Alice had", inChest, withAlice, inChest+withAlice)
	}
	if inChest != 0 || withAlice != 10 {
		t.Errorf("after the crash there are %d diamonds in the chest and %d with Alice, want 0 and 10, as at /save-all", inChest, withAlice)
	}
}

// storeWatch is the world provider of TestUnloadDupeHelper: an mcdb.DB that
// tells when it stores the column at pos.
type storeWatch struct {
	*mcdb.DB
	pos    world.ChunkPos
	stored chan struct{}
}

func (p *storeWatch) StoreColumn(pos world.ChunkPos, dim world.Dimension, col *chunk.Column) error {
	err := p.DB.StoreColumn(pos, dim, col)
	if pos == p.pos && err == nil {
		select {
		case p.stored <- struct{}{}:
		default:
		}
	}
	return err
}

// saveAll runs /save-all as the console does on srv and waits until it
// printed that it saved.
func saveAll(t *testing.T, srv *server.Server) {
	t.Helper()
	outputs := make(chan *cmd.Output, 2)
	<-srv.World().Exec(func(tx *world.Tx) {
		cmd.ExecuteLine(consoleSource{out: outputs}, "/save-all", tx, nil)
	})
	for timeout := time.After(30 * time.Second); ; {
		select {
		case o := <-outputs:
			for _, e := range o.Errors() {
				t.Fatalf("/save-all: %v", e)
			}
			for _, m := range o.Messages() {
				if m.String() == "Saved the world." {
					return
				}
			}
		case <-timeout:
			t.Fatal("/save-all did not print that it saved within 30s")
		}
	}
}

// keepLoaded keeps the chunk of the player h loaded with a loader, as the
// session of a client does: a client of clienttest views no chunks, and the
// world would unload the chunk of its player with the player in it.
func keepLoaded(t *testing.T, w *world.World, h *world.EntityHandle) *world.Loader {
	t.Helper()
	var loader *world.Loader
	var pos world.ChunkPos
	<-w.Exec(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		p := e.Position()
		pos = world.ChunkPos{int32(p[0]) >> 4, int32(p[2]) >> 4}
		loader = world.NewLoader(1, w, world.NopViewer{})
		loader.Move(tx, p)
	})
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		loaded := false
		<-w.Exec(func(tx *world.Tx) {
			loader.Load(tx, 9)
			_, loaded = loader.Chunk(pos)
		})
		if loaded {
			return loader
		}
		if time.Now().After(deadline) {
			t.Fatal("the chunk of the player did not load")
		}
	}
}

// TestUnloadDupeHelper is the subprocess of TestCrashAfterUnloadHasNoDupe.
// It prints UNLOADED once the chunk of the chest unloaded and the saver had
// the time to write it, and waits to be killed.
func TestUnloadDupeHelper(t *testing.T) {
	dir := os.Getenv(unloadHelperEnv)
	if dir == "" {
		t.Skip("only run by TestCrashAfterUnloadHasNoDupe")
	}
	log := slog.New(slog.DiscardHandler)
	db, err := mcdb.Config{Log: log}.Open(filepath.Join(dir, "world"))
	if err != nil {
		t.Fatal(err)
	}
	watch := &storeWatch{DB: db, pos: world.ChunkPos{int32(farChest.X() >> 4), int32(farChest.Z() >> 4)}, stored: make(chan struct{}, 1)}
	players, err := playerdb.NewProvider(filepath.Join(dir, "players"))
	if err != nil {
		t.Fatal(err)
	}
	l := clienttest.NewListener()
	conf := server.Config{
		Log:            log,
		WorldProvider:  watch,
		PlayerProvider: players,
		DisableNether:  true,
		DisableEnd:     true,
		Generator:      func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
		Listeners:      []func(server.Config, server.Host) (server.Listener, error){func(server.Config, server.Host) (server.Listener, error) { return l, nil }},
	}
	srv := conf.New()
	builtin.Register(srv)
	srv.Listen()
	spawned := make(chan *world.EntityHandle, 1)
	go func() {
		for p := range srv.Accept() {
			spawned <- p.H()
		}
	}()
	l.Send(&clienttest.Join{L: client.Login{
		Identity: dupeAlice,
		Locale:   language.English,
		Addr:     &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132},
	}})
	var h *world.EntityHandle
	select {
	case h = <-spawned:
	case <-time.After(30 * time.Second):
		t.Fatal("Alice did not spawn")
	}
	do := func(f func(tx *world.Tx, p *player.Player)) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := player.Do(h, f).Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}

	// Alice takes 10 diamonds, and /save-all saves her with them.
	do(func(_ *world.Tx, p *player.Player) {
		_ = p.Inventory().SetItem(0, item.NewStack(item.Diamond{}, 10))
	})
	saveAll(t, srv)

	// She puts them in a chest whose chunk nothing views.
	do(func(tx *world.Tx, p *player.Player) {
		tx.SetBlock(farChest, block.NewChest(), nil)
		_ = p.Inventory().SetItem(0, item.Stack{})
		if _, err := tx.Block(farChest).(block.Chest).Inventory(tx, farChest).AddItem(item.NewStack(item.Diamond{}, 10)); err != nil {
			t.Errorf("put the diamonds in the chest: %v", err)
		}
	})
	// The chunk of the chest unloads, as the world does every
	// ChunkUnloadInterval. A client of clienttest views no chunks: a loader
	// keeps that of Alice loaded, as her session would.
	keepLoaded(t, srv.World(), h)
	<-srv.World().Exec(func(tx *world.Tx) {
		if tx.World().CollectGarbage(tx); tx.ChunkLoaded(watch.pos) {
			t.Error("the chunk of the chest is still loaded")
		}
	})
	if _, online := srv.Player(dupeAlice.UUID); !online {
		t.Fatal("Alice left as the chunks unloaded")
	}
	select {
	case <-watch.stored:
		fmt.Println("chunk of the chest stored")
	case <-time.After(3 * time.Second):
		fmt.Println("chunk of the chest not stored within 3s")
	}
	fmt.Println("UNLOADED")
	select {}
}
