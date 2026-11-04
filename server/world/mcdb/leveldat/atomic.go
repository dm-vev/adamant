package leveldat

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// fileSystem holds the file operations WriteFile does, so that tests can make
// any one of them fail.
type fileSystem struct {
	create  func(name string) (file, error)
	rename  func(oldpath, newpath string) error
	remove  func(name string) error
	syncDir func(dir string) error
}

// file is a file opened for writing by a fileSystem.
type file interface {
	io.Writer
	Sync() error
	Close() error
}

// osFS is the fileSystem of the operating system.
var osFS = fileSystem{
	create: func(name string) (file, error) {
		return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	},
	rename:  os.Rename,
	remove:  os.Remove,
	syncDir: syncDir,
}

// syncDir syncs the directory at dir, so that the renames of files in it
// reach the disk.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		// A directory cannot be opened for syncing on Windows, where NTFS
		// journals renames itself.
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}

// writeFileAtomic replaces the file at name with data, keeping the file it
// replaces as name+"_old". This is the order Bedrock Dedicated Server saves
// its level.dat in: data is written to name+"_new", the file at name is
// renamed to name+"_old" and name+"_new" to name. Unlike BDS, the new file is
// synced before it is renamed, and the directory after. If writeFileAtomic
// fails before the last rename, the file at name is left as it was; if it
// fails syncing the directory, the file at name holds data, but the renames
// may not have reached the disk.
func (fsys fileSystem) writeFileAtomic(name string, data []byte) error {
	tmp, old := name+"_new", name+"_old"
	f, err := fsys.create(tmp)
	if err != nil {
		return fmt.Errorf("level.dat: create %v: %w", filepath.Base(tmp), err)
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = fsys.remove(tmp)
		return fmt.Errorf("level.dat: write %v: %w", filepath.Base(tmp), err)
	}

	kept := true
	if err := fsys.rename(name, old); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			_ = fsys.remove(tmp)
			return fmt.Errorf("level.dat: keep previous file as %v: %w", filepath.Base(old), err)
		}
		// No file to keep: the world is new, or its level.dat was lost and
		// it was read from name+"_old", which stays as it is.
		kept = false
	}
	if err := fsys.rename(tmp, name); err != nil {
		err = fmt.Errorf("level.dat: rename %v to %v: %w", filepath.Base(tmp), filepath.Base(name), err)
		if kept {
			if rerr := fsys.rename(old, name); rerr != nil {
				// name is missing now, but name+"_old" holds what it held.
				return fmt.Errorf("%w; restore previous file: %w", err, rerr)
			}
		}
		_ = fsys.remove(tmp)
		return err
	}
	if err := fsys.syncDir(filepath.Dir(name)); err != nil {
		return fmt.Errorf("level.dat: sync directory: %w", err)
	}
	return nil
}
