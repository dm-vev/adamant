package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/codec"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// windowProbe is the record TestBDSWindows writes to BDS_WINDOWS_OUT: what
// BDS sent a client that opened and closed its inventory in the orders a real
// client does, and in those it does not.
type windowProbe struct {
	// Version is the BDS the record was taken from, such as 1.26.61.1.
	Version string `json:"version"`
	// Protocol is the protocol the probe client spoke.
	Protocol int32 `json:"protocol"`
	// Cases are the steps of the probe, in order.
	Cases []windowCase `json:"cases"`
}

// windowCase is a step of the probe: what the client sent and what BDS
// sent back within the time the step waited for.
type windowCase struct {
	Name string `json:"name"`
	// Sent is what the client sent, one line each.
	Sent []string `json:"sent"`
	// Got is the packets of BDS that matter to windows, one line each.
	Got []string `json:"got"`
}

// TestBDSWindows drives the BDS whose signaling URL is BDS_ADDR as a client
// does its inventory: it opens it, closes it with the window ID it has and
// with others, asks to open it again while it is open, and has the server
// teleport it, kill it and take it to another dimension with it open. It
// records what BDS sends back after each step, as JSON to the file
// BDS_WINDOWS_OUT if that is set, and logs it. BDS_PROTOCOL is the protocol
// the client speaks (2193 if unset) and BDS_VERSION names the BDS.
//
// Chests and other menus are not probed: a client that sends no chunk
// acknowledgements, as this one, is sent no ContainerOpen for the block it
// clicks, by an InventoryTransaction or a PlayerAuthInput.
func TestBDSWindows(t *testing.T) {
	addr := os.Getenv("BDS_ADDR")
	if addr == "" {
		t.Skip("BDS_ADDR is not set")
	}
	p := codec.V2193
	if s := os.Getenv("BDS_PROTOCOL"); s != "" {
		n, _ := strconv.Atoi(s)
		var ok bool
		if p, ok = codec.ByNumber(int32(n)); !ok {
			t.Fatalf("no codecs of protocol %s", s)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c, err := Dial(ctx, addr, Config{Identity: login.Identity{DisplayName: "Windows"}, Protocol: p})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	pr := newWindowProber(t, c)
	defer pr.stop()
	pr.run()

	rec := windowProbe{Version: os.Getenv("BDS_VERSION"), Protocol: p.Number, Cases: pr.cases}
	b, err := json.MarshalIndent(rec, "", "\t")
	if err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("BDS_WINDOWS_OUT"); out != "" {
		if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// windowProber walks a client that sends PlayerAuthInput every tick and
// keeps what BDS sends of windows.
type windowProber struct {
	t *testing.T
	c *Conn

	mu   sync.Mutex
	pos  [3]float32
	log  []string
	done chan struct{}
	wg   sync.WaitGroup
	tick uint64

	cases []windowCase
	sent  []string
}

func newWindowProber(t *testing.T, c *Conn) *windowProber {
	pr := &windowProber{t: t, c: c, pos: c.game.PlayerPosition, done: make(chan struct{})}
	pr.wg.Add(2)
	go pr.read()
	go pr.input()
	return pr
}

func (pr *windowProber) stop() {
	close(pr.done)
	_ = pr.c.Close()
	pr.wg.Wait()
}

// read keeps the packets of BDS that matter to windows.
func (pr *windowProber) read() {
	defer pr.wg.Done()
	for {
		pk, err := pr.c.ReadPacket()
		if err != nil {
			return
		}
		pr.mu.Lock()
		switch pk := pk.(type) {
		case *mc.ContainerOpenPacket:
			pr.log = append(pr.log, fmt.Sprintf("ContainerOpen window=%d type=%d pos=%v entity=%d", pk.WindowID, pk.ContainerType, pk.ContainerPosition, pk.ContainerEntityUniqueID))
		case *mc.ContainerClosePacket:
			pr.log = append(pr.log, fmt.Sprintf("ContainerClose window=%d type=%d serverSide=%v", pk.WindowID, pk.ContainerType, pk.ServerSide))
		case *mc.ModalFormRequestPacket:
			pr.log = append(pr.log, "ModalFormRequest")
		case *mc.ChangeDimensionPacket:
			pr.log = append(pr.log, fmt.Sprintf("ChangeDimension dimension=%d", pk.Dimension))
		case *mc.RespawnPacket:
			pr.log = append(pr.log, fmt.Sprintf("Respawn state=%d", pk.State))
		case *mc.CommandOutputPacket:
			for _, m := range pk.OutputMessages {
				pr.log = append(pr.log, fmt.Sprintf("CommandOutput success=%v %s", m.Success, m.Message))
			}
		case *mc.MovePlayerPacket:
			if pk.EntityRuntimeID == pr.c.game.EntityRuntimeID && pk.Mode == mc.MoveModeTeleport {
				pr.pos = pk.Position
				pr.log = append(pr.log, fmt.Sprintf("MovePlayer teleport to %v", pk.Position))
			}
		}
		pr.mu.Unlock()
	}
}

// input sends the position of the client every tick.
func (pr *windowProber) input() {
	defer pr.wg.Done()
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-pr.done:
			return
		case <-t.C:
		}
		pr.mu.Lock()
		pr.tick++
		pos, tick := pr.pos, pr.tick
		pr.mu.Unlock()
		_ = pr.c.WritePacket(&mc.PlayerAuthInputPacket{Position: pos, InputMode: 1, Tick: tick})
		_ = pr.c.Flush()
	}
}

// send writes a packet of the client.
func (pr *windowProber) send(what string, pk proto.Packet) {
	pr.sent = append(pr.sent, what)
	if err := pr.c.WritePacket(pk); err != nil {
		pr.t.Fatal(err)
	}
	if err := pr.c.Flush(); err != nil {
		pr.t.Fatal(err)
	}
}

// step runs f, which sends, then waits for BDS and records what it sent.
func (pr *windowProber) step(name string, wait time.Duration, f func()) {
	pr.mu.Lock()
	pr.log = nil
	pr.mu.Unlock()
	pr.sent = nil
	f()
	time.Sleep(wait)
	pr.mu.Lock()
	got := append([]string(nil), pr.log...)
	pr.log = nil
	pr.mu.Unlock()
	pr.cases = append(pr.cases, windowCase{Name: name, Sent: pr.sent, Got: got})
	pr.t.Logf("%s: sent %v, got %v", name, pr.sent, got)
}

// until waits for cond, which holds the lock, for up to 20 seconds.
func (pr *windowProber) until(what string, cond func() bool) {
	for range 400 {
		pr.mu.Lock()
		ok := cond()
		pr.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	pr.t.Fatalf("timed out waiting for %s", what)
}

func (pr *windowProber) command(line string) {
	pr.send("/"+line, &mc.CommandRequestPacket{
		CommandLine:   "/" + line,
		CommandOrigin: mc.CommandOrigin{PlayerUniqueID: pr.c.game.EntityUniqueID},
		Version:       "latest",
	})
}

func (pr *windowProber) openInventory() {
	pr.send("Interact OpenInventory", &mc.InteractPacket{ActionType: mc.InteractActionOpenInventory, TargetEntityRuntimeID: pr.c.game.EntityRuntimeID})
}

func (pr *windowProber) closeWindow(window, typ byte) {
	pr.send(fmt.Sprintf("ContainerClose window=%d type=%d", window, typ), &mc.ContainerClosePacket{WindowID: window, ContainerType: typ})
}

func (pr *windowProber) run() {
	const settle = 900 * time.Millisecond
	time.Sleep(2 * time.Second)
	// The position the server holds the client at is learnt from where it
	// teleports it to, here, as the position the game started it at is not
	// where the ground is.
	pr.step("setup: the position", 0, func() { pr.command("tp @s ~ ~ ~") })
	pr.until("the teleport", func() bool { return pr.pos[1] < 1000 })
	time.Sleep(time.Second)

	// The inventory, and closes of windows by the ID it has and by others.
	pr.step("inventory: open", settle, pr.openInventory)
	pr.step("inventory: open again while open", settle, pr.openInventory)
	pr.step("inventory: close with window 0", settle, func() { pr.closeWindow(0, 0xff) })
	pr.step("inventory: close with window 0 again, nothing open", settle, func() { pr.closeWindow(0, 0xff) })
	pr.step("inventory: close with window 0xff, nothing open", settle, func() { pr.closeWindow(0xff, 0xff) })
	pr.step("inventory: close with window 7, nothing open", settle, func() { pr.closeWindow(7, 0) })
	pr.step("inventory: open after the stale closes", settle, pr.openInventory)
	pr.step("inventory: close with window 0xff while open", settle, func() { pr.closeWindow(0xff, 0xff) })
	pr.step("inventory: open after closing with 0xff", settle, pr.openInventory)
	pr.step("inventory: close with window 7 while open", settle, func() { pr.closeWindow(7, 0) })
	pr.step("inventory: open after closing with window 7", settle, pr.openInventory)
	pr.step("inventory: close with window 0", settle, func() { pr.closeWindow(0, 0xff) })
	pr.step("inventory: close with window 0 once more", settle, func() { pr.closeWindow(0, 0xff) })

	// The inventory open while the server moves the client, takes it to
	// another dimension and kills it: none of which closes it. The client does
	// not respawn: BDS did not open the inventory of the one that did.
	pr.step("teleport: open the inventory", settle, pr.openInventory)
	pr.step("teleport: far away", 2*time.Second, func() { pr.command("tp @s ~ ~ ~40") })
	pr.step("teleport: open the inventory again", settle, pr.openInventory)
	pr.step("teleport: close it", settle, func() { pr.closeWindow(0, 0xff) })
	pr.step("dimension: open the inventory", settle, pr.openInventory)
	pr.step("dimension: to the nether", 6*time.Second, func() { pr.command("execute in nether run tp @s 0 100 0") })
	pr.step("dimension: open the inventory again", settle, pr.openInventory)
	pr.step("dimension: to the overworld", 6*time.Second, func() { pr.command("execute in overworld run tp @s 0 140 0") })
	pr.step("dimension: open the inventory again", settle, pr.openInventory)
	pr.step("dimension: close", settle, func() { pr.closeWindow(0, 0xff) })
	pr.step("dimension: open the inventory after closing", settle, pr.openInventory)
	pr.step("dimension: close", settle, func() { pr.closeWindow(0, 0xff) })
	pr.step("death: open the inventory", settle, pr.openInventory)
	pr.step("death: killed", 2*time.Second, func() { pr.command("kill @s") })
}
