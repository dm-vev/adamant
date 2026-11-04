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
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// BackupFormat is how a backup is written.
type BackupFormat uint8

const (
	// BackupFolder writes a backup as a folder holding a copy of the world
	// that opens as it is, whose tables are hard-links to those of the
	// world and of the other backups on the same file system.
	BackupFolder BackupFormat = iota
	// BackupZip writes a backup as a .mcworld archive, which the game
	// imports as a world: the files of the world at its root, the tables of
	// its database stored and the other files deflated, with backup.json as
	// adamant-backup.json and the admin lists in adamant-admin/.
	BackupZip
)

// String returns the name of f in config.toml: "folder" or "zip".
func (f BackupFormat) String() string {
	if f == BackupZip {
		return "zip"
	}
	return "folder"
}

// BackupConfig is how a Server backs up its world (Server.Backup). A
// Server whose BackupConfig has no Folder makes no backups.
type BackupConfig struct {
	// Folder holds the backups. It must not lie inside the folder of the
	// world.
	Folder string
	// Format is how each backup is written.
	Format BackupFormat
	// Interval is the time between two scheduled backups, from the end of
	// one to the start of the next. Zero or less schedules none.
	Interval time.Duration
	// Keep is how many backups are kept: older ones are deleted after each
	// backup. If zero, DefaultBackupKeep.
	Keep int
	// MaxAge deletes the backups older than it after each backup, the
	// newest excepted. Zero or less deletes none for its age.
	MaxAge time.Duration
	// MinFreeSpace is how many bytes a backup must leave free on the disk
	// of Folder: one that would leave less is refused. Zero checks only
	// that it fits.
	MinFreeSpace int64
	// SaveFirst saves a checkpoint before each backup, so that it holds the
	// world as it is when the backup starts.
	SaveFirst bool
	// Verify is how each backup is verified before it counts.
	Verify mcdb.Verification
}

// DefaultBackupKeep is the Keep of a BackupConfig that sets none.
const DefaultBackupKeep = 10

// BackupInfo describes a backup.
type BackupInfo struct {
	// Name is the name of the backup, as 2026-11-04T14-00-00Z-manual, and
	// Path its folder or archive.
	Name, Path string
	// Created is when the backup was started, and Took how long it took.
	Created time.Time
	Took    time.Duration
	// Reason is why it was made, as "manual" or "scheduled", and By who
	// made it.
	Reason string
	By     Actor
	Format BackupFormat
	// Checkpoint is the record of the checkpoint the world of the backup
	// holds, read when it was verified. Its ID is 0 if it was not, or if
	// the world was never checkpointed.
	Checkpoint mcdb.CheckpointRecord
	// Files and Bytes are the files of the world of the backup and their
	// size, and Linked the number of them hard-linked to the world's.
	Files  int
	Bytes  int64
	Linked int
	// Verified is how the backup was verified: "record", "full" or "off".
	Verified string
	// Admin are the files of the admin lists copied with the world.
	Admin []string
}

var (
	// ErrBackupsOff is the error of Server.Backup on a server whose
	// BackupConfig has no Folder.
	ErrBackupsOff = errors.New("backups are off: no backup folder is set")
	// ErrBackupRunning is the error of Server.Backup while another backup
	// runs.
	ErrBackupRunning = errors.New("a backup is running")
	// ErrBackupNotFound is the error of a backup asked for by a name the
	// backup folder holds none of.
	ErrBackupNotFound = errors.New("no backup of that name")
)

// errServerClosing is the cause of the cancellation of a backup by the
// close of the server.
var errServerClosing = errors.New("the server is closing")

// worldSnapshotter is a world provider that copies its world while it
// runs, as mcdb.DB does.
type worldSnapshotter interface {
	Snapshot(ctx context.Context, dst string, o mcdb.SnapshotOptions) (mcdb.SnapshotResult, error)
	// Dir returns the folder of the world.
	Dir() string
}

// backups runs the backups of a Server, one at a time.
type backups struct {
	srv *Server
	// ctx is cancelled as the server closes, which cancels the backup
	// running and refuses new ones.
	ctx    context.Context
	cancel context.CancelCauseFunc
	wg     sync.WaitGroup
	// stop ends the schedule, and done is closed once it ended.
	stop, done chan struct{}

	mu      sync.Mutex
	running *runningBackup
	// hold is the save /save hold holds, nil if none.
	hold   *saveHold
	closed bool
	last   BackupStatus
	// lastEnd is when the last backup ended, or the schedule started, by
	// the clock of the machine: the next scheduled backup is due an
	// interval after it.
	lastEnd time.Time

	// now and free, if not nil, replace the clock and diskFree, for tests.
	now  func() time.Time
	free func(path string) (uint64, bool, error)
}

// runningBackup is the backup running.
type runningBackup struct {
	started time.Time
	by      Actor
}

// BackupStatus is the state of the backups of a Server.
type BackupStatus struct {
	// Running is set while a backup runs, Started when it started and By
	// who started it.
	Running bool
	Started time.Time
	By      Actor
	// Last is the last backup made since the server started, and LastErr
	// the error of the last backup that failed after it, if any, at
	// LastErrAt.
	Last      *BackupInfo
	LastErr   error
	LastErrAt time.Time
	// Next is when the next scheduled backup is due, zero if none is.
	Next time.Time
}

// init readies the backups of srv.
func (b *backups) init(srv *Server) {
	b.srv = srv
	b.ctx, b.cancel = context.WithCancelCause(context.Background())
	b.stop, b.done = make(chan struct{}), make(chan struct{})
}

// log returns the logger of the backups.
func (b *backups) log() *slog.Logger { return logging.Sub(b.srv.conf.Log, logging.Save) }

// clock returns the time now.
func (b *backups) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// close cancels the backup running, waits for it to end, as it deletes what
// it wrote, and refuses backups from then on. It also ends the schedule,
// which Server.Listen started.
func (b *backups) close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.cancel(errServerClosing)
	b.wg.Wait()
	<-b.done
}

// run makes a backup every BackupConfig.Interval, from the end of the last
// backup, scheduled or not, until the server closes. The first is due an
// interval after the start, so that a server that crashes as it starts does
// not fill the disk. One due while saving is off is skipped. It first
// deletes the backups a crash left unfinished.
func (b *backups) run() {
	defer close(b.done)
	b.sweepAtStart()
	conf := b.srv.conf.Backup
	if conf.Interval <= 0 || conf.Folder == "" {
		<-b.ctx.Done()
		return
	}
	if _, ok := b.srv.conf.WorldProvider.(worldSnapshotter); !ok {
		b.log().Info("Backups are not scheduled: the world provider cannot copy its world while it runs.")
		<-b.ctx.Done()
		return
	}
	b.mu.Lock()
	b.lastEnd = time.Now()
	b.mu.Unlock()
	for {
		due := b.due(conf.Interval)
		t := time.NewTimer(time.Until(due))
		select {
		case <-b.ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if b.due(conf.Interval).After(time.Now()) {
			// A backup ended meanwhile.
			continue
		}
		if !b.srv.SaveEnabledState() {
			b.log().Info("Skipped a scheduled backup: saving is off. /backup makes one anyway.")
			b.srv.audit.Record(audit.Record{Actor: audit.Actor{Kind: audit.KindServer}, Action: "backup", Result: audit.ResultRefused,
				Output: []string{"Backup (autosave) refused: saving is off."}})
			b.mu.Lock()
			b.lastEnd = time.Now()
			b.mu.Unlock()
			continue
		}
		_, _ = b.srv.Backup(b.ctx, Actor{Kind: ActorServer, Name: "Server"}, "autosave")
	}
}

// due returns when the next scheduled backup is due, an interval after the
// last ended, and records it as BackupStatus.Next.
func (b *backups) due(interval time.Duration) time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.last.Next = b.lastEnd.Add(interval)
	return b.last.Next
}

// begin starts a backup by the actor passed, or fails if one is running or
// the server closes.
func (b *backups) begin(by Actor) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errServerClosing
	}
	if r := b.running; r != nil {
		return fmt.Errorf("%w (started %s by %s)", ErrBackupRunning, r.started.Local().Format(time.TimeOnly), r.by)
	}
	b.running = &runningBackup{started: b.clock(), by: by}
	b.wg.Add(1)
	return nil
}

// end ends the backup running, which made info or failed with err.
func (b *backups) end(info BackupInfo, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.running, b.lastEnd = nil, time.Now()
	if err != nil {
		b.last.LastErr, b.last.LastErrAt = err, b.clock()
	} else {
		b.last.Last, b.last.LastErr, b.last.LastErrAt = &info, nil, time.Time{}
	}
	b.wg.Done()
}

// BackupStatus returns the state of the backups of the server.
func (srv *Server) BackupStatus() BackupStatus {
	b := &srv.backups
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.last
	if r := b.running; r != nil {
		s.Running, s.Started, s.By = true, r.started, r.by
	}
	return s
}

// Backup backs up the server: it saves a checkpoint first if
// BackupConfig.SaveFirst is set, then copies the world while it runs
// (mcdb.DB.Snapshot) and the files of the admin lists into a new backup in
// BackupConfig.Folder, verifies it, publishes it under its name and
// deletes the backups the config keeps no more. by is who asked for it and
// reason why, which name it: "manual" if empty.
//
// One backup runs at a time: Backup fails with ErrBackupRunning while one
// does. It waits for no world owner but for the checkpoint first, as
// /save-all does, and must not be called on one. A backup that would leave
// less than BackupConfig.MinFreeSpace free is refused. A backup that fails,
// or is cancelled by ctx or by the close of the server, leaves nothing in
// the folder.
//
// Every call is recorded in the audit log, as action "backup", with how it
// ended: "ok", "refused" (backups off, one running, the server closing, too
// little space) or "error".
func (srv *Server) Backup(ctx context.Context, by Actor, reason string) (BackupInfo, error) {
	b := &srv.backups
	info, err := b.start(ctx, by, reason)
	b.record(by, reason, info, err)
	if err != nil {
		b.log().Error("Backup failed: "+err.Error(), "by", by.String(), "reason", backupReason(reason))
		return BackupInfo{}, err
	}
	b.log().Info("Backup done.", "name", info.Name, "bytes", info.Bytes, "files", info.Files, "linked", info.Linked, "checkpoint", info.Checkpoint.ID, "verified", info.Verified, "took", info.Took.Round(time.Millisecond))
	return info, nil
}

// refusedError is the error of a backup refused before it wrote anything.
type refusedError struct{ error }

// Unwrap returns the error refusedError wraps.
func (e refusedError) Unwrap() error { return e.error }

// start makes the backup of Server.Backup, unless it is refused, and
// returns what it made of it, the name included, even if it failed.
func (b *backups) start(ctx context.Context, by Actor, reason string) (BackupInfo, error) {
	srv := b.srv
	if srv.conf.Backup.Folder == "" {
		return BackupInfo{}, refusedError{ErrBackupsOff}
	}
	snap, ok := srv.conf.WorldProvider.(worldSnapshotter)
	if !ok {
		return BackupInfo{}, refusedError{fmt.Errorf("backup: the world provider %T cannot copy its world while it runs", srv.conf.WorldProvider)}
	}
	if err := b.begin(by); err != nil {
		return BackupInfo{}, refusedError{fmt.Errorf("backup: %w", err)}
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stop := context.AfterFunc(b.ctx, func() { cancel(context.Cause(b.ctx)) })
	defer stop()

	info, err := b.backup(ctx, snap, by, reason)
	if err != nil && ctx.Err() != nil {
		err = fmt.Errorf("backup cancelled: %w", context.Cause(ctx))
	}
	b.end(info, err)
	return info, err
}

// record writes to the audit log how the backup by the actor passed, for
// reason, ended: it made info, or failed or was refused with err.
func (b *backups) record(by Actor, reason string, info BackupInfo, err error) {
	r := audit.Record{Actor: by.auditActor(), Action: "backup", Target: info.Name, Result: audit.ResultOK}
	switch {
	case err == nil:
		r.Output = []string{fmt.Sprintf("Backup %s done (%s): %s, %d files (%d linked), checkpoint %d, verified (%s), took %.2f s.",
			info.Name, info.Reason, formatBytes(info.Bytes), info.Files, info.Linked, info.Checkpoint.ID, info.Verified, info.Took.Seconds())}
	case errors.As(err, new(refusedError)):
		// A backup refused for its space had a name, of no backup made.
		r.Target, r.Result, r.Output = "", audit.ResultRefused, []string{fmt.Sprintf("Backup (%s) refused: %v", backupReason(reason), err)}
	default:
		r.Result, r.Output = audit.ResultError, []string{fmt.Sprintf("Backup (%s) failed: %v", backupReason(reason), err)}
	}
	b.srv.audit.Record(r)
}

// backup makes the backup of Server.Backup.
func (b *backups) backup(ctx context.Context, snap worldSnapshotter, by Actor, reason string) (BackupInfo, error) {
	srv, conf := b.srv, b.srv.conf.Backup
	start := b.clock()
	info := BackupInfo{Created: start.UTC(), Reason: backupReason(reason), By: by, Format: conf.Format, Verified: verificationName(conf.Verify)}
	if err := os.MkdirAll(conf.Folder, 0755); err != nil {
		return info, fmt.Errorf("backup: %w", err)
	}
	b.sweep(partialPrefix)
	if !srv.playersInWorld {
		if _, nop := srv.conf.PlayerProvider.(player.NopProvider); !nop {
			b.log().Warn("The players are saved apart from the world: the backup holds the world and not them.")
		}
	}
	var saved uint64
	if conf.SaveFirst {
		if err := srv.checkpoints.checkpoint(ctx, "backup"); err != nil {
			if ctx.Err() != nil {
				return info, err
			}
			b.log().Warn("The checkpoint before the backup failed: the backup holds the last checkpoint saved.", "error", err)
		} else {
			saved = srv.checkpoints.lastID()
		}
	}
	info.Name = b.name(info.Created, info.Reason)
	if err := b.checkSpace(snap.Dir()); err != nil {
		return info, err
	}
	partial := filepath.Join(conf.Folder, partialPrefix+info.Name)
	err := b.write(ctx, snap, partial, &info)
	if err == nil && saved != 0 && conf.Verify != mcdb.VerifyOff && info.Checkpoint.ID < saved {
		err = fmt.Errorf("backup: the copy holds checkpoint %d, older than checkpoint %d saved before it", info.Checkpoint.ID, saved)
	}
	if err == nil {
		info.Took = b.clock().Sub(start)
		if conf.Format == BackupZip {
			err = b.publishArchive(ctx, partial, &info)
		} else {
			err = b.publish(partial, &info)
		}
	}
	if err != nil {
		_ = os.RemoveAll(partial)
		return info, err
	}
	b.retain(info.Name)
	return info, nil
}

// partialPrefix prefixes the folder a backup is written into before it is
// published under its name.
const partialPrefix = ".partial-"

// sweep deletes what a crash or a kill left of backups unpublished, and of
// archives being verified: the files and folders of the backup folder whose
// names have one of the prefixes passed.
func (b *backups) sweep(prefixes ...string) {
	entries, err := os.ReadDir(b.srv.conf.Backup.Folder)
	if err != nil {
		return
	}
	for _, e := range entries {
		if slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(e.Name(), p) }) {
			path := filepath.Join(b.srv.conf.Backup.Folder, e.Name())
			if err := os.RemoveAll(path); err != nil {
				b.log().Error("Delete an unfinished backup: "+err.Error(), "path", path)
			} else {
				b.log().Info("Deleted an unfinished backup.", "path", path)
			}
		}
	}
}

// backupReason returns reason as the name of a backup holds it: lower case
// letters, digits and dashes, "manual" if it has none.
func backupReason(reason string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(reason)) {
		if sb.Len() >= 32 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case sb.Len() > 0 && !strings.HasSuffix(sb.String(), "-"):
			sb.WriteByte('-')
		}
	}
	if s := strings.Trim(sb.String(), "-"); s != "" {
		return s
	}
	return "manual"
}

// verificationName returns the name of v in config.toml.
func verificationName(v mcdb.Verification) string {
	switch v {
	case mcdb.VerifyFull:
		return "full"
	case mcdb.VerifyOff:
		return "off"
	default:
		return "record"
	}
}

// name returns the name of a backup created at t for reason: its time in
// UTC, then reason, as 2026-11-04T14-00-00Z-manual, with a number after
// if the folder holds one of that name already.
func (b *backups) name(t time.Time, reason string) string {
	base := t.UTC().Format("2006-01-02T15-04-05Z") + "-" + reason
	name := base
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(b.srv.conf.Backup.Folder, name)); errors.Is(err, fs.ErrNotExist) {
			if _, err := os.Lstat(filepath.Join(b.srv.conf.Backup.Folder, name+zipExtension)); errors.Is(err, fs.ErrNotExist) {
				return name
			}
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

// write writes the backup of info into the folder partial: the copy of the
// world in world/, the files of the admin lists in admin/, and backup.json.
func (b *backups) write(ctx context.Context, snap worldSnapshotter, partial string, info *BackupInfo) error {
	if err := os.Mkdir(partial, 0755); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	res, err := snap.Snapshot(ctx, filepath.Join(partial, backupWorld), mcdb.SnapshotOptions{Verify: b.srv.conf.Backup.Verify})
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	for _, r := range res.Raced {
		b.log().Debug("The copy of the world raced its database and was made again.", "reason", r)
	}
	info.Checkpoint, info.Files, info.Bytes, info.Linked = res.Checkpoint, res.Files, res.Bytes, res.Linked
	if info.Admin, err = b.copyAdmin(filepath.Join(partial, backupAdmin)); err != nil {
		return fmt.Errorf("backup: copy the admin lists: %w", err)
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return nil
}

// The folders of a backup: the copy of the world, and the files of the
// admin lists.
const (
	backupWorld = "world"
	backupAdmin = "admin"
	// backupJSON describes a backup in its folder.
	backupJSON = "backup.json"
)

// adminFiles returns the paths of the files of the admin lists of the
// server: the whitelist, the operators, the bans, the known players, and
// the permissions beside them if there are any.
func (b *backups) adminFiles() []string {
	srv := b.srv
	var paths []string
	if srv.whitelist != nil && srv.whitelist.filePath != "" {
		paths = append(paths, srv.whitelist.filePath)
	}
	if srv.operators != nil {
		paths = append(paths, srv.operators.filePath)
	}
	if srv.bans != nil {
		paths = append(paths, srv.bans.file)
	}
	if srv.known != nil {
		paths = append(paths, srv.known.path)
	}
	paths = slices.DeleteFunc(paths, func(p string) bool { return p == "" })
	if len(paths) > 0 {
		paths = append(paths, filepath.Join(filepath.Dir(paths[0]), "permissions.json"))
	}
	return paths
}

// copyAdmin copies the files of the admin lists into the folder dst and
// returns their names. Each is replaced whole when written (writeFileAtomic),
// so that a copy holds it whole, as it was before or after a write. Files
// missing are left out.
func (b *backups) copyAdmin(dst string) ([]string, error) {
	if err := os.Mkdir(dst, 0755); err != nil {
		return nil, err
	}
	var names []string
	seen := map[string]bool{}
	for _, path := range b.adminFiles() {
		name := filepath.Base(path)
		if seen[name] {
			continue
		}
		seen[name] = true
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		if err := writeSynced(filepath.Join(dst, name), data); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, syncFolder(dst)
}

// writeSynced writes data to the new file path and syncs it.
func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// syncFolder syncs the folder at path, so that the files made and renamed
// in it reach the disk. Not every file system syncs a folder: an error of
// it is not one of the files.
func syncFolder(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	_ = d.Sync()
	return d.Close()
}

// backupFile is backup.json, which describes a backup.
type backupFile struct {
	Name       string            `json:"name"`
	Created    time.Time         `json:"created"`
	Reason     string            `json:"reason"`
	By         string            `json:"by"`
	ByKind     string            `json:"byKind,omitempty"`
	Adamant    string            `json:"adamant"`
	Commit     string            `json:"commit,omitempty"`
	Minecraft  string            `json:"minecraft"`
	Format     string            `json:"format"`
	World      string            `json:"world"`
	Checkpoint *backupCheckpoint `json:"checkpoint,omitempty"`
	Files      int               `json:"files"`
	Bytes      int64             `json:"bytes"`
	Linked     int               `json:"linked"`
	Verified   string            `json:"verified"`
	Took       string            `json:"took"`
	Admin      []string          `json:"admin"`
}

// backupCheckpoint is the checkpoint a backup holds, in backup.json.
type backupCheckpoint struct {
	ID   uint64    `json:"id"`
	Time time.Time `json:"time"`
}

// fileOf returns the backup.json of info.
func fileOf(info BackupInfo) backupFile {
	f := backupFile{
		Name: info.Name, Created: info.Created, Reason: info.Reason, By: info.By.String(), ByKind: string(info.By.Kind),
		Minecraft: version.Minecraft, Format: info.Format.String(), World: backupWorld,
		Files: info.Files, Bytes: info.Bytes, Linked: info.Linked, Verified: info.Verified,
		Took: fmt.Sprintf("%.2fs", info.Took.Seconds()), Admin: info.Admin,
	}
	if f.Admin == nil {
		f.Admin = []string{}
	}
	f.Adamant, f.Commit = buildVersion()
	if info.Checkpoint.ID != 0 {
		f.Checkpoint = &backupCheckpoint{ID: info.Checkpoint.ID, Time: info.Checkpoint.Time}
	}
	return f
}

// infoOf returns the BackupInfo of the backup.json f of the backup at path.
func infoOf(f backupFile, path string) BackupInfo {
	info := BackupInfo{
		Name: f.Name, Path: path, Created: f.Created, Reason: f.Reason, By: Actor{Kind: ActorKind(f.ByKind), Name: f.By},
		Files: f.Files, Bytes: f.Bytes, Linked: f.Linked, Verified: f.Verified,
	}
	if f.Format == BackupZip.String() {
		info.Format = BackupZip
	}
	info.Took, _ = time.ParseDuration(f.Took)
	if f.Checkpoint != nil {
		info.Checkpoint = mcdb.CheckpointRecord{ID: f.Checkpoint.ID, Time: f.Checkpoint.Time}
	}
	info.Admin = f.Admin
	return info
}

// buildVersion returns the version of the module of the program and its
// commit, as the build records them.
func buildVersion() (ver, commit string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown", ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			commit = s.Value
		}
	}
	return info.Main.Version, commit
}

// publish writes backup.json into the folder partial and publishes the
// backup under its name.
func (b *backups) publish(partial string, info *BackupInfo) error {
	data, err := json.MarshalIndent(fileOf(*info), "", "  ")
	if err != nil {
		return err
	}
	if err := writeSynced(filepath.Join(partial, backupJSON), append(data, '\n')); err != nil {
		return fmt.Errorf("backup: write %s: %w", backupJSON, err)
	}
	if err := syncFolder(partial); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	folder := b.srv.conf.Backup.Folder
	info.Path = filepath.Join(folder, info.Name)
	if err := os.Rename(partial, info.Path); err != nil {
		return fmt.Errorf("backup: publish: %w", err)
	}
	if err := syncFolder(folder); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}
