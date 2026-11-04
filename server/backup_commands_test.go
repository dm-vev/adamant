package server_test

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/cmd/builtin"
	"github.com/go-gl/mathgl/mgl64"
)

// laterSource is a source of level 4 that keeps every output it is sent,
// the output commands send later too, which it waits for as a
// cmd.Deferrer.
type laterSource struct {
	mu   sync.Mutex
	out  []string
	wait sync.WaitGroup
}

func (*laterSource) Position() mgl64.Vec3       { return mgl64.Vec3{} }
func (*laterSource) PermissionLevel() cmd.Level { return cmd.LevelOwner }
func (s *laterSource) Defer() func()            { s.wait.Add(1); return s.wait.Done }
func (s *laterSource) SendCommandOutput(o *cmd.Output) {
	if o.MessageCount()+o.ErrorCount() == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.out = append(s.out, strings.Split(outputLines(o), "\n")...)
}

// run runs line and returns its output, that sent later included.
func (s *laterSource) run(t *testing.T, line string) string {
	t.Helper()
	s.mu.Lock()
	s.out = nil
	s.mu.Unlock()
	cmd.ExecuteLine(s, line, nil, nil)
	done := make(chan struct{})
	go func() { s.wait.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatalf("%s: the output sent later did not come", line)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.out, "\n")
}

// TestBackupCommands checks /backup, /backup list, status and verify, and
// BDS's /save hold, query and resume, as the console runs them: their
// outputs, the output of the backup and of its verification sent once
// they are done, the files of a save held listed relative to the hold
// folder, which the line before "Data saved" names, with their sizes.
func TestBackupCommands(t *testing.T) {
	dir := t.TempDir()
	uc := server.DefaultConfig()
	uc.World.Folder, uc.Players.Folder = filepath.Join(dir, "world"), filepath.Join(dir, "players")
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	uc.Backup.Folder, uc.Backup.Interval = filepath.Join(dir, "backups"), "0s"
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	conf.Log = slog.New(slog.DiscardHandler)
	srv := conf.New()
	builtin.Register(srv)
	t.Cleanup(func() { cmd.SetPermissions(nil); cmd.SetObserver(nil); cmd.SetAdminFeedback(nil) })
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })
	src := &laterSource{}

	if got := src.run(t, "/backup list"); got != "There are no backups" {
		t.Errorf("/backup list of none: %q", got)
	}
	got := src.run(t, "/backup")
	m := regexp.MustCompile(`^Backing up\.\.\.\nBackup (\S+-manual) done: [0-9.]+ [KMG]iB, \d+ files( \(\d+ linked\))?, [0-9.]+ s, checkpoint \d+, verified \(record\)\.$`).FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("/backup: %q", got)
	}
	name := m[1]
	if got := src.run(t, "/backup list"); !strings.HasPrefix(got, "There are 1 backup(s):\n"+name+": ") || !strings.Contains(got, " by API, folder, ") {
		t.Errorf("/backup list: %q", got)
	}
	if got := src.run(t, "/backup status"); !strings.HasPrefix(got, "No backup is running.\nThe last backup is "+name+": ") ||
		!strings.HasSuffix(got, "\nNo backup is scheduled.") {
		t.Errorf("/backup status: %q", got)
	}
	if got := src.run(t, "/backup verify "+name+" full"); !regexp.MustCompile(`^Verifying \S+ \(full\)\.\.\.\nBackup \S+ is whole \(full\): it holds checkpoint \d+\.$`).MatchString(got) {
		t.Errorf("/backup verify full: %q", got)
	}
	if got := src.run(t, "/backup verify nothing"); !strings.Contains(got, "error: no backup of that name") {
		t.Errorf("/backup verify of no backup: %q", got)
	}

	if got := src.run(t, "/save query"); got != "error: No save is held: /save hold prepares one." {
		t.Errorf("/save query of none: %q", got)
	}
	if got := src.run(t, "/save hold"); got != "Saving..." {
		t.Errorf("/save hold: %q", got)
	}
	if got := src.run(t, "/save hold"); got != "error: "+server.ErrSaveHeld.Error() {
		t.Errorf("a second /save hold: %q", got)
	}
	folder := filepath.Join(dir, "backups", ".hold")
	deadline := time.Now().Add(30 * time.Second)
	for {
		got := src.run(t, "/save query")
		if got != "A previous save has not been completed." {
			lines := strings.Split(got, "\n")
			if len(lines) != 3 || lines[0] != "The files to copy are in "+folder+", a copy of the world that does not change." ||
				lines[1] != "Data saved. Files are now ready to be copied." {
				t.Fatalf("/save query: %q", got)
			}
			for _, f := range strings.Split(lines[2], ", ") {
				path, size, _ := strings.Cut(f, ":")
				n, _ := strconv.ParseInt(size, 10, 64)
				if fi, err := os.Stat(filepath.Join(folder, filepath.FromSlash(path))); err != nil || fi.Size() != n {
					t.Errorf("listed %s: %v, %v", f, fi, err)
				}
			}
			if !strings.Contains(lines[2], "world/level.dat:") || !strings.Contains(lines[2], "world/db/CURRENT:") {
				t.Errorf("/save query lists %q", lines[2])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the save held was not ready in 30 seconds")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := src.run(t, "/save resume"); got != "Changes to the world are resumed." {
		t.Errorf("/save resume: %q", got)
	}
	if _, err := os.Stat(folder); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the hold folder after /save resume: %v", err)
	}
}
