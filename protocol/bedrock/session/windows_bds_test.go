package session_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// bdsWindows is the capture of testdata/bedrock_1_26_52_windows.json and
// testdata/bedrock_1_26_61_windows.json: what BDS sent a client that opened,
// closed and asked again to open its inventory, and was teleported, taken to
// another dimension and killed with it open
// (net/bedrock/client TestBDSWindows).
type bdsWindows struct {
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
	Cases    []struct {
		Name string   `json:"name"`
		Sent []string `json:"sent"`
		Got  []string `json:"got"`
	} `json:"cases"`
}

// windowsKnownDiverging are the steps of the capture, by name and, for a
// name that repeats, its count after a #, that the session does not answer as
// BDS did, and why. TestWindowsAgainstBDS fails if one of them no longer
// diverges, so that it leaves this list with the change that fixed it.
var windowsKnownDiverging = map[string]string{
	"inventory: close with window 0xff, nothing open": "the session sends nothing back to the 0xff of the chat, " +
		"which BDS answers with ContainerClose like any other close",
	"inventory: close with window 0xff while open": "as above: the inventory is closed, but nothing is sent back",
	"teleport: open the inventory again": "BDS goes on holding the inventory open through a teleport and ignores a second " +
		"OpenInventory; the session takes one that comes more than a second after the first for a client that closed it " +
		"without saying so, and opens it again",
	"dimension: open the inventory again":   "as above, and the session ends the inventory at a change of dimension, which BDS does not",
	"dimension: open the inventory again#2": "as above",
}

// windowsGrace is the ticks the replay waits before a step: BDS waited
// 900 ms and more between two, which is more than the second the session
// gives a window before it takes the client for one that closed it.
const windowsGrace = afterGrace

var (
	windowsCloseSent = regexp.MustCompile(`^ContainerClose window=(\d+) type=(\d+)$`)
	windowsEchoGot   = regexp.MustCompile(`^ContainerClose window=\d+ type=\d+ serverSide=false$`)
)

// TestWindowsAgainstBDS replays the steps of the captures on a session: the
// client's requests to open its inventory and its closes by the window it
// names, with the teleport, the dimension change and the death the server
// did, and compares whether the client was sent a ContainerOpen and
// whether it was sent the answer to a close. The window IDs differ: BDS
// counts its windows, the inventory of the session is window 0.
func TestWindowsAgainstBDS(t *testing.T) {
	var captures []bdsWindows
	for _, name := range []string{"bedrock_1_26_52_windows.json", "bedrock_1_26_61_windows.json"} {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var c bdsWindows
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatal(err)
		}
		captures = append(captures, c)
	}
	// Both versions of BDS answer alike, step by step.
	a, b := captures[0], captures[1]
	if len(a.Cases) != len(b.Cases) {
		t.Fatalf("BDS %s has %d steps, BDS %s %d", a.Version, len(a.Cases), b.Version, len(b.Cases))
	}
	for i := range a.Cases {
		if a.Cases[i].Name != b.Cases[i].Name || bdsOutcome(a.Cases[i].Got) != bdsOutcome(b.Cases[i].Got) {
			t.Errorf("step %q: BDS %s answered %v, BDS %s %v", a.Cases[i].Name, a.Version, a.Cases[i].Got, b.Version, b.Cases[i].Got)
		}
	}

	overworld := goldenWorld(t)
	t.Cleanup(func() { _ = overworld.Close() })
	nether := world.Config{
		Log:             slog.New(slog.DiscardHandler),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Dim:             world.Nether,
		Generator:       goldenGenerator(),
		Provider:        &goldenProvider{NopProvider: world.NopProvider{Set: goldenSettings()}},
		Seed:            goldenSeed,
	}.New()
	t.Cleanup(func() { _ = nether.Close() })
	s := newWindowsScenarioIn(t, overworld)

	seen := map[string]int{}
	diverged := map[string]bool{}
	for _, step := range b.Cases {
		key := step.Name
		if seen[step.Name]++; seen[step.Name] > 1 {
			key = fmt.Sprintf("%s#%d", step.Name, seen[step.Name])
		}
		ticks := windowsGrace
		if step.Name == "inventory: open again while open" {
			// A repeat of the request, within the time it takes to answer.
			ticks = 2
		}
		s.ticks(ticks)
		pks, ok := s.replay(t, step.Name, step.Sent, nether, overworld)
		if !ok {
			continue
		}
		got, want := sessionOutcome(pks), bdsOutcome(step.Got)
		_, known := windowsKnownDiverging[key]
		switch {
		case got != want && !known:
			t.Errorf("step %q: the session answered %s, BDS %s (%v)", key, got, want, step.Got)
		case got == want && known:
			t.Errorf("step %q is listed as diverging from BDS, but the session answers as BDS: %s", key, got)
		case got != want:
			diverged[key] = true
		}
	}
	for key := range windowsKnownDiverging {
		if !diverged[key] {
			t.Errorf("step %q is listed as diverging, but the capture has no such step or it did not diverge", key)
		}
	}
}

// replay sends what the client of a step sent and does what the server did,
// and returns what the client was sent. ok is false for a step that has
// nothing to replay.
func (s *windowsScenario) replay(t *testing.T, name string, sent []string, nether, overworld *world.World) (pks []proto.Packet, ok bool) {
	t.Helper()
	if strings.HasPrefix(name, "setup:") {
		return nil, false
	}
	pks = s.do(func() {
		for _, line := range sent {
			switch {
			case line == "Interact OpenInventory":
				s.send(&mc.InteractPacket{ActionType: mc.InteractActionOpenInventory, TargetEntityRuntimeID: 1})
			case windowsCloseSent.MatchString(line):
				m := windowsCloseSent.FindStringSubmatch(line)
				window, _ := strconv.Atoi(m[1])
				typ, _ := strconv.Atoi(m[2])
				s.send(&mc.ContainerClosePacket{WindowID: byte(window), ContainerType: byte(typ)})
			case strings.HasPrefix(line, "/tp "):
				s.run(func(_ *world.Tx, p *player.Player) { p.Teleport(p.Position().Add([3]float64{3, 0, 3})) })
			case strings.HasPrefix(line, "/execute in nether"):
				s.run(func(tx *world.Tx, p *player.Player) { p.TeleportToWorld(tx, nether, p.Position()) })
				s.w = nether
			case strings.HasPrefix(line, "/execute in overworld"):
				s.run(func(tx *world.Tx, p *player.Player) { p.TeleportToWorld(tx, overworld, p.Position()) })
				s.w = overworld
			case line == "/kill @s":
				s.run(func(_ *world.Tx, p *player.Player) { p.Hurt(1000, entity.FreezeDamageSource{}) })
			default:
				t.Fatalf("step %q: cannot replay %q", name, line)
			}
		}
	})
	return pks, true
}

// outcome is what a step did to the client: it was sent the ContainerOpen of
// a window, and the answer to a close.
type outcome struct{ opened, answered bool }

func (o outcome) String() string {
	return fmt.Sprintf("{opened %v, answered %v}", o.opened, o.answered)
}

func bdsOutcome(got []string) (o outcome) {
	for _, g := range got {
		if strings.HasPrefix(g, "ContainerOpen ") {
			o.opened = true
		}
		if windowsEchoGot.MatchString(g) {
			o.answered = true
		}
	}
	return o
}

func sessionOutcome(pks []proto.Packet) (o outcome) {
	for _, pk := range pks {
		switch pk := pk.(type) {
		case *mc.ContainerOpenPacket:
			o.opened = true
		case *mc.ContainerClosePacket:
			if !pk.ServerSide {
				o.answered = true
			}
		}
	}
	return o
}
