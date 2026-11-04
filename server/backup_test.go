package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/playerdb"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// backupConfig returns the Config of a server saving its world, players and
// lists in dir, as config.toml has them, backing up into dir/backups as
// backup says.
func backupConfig(t testing.TB, dir string, backup BackupConfig) Config {
	t.Helper()
	uc := DefaultConfig()
	uc.World.Folder, uc.Players.Folder = filepath.Join(dir, "world"), filepath.Join(dir, "players")
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	uc.Server.IdentityKey = "name"
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	backup.Folder = filepath.Join(dir, "backups")
	conf.Backup = backup
	return conf
}

// TestBackupHoldsItsCheckpoint backs up a server while Alice holds 10
// diamonds, with SaveFirst. She then puts them in a chest, which a
// checkpoint saves. The backup holds Alice with her 10 diamonds and no
// chest, at the checkpoint saved before it, with the admin lists, and
// describes itself in backup.json.
func TestBackupHoldsItsCheckpoint(t *testing.T) {
	dir := t.TempDir()
	conf := backupConfig(t, dir, BackupConfig{SaveFirst: true, Verify: mcdb.VerifyFull})
	jt := newJoinTest(t, conf.PlayerProvider, conf)
	jt.start()
	h := jt.join(t, joinLogin("Alice", ""))
	if _, err := jt.srv.Op("Alice"); err != nil {
		t.Fatal(err)
	}
	chest := cube.Pos{8, 100, 8}
	do := func(f func(tx *world.Tx, p *player.Player)) {
		if err := player.Do(h, f).Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	do(func(_ *world.Tx, p *player.Player) { _ = p.Inventory().SetItem(0, item.NewStack(item.Diamond{}, 10)) })

	info, err := jt.srv.Backup(context.Background(), Actor{Kind: ActorAPI, Name: "test"}, "Before the chest!")
	if err != nil {
		t.Fatal(err)
	}
	saved := jt.srv.checkpoints.lastID()
	do(func(tx *world.Tx, p *player.Player) {
		tx.SetBlock(chest, block.NewChest(), nil)
		_ = p.Inventory().SetItem(0, item.Stack{})
		_, _ = tx.Block(chest).(block.Chest).Inventory(tx, chest).AddItem(item.NewStack(item.Diamond{}, 10))
	})
	if err := jt.srv.SaveAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := jt.srv.VerifyBackup(info.Name, true); err != nil {
		t.Fatal(err)
	}
	if err := jt.srv.Close(); err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(info.Name, "-before-the-chest") || info.Path != filepath.Join(dir, "backups", info.Name) {
		t.Errorf("backup %q at %q", info.Name, info.Path)
	}
	if info.Checkpoint.ID == 0 || info.Checkpoint.ID != saved {
		t.Errorf("the backup holds checkpoint %d, want %d, saved before it", info.Checkpoint.ID, saved)
	}
	tables, _ := filepath.Glob(filepath.Join(info.Path, backupWorld, "db", "*.ldb"))
	if info.Linked != len(tables) || info.Verified != "full" || !slices.Contains(info.Admin, "ops.json") || !slices.Contains(info.Admin, "white-list.txt") {
		t.Errorf("backup %+v: want its %d tables linked, verified in full, with ops.json and white-list.txt", info, len(tables))
	}
	data, err := os.ReadFile(filepath.Join(info.Path, backupJSON))
	if err != nil {
		t.Fatal(err)
	}
	var f backupFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Name != info.Name || f.Reason != "before-the-chest" || f.By != "test" || f.Format != "folder" || f.World != "world" || f.Checkpoint == nil || f.Checkpoint.ID != saved || f.Verified != "full" {
		t.Errorf("backup.json is\n%s", data)
	}
	if ops, err := os.ReadFile(filepath.Join(info.Path, backupAdmin, "ops.json")); err != nil || !strings.Contains(strings.ToLower(string(ops)), "alice") {
		t.Errorf("admin/ops.json holds %q (%v), want Alice", ops, err)
	}

	alice, inChest := backedUp(t, filepath.Join(info.Path, backupWorld), chest, saved)
	if alice != 10 || inChest != -1 {
		t.Errorf("the backup holds Alice with %d diamonds and a chest of %d, want 10 and no chest", alice, inChest)
	}
}

// backedUp opens the world in dir, checks it holds the checkpoint id, and
// returns the diamonds of Alice and those of the chest at pos, -1 if
// there is none.
func backedUp(t *testing.T, dir string, pos cube.Pos, id uint64) (alice, inChest int) {
	t.Helper()
	db, err := mcdb.Config{Log: slog.New(slog.DiscardHandler)}.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := db.LastCheckpoint(); err != nil || r.ID != id {
		t.Errorf("the world of the backup holds checkpoint %d (%v), want %d", r.ID, err, id)
	}
	w := world.Config{Provider: db, Generator: world.NopGenerator{}, ReadOnly: true, Synchronous: true, Log: slog.New(slog.DiscardHandler)}.New()
	defer w.Close()
	inChest = -1
	<-w.Exec(func(tx *world.Tx) {
		if c, ok := tx.Block(pos).(block.Chest); ok {
			inChest = diamondsIn(c.Inventory(tx, pos))
		}
	})
	d, _, err := playerdb.NewWorldProvider(db, nil).Load(identity.OfflineUUID("Alice"), func(world.Dimension) *world.World { return w })
	if err != nil {
		t.Fatalf("load Alice from the backup: %v", err)
	}
	return diamondsIn(d.Inventory), inChest
}

// TestBackupRetentionAndSpace makes backups an hour apart by a fake clock
// and checks what each leaves in the folder: the Keep newest, none older
// than MaxAge but the newest, the files the server did not write, a refusal
// below MinFreeSpace after deleting what the backup would have, and no
// unfinished backup after a start.
func TestBackupRetentionAndSpace(t *testing.T) {
	dir := t.TempDir()
	conf := backupConfig(t, dir, BackupConfig{Keep: 3})
	jt := newJoinTest(t, conf.PlayerProvider, conf)
	jt.start()
	srv := jt.srv
	defer srv.Close()
	clock := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	srv.backups.now = func() time.Time { return clock }
	folder := conf.Backup.Folder
	foreign := []string{"notes.txt", "copied-by-hand"}
	if err := os.MkdirAll(filepath.Join(folder, foreign[1]), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, foreign[0]), []byte("mine"), 0644); err != nil {
		t.Fatal(err)
	}
	backup := func() error {
		clock = clock.Add(time.Hour)
		_, err := srv.Backup(context.Background(), Actor{Kind: ActorServer}, "scheduled")
		return err
	}
	names := func() []string {
		list, err := srv.Backups()
		if err != nil {
			t.Fatal(err)
		}
		var hours []string
		for _, info := range list {
			hours = append(hours, info.Created.Format("15"))
		}
		return hours
	}
	for range 5 {
		if err := backup(); err != nil {
			t.Fatal(err)
		}
	}
	if got := names(); !slices.Equal(got, []string{"05", "04", "03"}) {
		t.Fatalf("Keep = 3 keeps the backups of hours %v, want 05, 04, 03", got)
	}

	srv.conf.Backup.MaxAge = 90 * time.Minute
	if err := backup(); err != nil {
		t.Fatal(err)
	}
	if got := names(); !slices.Equal(got, []string{"06", "05"}) {
		t.Fatalf("MaxAge = 90m keeps %v, want 06 and 05", got)
	}
	clock = clock.Add(24 * time.Hour)
	srv.backups.retain("")
	if got := names(); !slices.Equal(got, []string{"06"}) {
		t.Fatalf("a day later MaxAge keeps %v, want the newest only", got)
	}

	srv.conf.Backup.MaxAge, srv.conf.Backup.Keep = 0, 2
	if err := backup(); err != nil {
		t.Fatal(err)
	}
	srv.conf.Backup.MinFreeSpace = 1 << 30
	srv.backups.free = func(string) (uint64, bool, error) { return 1<<30 - 1, true, nil }
	err := backup()
	if err == nil || !strings.Contains(err.Error(), "backup refused") || !strings.Contains(err.Error(), "under MinFreeSpace 1.0 GiB") {
		t.Fatalf("a backup on a full disk: %v, want it refused", err)
	}
	if got := names(); len(got) != 1 {
		t.Fatalf("a refused backup leaves %v, want the newest only: it deletes first what it would have", got)
	}
	srv.backups.free = func(string) (uint64, bool, error) { return 2 << 30, true, nil }
	if err := backup(); err != nil {
		t.Fatal(err)
	}

	partial := filepath.Join(folder, partialPrefix+"crashed")
	if err := os.MkdirAll(filepath.Join(partial, "world"), 0755); err != nil {
		t.Fatal(err)
	}
	srv.backups.sweepAtStart()
	if _, err := os.Lstat(partial); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an unfinished backup is left at start: %v", err)
	}
	for _, name := range foreign {
		if _, err := os.Lstat(filepath.Join(folder, name)); err != nil {
			t.Errorf("%v, which the server did not write, is gone: %v", name, err)
		}
	}
}

// blockingSnapshots is a world whose snapshots write a file and wait for
// their context to be done.
type blockingSnapshots struct {
	*mcdb.DB
	started chan struct{}
	once    *sync.Once
}

func (b blockingSnapshots) Snapshot(ctx context.Context, dst string, _ mcdb.SnapshotOptions) (mcdb.SnapshotResult, error) {
	if err := os.MkdirAll(filepath.Join(dst, "db"), 0755); err != nil {
		return mcdb.SnapshotResult{}, err
	}
	if err := os.WriteFile(filepath.Join(dst, "db", "CURRENT"), []byte("MANIFEST-000001\n"), 0644); err != nil {
		return mcdb.SnapshotResult{}, err
	}
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return mcdb.SnapshotResult{}, ctx.Err()
}

// TestBackupCancelledByClose closes a server while its backup copies the
// world: the backup fails as cancelled, leaves nothing in the folder, and
// the server saves its last checkpoint.
func TestBackupCancelledByClose(t *testing.T) {
	dir := t.TempDir()
	conf := backupConfig(t, dir, BackupConfig{SaveFirst: true})
	db := conf.WorldProvider.(*mcdb.DB)
	snaps := blockingSnapshots{DB: db, started: make(chan struct{}), once: new(sync.Once)}
	conf.WorldProvider = snaps
	jt := newJoinTest(t, conf.PlayerProvider, conf)
	jt.start()
	before := jt.srv.checkpoints.lastID()
	failed := make(chan error, 1)
	go func() {
		_, err := jt.srv.Backup(context.Background(), Actor{Kind: ActorConsole, Name: "Console"}, "")
		failed <- err
	}()
	select {
	case <-snaps.started:
	case <-time.After(30 * time.Second):
		t.Fatal("the backup did not start")
	}
	if _, err := jt.srv.Backup(context.Background(), Actor{Kind: ActorAPI}, ""); !errors.Is(err, ErrBackupRunning) || !strings.Contains(err.Error(), "by Console") {
		t.Errorf("a second backup: %v, want ErrBackupRunning by Console", err)
	}
	if err := jt.srv.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-failed; err == nil || !strings.Contains(err.Error(), "the server is closing") {
		t.Errorf("the backup cancelled by the close: %v", err)
	}
	if entries, err := os.ReadDir(conf.Backup.Folder); err != nil || len(entries) != 0 {
		t.Errorf("the backup folder holds %v (%v) after a cancelled backup, want nothing", entries, err)
	}
	if _, err := jt.srv.Backup(context.Background(), Actor{Kind: ActorAPI}, ""); err == nil {
		t.Error("a backup after the close did not fail")
	}
	reopened, err := mcdb.Config{Log: slog.New(slog.DiscardHandler)}.Open(filepath.Join(dir, "world"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	// The checkpoint of the backup, then that of the close.
	if r, err := reopened.LastCheckpoint(); err != nil || r.ID != before+2 {
		t.Errorf("the world holds checkpoint %d (%v) after the close, want %d", r.ID, err, before+2)
	}
}

// TestBackupsOff checks the errors of a server that cannot back up.
func TestBackupsOff(t *testing.T) {
	jt := newJoinTest(t, nil, Config{})
	jt.start()
	defer jt.srv.Close()
	if _, err := jt.srv.Backup(context.Background(), Actor{}, ""); !errors.Is(err, ErrBackupsOff) {
		t.Errorf("no folder: %v", err)
	}
	jt.srv.conf.Backup.Folder = t.TempDir()
	if _, err := jt.srv.Backup(context.Background(), Actor{}, ""); err == nil || !strings.Contains(err.Error(), "cannot copy its world") {
		t.Errorf("no mcdb world: %v", err)
	}
	if got := backupReason("  Hello, World / 2!  "); got != "hello-world-2" {
		t.Errorf("backupReason = %q", got)
	}
	if got := backupReason(strings.Repeat("ab", 40)); len(got) != 32 {
		t.Errorf("backupReason of 80 letters is %d long, want 32", len(got))
	}
}

// stallColumn returns a column of terrain of distinct blocks, some KiB
// encoded, which seed varies.
func stallColumn(seed int) *chunk.Column {
	world.DefaultBlockRegistry.Finalize()
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	blocks := []world.Block{block.Stone{}, block.Dirt{}, block.Grass{}, block.Sand{}, block.Gravel{}, block.Clay{}, block.Andesite{}, block.Diorite{}}
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := int16(-64); y < 64; y++ {
				r := int(x)*7 + int(z)*13 + int(y)*3 + seed*31
				c.SetBlock(x, y, z, 0, world.BlockRuntimeID(blocks[(r^(r>>3))%len(blocks)]))
			}
		}
	}
	return &chunk.Column{Chunk: c}
}

// probeOwner runs a transaction on the owner of w about every millisecond
// until stop is closed, and returns the longest it waited for one and the
// number of transactions.
func probeOwner(w *world.World, stop <-chan struct{}) (longest time.Duration, n int) {
	for {
		select {
		case <-stop:
			return longest, n
		default:
		}
		start := time.Now()
		<-w.Exec(func(*world.Tx) {})
		longest = max(longest, time.Since(start))
		n++
		time.Sleep(time.Millisecond)
	}
}

// BenchmarkBackupStall backs up a running server whose world holds
// ADAMANT_BENCH_COLUMNS terrain columns (2,000 by default), in
// ADAMANT_BENCH_DIR if set, without and with the checkpoint first, while a
// probe runs a transaction on the owner of the world about every
// millisecond. It reports the longest the probe waited during the backups,
// and during as long without one, and the CPU time of the process per
// backup.
func BenchmarkBackupStall(b *testing.B) {
	columns := 2000
	if v := os.Getenv("ADAMANT_BENCH_COLUMNS"); v != "" {
		if _, err := fmt.Sscan(v, &columns); err != nil {
			b.Fatal(err)
		}
	}
	dir := b.TempDir()
	if v := os.Getenv("ADAMANT_BENCH_DIR"); v != "" {
		var err error
		if dir, err = os.MkdirTemp(v, "backup-stall-"); err != nil {
			b.Fatal(err)
		}
		defer os.RemoveAll(dir)
	}
	conf := backupConfig(b, dir, BackupConfig{Keep: 2})
	db := conf.WorldProvider.(*mcdb.DB)
	for i := range columns {
		if err := db.StoreColumn(world.ChunkPos{int32(i % 64), int32(i / 64)}, world.Overworld, stallColumn(i)); err != nil {
			b.Fatal(err)
		}
	}
	conf.Log = slog.New(slog.DiscardHandler)
	conf.DisableNether, conf.DisableEnd = true, true
	conf.Generator = func(world.Dimension, int64) world.Generator { return world.NopGenerator{} }
	srv := conf.New()
	srv.Listen()
	defer srv.Close()
	w := srv.World()
	for _, saveFirst := range []bool{false, true} {
		name := "snapshot"
		if saveFirst {
			name = "checkpoint+snapshot"
		}
		b.Run(name, func(b *testing.B) {
			srv.conf.Backup.SaveFirst = saveFirst
			var stall, idle, cpu time.Duration
			var bytes int64
			for b.Loop() {
				stop, probed := make(chan struct{}), make(chan time.Duration, 1)
				go func() { d, _ := probeOwner(w, stop); probed <- d }()
				start, startCPU := time.Now(), processCPUTime()
				info, err := srv.Backup(context.Background(), Actor{Kind: ActorAPI}, "bench")
				cpu += processCPUTime() - startCPU
				took := time.Since(start)
				close(stop)
				stall = max(stall, <-probed)
				if err != nil {
					b.Fatal(err)
				}
				bytes = info.Bytes
				b.StopTimer()
				stop, probed = make(chan struct{}), make(chan time.Duration, 1)
				go func() { d, _ := probeOwner(w, stop); probed <- d }()
				time.Sleep(took)
				close(stop)
				idle = max(idle, <-probed)
				b.StartTimer()
			}
			b.ReportMetric(float64(bytes)/(1<<20), "MiB")
			b.ReportMetric(float64(stall.Microseconds())/1000, "stall-max-ms")
			b.ReportMetric(float64(idle.Microseconds())/1000, "idle-max-ms")
			b.ReportMetric(cpu.Seconds()*1000/float64(b.N), "cpu-ms/op")
		})
	}
}
