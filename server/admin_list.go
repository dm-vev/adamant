package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/identity"
)

// entry is a player on one of the server's lists: the whitelist, the
// operators or the bans.
type entry struct {
	key identity.Key
	// name is the name of the player as last seen, or as the admin typed
	// it; for a key of a name, the key.
	name string
	// reason is why the player is banned, for a ban.
	reason string
	// ip is an address a ban of a player was written with by hand, which
	// the bans keep as they read it.
	ip string
}

// entries are the players of a list, in the order of its file, by key. A
// nil order is a list that was never read from a file nor changed, which
// some files write apart from an empty list.
type entries struct {
	order []entry
	index map[identity.Key]int
}

// has reports whether the list holds the key passed.
func (e *entries) has(k identity.Key) bool {
	_, ok := e.index[k]
	return ok
}

// get returns the entry of the key passed.
func (e *entries) get(k identity.Key) (entry, bool) {
	if i, ok := e.index[k]; ok {
		return e.order[i], true
	}
	return entry{}, false
}

// add appends en, unless the list holds its key. It reports whether it
// did.
func (e *entries) add(en entry) bool {
	if e.has(en.key) {
		return false
	}
	if e.index == nil {
		e.index = make(map[identity.Key]int)
	}
	e.index[en.key] = len(e.order)
	e.order = append(e.order, en)
	return true
}

// put replaces the entry of the key of en where it stands, or appends en.
func (e *entries) put(en entry) {
	if i, ok := e.index[en.key]; ok {
		e.order[i] = en
		return
	}
	e.add(en)
}

// remove takes the entry of the key passed off the list. It reports
// whether the list held it.
func (e *entries) remove(k identity.Key) bool {
	i, ok := e.index[k]
	if !ok {
		return false
	}
	e.order = slices.Delete(e.order, i, i+1)
	delete(e.index, k)
	for j := i; j < len(e.order); j++ {
		e.index[e.order[j].key] = j
	}
	return true
}

// keys returns the keys of the list, as text, in order.
func (e *entries) keys() []string {
	out := make([]string, 0, len(e.order))
	for _, en := range e.order {
		out = append(out, en.key.String())
	}
	return out
}

// clone returns a copy of the list, which changes apart from it.
func (e *entries) clone() entries {
	c := entries{order: slices.Clone(e.order), index: make(map[identity.Key]int, len(e.index))}
	for k, i := range e.index {
		c.index[k] = i
	}
	return c
}

// newEntries returns an empty list read from a file, which writes as an
// empty list rather than as one never read.
func newEntries(n int) entries {
	return entries{order: make([]entry, 0, n), index: make(map[identity.Key]int, n)}
}

// nameEntry returns the entry of a list of names for the name passed. It
// fails for a name that normalises to "".
func nameEntry(name string) (entry, error) {
	k, err := identity.OfName(name)
	if err != nil {
		return entry{}, err
	}
	n, _ := k.Name()
	return entry{key: k, name: n}, nil
}

// writeFileAtomic replaces the file at path with data, so that a crash
// leaves the file whole, old or new: it writes a temporary file beside it,
// syncs it, renames it over the file and syncs the directory. The file
// keeps the permissions it had, 0644 if new. The directory is made if
// missing. what names the file in errors, as "write <what>: ...".
func writeFileAtomic(path string, data []byte, what string) (err error) {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0777); err != nil {
			return fmt.Errorf("create %s directory: %w", what, err)
		}
	}
	mode := fs.FileMode(0644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", what, err)
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", what, err)
	}
	if err = f.Chmod(mode); err != nil {
		return fmt.Errorf("write %s: %w", what, err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("write %s: %w", what, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", what, err)
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", what, err)
	}
	if d, derr := os.Open(dir); derr == nil {
		// Not every file system syncs a directory: the file is written
		// either way.
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// errNotExist reports whether err is that of a file that does not exist.
func errNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

// admission is the Allower of the admin lists of the server, which
// UserConfig.Config composes: the bans first, by name or by address, then
// the operators, who join whether or not the whitelist holds them, as
// Nukkit lets them, then the whitelist. Any of the lists may be nil.
type admission struct {
	whitelist *Whitelist
	operators *operatorsList
	bans      *banList
}

// Allow refuses a banned player with the reason of the ban, allows an
// operator, and then holds the player to the whitelist.
func (a admission) Allow(addr net.Addr, id client.Identity, p client.Profile) (string, bool) {
	addrString := ""
	if addr != nil {
		addrString = addr.String()
	}
	if a.bans != nil {
		if reason, ok := a.bans.banned(addrString, id.Name); ok {
			msg := "You are banned from this server."
			if reason != "" {
				msg += " Reason: " + reason
			}
			return msg, false
		}
	}
	if a.operators != nil && a.operators.isOperator(id.Name) {
		return "", true
	}
	if a.whitelist != nil {
		return a.whitelist.Allow(addr, id, p)
	}
	return "", true
}

// adminAccess and underlyingWhitelist hand Config.New the lists of the
// admission, which the Server changes through its admin API.
func (a admission) adminAccess() (*operatorsList, *banList) { return a.operators, a.bans }
func (a admission) underlyingWhitelist() *Whitelist         { return a.whitelist }

var _ Allower = admission{}
