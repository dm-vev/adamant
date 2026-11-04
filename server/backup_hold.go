package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// holdFolder is the folder of the backup folder that holds the copy of the
// world /save hold prepares. Its name starts with a dot: it is no backup,
// which Backups lists, nor one the retention deletes.
const holdFolder = ".hold"

// ErrSaveHeld is the error of Server.HoldSave while a save is held: /save
// resume ends it.
var ErrSaveHeld = errors.New("a save is held already: /save query lists its files, /save resume ends it")

// HoldFile is a file of a save held by Server.HoldSave: its path, relative
// to SaveHold.Folder, with slashes, and its length.
type HoldFile struct {
	Path string
	Size int64
}

// SaveHold is the state of the save Server.HoldSave holds.
type SaveHold struct {
	// Held reports whether a save is held, Ready whether its files are
	// ready to be copied, and Err why it failed, if it did.
	Held, Ready bool
	Err         error
	// Folder is the folder that holds the files of the save: the copy of
	// the world, in a folder of the name of the world's.
	Folder string
	// Files are the files of the save, sorted by path.
	Files []HoldFile
	// Checkpoint is the record of the checkpoint the copy holds.
	Checkpoint mcdb.CheckpointRecord
}

// saveHold is a save held by HoldSave: done is closed once its files are
// ready or it failed, and state is then what QuerySave tells.
type saveHold struct {
	cancel context.CancelFunc
	done   chan struct{}
	state  SaveHold
}

// HoldSave prepares a copy of the world that its files may be copied from,
// as BDS's save hold does: a checkpoint first, if BackupConfig.SaveFirst
// is set, then a copy of the world made while it runs (mcdb.DB.Snapshot),
// verified as BackupConfig.Verify says, in the folder .hold of
// BackupConfig.Folder. It returns at once; QuerySave tells when the files
// are ready, and ResumeSave deletes them.
//
// Unlike BDS's, the files are not those of the world folder, which keeps
// changing, but those of the copy, which does not: the lengths QuerySave
// tells are their sizes. HoldSave fails with ErrSaveHeld while a save is
// held.
func (srv *Server) HoldSave(by Actor) error {
	b := &srv.backups
	conf := srv.conf.Backup
	if conf.Folder == "" {
		return ErrBackupsOff
	}
	snap, ok := srv.conf.WorldProvider.(worldSnapshotter)
	if !ok {
		return fmt.Errorf("save hold: the world provider %T cannot copy its world while it runs", srv.conf.WorldProvider)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errServerClosing
	}
	if b.hold != nil {
		return ErrSaveHeld
	}
	ctx, cancel := context.WithCancel(b.ctx)
	h := &saveHold{cancel: cancel, done: make(chan struct{})}
	b.hold = h
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		state := b.prepareHold(ctx, snap, by)
		b.mu.Lock()
		h.state = state
		b.mu.Unlock()
		close(h.done)
	}()
	return nil
}

// prepareHold makes the copy of HoldSave, and returns the state of the
// save once its files are ready or it failed, in which case it deleted
// what it wrote.
func (b *backups) prepareHold(ctx context.Context, snap worldSnapshotter, by Actor) SaveHold {
	conf := b.srv.conf.Backup
	folder := filepath.Join(conf.Folder, holdFolder)
	state := SaveHold{Held: true, Folder: folder}
	fail := func(err error) SaveHold {
		_ = os.RemoveAll(folder)
		state.Err = err
		if ctx.Err() != nil {
			b.log().Info("Save hold stopped before its files were ready.", "by", by.String())
		} else {
			b.log().Error("Save hold failed: "+err.Error(), "by", by.String())
		}
		return state
	}
	if err := os.RemoveAll(folder); err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(folder, 0755); err != nil {
		return fail(err)
	}
	if conf.SaveFirst {
		if err := b.srv.checkpoints.checkpoint(ctx, "save hold"); err != nil {
			if ctx.Err() != nil {
				return fail(err)
			}
			b.log().Warn("The checkpoint before the save hold failed: the copy holds the last checkpoint saved.", "error", err)
		}
	}
	if err := b.checkSpace(snap.Dir()); err != nil {
		return fail(err)
	}
	world := filepath.Join(folder, filepath.Base(snap.Dir()))
	res, err := snap.Snapshot(ctx, world, mcdb.SnapshotOptions{Verify: conf.Verify})
	if err != nil {
		return fail(err)
	}
	files, err := holdFiles(folder)
	if err != nil {
		return fail(err)
	}
	state.Ready, state.Files, state.Checkpoint = true, files, res.Checkpoint
	b.log().Info("Save held: its files are ready to be copied.", "folder", folder, "files", len(files), "checkpoint", res.Checkpoint.ID, "by", by.String())
	return state
}

// holdFiles returns the files under folder, relative to it, sorted.
func holdFiles(folder string) ([]HoldFile, error) {
	var files []HoldFile
	err := filepath.WalkDir(folder, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(folder, path)
		if err != nil {
			return err
		}
		files = append(files, HoldFile{Path: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	slices.SortFunc(files, func(a, b HoldFile) int { return strings.Compare(a.Path, b.Path) })
	return files, err
}

// QuerySave returns the state of the save HoldSave holds, as BDS's save
// query tells it: none held, held and being prepared, held and failed, or
// held with its files ready to be copied.
func (srv *Server) QuerySave() SaveHold {
	b := &srv.backups
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.hold
	if h == nil {
		return SaveHold{}
	}
	select {
	case <-h.done:
		return h.state
	default:
		return SaveHold{Held: true, Folder: filepath.Join(srv.conf.Backup.Folder, holdFolder)}
	}
}

// ResumeSave ends the save HoldSave holds, as BDS's save resume does: it
// stops a copy being prepared, waits for it to stop, and deletes the files
// of the save. A server that holds none deletes nothing. It may wait for
// the checkpoint of a copy being prepared to stop, and must not be called
// on a world owner.
func (srv *Server) ResumeSave() error {
	b := &srv.backups
	b.mu.Lock()
	h := b.hold
	b.hold = nil
	b.mu.Unlock()
	if h == nil {
		return nil
	}
	h.cancel()
	<-h.done
	if err := os.RemoveAll(filepath.Join(srv.conf.Backup.Folder, holdFolder)); err != nil {
		return fmt.Errorf("save resume: %w", err)
	}
	return nil
}
