package server

import (
	"archive/zip"
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

	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/playerdb"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/df-mc/goleveldb/leveldb/opt"
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
// ADAMANT_BENCH_DIR if set, without and with the checkpoint first, and as an
// archive, while a
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
	for _, bc := range []struct {
		name      string
		saveFirst bool
		format    BackupFormat
	}{{"snapshot", false, BackupFolder}, {"checkpoint+snapshot", true, BackupFolder}, {"zip", false, BackupZip}} {
		b.Run(bc.name, func(b *testing.B) {
			srv.conf.Backup.SaveFirst, srv.conf.Backup.Format = bc.saveFirst, bc.format
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

// TestBackupSchedule runs the schedule with an interval of 100 ms: backups
// by the server for autosave, the next one due an interval after the last
// ended, and none while saving is off.
func TestBackupSchedule(t *testing.T) {
	dir := t.TempDir()
	conf := backupConfig(t, dir, BackupConfig{Interval: 100 * time.Millisecond, Keep: 100})
	jt := newJoinTest(t, conf.PlayerProvider, conf)
	start := time.Now()
	jt.start()
	defer jt.srv.Close()
	var list []BackupInfo
	for deadline := time.Now().Add(30 * time.Second); len(list) < 2; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d scheduled backups in 30s, want 2", len(list))
		}
		var err error
		if list, err = jt.srv.Backups(); err != nil {
			t.Fatal(err)
		}
	}
	first := list[len(list)-1]
	if first.Reason != "autosave" || first.By.Kind != ActorServer || first.Created.Sub(start) < 100*time.Millisecond {
		t.Errorf("the first scheduled backup is %+v, want one for autosave by the server an interval after the start", first)
	}
	s := jt.srv.BackupStatus()
	if s.Last == nil || s.Next.IsZero() {
		t.Errorf("status %+v, want the last backup and the next one due", s)
	}

	jt.srv.SaveEnabled(false)
	// Let the backup due or running end.
	time.Sleep(300 * time.Millisecond)
	before, _ := jt.srv.Backups()
	time.Sleep(500 * time.Millisecond)
	after, _ := jt.srv.Backups()
	if len(after) != len(before) {
		t.Errorf("%d backups scheduled while saving was off", len(after)-len(before))
	}
	if _, err := jt.srv.Backup(context.Background(), Actor{Kind: ActorConsole}, ""); err != nil {
		t.Errorf("a backup asked for while saving is off: %v", err)
	}

	if err := jt.srv.Close(); err != nil {
		t.Fatal(err)
	}
	var scheduled, skipped int
	for _, r := range readAudit(t, filepath.Join(dir, "audit.log")) {
		switch {
		case r.Action != "backup" || r.Actor.Kind != audit.KindServer:
		case r.Result == audit.ResultOK && len(r.Output) == 1 && strings.Contains(r.Output[0], "done (autosave)"):
			scheduled++
		case r.Result == audit.ResultRefused && slices.Equal(r.Output, []string{"Backup (autosave) refused: saving is off."}):
			skipped++
		}
	}
	if scheduled < 2 || skipped == 0 {
		t.Errorf("the audit log records %d scheduled backups and %d skipped, want 2 or more and 1 or more", scheduled, skipped)
	}
}

// readAudit returns the records of the audit log at path.
func readAudit(t *testing.T, path string) []audit.Record {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []audit.Record
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r audit.Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

// TestBackupAudited checks that every backup is recorded in the audit log
// with how it ended, by the actor that asked for it: those made, the
// backups that retention deleted, and those refused for the space of the
// disk or as backups are off.
func TestBackupAudited(t *testing.T) {
	dir := t.TempDir()
	conf := backupConfig(t, dir, BackupConfig{Keep: 1})
	jt := newJoinTest(t, conf.PlayerProvider, conf)
	jt.start()
	srv := jt.srv
	clock := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	srv.backups.now = func() time.Time { return clock }
	backup := func(by Actor, reason string) (BackupInfo, error) {
		clock = clock.Add(time.Hour)
		return srv.Backup(context.Background(), by, reason)
	}
	first, err := backup(Actor{Kind: ActorRemote, Name: "remote:panel"}, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := backup(Actor{Kind: ActorServer, Name: "Server"}, "autosave")
	if err != nil {
		t.Fatal(err)
	}
	srv.conf.Backup.MinFreeSpace = 1 << 30
	srv.backups.free = func(string) (uint64, bool, error) { return 1 << 20, true, nil }
	if _, err := backup(Actor{Kind: ActorConsole, Name: "Console"}, ""); err == nil {
		t.Fatal("a backup on a full disk was not refused")
	}
	srv.conf.Backup.Folder = ""
	if _, err := backup(Actor{}, "plugin"); !errors.Is(err, ErrBackupsOff) {
		t.Fatalf("a backup with backups off: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}

	type rec struct {
		kind          audit.Kind
		token, action string
		target        string
		result        audit.Result
		output        string
	}
	var got []rec
	for _, r := range readAudit(t, filepath.Join(dir, "audit.log")) {
		if strings.HasPrefix(r.Action, "backup") {
			got = append(got, rec{r.Actor.Kind, r.Actor.Token, r.Action, r.Target, r.Result, strings.Join(r.Output, "|")})
		}
	}
	want := []rec{
		{audit.KindRemote, "panel", "backup", first.Name, audit.ResultOK, "Backup " + first.Name + " done (manual): "},
		{audit.KindServer, "", "backup-delete", first.Name, audit.ResultOK, "Deleted: more than Keep = 1 backups."},
		{audit.KindServer, "", "backup", second.Name, audit.ResultOK, "Backup " + second.Name + " done (autosave): "},
		{audit.KindConsole, "", "backup", "", audit.ResultRefused, "Backup (manual) refused: backup refused: 1.0 MiB free on "},
		{audit.KindAPI, "", "backup", "", audit.ResultRefused, "Backup (plugin) refused: " + ErrBackupsOff.Error()},
	}
	ok := len(got) == len(want)
	for i := 0; ok && i < len(want); i++ {
		g, w := got[i], want[i]
		ok = g.kind == w.kind && g.token == w.token && g.action == w.action && g.target == w.target && g.result == w.result && strings.HasPrefix(g.output, w.output)
	}
	if !ok {
		t.Errorf("the audit log records the backups as\n%+v\nwant\n%+v", got, want)
	}
}

// TestMcworldZip backs up, as .mcworld archives, a server whose world has
// tables: an archive holds the world at its root, its tables stored and its
// other files deflated, with adamant-backup.json and the admin lists; the
// server lists it, verifies it, and deletes the archives Keep keeps no
// more; unpacked without the adamant- entries, it opens as the world at its
// checkpoint.
func TestMcworldZip(t *testing.T) {
	dir := t.TempDir()
	db, err := mcdb.Config{Log: slog.New(slog.DiscardHandler), LDBOptions: &opt.Options{WriteBuffer: 64 << 10}}.Open(filepath.Join(dir, "world"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 200 {
		if err := db.StoreColumn(world.ChunkPos{int32(i % 16), int32(i / 16)}, world.Overworld, stallColumn(i)); err != nil {
			t.Fatal(err)
		}
	}
	ops, err := loadOperators(filepath.Join(dir, "ops.json"))
	if err != nil {
		t.Fatal(err)
	}
	conf := Config{WorldProvider: db, PlayerProvider: playerdb.NewWorldProvider(db, nil), IdentityKey: identity.Name,
		Allower: admission{mode: identity.Name, operators: ops, bans: &banList{ips: map[string]banEntry{}}, whitelist: &Whitelist{}},
		Backup:  BackupConfig{Folder: filepath.Join(dir, "backups"), Format: BackupZip, Keep: 2, SaveFirst: true, Verify: mcdb.VerifyFull}}
	jt := newJoinTest(t, conf.PlayerProvider, conf)
	jt.start()
	defer jt.srv.Close()
	if _, err := jt.srv.Op("Alice"); err != nil {
		t.Fatal(err)
	}
	var infos []BackupInfo
	for range 3 {
		info, err := jt.srv.Backup(context.Background(), Actor{Kind: ActorAPI, Name: "test"}, "zip")
		if err != nil {
			t.Fatal(err)
		}
		infos = append(infos, info)
	}
	info := infos[2]
	if info.Format != BackupZip || info.Path != filepath.Join(dir, "backups", info.Name+zipExtension) {
		t.Fatalf("backup %+v, want a .mcworld archive", info)
	}
	list, err := jt.srv.Backups()
	if err != nil || len(list) != 2 || list[0].Name != info.Name || list[0].Format != BackupZip || list[0].Checkpoint.ID != info.Checkpoint.ID {
		t.Fatalf("the backups listed are %+v (%v), want the 2 newest archives", list, err)
	}
	if _, err := os.Lstat(infos[0].Path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the oldest archive is left: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "backups"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("%s is left in the backup folder", e.Name())
		}
	}
	if _, err := jt.srv.VerifyBackup(info.Name, true); err != nil {
		t.Fatal(err)
	}

	r, err := zip.OpenReader(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	methods := map[string]uint16{}
	for _, f := range r.File {
		methods[f.Name] = f.Method
	}
	tables := 0
	for name, method := range methods {
		switch {
		case strings.HasSuffix(name, ".ldb"):
			tables++
			if method != zip.Store || !strings.HasPrefix(name, "db/") {
				t.Errorf("table %s: method %d, want stored in db/", name, method)
			}
		case method != zip.Deflate:
			t.Errorf("%s: method %d, want deflated", name, method)
		case strings.HasPrefix(name, "world/"):
			t.Errorf("%s: the world is not at the root of the archive", name)
		}
	}
	for _, name := range []string{"level.dat", "db/CURRENT", archiveJSON, archiveAdmin + "ops.json"} {
		if _, ok := methods[name]; !ok {
			t.Errorf("the archive lacks %s: %v", name, methods)
		}
	}
	if tables == 0 {
		t.Error("the archive holds no table")
	}
	f, err := readArchiveFile(info.Path)
	if err != nil || f.Format != "zip" || f.Name != info.Name {
		t.Errorf("%s holds %+v (%v)", archiveJSON, f, err)
	}

	unpacked := filepath.Join(t.TempDir(), "restored")
	if err := unzipWorld(info.Path, unpacked); err != nil {
		t.Fatal(err)
	}
	if rec, err := mcdb.VerifyBackup(unpacked, true); err != nil || rec.ID != info.Checkpoint.ID {
		t.Fatalf("the world unpacked verifies as checkpoint %d (%v), want %d", rec.ID, err, info.Checkpoint.ID)
	}
	restored, err := mcdb.Config{Log: slog.New(slog.DiscardHandler)}.Open(unpacked)
	if err != nil {
		t.Fatal(err)
	}
	col, err := restored.LoadColumn(world.ChunkPos{3, 5}, world.Overworld)
	if err != nil || col == nil {
		t.Errorf("a column of the world unpacked: %v", err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
}
