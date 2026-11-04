package server

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestAdminListsWriteAtomically checks that the lists replace their files
// whole: no temporary file is left beside them, a file keeps the
// permissions an admin gave it, and a write that fails leaves the file as
// it was.
func TestAdminListsWriteAtomically(t *testing.T) {
	dir := t.TempDir()
	srv := adminConfig(t, filepath.Join(dir, "white-list.txt"), true)
	ops := filepath.Join(dir, "ops.json")
	if err := os.Chmod(ops, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Op("Steve"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.WhitelistAdd("Steve"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.BanPlayer("Griefer", ""); err != nil {
		t.Fatal(err)
	}
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, n := range names {
		files = append(files, n.Name())
	}
	if want := []string{"banned-players.json", "ops.json", "white-list.txt"}; !slices.Equal(files, want) {
		t.Errorf("the directory holds %q, want %q", files, want)
	}
	if fi, err := os.Stat(ops); err != nil || fi.Mode().Perm() != 0600 {
		t.Errorf("ops.json: %v, %v; want it kept 0600", fi.Mode(), err)
	}

	if os.Getuid() == 0 {
		t.Skip("root writes in a directory it may not write in")
	}
	before, _ := os.ReadFile(ops)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if _, err := srv.Op("Alex"); err == nil || !strings.HasPrefix(err.Error(), "write ops: ") {
		t.Errorf("Op in a directory that cannot be written: %v", err)
	}
	if after, _ := os.ReadFile(ops); string(after) != string(before) {
		t.Errorf("ops.json after a failed write:\n%s\nwant:\n%s", after, before)
	}
	if _, err := srv.WhitelistAdd("Alex"); err == nil {
		t.Error("WhitelistAdd in a directory that cannot be written did not fail")
	}
	if entries, _ := srv.WhitelistEntries(); !slices.Equal(entries, []string{"steve"}) {
		t.Errorf("the whitelist after a failed write holds %q, want it as it was", entries)
	}
}
