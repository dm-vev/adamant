package server

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// Backups returns the backups in the backup folder, newest first: the
// folders and archives that hold a backup.json the server can read. Other
// files in the folder are none of its backups, and are left out.
func (srv *Server) Backups() ([]BackupInfo, error) {
	if srv.conf.Backup.Folder == "" {
		return nil, ErrBackupsOff
	}
	return srv.backups.list()
}

// list returns the backups in the backup folder, newest first.
func (b *backups) list() ([]BackupInfo, error) {
	folder := b.srv.conf.Backup.Folder
	entries, err := os.ReadDir(folder)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var list []BackupInfo
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			// Partial backups, the hold of /save hold and hidden files.
			continue
		}
		path := filepath.Join(folder, e.Name())
		var f backupFile
		switch {
		case e.IsDir():
			data, err := os.ReadFile(filepath.Join(path, backupJSON))
			if err != nil || json.Unmarshal(data, &f) != nil {
				continue
			}
		case strings.HasSuffix(e.Name(), zipExtension):
			if f, err = readArchiveFile(path); err != nil {
				continue
			}
		default:
			continue
		}
		if f.Name == "" || f.Created.IsZero() {
			continue
		}
		list = append(list, infoOf(f, path))
	}
	slices.SortFunc(list, func(a, b BackupInfo) int {
		return cmp.Or(b.Created.Compare(a.Created), strings.Compare(b.Name, a.Name))
	})
	return list, nil
}

// keep returns how many backups the config keeps.
func (b *backups) keep() int {
	if k := b.srv.conf.Backup.Keep; k > 0 {
		return k
	}
	return DefaultBackupKeep
}

// retain deletes the backups the config keeps no more after the backup
// newest was made: those after the Keep newest and those older than MaxAge,
// but never the newest. Backups the server did not write are never
// deleted: list leaves them out.
func (b *backups) retain(newest string) {
	list, err := b.list()
	if err != nil {
		b.log().Error("List the backups to delete the old ones: " + err.Error())
		return
	}
	maxAge, now := b.srv.conf.Backup.MaxAge, b.clock()
	for i, info := range list {
		if i == 0 || info.Name == newest {
			continue
		}
		switch {
		case i >= b.keep():
			b.delete(info, fmt.Sprintf("more than Keep = %d backups", b.keep()))
		case maxAge > 0 && now.Sub(info.Created) > maxAge:
			b.delete(info, fmt.Sprintf("older than MaxAge = %v", maxAge))
		}
	}
}

// delete deletes the backup of info, for the reason passed. A backup in the
// folder format frees only the tables no other backup and not the world
// links.
func (b *backups) delete(info BackupInfo, why string) {
	if err := os.RemoveAll(info.Path); err != nil {
		b.log().Error("Delete a backup: "+err.Error(), "name", info.Name)
		return
	}
	b.log().Info("Deleted a backup.", "name", info.Name, "why", why)
}

// checkSpace refuses the backup about to be made if it would leave less
// than BackupConfig.MinFreeSpace free on the disk of the backup folder. If
// so, and the folder holds as many backups as the config keeps, the oldest
// that the backup would have deleted are deleted first, and the space
// checked again: a full disk does not stop the schedule while old backups
// remain.
func (b *backups) checkSpace(dir string) error {
	conf := b.srv.conf.Backup
	need, err := b.estimate(dir)
	if err != nil {
		b.log().Warn("Could not estimate the size of the backup: its space is not checked.", "error", err)
		return nil
	}
	free, ok, err := b.diskFree(conf.Folder)
	if err != nil || !ok {
		if err != nil {
			b.log().Warn("Could not tell the space free for the backup: it is not checked.", "error", err)
		}
		return nil
	}
	if fits(free, need, conf.MinFreeSpace) {
		return nil
	}
	if list, err := b.list(); err == nil && len(list) >= b.keep() {
		for _, info := range list[max(1, b.keep()-1):] {
			b.delete(info, "the disk is short of space for a new backup, which would delete it")
		}
		if free, ok, err = b.diskFree(conf.Folder); err == nil && ok && fits(free, need, conf.MinFreeSpace) {
			return nil
		}
	}
	return fmt.Errorf("backup refused: %s free on %s, and the backup needs about %s, under MinFreeSpace %s", formatBytes(int64(min(free, 1<<62))), conf.Folder, formatBytes(need), formatBytes(conf.MinFreeSpace))
}

// fits reports whether need bytes leave at least minFree of free.
func fits(free uint64, need, minFree int64) bool {
	return int64(min(free, 1<<62))-need >= minFree
}

// diskFree returns the bytes free on the disk of the folder at path.
func (b *backups) diskFree(path string) (uint64, bool, error) {
	if b.free != nil {
		return b.free(path)
	}
	return diskFree(path)
}

// estimate returns about how many bytes the backup of the world in the
// folder dir takes on the disk of the backup folder: the files of the world
// but its tables if they are hard-linked, as on the same file system, or
// all of them; and, for an archive, the world again.
func (b *backups) estimate(dir string) (int64, error) {
	var tables, rest int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if ext := filepath.Ext(path); filepath.Base(filepath.Dir(path)) == "db" && (ext == ".ldb" || ext == ".sst") {
			tables += fi.Size()
		} else {
			rest += fi.Size()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	need := rest
	if !sameDevice(dir, b.srv.conf.Backup.Folder) {
		need += tables
	}
	if b.srv.conf.Backup.Format == BackupZip {
		// The archive, written while the copy it is made of exists.
		need += tables + rest
	}
	return need, nil
}

// formatBytes formats n bytes as "512 B", "12.5 MiB" or "1.0 GiB".
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, units := float64(n)/unit, "KMGTPE"
	i := 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %ciB", v, units[i])
}

// VerifyBackup verifies the backup of the name passed, as Server.Backup
// verifies each backup, in full if full is set (mcdb.VerifyBackup), and
// checks that the world it holds is at the checkpoint its backup.json
// records. It reads the backup alone, while the server runs, and may take
// long: in full, about as long as reading the whole world.
func (srv *Server) VerifyBackup(name string, full bool) (BackupInfo, error) {
	list, err := srv.Backups()
	if err != nil {
		return BackupInfo{}, err
	}
	i := slices.IndexFunc(list, func(info BackupInfo) bool { return info.Name == name })
	if i < 0 {
		return BackupInfo{}, fmt.Errorf("%w: %q", ErrBackupNotFound, name)
	}
	info := list[i]
	var rec mcdb.CheckpointRecord
	if info.Format == BackupZip {
		rec, err = srv.backups.verifyArchive(info.Path, full)
	} else {
		rec, err = mcdb.VerifyBackup(filepath.Join(info.Path, backupWorld), full)
	}
	if err != nil {
		return info, fmt.Errorf("verify backup %s: %w", name, err)
	}
	if info.Checkpoint.ID != 0 && rec.ID != info.Checkpoint.ID {
		return info, fmt.Errorf("verify backup %s: its world holds checkpoint %d, its %s says %d", name, rec.ID, backupJSON, info.Checkpoint.ID)
	}
	return info, nil
}

// sweepAtStart deletes the backups a crash left unpublished, unless a
// backup runs, which sweeps itself.
func (b *backups) sweepAtStart() {
	if b.srv.conf.Backup.Folder == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running == nil && !b.closed {
		b.sweep(partialPrefix, verifyPrefix)
	}
}
