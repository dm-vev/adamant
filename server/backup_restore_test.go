package server

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// TestRestoreProcedure follows the restore procedure of docs/admin.md, for
// a backup of each format: Alice holds 10 diamonds as the backup is made,
// then puts them in a chest, which a checkpoint saves; the backup is
// verified in full, the server stopped, the world moved aside, the backup
// copied into its place (cp -a of the folder, the .mcworld extracted but
// its adamant-* entries), and the server started again. It opens the world
// at the checkpoint of the backup, as its log says, and Alice joins with
// her 10 diamonds, the chest gone; the world moved aside is untouched.
func TestRestoreProcedure(t *testing.T) {
	for _, format := range []BackupFormat{BackupFolder, BackupZip} {
		t.Run(format.String(), func(t *testing.T) {
			if format == BackupFolder {
				if _, err := exec.LookPath("cp"); err != nil {
					t.Skip("no cp to follow the procedure with")
				}
			}
			dir := t.TempDir()
			chest := cube.Pos{8, 100, 8}
			info := backUpBeforeChest(t, dir, format, chest)

			// 3. Keep the current world.
			worldDir := filepath.Join(dir, "world")
			kept := worldDir + ".before-restore"
			if err := os.Rename(worldDir, kept); err != nil {
				t.Fatal(err)
			}
			// 4. Copy the backup into place.
			if format == BackupFolder {
				if out, err := exec.Command("cp", "-a", filepath.Join(info.Path, "world"), worldDir).CombinedOutput(); err != nil {
					t.Fatalf("cp -a: %v\n%s", err, out)
				}
			} else {
				extractMcworld(t, info.Path, worldDir)
			}

			// 7. Start: the world opens at the checkpoint of the backup.
			var logs bytes.Buffer
			conf := backupConfigLog(t, dir, BackupConfig{}, slog.New(slog.NewTextHandler(&logs, nil)))
			if got := logs.String(); !strings.Contains(got, `msg="Opened the world at its last checkpoint."`) ||
				!strings.Contains(got, " checkpoint="+strconv.FormatUint(info.Checkpoint.ID, 10)+" ") {
				t.Errorf("the log of the start:\n%s\nwant the world opened at checkpoint %d", got, info.Checkpoint.ID)
			}
			jt := newJoinTest(t, conf.PlayerProvider, conf)
			jt.start()
			h := jt.join(t, joinLogin("Alice", ""))
			n, err := player.Call(context.Background(), h, func(_ *world.Tx, p *player.Player) (int, error) {
				return diamondsIn(p.Inventory()), nil
			})
			if err != nil || n != 10 {
				t.Errorf("Alice joins the restored world with %d diamonds (%v), want 10", n, err)
			}
			hasChest := true
			<-jt.srv.World().Exec(func(tx *world.Tx) { _, hasChest = tx.Block(chest).(block.Chest) })
			if hasChest {
				t.Error("the restored world holds the chest placed after the backup")
			}
			if err := jt.srv.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := mcdb.Config{Log: slog.New(slog.DiscardHandler)}.Open(kept)
			if err != nil {
				t.Fatal(err)
			}
			last, _ := db.LastCheckpoint()
			_ = db.Close()
			if last.ID <= info.Checkpoint.ID {
				t.Errorf("the world moved aside holds checkpoint %d, want one after the backup's %d", last.ID, info.Checkpoint.ID)
			}
			if alice, inChest := backedUp(t, kept, chest, last.ID); alice != 0 || inChest != 10 {
				t.Errorf("the world moved aside holds Alice with %d diamonds and a chest of %d, want 0 and 10", alice, inChest)
			}
		})
	}
}

// backUpBeforeChest makes the backup of TestRestoreProcedure in dir, then
// the changes after it, verifies the backup in full and stops the server,
// as steps 1 and 2 of the procedure do.
func backUpBeforeChest(t *testing.T, dir string, format BackupFormat, chest cube.Pos) BackupInfo {
	t.Helper()
	conf := backupConfig(t, dir, BackupConfig{SaveFirst: true, Format: format, Verify: mcdb.VerifyRecord})
	jt := newJoinTest(t, conf.PlayerProvider, conf)
	jt.start()
	h := jt.join(t, joinLogin("Alice", ""))
	do := func(f func(tx *world.Tx, p *player.Player)) {
		if err := player.Do(h, f).Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	do(func(_ *world.Tx, p *player.Player) { _ = p.Inventory().SetItem(0, item.NewStack(item.Diamond{}, 10)) })
	info, err := jt.srv.Backup(context.Background(), Actor{Kind: ActorConsole, Name: "Console"}, "")
	if err != nil {
		t.Fatal(err)
	}
	do(func(tx *world.Tx, p *player.Player) {
		tx.SetBlock(chest, block.NewChest(), nil)
		_ = p.Inventory().SetItem(0, item.Stack{})
		_, _ = tx.Block(chest).(block.Chest).Inventory(tx, chest).AddItem(item.NewStack(item.Diamond{}, 10))
	})
	if err := jt.srv.SaveAll(); err != nil {
		t.Fatal(err)
	}
	// 1. Pick the backup, and verify it in full.
	if _, err := jt.srv.VerifyBackup(info.Name, true); err != nil {
		t.Fatal(err)
	}
	// 2. Stop the server.
	if err := jt.srv.Close(); err != nil {
		t.Fatal(err)
	}
	return info
}

// backupConfigLog is backupConfig, logging the opening of the world to log.
func backupConfigLog(t *testing.T, dir string, backup BackupConfig, log *slog.Logger) Config {
	t.Helper()
	uc := DefaultConfig()
	uc.World.Folder, uc.Players.Folder = filepath.Join(dir, "world"), filepath.Join(dir, "players")
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	uc.Server.IdentityKey = "name"
	conf, err := uc.Config(log)
	if err != nil {
		t.Fatal(err)
	}
	backup.Folder = filepath.Join(dir, "backups")
	conf.Backup = backup
	return conf
}

// extractMcworld extracts the .mcworld at path into the folder dst, which
// it makes, leaving out its adamant-* entries, as unzip -x 'adamant-*'
// does.
func extractMcworld(t *testing.T, path, dst string) {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "adamant-") || strings.HasSuffix(f.Name, "/") {
			continue
		}
		out := filepath.Join(dst, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			t.Fatal(err)
		}
		in, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(in)
		_ = in.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
