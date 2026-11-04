package server

import (
	"archive/zip"
	"compress/flate"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// zipExtension is the extension of a backup written as an archive, which
// the game imports as a world.
const zipExtension = ".mcworld"

// The entries of a backup archive beside the files of the world, which it
// holds at its root as a .mcworld does: the backup.json of the backup, and
// the files of the admin lists.
const (
	archiveJSON  = "adamant-backup.json"
	archiveAdmin = "adamant-admin/"
)

// verifyPrefix prefixes the folders a backup archive is unpacked into to be
// verified.
const verifyPrefix = ".verify-"

// publishArchive writes the backup made in the folder partial as an archive
// and publishes it under its name with zipExtension: the world at its
// root, the tables stored, as they are compressed already, and the other
// files deflated; the admin lists under archiveAdmin, and backup.json as
// archiveJSON, last. The archive is checked as BackupConfig.Verify says,
// then renamed into place, and partial deleted.
func (b *backups) publishArchive(ctx context.Context, partial string, info *BackupInfo) (err error) {
	folder := b.srv.conf.Backup.Folder
	tmp := filepath.Join(folder, partialPrefix+info.Name+zipExtension)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	zw := zip.NewWriter(f)
	zw.RegisterCompressor(zip.Deflate, newFlateWriter)
	entries, err := zipTree(ctx, zw, filepath.Join(partial, backupWorld), "")
	if err != nil {
		return fmt.Errorf("backup: write the archive: %w", err)
	}
	admin, err := zipTree(ctx, zw, filepath.Join(partial, backupAdmin), archiveAdmin)
	if err != nil {
		return fmt.Errorf("backup: write the archive: %w", err)
	}
	info.Took = b.clock().Sub(info.Created)
	if err := zipJSON(zw, fileOf(*info)); err != nil {
		return fmt.Errorf("backup: write the archive: %w", err)
	}
	if err = zw.Close(); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("backup: write the archive: %w", err)
	}
	if b.srv.conf.Backup.Verify != mcdb.VerifyOff {
		if err := checkArchive(tmp, entries+admin+1, b.srv.conf.Backup.Verify == mcdb.VerifyFull); err != nil {
			return fmt.Errorf("backup: check the archive: %w", err)
		}
	}
	info.Path = filepath.Join(folder, info.Name+zipExtension)
	if err := os.Rename(tmp, info.Path); err != nil {
		return fmt.Errorf("backup: publish: %w", err)
	}
	if err := syncFolder(folder); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := os.RemoveAll(partial); err != nil {
		b.log().Error("Delete the copy an archive was made of: "+err.Error(), "path", partial)
	}
	return nil
}

// flateWriters pools the compressors of archives, as archive/zip pools
// those of its own: each holds about a MiB.
var flateWriters sync.Pool

// pooledFlate is a compressor of flateWriters, put back once closed.
type pooledFlate struct{ *flate.Writer }

func (w *pooledFlate) Close() error {
	err := w.Writer.Close()
	flateWriters.Put(w)
	return err
}

// newFlateWriter returns a compressor of an entry of an archive, deflating
// at flate.BestSpeed: the files of a world but its tables are small, and
// the tables, compressed already, are stored.
func newFlateWriter(w io.Writer) (io.WriteCloser, error) {
	if fw, ok := flateWriters.Get().(*pooledFlate); ok {
		fw.Reset(w)
		return fw, nil
	}
	fw, err := flate.NewWriter(w, flate.BestSpeed)
	if err != nil {
		return nil, err
	}
	return &pooledFlate{fw}, nil
}

// zipTree writes the files of the folder dir into zw, each under prefix and
// its path in dir, and returns how many it wrote. A missing folder writes
// none.
func zipTree(ctx context.Context, zw *zip.Writer, dir, prefix string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && p == dir {
			return filepath.SkipDir
		} else if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		n++
		return zipFile(zw, p, prefix+filepath.ToSlash(rel), d)
	})
	return n, err
}

// zipFile writes the file at p into zw as name: stored if it is a table of
// the database, which is compressed already, deflated otherwise.
func zipFile(zw *zip.Writer, p, name string, d fs.DirEntry) error {
	fi, err := d.Info()
	if err != nil {
		return err
	}
	h, err := zip.FileInfoHeader(fi)
	if err != nil {
		return err
	}
	h.Name, h.Method = name, zip.Deflate
	if ext := path.Ext(name); ext == ".ldb" || ext == ".sst" {
		h.Method = zip.Store
	}
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	in, err := os.Open(p)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(w, in)
	return err
}

// zipJSON writes the backup.json f into zw as archiveJSON.
func zipJSON(zw *zip.Writer, f backupFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	w, err := zw.CreateHeader(&zip.FileHeader{Name: archiveJSON, Method: zip.Deflate, Modified: f.Created})
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// checkArchive checks the archive at path: that it holds n entries, and,
// if full, that each reads whole with its checksum.
func checkArchive(path string, n int, full bool) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	if len(r.File) != n {
		return fmt.Errorf("%d entries, want %d", len(r.File), n)
	}
	if !full {
		return nil
	}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(io.Discard, rc)
		if cerr := rc.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	return nil
}

// readArchiveFile reads the backup.json of the backup archive at path.
func readArchiveFile(path string) (backupFile, error) {
	var f backupFile
	r, err := zip.OpenReader(path)
	if err != nil {
		return f, err
	}
	defer r.Close()
	rc, err := r.Open(archiveJSON)
	if err != nil {
		return f, err
	}
	defer rc.Close()
	err = json.NewDecoder(rc).Decode(&f)
	return f, err
}

// verifyArchive verifies the world of the backup archive at path, unpacked
// into a folder of the backup folder made for it, and deleted after.
func (b *backups) verifyArchive(path string, full bool) (mcdb.CheckpointRecord, error) {
	dir, err := os.MkdirTemp(b.srv.conf.Backup.Folder, verifyPrefix+"*")
	if err != nil {
		return mcdb.CheckpointRecord{}, err
	}
	defer os.RemoveAll(dir)
	if err := unzipWorld(path, dir); err != nil {
		return mcdb.CheckpointRecord{}, fmt.Errorf("unpack: %w", err)
	}
	return mcdb.VerifyBackup(dir, full)
}

// unzipWorld unpacks the files of the world of the archive at path into the
// folder dir: all but backup.json and the admin lists.
func unzipWorld(path, dir string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == archiveJSON || strings.HasPrefix(f.Name, archiveAdmin) || strings.HasSuffix(f.Name, "/") {
			continue
		}
		if !fs.ValidPath(f.Name) {
			return fmt.Errorf("the entry %q lies outside the world", f.Name)
		}
		to := filepath.Join(dir, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
			return err
		}
		if err := unzipFile(f, to); err != nil {
			return err
		}
	}
	return nil
}

// unzipFile writes the entry f to the new file to.
func unzipFile(f *zip.File, to string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
