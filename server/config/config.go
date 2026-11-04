// Package config checks the values of config.toml before the server starts
// with them, so that a file with bad values is refused with every one of its
// problems named at once, rather than one at a time or not at all.
//
// Each part of the file, such as server.UserConfig or the UserConfig of a
// protocol adapter, has a Check method that declares the bounds of the keys
// it reads, with the helpers of this package:
//
//	func (uc UserConfig) Check(p *config.Problems) {
//		config.AtLeast(p, "Players.MaxCount", uc.Players.MaxCount, 0)
//		config.AtMost(p, "World.Workers", uc.World.Workers, 1024)
//		config.Range(p, "Players.MaximumChunkRadius", uc.Players.MaximumChunkRadius, 0, 96)
//		config.OneOf(p, "Server.Mode", uc.Server.Mode, "fast", "safe")
//		if d, ok := config.Duration(p, "World.Interval", uc.World.Interval); ok {
//			config.Range(p, "World.Interval", d, time.Second, time.Hour)
//		}
//		config.Address(p, "Network.Address", "udp", uc.Network.Address)
//		config.Folder(p, "World.Folder", uc.World.Folder)
//	}
//
// A key is named by its table and its name as the file writes them, joined
// by a dot. The program finds the line of the key in the file from that
// name, and reports there every problem a Check adds. A check that needs
// more than a helper adds its problem itself, with Problems.Errorf, or
// Problems.Warnf for a value the part reads anyway, as one it falls back
// from. A parser the part already has reports through Problems.Add.
//
// The helpers report and return whether the value passed. They never
// change it: the part reads its keys as it did, and Check only declares
// which values it refuses.
//
// A new key needs nothing more to be known: the program finds the keys of
// the file no part reads from the fields of the structs of the parts, and
// warns of them, or refuses them with StrictConfig of [Server]. A part
// that reads the keys of a table it does not know beforehand reads it into
// a map, whose keys are all known.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Problem is something wrong with one value of a config file, or with the
// file itself.
type Problem struct {
	// Key names the key of the value, as "Players.MaxCount", or is empty if
	// the problem is of no single key.
	Key string
	// Line is the line of the file the problem is at, or 0 if unknown.
	Line int
	// Message says what is wrong, naming the key and what it may be.
	Message string
	// Warning marks a problem the server starts with anyway, such as a key
	// that nothing reads or a value it falls back from, unless the file is
	// checked strictly.
	Warning bool
}

// String returns the message of the problem, after its line if known.
func (p Problem) String() string {
	if p.Line > 0 {
		return fmt.Sprintf("line %d: %s", p.Line, p.Message)
	}
	return p.Message
}

// Problems collects the problems of a config file. Its zero value is ready
// to use.
type Problems struct {
	list []Problem
}

// Errorf adds a problem of the key passed that keeps the server from
// starting. The message should name the key and what it may be.
func (p *Problems) Errorf(key, format string, args ...any) {
	p.list = append(p.list, Problem{Key: key, Message: fmt.Sprintf(format, args...)})
}

// Warnf adds a problem of the key passed that the server starts with, which
// a strict check makes an error.
func (p *Problems) Warnf(key, format string, args ...any) {
	p.list = append(p.list, Problem{Key: key, Message: fmt.Sprintf(format, args...), Warning: true})
}

// Add adds the error of parsing the value of the key passed, such as that of
// a parser the part already has, as a problem that keeps the server from
// starting. A nil error adds none. It reports whether err was nil.
func (p *Problems) Add(key string, err error) bool {
	if err == nil {
		return true
	}
	p.Errorf(key, "%s: %v", key, err)
	return false
}

// AddProblem adds a problem as it is.
func (p *Problems) AddProblem(pr Problem) { p.list = append(p.list, pr) }

// List returns the problems added, in the order they were added.
func (p *Problems) List() []Problem { return p.list }

// Locate sets the line of each problem of a key that has none to the line
// that line returns for its key, 0 if the file lacks it, and orders the
// problems by their lines, those of no line first.
func (p *Problems) Locate(line func(key string) int) {
	for i, pr := range p.list {
		if pr.Line == 0 && pr.Key != "" {
			p.list[i].Line = line(pr.Key)
		}
	}
	slices.SortStableFunc(p.list, func(a, b Problem) int { return cmp.Compare(a.Line, b.Line) })
}

// Err returns an *Error of the problems that keep the server from starting,
// or nil if there are none. If strict, warnings keep it from starting too.
func (p *Problems) Err(strict bool) error {
	var e Error
	for _, pr := range p.list {
		if !pr.Warning || strict {
			e.Problems = append(e.Problems, pr)
		}
	}
	if len(e.Problems) == 0 {
		return nil
	}
	return &e
}

// Error is the error of a config file with problems that keep the server
// from starting.
type Error struct {
	// File names the file, if known.
	File string
	// Problems are the problems of the file, each one once.
	Problems []Problem
}

// Error returns the problem if there is one, and otherwise the number of
// problems with each of them on a line of its own.
func (e *Error) Error() string {
	if len(e.Problems) == 1 {
		return e.Problems[0].String()
	}
	file := cmp.Or(e.File, "the config")
	var b strings.Builder
	fmt.Fprintf(&b, "%s has %d problems:", file, len(e.Problems))
	for _, pr := range e.Problems {
		b.WriteString("\n\t")
		b.WriteString(pr.String())
	}
	return b.String()
}

// Ordered is a value the bounds of Range and AtLeast compare, such as an int
// or a time.Duration.
type Ordered interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64
}

// Range checks that v of the key passed lies within min and max, both
// included. Its message, as those of AtLeast and AtMost, prints v with %v,
// so that a time.Duration reads as "90s".
func Range[T Ordered](p *Problems, key string, v, min, max T) bool {
	if v < min || v > max {
		p.Errorf(key, "%s = %v: want %v to %v", key, v, min, max)
		return false
	}
	return true
}

// AtLeast checks that v of the key passed is min or more.
func AtLeast[T Ordered](p *Problems, key string, v, min T) bool {
	if v < min {
		p.Errorf(key, "%s = %v: want %v or more", key, v, min)
		return false
	}
	return true
}

// AtMost checks that v of the key passed is max or less.
func AtMost[T Ordered](p *Problems, key string, v, max T) bool {
	if v > max {
		p.Errorf(key, "%s = %v: want %v or less", key, v, max)
		return false
	}
	return true
}

// OneOf checks that the value of the key passed is one of the values
// passed, compared without regard to case or the spaces around it, as the
// parts of config.toml read their names. An empty value passes: every key of
// config.toml reads it as its default.
func OneOf(p *Problems, key, value string, values ...string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return true
	}
	for _, allowed := range values {
		if strings.EqualFold(v, allowed) {
			return true
		}
	}
	p.Errorf(key, "%s = %q: want %s", key, value, Quoted(values...))
	return false
}

// Quoted returns the values passed quoted and listed as "a", "b" or "c", as
// OneOf lists them.
func Quoted(values ...string) string {
	q := make([]string, len(values))
	for i, v := range values {
		q[i] = strconv.Quote(v)
	}
	if len(q) < 2 {
		return strings.Join(q, "")
	}
	return strings.Join(q[:len(q)-1], ", ") + " or " + q[len(q)-1]
}

// Duration checks that the value of the key passed is a duration such as
// "90s" or "5m", and returns it. An empty value passes as 0. Bound the
// duration returned with Range or AtLeast.
func Duration(p *Problems, key, value string) (time.Duration, bool) {
	v := strings.TrimSpace(value)
	if v == "" {
		return 0, true
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		p.Errorf(key, "%s = %q: want a duration such as \"90s\" or \"5m\"", key, value)
		return 0, false
	}
	return d, true
}

// Address checks that the value of the key passed is an address the
// network passed ("tcp" or "udp") listens on, such as ":19132" or
// "127.0.0.1:19133": a host, which may be empty, and a port of 0 to 65535,
// at which the system picks a free one. The host is not resolved. An empty
// value passes: the keys of addresses read it as their default or as off.
func Address(p *Problems, key, network, value string) bool {
	if value == "" {
		return true
	}
	_, port, err := net.SplitHostPort(value)
	if err == nil {
		_, err = net.LookupPort(network, port)
	}
	if err != nil {
		p.Errorf(key, "%s = %q: want a host and a port of 0 to 65535, such as \":19132\" or \"127.0.0.1:19132\"", key, value)
		return false
	}
	return true
}

// Folder checks that the value of the key passed names a folder the server
// may write in: a folder it may write in, or none yet, where the server may
// make one. An empty value names the working directory.
func Folder(p *Problems, key, path string) bool {
	return folder(p, key, path, true)
}

// ReadFolder checks that the value of the key passed names a folder the
// server reads: a folder, or none yet, where the server may make one.
func ReadFolder(p *Problems, key, path string) bool {
	return folder(p, key, path, false)
}

// folder checks a folder that the server makes if missing, and writes in if
// write is true.
func folder(p *Problems, key, path string, write bool) bool {
	path = cmp.Or(path, ".")
	info, err := os.Stat(path)
	switch {
	case err == nil && !info.IsDir():
		p.Errorf(key, "%s = %q: it is a file, want a folder", key, path)
		return false
	case err == nil && !write:
		return true
	case err == nil:
		return writable(p, key, path, path)
	}
	// The server makes the folder in the nearest folder that exists.
	parent := path
	for {
		next := filepath.Dir(parent)
		if next == parent {
			return true
		}
		parent = next
		info, perr := os.Stat(parent)
		switch {
		case errors.Is(perr, os.ErrNotExist):
			continue
		case perr != nil || !info.IsDir():
			p.Errorf(key, "%s = %q: %s cannot hold a folder", key, path, parent)
			return false
		case !errors.Is(err, os.ErrNotExist):
			p.Errorf(key, "%s = %q: %v", key, path, err)
			return false
		}
		return writable(p, key, path, parent)
	}
}

// File checks that the value of the key passed names a file the server
// reads, and writes in its folder if missing: a file, or none yet, in a
// folder that exists and that the server may write in.
func File(p *Problems, key, path string) bool {
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		p.Errorf(key, "%s = %q: it is a folder, want a file", key, path)
		return false
	case err == nil:
		return true
	}
	dir := filepath.Dir(path)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		p.Errorf(key, "%s = %q: there is no folder %s to make it in", key, path, dir)
		return false
	}
	if !errors.Is(err, os.ErrNotExist) {
		p.Errorf(key, "%s = %q: %v", key, path, err)
		return false
	}
	return writable(p, key, path, dir)
}

// writable checks that the server may make files in dir, which the value of
// the key passed, path, names or lies in, by making one and removing it.
func writable(p *Problems, key, path, dir string) bool {
	f, err := os.CreateTemp(dir, ".config-check-*")
	if err != nil {
		p.Errorf(key, "%s = %q: the server may not write in %s", key, path, dir)
		return false
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return true
}
