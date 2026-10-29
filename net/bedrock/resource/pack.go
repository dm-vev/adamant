// Package resource reads the resource and behaviour packs a server sends
// its clients: a zip archive, or a directory zipped as it is read, with a
// manifest naming the pack, its version and its modules.
package resource

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// maxPack bounds the size of a pack read.
const maxPack = 1 << 30

// Pack is a resource pack read whole into memory.
type Pack struct {
	manifest    *Manifest
	downloadURL string
	content     *bytes.Reader
	contentKey  string
	checksum    [32]byte
}

// ReadPath reads the pack at a path: a zip archive (.mcpack, .zip) or a
// directory.
func ReadPath(name string) (*Pack, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("open resource pack path: %w", err)
	}
	if info.IsDir() {
		content, err := zipDir(name)
		if err != nil {
			return nil, err
		}
		return compile(content)
	}
	content, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read resource pack file content: %w", err)
	}
	return compile(content)
}

// Read reads a pack from a zip archive read from r.
func Read(r io.Reader) (*Pack, error) {
	content, err := io.ReadAll(io.LimitReader(r, maxPack+1))
	if err != nil {
		return nil, fmt.Errorf("read resource pack: %w", err)
	}
	if len(content) > maxPack {
		return nil, errors.New("resource pack too large")
	}
	return compile(content)
}

// compile reads the manifest of the archive in content.
func compile(content []byte) (*Pack, error) {
	manifest, err := readManifest(content)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return &Pack{manifest: manifest, checksum: sha256.Sum256(content), content: bytes.NewReader(content)}, nil
}

// zipDir zips the files of a directory, their paths relative to it.
func zipDir(dir string) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return fmt.Errorf("find relative path: %w", err)
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			_, err := w.Create(rel + "/")
			return err
		}
		f, err := w.Create(rel)
		if err != nil {
			return fmt.Errorf("create new zip file: %w", err)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("open resource pack file %v: %w", p, err)
		}
		_, err = f.Write(data)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("build zip archive: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// readManifest reads manifest.json of the archive in content, found at the
// root or in any directory, as the game finds it.
func readManifest(content []byte) (*Manifest, error) {
	r, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("open zip reader: %w", err)
	}
	f := find(r, "manifest.json")
	if f == nil {
		return nil, errors.New("load manifest: 'manifest.json' not found in zip")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("open zip file %v: %w", f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read manifest file: %w", err)
	}
	manifest := &Manifest{}
	if err := UnmarshalJSONC(data, manifest); err != nil {
		return nil, fmt.Errorf("decode manifest JSON: %w (data: %v)", err, string(data))
	}
	manifest.worldTemplate = find(r, "level.dat") != nil
	return manifest, nil
}

// find returns the first file of the archive named name or whose base name
// it is.
func find(r *zip.Reader, name string) *zip.File {
	for _, f := range r.File {
		if f.Name == name || path.Base(f.Name) == name {
			return f
		}
	}
	return nil
}

// stripJSONC turns the JSON with comments of a manifest into JSON as the
// game's packs are read: comments (//, # and /* */) go, and so do spaces,
// tabs and line breaks outside strings, and line breaks inside them.
func stripJSONC(s []byte) []byte {
	out := make([]byte, 0, len(s))
	var (
		quote, escaped                          bool
		inComment, singleLine, canStart, canEnd bool
	)
	for _, c := range s {
		if c == '\\' || escaped {
			if !inComment {
				out = append(out, c)
			}
			escaped = !escaped
			continue
		}
		if c == '"' && !inComment {
			quote = !quote
		}
		if (c == ' ' || c == '\t') && !quote {
			continue
		}
		if c == '\n' {
			if singleLine {
				inComment, canStart = false, false
			}
			continue
		}
		if quote && !inComment {
			out = append(out, c)
			continue
		}
		if inComment {
			switch {
			case c == '*' && !singleLine:
				canEnd = true
			case canEnd && c == '/' && !singleLine:
				inComment, canStart = false, false
			default:
				canEnd = false
			}
			continue
		}
		switch {
		case canStart && (c == '*' || c == '/'):
			inComment, singleLine = true, c == '/'
		case c == '/':
			canStart = true
		case c == '#':
			inComment, singleLine = true, true
		default:
			out = append(out, c)
		}
	}
	return out
}

// Name returns the name of the pack.
func (pack *Pack) Name() string { return pack.manifest.Header.Name }

// UUID returns the UUID of the pack.
func (pack *Pack) UUID() UUID { return pack.manifest.Header.UUID }

// Description returns the description of the pack.
func (pack *Pack) Description() string { return pack.manifest.Header.Description }

// Version returns the version of the pack, as "x.y.z".
func (pack *Pack) Version() string {
	v := pack.manifest.Header.Version
	return strconv.Itoa(v[0]) + "." + strconv.Itoa(v[1]) + "." + strconv.Itoa(v[2])
}

// Modules returns the modules of the pack.
func (pack *Pack) Modules() []Module { return pack.manifest.Modules }

// Dependencies returns the packs the pack depends on.
func (pack *Pack) Dependencies() []Dependency { return pack.manifest.Dependencies }

// HasScripts reports whether the pack has scripts the client runs.
func (pack *Pack) HasScripts() bool { return pack.hasModule("client_data") }

// HasBehaviours reports whether the pack has behaviours.
func (pack *Pack) HasBehaviours() bool { return pack.hasModule("client_data", "data") }

// HasTextures reports whether the pack has resources.
func (pack *Pack) HasTextures() bool { return pack.hasModule("resources") }

func (pack *Pack) hasModule(types ...string) bool {
	for _, m := range pack.manifest.Modules {
		for _, t := range types {
			if m.Type == t {
				return true
			}
		}
	}
	return false
}

// HasWorldTemplate reports whether the pack holds a world template.
func (pack *Pack) HasWorldTemplate() bool { return pack.manifest.worldTemplate }

// DownloadURL returns the URL clients download the pack from, empty for
// one sent over the connection.
func (pack *Pack) DownloadURL() string { return pack.downloadURL }

// WithDownloadURL returns a copy of the pack downloaded from url.
func (pack Pack) WithDownloadURL(url string) *Pack {
	pack.downloadURL = url
	return &pack
}

// Checksum returns the SHA-256 of the archive of the pack.
func (pack *Pack) Checksum() [32]byte { return pack.checksum }

// Len returns the size of the archive of the pack.
func (pack *Pack) Len() int { return pack.content.Len() }

// DataChunkCount returns how many chunks of length bytes the pack is sent
// in.
func (pack *Pack) DataChunkCount(length int) int {
	return (pack.Len() + length - 1) / length
}

// Encrypted reports whether the pack is encrypted, with a content key.
func (pack *Pack) Encrypted() bool { return pack.contentKey != "" }

// ContentKey returns the key the pack is encrypted with, or "".
func (pack *Pack) ContentKey() string { return pack.contentKey }

// WithContentKey returns a copy of the pack encrypted with key.
func (pack Pack) WithContentKey(key string) *Pack {
	pack.contentKey = key
	return &pack
}

// ReadAt reads bytes of the archive of the pack.
func (pack *Pack) ReadAt(b []byte, off int64) (int, error) { return pack.content.ReadAt(b, off) }

// ReadFile returns the content of a file of the pack, its path compared in
// any case.
func (pack *Pack) ReadFile(filePath string) ([]byte, error) {
	r, err := zip.NewReader(pack.content, pack.content.Size())
	if err != nil {
		return nil, fmt.Errorf("failed to create zip reader: %w", err)
	}
	for _, f := range r.File {
		if strings.EqualFold(f.Name, filePath) {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("file %s not found in the resource pack", filePath)
}

// Manifest returns the manifest of the pack.
func (pack *Pack) Manifest() Manifest { return *pack.manifest }

// String returns the name, version, UUID and description of the pack.
func (pack *Pack) String() string {
	return fmt.Sprintf("%v v%v (%v): %v", pack.Name(), pack.Version(), pack.UUID(), pack.Description())
}

// UUID is the UUID of a pack or a module.
type UUID [16]byte

// String returns the UUID in its canonical form.
func (u UUID) String() string {
	var b [36]byte
	hex.Encode(b[0:8], u[0:4])
	b[8] = '-'
	hex.Encode(b[9:13], u[4:6])
	b[13] = '-'
	hex.Encode(b[14:18], u[6:8])
	b[18] = '-'
	hex.Encode(b[19:23], u[8:10])
	b[23] = '-'
	hex.Encode(b[24:], u[10:])
	return string(b[:])
}

// ParseUUID parses a UUID in the forms google/uuid takes: canonical, in
// braces, as a URN, or as 32 hex digits, which the manifests of packs were
// read with.
func ParseUUID(s string) (UUID, error) {
	var u UUID
	switch len(s) {
	case 36:
	case 38:
		// Braces, which google/uuid takes as any two characters.
		s = s[1:37]
	case 45:
		if !strings.EqualFold(s[:9], "urn:uuid:") {
			return u, fmt.Errorf("invalid urn prefix: %q", s[:9])
		}
		s = s[9:]
	case 32:
		if _, err := hex.Decode(u[:], []byte(s)); err != nil {
			return UUID{}, fmt.Errorf("invalid UUID format: %q", s)
		}
		return u, nil
	default:
		return u, fmt.Errorf("invalid UUID length: %d", len(s))
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return u, fmt.Errorf("invalid UUID format: %q", s)
	}
	for i, x := range [16]int{0, 2, 4, 6, 9, 11, 14, 16, 19, 21, 24, 26, 28, 30, 32, 34} {
		if _, err := hex.Decode(u[i:i+1], []byte(s[x:x+2])); err != nil {
			return UUID{}, fmt.Errorf("invalid UUID format: %q", s)
		}
	}
	return u, nil
}

// MarshalText encodes the UUID as text.
func (u UUID) MarshalText() ([]byte, error) { return []byte(u.String()), nil }

// UnmarshalText decodes a UUID from text.
func (u *UUID) UnmarshalText(b []byte) error {
	v, err := ParseUUID(string(b))
	if err != nil {
		return err
	}
	*u = v
	return nil
}

// UnmarshalJSONC decodes JSON with comments into v as manifests are read:
// the first value, leniently.
func UnmarshalJSONC(data []byte, v any) error {
	return json.NewDecoder(bytes.NewReader(stripJSONC(data))).Decode(v)
}
