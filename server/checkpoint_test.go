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
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/playerdb"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// dupeHelperEnv runs TestCheckpointDupeHelper in a subprocess of
// TestCheckpointAfterDepositHasNoDupe, with the directory of its world and
// players.
const dupeHelperEnv = "ADAMANT_DUPE_HELPER"

// dupeAlice is the player of the helper, the same in both of its sessions.
var dupeAlice = client.Identity{Name: "Alice", UUID: uuid.MustParse("6d1c62f5-1e30-4a4b-9a43-6f4b8bd0a7a1")}

// dupeChest is where the helper's Alice puts her diamonds.
var dupeChest = cube.Pos{8, 100, 8}

// TestCheckpointAfterDepositHasNoDupe crashes a server just after /save-all.
// Its helper runs a server saving to a world and a players database: Alice
// joins, takes 10 diamonds and quits, which saves her with them. She joins
// again and puts the 10 diamonds in a chest, and /save-all is run. The
// helper then tells this test, which kills it with SIGKILL, opens what it
// left on disk and counts the diamonds in the chest and in Alice's record:
// there are 10, in the chest, as at /save-all.
func TestCheckpointAfterDepositHasNoDupe(t *testing.T) {
	if os.Getenv(dupeHelperEnv) != "" {
		t.Skip("in the subprocess")
	}
	dir := t.TempDir()
	c := exec.Command(os.Args[0], "-test.run=^TestCheckpointDupeHelper$", "-test.v")
	c.Env = append(os.Environ(), dupeHelperEnv+"="+dir)
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c.Stderr = c.Stdout
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	saved := make(chan struct{})
	var lines []string
	go func() {
		s := bufio.NewScanner(out)
		for s.Scan() {
			lines = append(lines, s.Text())
			if s.Text() == "SAVED" {
				close(saved)
				_, _ = io.Copy(io.Discard, out)
				return
			}
		}
	}()
	select {
	case <-saved:
	case <-time.After(60 * time.Second):
		_ = c.Process.Kill()
		_ = c.Wait()
		t.Fatalf("the helper did not save within 60s:\n%s", strings.Join(lines, "\n"))
	}
	// The crash.
	if err := c.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = c.Wait()

	world.DefaultBlockRegistry.Finalize()
	db, err := mcdb.Config{Log: slog.New(slog.DiscardHandler)}.Open(filepath.Join(dir, "world"))
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Generator: world.NopGenerator{}, ReadOnly: true, Synchronous: true, Log: slog.New(slog.DiscardHandler)}.New()
	defer w.Close()
	inChest := 0
	<-w.Exec(func(tx *world.Tx) {
		if chest, ok := tx.Block(dupeChest).(block.Chest); ok {
			inChest = diamonds(chest.Inventory(tx, dupeChest))
		}
	})
	players, err := playerdb.NewProvider(filepath.Join(dir, "players"))
	if err != nil {
		t.Fatal(err)
	}
	defer players.Close()
	// The server keys players by name, without Xbox Live.
	alice, _, err := players.Load(identity.OfflineUUID(dupeAlice.Name), func(world.Dimension) *world.World { return w })
	if err != nil {
		t.Fatalf("load Alice: %v", err)
	}
	withAlice := diamonds(alice.Inventory)
	t.Logf("after the crash: %d diamonds in the chest, %d with Alice", inChest, withAlice)
	if inChest+withAlice != 10 {
		t.Errorf("after the crash there are %d diamonds in the chest and %d with Alice: %d, want the 10 Alice had", inChest, withAlice, inChest+withAlice)
	}
	if inChest != 10 || withAlice != 0 {
		t.Errorf("after the crash there are %d diamonds in the chest and %d with Alice, want 10 and 0, as at /save-all", inChest, withAlice)
	}
}

// diamonds counts the diamonds in inv.
func diamonds(inv *inventory.Inventory) int {
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.Slots() {
		if _, ok := it.Item().(item.Diamond); ok {
			n += it.Count()
		}
	}
	return n
}

// consoleSource is a source of commands that is no entity, as the console,
// and sends their output to out.
type consoleSource struct{ out chan<- *cmd.Output }

func (consoleSource) Position() mgl64.Vec3              { return mgl64.Vec3{} }
func (s consoleSource) SendCommandOutput(o *cmd.Output) { s.out <- o }

// TestCheckpointDupeHelper is the subprocess of
// TestCheckpointAfterDepositHasNoDupe. It prints SAVED once /save-all
// printed that it saved the world, and waits to be killed.
func TestCheckpointDupeHelper(t *testing.T) {
	dir := os.Getenv(dupeHelperEnv)
	if dir == "" {
		t.Skip("only run by TestCheckpointAfterDepositHasNoDupe")
	}
	log := slog.New(slog.DiscardHandler)
	db, err := mcdb.Config{Log: log}.Open(filepath.Join(dir, "world"))
	if err != nil {
		t.Fatal(err)
	}
	players, err := playerdb.NewProvider(filepath.Join(dir, "players"))
	if err != nil {
		t.Fatal(err)
	}
	l := clienttest.NewListener()
	conf := server.Config{
		Log:            log,
		WorldProvider:  db,
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
	join := func() *world.EntityHandle {
		l.Send(&clienttest.Join{L: client.Login{
			Identity: dupeAlice,
			Locale:   language.English,
			Addr:     &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132},
		}})
		select {
		case h := <-spawned:
			return h
		case <-time.After(30 * time.Second):
			t.Fatal("Alice did not spawn")
			return nil
		}
	}
	do := func(h *world.EntityHandle, f func(tx *world.Tx, p *player.Player)) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := player.Do(h, f).Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}

	// Alice takes 10 diamonds and quits, which saves her with them.
	h := join()
	do(h, func(_ *world.Tx, p *player.Player) {
		_ = p.Inventory().SetItem(0, item.NewStack(item.Diamond{}, 10))
		p.Disconnect("bye")
	})
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, online := srv.Player(dupeAlice.UUID); !online {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Alice is still online after quitting")
		}
	}

	// She joins again and puts them in a chest.
	h = join()
	do(h, func(tx *world.Tx, p *player.Player) {
		if n := diamonds(p.Inventory()); n != 10 {
			t.Errorf("Alice joined again with %d diamonds, want 10", n)
		}
		tx.SetBlock(dupeChest, block.NewChest(), nil)
		_ = p.Inventory().SetItem(0, item.Stack{})
		if _, err := tx.Block(dupeChest).(block.Chest).Inventory(tx, dupeChest).AddItem(item.NewStack(item.Diamond{}, 10)); err != nil {
			t.Errorf("put the diamonds in the chest: %v", err)
		}
	})

	// /save-all, as the console runs it. Its output is empty: it prints
	// once it saved.
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
					fmt.Println("SAVED")
					select {}
				}
			}
		case <-timeout:
			t.Fatal("/save-all did not print that it saved within 30s")
		}
	}
}
