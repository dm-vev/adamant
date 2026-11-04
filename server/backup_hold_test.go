package server

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// TestSaveHold checks HoldSave, QuerySave and ResumeSave, BDS's save hold,
// query and resume: the copy of the world is prepared in backups/.hold,
// in a folder of the name of the world's; once ready its files are
// listed with their sizes, relative to the hold folder, and the copy
// verifies; it is no backup Backups lists; a second hold fails while one
// is held; resume deletes it. A hold whose copy is being prepared is held
// and not ready, and resume stops it.
func TestSaveHold(t *testing.T) {
	dir := t.TempDir()
	conf := backupConfig(t, dir, BackupConfig{SaveFirst: true, Verify: mcdb.VerifyRecord})
	jt := newJoinTest(t, conf.PlayerProvider, conf)
	jt.start()
	defer jt.srv.Close()
	srv, console := jt.srv, Actor{Kind: ActorConsole, Name: "Console"}
	if h := srv.QuerySave(); h.Held {
		t.Fatalf("a save is held before /save hold: %+v", h)
	}
	if err := srv.HoldSave(console); err != nil {
		t.Fatal(err)
	}
	if err := srv.HoldSave(console); !errors.Is(err, ErrSaveHeld) {
		t.Errorf("a second hold: %v, want ErrSaveHeld", err)
	}
	h := waitHold(t, srv)
	folder := filepath.Join(dir, "backups", holdFolder)
	if h.Err != nil || h.Folder != folder || h.Checkpoint.ID == 0 {
		t.Fatalf("held save: %+v, want ready in %s at a checkpoint", h, folder)
	}
	var paths []string
	for _, f := range h.Files {
		paths = append(paths, f.Path)
		fi, err := os.Stat(filepath.Join(folder, filepath.FromSlash(f.Path)))
		if err != nil || fi.Size() != f.Size {
			t.Errorf("%s of %d bytes: %v, %v", f.Path, f.Size, fi, err)
		}
	}
	for _, want := range []string{"world/level.dat", "world/db/CURRENT"} {
		if !slices.Contains(paths, want) {
			t.Errorf("the files of the save, %q, lack %s", paths, want)
		}
	}
	if !slices.IsSorted(paths) {
		t.Errorf("the files of the save are not sorted: %q", paths)
	}
	if rec, err := mcdb.VerifyBackup(filepath.Join(folder, "world"), true); err != nil || rec.ID != h.Checkpoint.ID {
		t.Errorf("the copy of the save verifies at checkpoint %d, %v, want %d", rec.ID, err, h.Checkpoint.ID)
	}
	if list, err := srv.Backups(); err != nil || len(list) != 0 {
		t.Errorf("Backups lists %v, %v: the save held is no backup", list, err)
	}
	if err := srv.ResumeSave(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(folder); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the hold folder after resume: %v", err)
	}
	if h := srv.QuerySave(); h.Held {
		t.Errorf("a save is held after resume: %+v", h)
	}
	if err := srv.ResumeSave(); err != nil {
		t.Errorf("resume with no save held: %v", err)
	}
}

// TestSaveHoldStoppedByResume checks a save whose copy is being prepared:
// it is held and not ready, and resume stops it and deletes its folder.
func TestSaveHoldStoppedByResume(t *testing.T) {
	dir := t.TempDir()
	conf := backupConfig(t, dir, BackupConfig{})
	snaps := blockingSnapshots{DB: conf.WorldProvider.(*mcdb.DB), started: make(chan struct{}), once: new(sync.Once)}
	conf.WorldProvider = snaps
	jt := newJoinTest(t, conf.PlayerProvider, conf)
	jt.start()
	defer jt.srv.Close()
	if err := jt.srv.HoldSave(Actor{Kind: ActorConsole}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-snaps.started:
	case <-time.After(30 * time.Second):
		t.Fatal("the copy of the save did not start")
	}
	if h := jt.srv.QuerySave(); !h.Held || h.Ready || h.Err != nil {
		t.Errorf("a save being prepared: %+v, want held and not ready", h)
	}
	if err := jt.srv.ResumeSave(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups", holdFolder)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the hold folder after resume: %v", err)
	}
}

// waitHold waits for the save held by srv to be ready or to fail.
func waitHold(t *testing.T, srv *Server) SaveHold {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		h := srv.QuerySave()
		if !h.Held {
			t.Fatal("no save is held")
		}
		if h.Ready || h.Err != nil {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatal("the save held was not ready in 30 seconds")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
