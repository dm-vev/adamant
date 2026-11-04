package server

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/google/uuid"
)

// saveLog records the writes of a recordingWorld and recordingPlayers.
type saveLog struct {
	mu     sync.Mutex
	events []string
}

func (l *saveLog) add(e string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

// take returns the writes recorded since the last call.
func (l *saveLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	events := l.events
	l.events = nil
	return events
}

// recordingWorld is a world provider that records the columns it stores and
// the writes of its settings.
type recordingWorld struct {
	world.NopProvider
	log *saveLog
}

func (p *recordingWorld) StoreColumn(world.ChunkPos, world.Dimension, *chunk.Column) error {
	p.log.add("column")
	return nil
}

func (p *recordingWorld) WriteSettings() error {
	p.log.add("level.dat")
	return nil
}

// recordingPlayers is a player provider that records the players it saves.
type recordingPlayers struct {
	player.NopProvider
	log *saveLog
}

func (p *recordingPlayers) Save(uuid.UUID, player.Config, *world.World) error {
	p.log.add("player")
	return nil
}

// TestCheckpointsRunInOrderAndHold checks the checkpoints of a server: they
// run every SaveInterval and write the columns changed, then the players,
// then level.dat; save off holds them; closing the server saves one last
// checkpoint after the players saved as they left.
func TestCheckpointsRunInOrderAndHold(t *testing.T) {
	log := &saveLog{}
	jt := newJoinTest(t, &recordingPlayers{log: log}, Config{WorldProvider: &recordingWorld{log: log}, SaveInterval: 50 * time.Millisecond})
	jt.start()
	h := jt.join(t, joinLogin("Alice", ""))

	// awaitCheckpoint waits for a checkpoint for which ok holds, of those
	// written since the last call, each the writes up to its level.dat.
	var pending []string
	awaitCheckpoint := func(what string, ok func(cp []string) bool) []string {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			pending = append(pending, log.take()...)
			for {
				end := slices.Index(pending, "level.dat")
				if end < 0 {
					break
				}
				cp := pending[:end+1]
				pending = pending[end+1:]
				if ok(cp) {
					return cp
				}
			}
		}
		t.Fatalf("no checkpoint %s within 10s", what)
		return nil
	}
	awaitCheckpoint("saving Alice", func(cp []string) bool { return slices.Contains(cp, "player") })

	if err := player.Do(h, func(tx *world.Tx, p *player.Player) {
		tx.SetBlock(cube.PosFromVec3(p.Position()).Add(cube.Pos{2, 0, 0}), block.Stone{}, nil)
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	cp := awaitCheckpoint("saving a column changed", func(cp []string) bool { return slices.Contains(cp, "column") })
	want := strings.Repeat("column ", strings.Count(strings.Join(cp, " "), "column")) + "player level.dat"
	if got := strings.Join(cp, " "); got != want {
		t.Errorf("a checkpoint wrote %q, want the columns, then Alice, then level.dat", got)
	}

	jt.srv.SaveEnabled(false)
	if jt.srv.SaveEnabledState() {
		t.Fatal("saving is still on after save off")
	}
	// A checkpoint under way when saving turned off ends first.
	time.Sleep(100 * time.Millisecond)
	_, pending = log.take(), nil
	time.Sleep(300 * time.Millisecond)
	if held := log.take(); len(held) != 0 {
		t.Errorf("with saving off, the server wrote %v, want nothing", held)
	}
	jt.srv.SaveEnabled(true)
	awaitCheckpoint("once saving is on again", func([]string) bool { return true })

	jt.srv.SaveEnabled(false)
	time.Sleep(100 * time.Millisecond)
	_, pending = log.take(), nil
	if err := jt.srv.Close(); err != nil {
		t.Fatal(err)
	}
	// Alice is saved as she leaves, and the last checkpoint after her,
	// although saving is off.
	if got := log.take(); len(got) < 2 || got[0] != "player" || !slices.Contains(got[1:], "level.dat") || slices.Contains(got[1:], "player") {
		t.Errorf("closing the server wrote %v, want Alice, then a checkpoint with level.dat", got)
	}
}
