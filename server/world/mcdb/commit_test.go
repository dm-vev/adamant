package mcdb

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/google/uuid"
)

// commitHelperEnv runs TestCommitCrashHelper in a subprocess of
// TestCrashDuringCommitIsAllOrNothing, with the directory of its world.
const commitHelperEnv = "ADAMANT_COMMIT_HELPER"

var (
	// markerA is changed by both checkpoints of the helper, markerB is set
	// by the second only.
	markerA, markerB = world.ChunkPos{0, 0}, world.ChunkPos{1, 0}
	commitAlice      = uuid.MustParse("6d1c62f5-1e30-4a4b-9a43-6f4b8bd0a7a1")
)

// markerColumn returns a column holding b at its bottom corner.
func markerColumn(t *testing.T, b world.Block) *chunk.Column {
	t.Helper()
	world.DefaultBlockRegistry.Finalize()
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	c.SetBlock(0, 0, 0, 0, world.BlockRuntimeID(b))
	return &chunk.Column{Chunk: c}
}

// fillerColumn returns a column of distinct blocks, some tens of KiB
// encoded, for a commit to take more than one batch of its transaction.
func fillerColumn(t *testing.T, seed int) *chunk.Column {
	t.Helper()
	world.DefaultBlockRegistry.Finalize()
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	blocks := []world.Block{block.Stone{}, block.Dirt{}, block.Grass{}, block.Sand{}, block.Gravel{}, block.Clay{}}
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := int16(0); y < 64; y++ {
				c.SetBlock(x, y, z, 0, world.BlockRuntimeID(blocks[(int(x)*7+int(z)*13+int(y)*3+seed)%len(blocks)]))
			}
		}
	}
	return &chunk.Column{Chunk: c}
}

// blockAt returns the block at the bottom corner of the column at pos in
// db, or nil if db holds no column there.
func blockAt(t *testing.T, db *DB, pos world.ChunkPos) world.Block {
	t.Helper()
	col, err := db.LoadColumn(pos, world.Overworld)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	b, _ := world.BlockByRuntimeID(col.Chunk.Block(0, 0, 0, 0))
	return b
}

// TestCrashDuringCommitIsAllOrNothing crashes a commit halfway through. Its
// helper commits checkpoint 1, stone at marker A and Alice's record "one"
// with the settings at tick 100, then checkpoint 2, dirt at A, stone at
// marker B, filler columns and Alice's record "two" at tick 200, through a
// transaction, and is killed with SIGKILL once half of the transaction was
// written. Reopened, the database holds checkpoint 1 exactly: stone at A,
// nothing at B, Alice's "one", the record of checkpoint 1 and its settings.
func TestCrashDuringCommitIsAllOrNothing(t *testing.T) {
	if os.Getenv(commitHelperEnv) != "" {
		t.Skip("in the subprocess")
	}
	dir := t.TempDir()
	c := exec.Command(os.Args[0], "-test.run=^TestCommitCrashHelper$", "-test.v")
	c.Env = append(os.Environ(), commitHelperEnv+"="+dir)
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c.Stderr = c.Stdout
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	half := make(chan struct{})
	var lines []string
	go func() {
		s := bufio.NewScanner(out)
		for s.Scan() {
			lines = append(lines, s.Text())
			if s.Text() == "HALF" {
				close(half)
				_, _ = io.Copy(io.Discard, out)
				return
			}
		}
	}()
	select {
	case <-half:
	case <-time.After(60 * time.Second):
		_ = c.Process.Kill()
		_ = c.Wait()
		t.Fatalf("the helper did not reach half of its commit within 60s:\n%s", strings.Join(lines, "\n"))
	}
	// The crash.
	if err := c.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = c.Wait()

	db, err := Config{Log: slog.New(slog.DiscardHandler)}.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, b := blockAt(t, db, markerA), blockAt(t, db, markerB)
	record, err := db.LoadPlayer(commitAlice)
	t.Logf("after the crash: %v at A, %v at B, Alice %q (%v), tick %d", a, b, record, err, db.Settings().CurrentTick)
	if a != (block.Stone{}) || b != nil {
		t.Errorf("after the crash A holds %v and B %v, want stone and nothing, as at checkpoint 1", a, b)
	}
	if err != nil || string(record) != "one" {
		t.Errorf("after the crash Alice's record is %q (%v), want \"one\", as at checkpoint 1", record, err)
	}
	if r, ok, err := db.lastCheckpoint(); err != nil || !ok || r.ID != 1 {
		t.Errorf("after the crash the last checkpoint is %d (%t, %v), want 1", r.ID, ok, err)
	}
	if tick := db.Settings().CurrentTick; tick != 100 {
		t.Errorf("after the crash the current tick is %d, want 100, as at checkpoint 1", tick)
	}
}

// TestCommitCrashHelper is the subprocess of
// TestCrashDuringCommitIsAllOrNothing. It prints HALF once half of its
// second commit was written, and waits to be killed.
func TestCommitCrashHelper(t *testing.T) {
	dir := os.Getenv(commitHelperEnv)
	if dir == "" {
		t.Skip("only run by TestCrashDuringCommitIsAllOrNothing")
	}
	db, err := Config{Log: slog.New(slog.DiscardHandler)}.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	commitBatchLimit, transactionChunk = 0, 64<<10
	checkpoint := func(id uint64, tick int64, a world.Block, b bool, alice string) {
		c := db.BeginCommit()
		if err := c.StoreColumn(markerA, world.Overworld, markerColumn(t, a)); err != nil {
			t.Fatal(err)
		}
		if b {
			if err := c.StoreColumn(markerB, world.Overworld, markerColumn(t, block.Stone{})); err != nil {
				t.Fatal(err)
			}
			for i := range 16 {
				if err := c.StoreColumn(world.ChunkPos{int32(i + 2), 0}, world.Overworld, fillerColumn(t, i)); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := db.SavePlayer(commitAlice, []byte(alice)); err != nil {
			t.Fatal(err)
		}
		s := db.Settings().Snapshot()
		s.CurrentTick = tick
		c.SaveSettings(s)
		if err := c.Commit(id); err != nil {
			t.Fatal(err)
		}
	}
	checkpoint(1, 100, block.Stone{}, false, "one")
	commitHook = func() {
		fmt.Println("HALF")
		select {}
	}
	checkpoint(2, 200, block.Dirt{}, true, "two")
	t.Fatal("the second commit returned")
}

// TestOldLevelDatIsReconciled checks that a level.dat older than the last
// checkpoint committed, as a crash between the commit and the write of
// level.dat leaves it, is replaced at open by the settings of the
// checkpoint, with a Warn, and that a level.dat as new as it is kept.
func TestOldLevelDatIsReconciled(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	commitAt := func(id uint64, tick int64) {
		t.Helper()
		c := db.BeginCommit()
		s := db.Settings().Snapshot()
		s.CurrentTick, s.Time = tick, tick
		c.SaveSettings(s)
		if err := c.Commit(id); err != nil {
			t.Fatal(err)
		}
		if err := db.WriteSettings(); err != nil {
			t.Fatal(err)
		}
	}
	commitAt(1, 100)
	older, err := os.ReadFile(filepath.Join(dir, "level.dat"))
	if err != nil {
		t.Fatal(err)
	}
	commitAt(2, 200)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	if db, err = (Config{Log: log}).Open(dir); err != nil {
		t.Fatal(err)
	}
	if tick := db.Settings().CurrentTick; tick != 200 || strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("opened with level.dat as new as the checkpoint: tick %d, want 200, and logged %q, want no Warn", tick, logs.String())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// The crash: level.dat as before the last checkpoint.
	if err := os.WriteFile(filepath.Join(dir, "level.dat"), older, 0644); err != nil {
		t.Fatal(err)
	}
	logs.Reset()
	if db, err = (Config{Log: log}).Open(dir); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if s := db.Settings(); s.CurrentTick != 200 || s.Time != 200 {
		t.Errorf("opened with an older level.dat: tick %d and time %d, want 200 and 200, from the checkpoint", s.CurrentTick, s.Time)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "older than the last checkpoint") {
		t.Errorf("opened with an older level.dat logged %q, want a Warn", logs.String())
	}
}

// TestNoOwnerWritesDuringCommit runs a world that ticks, changes its chunks,
// unloads them and saves world data while checkpoints are committed through
// a DB, and checks that nothing writes to the database while a commit is
// open but the commit, and that the world as it was at the last checkpoint
// reads back from the database.
func TestNoOwnerWritesDuringCommit(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var open atomic.Bool
	var mu sync.Mutex
	var direct []string
	db.directWrite = func() {
		if open.Load() {
			mu.Lock()
			direct = append(direct, "a write outside the commit")
			mu.Unlock()
		}
	}
	w := world.Config{Provider: db, Generator: world.NopGenerator{}, RandomTickSpeed: -1, Log: slog.New(slog.DiscardHandler)}.New()

	stop := make(chan struct{})
	done := make(chan struct{})
	var round atomic.Int32
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			<-w.Exec(func(tx *world.Tx) {
				var b world.Block = block.Stone{}
				if i%2 == 1 {
					b = block.Dirt{}
				}
				for x := range 8 {
					tx.SetBlock(cube.Pos{x * 16, 10, 0}, b, nil)
				}
				_ = w.SaveWorldData("test:round", []byte(fmt.Sprint(i)))
				if i%3 == 0 {
					w.CollectGarbage(tx)
				}
			})
			round.Store(int32(i))
		}
	}()
	for id := uint64(1); id <= 10; id++ {
		c := db.BeginCommit()
		open.Store(true)
		cps, err := world.CaptureAllInto(context.Background(), []*world.World{w}, c, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, cp := range cps {
			if err := cp.Write(); err != nil {
				t.Error(err)
			}
		}
		err = c.Commit(id)
		open.Store(false)
		for _, cp := range cps {
			cp.Committed(err)
		}
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(stop)
	<-done
	if round.Load() < 10 {
		t.Errorf("the world ran %d rounds during the checkpoints, want more", round.Load())
	}
	mu.Lock()
	if len(direct) != 0 {
		t.Errorf("%d writes reached the database while a commit was open, want none", len(direct))
	}
	mu.Unlock()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if db, err = Open(dir); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	data, err := db.LoadWorldData("test:round")
	if err != nil || string(data) != fmt.Sprint(round.Load()) {
		t.Errorf("world data after closing = %q (%v), want the last round, %d", data, err, round.Load())
	}
}
