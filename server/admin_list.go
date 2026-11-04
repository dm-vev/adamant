package server

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
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
	// op is what an entry of ops.json holds beside the player.
	op opFields
	// role is what an entry of permissions.json holds beside the player.
	role roleFields
}

// opFields are the fields of an operator beside the player, as Java's
// ops.json writes them: the level of its own, and whether it joins a full
// server.
type opFields struct {
	// level is the level of the operator, if hasLevel; otherwise the
	// operator has the OperatorLevel of the server.
	level    cmd.Level
	hasLevel bool
	// bypass is bypassesPlayerLimit: the operator joins a full server.
	bypass bool
}

// set reports whether the fields hold anything an entry of names alone
// cannot write.
func (f opFields) set() bool { return f.hasLevel || f.bypass }

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

// match returns the entry of the player of the name passed in a list of
// a server that keys its players by name: the entry of the name, or else
// the first entry of an Xbox account whose player last had the name, which
// a server keyed by XUID wrote.
func (e *entries) match(name string) (entry, bool) {
	k, err := identity.OfName(name)
	if err != nil {
		return entry{}, false
	}
	if en, ok := e.get(k); ok {
		return en, true
	}
	n, _ := k.Name()
	for _, en := range e.order {
		if _, ok := en.key.XUID(); ok && identity.Normalize(en.name) == n {
			return en, true
		}
	}
	return entry{}, false
}

// byName returns the entries of Xbox accounts whose players last had the
// name passed, in order.
func (e *entries) byName(name string) []entry {
	n := identity.Normalize(name)
	var out []entry
	for _, en := range e.order {
		if _, ok := en.key.XUID(); ok && n != "" && identity.Normalize(en.name) == n {
			out = append(out, en)
		}
	}
	return out
}

// bind turns the pending entry of the name passed, an entry of the name on
// a list of a server keyed by XUID, into an entry of the account x, which
// joined with that name, where it stands, keeping its reason. If the list
// holds x already, the pending entry is dropped instead. It returns the
// pending entry, and reports whether there was one.
func (e *entries) bind(x identity.Key, name string) (entry, bool) {
	k, err := identity.OfName(name)
	if err != nil {
		return entry{}, false
	}
	i, ok := e.index[k]
	if !ok {
		return entry{}, false
	}
	pending := e.order[i]
	if e.has(x) {
		e.remove(k)
		return pending, true
	}
	en := pending
	en.key, en.name = x, strings.TrimSpace(name)
	delete(e.index, k)
	e.index[x] = i
	e.order[i] = en
	return pending, true
}

// pending returns the names of the pending entries of the list: the names
// a server keyed by XUID holds until an account joins with one.
func (e *entries) pending() []string {
	var out []string
	for _, en := range e.order {
		if n, ok := en.key.Name(); ok {
			out = append(out, n)
		}
	}
	return out
}

// accounts returns the number of entries of Xbox accounts of the list.
func (e *entries) accounts() int {
	n := 0
	for _, en := range e.order {
		if _, ok := en.key.XUID(); ok {
			n++
		}
	}
	return n
}

// xuidEntry returns the entry of the account of the XUID passed, which
// last had the name passed.
func xuidEntry(xuid, name string) (entry, error) {
	k, err := identity.OfXUID(xuid)
	if err != nil {
		return entry{}, err
	}
	return entry{key: k, name: strings.TrimSpace(name)}, nil
}

// parseEntryText reads an entry as the whitelist writes it, one a line:
// "xuid:<XUID>", then a space and the name the player last had if known,
// for an Xbox account, or a name. It reports false for a blank text.
func parseEntryText(text string) (entry, bool) {
	t := strings.TrimSpace(text)
	if len(t) > len(xuidTextPrefix) && strings.EqualFold(t[:len(xuidTextPrefix)], xuidTextPrefix) {
		digits, name, _ := strings.Cut(t[len(xuidTextPrefix):], " ")
		if en, err := xuidEntry(digits, name); err == nil {
			return en, true
		}
	}
	en, err := nameEntry(t)
	return en, err == nil
}

// entryText writes an entry as parseEntryText reads it.
func entryText(en entry) string {
	if _, ok := en.key.XUID(); ok && en.name != "" {
		return en.key.String() + " " + en.name
	}
	return en.key.String()
}

// xuidTextPrefix starts the text of an entry of an Xbox account.
const xuidTextPrefix = "xuid:"

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
//
// A server keyed by XUID refuses first an identity Xbox Live did not
// vouch for, or that has no XUID, then binds the pending entries of the
// name of the player, if any, to its account, and checks the lists by the
// account. A server keyed by name checks them by the name.
type admission struct {
	mode      identity.Mode
	log       *slog.Logger
	whitelist *Whitelist
	operators *operatorsList
	bans      *banList
	// roles is permissions.json, the roles of players.
	roles *roleList
	// known are the accounts a server keyed by XUID has seen; nil on one
	// keyed by name.
	known *knownPlayers
}

// errNoAccount is the message a server keyed by XUID refuses a player
// that Xbox Live did not vouch for with.
const errNoAccount = "This server needs an Xbox Live account."

// accountKey returns the key of the account of an identity, as a server
// keyed by XUID takes it: none if Xbox Live did not vouch for the
// identity, which may name any XUID.
func accountKey(id client.Identity) (identity.Key, bool) {
	if !id.Authenticated {
		return identity.Key{}, false
	}
	k, err := identity.OfXUID(id.XUID)
	return k, err == nil
}

// Allow refuses a banned player with the reason of the ban, allows an
// operator, and then holds the player to the whitelist.
func (a admission) Allow(addr net.Addr, id client.Identity, p client.Profile) (string, bool) {
	addrString := ""
	if addr != nil {
		addrString = addr.String()
	}
	if a.mode == identity.XUID {
		return a.allowAccount(addrString, id)
	}
	if a.bans != nil {
		if reason, ok := a.bans.banned(addrString, id.Name); ok {
			return bannedMessage(reason), false
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

// allowAccount is Allow on a server keyed by XUID.
func (a admission) allowAccount(addr string, id client.Identity) (string, bool) {
	k, ok := accountKey(id)
	if !ok {
		return errNoAccount, false
	}
	a.bind(k, id.Name)
	if a.bans != nil {
		if reason, ok := a.bans.bannedAccount(addr, k); ok {
			return bannedMessage(reason), false
		}
	}
	if a.operators.hasKey(k) {
		return "", true
	}
	if a.whitelist != nil {
		return a.whitelist.allowAccount(k)
	}
	return "", true
}

// bind binds the entries the lists hold of the name passed until an
// account joins with it to the account of the key passed, which did, and
// logs each.
func (a admission) bind(k identity.Key, name string) {
	type binder interface {
		bind(x identity.Key, name string) (entry, bool, error)
	}
	for _, l := range []struct {
		what string
		list binder
		ok   bool
	}{
		{"ban", a.bans, a.bans != nil},
		{"operator entry", a.operators, a.operators != nil},
		{"whitelist entry", a.whitelist, a.whitelist != nil},
		{"role", a.roles, a.roles != nil},
	} {
		if !l.ok {
			continue
		}
		pending, ok, err := l.list.bind(k, name)
		if !ok {
			continue
		}
		pn, _ := pending.key.Name()
		a.logger().Info(fmt.Sprintf("Bound the %s %q to %s (%s).", l.what, pn, k, strings.TrimSpace(name)))
		if err != nil {
			a.logger().Error("Write the list of the bound entry: "+err.Error(), "entry", l.what)
		}
	}
}

func (a admission) logger() *slog.Logger {
	if a.log == nil {
		return slog.Default()
	}
	return a.log
}

// bannedMessage returns what a banned player is refused with.
func bannedMessage(reason string) string {
	msg := "You are banned from this server."
	if reason != "" {
		msg += " Reason: " + reason
	}
	return msg
}

// warnHeld logs, at the start of a server, the entries of its lists that a
// server keyed in the other mode wrote: on one keyed by XUID, the
// operators and the members of the whitelist still held by name, which the
// first account that joins with the name becomes; on one keyed by name,
// the entries of Xbox accounts, which match the name their player last
// had.
func (a admission) warnHeld() {
	if a.mode == identity.XUID {
		var ops, members []string
		if a.operators != nil {
			a.operators.view(func(e *entries) { ops = e.pending() })
		}
		if a.whitelist != nil {
			a.whitelist.view(func(e *entries) { members = e.pending() })
		}
		if len(ops) > 0 {
			a.logger().Warn("Operators held by name: bound to the first Xbox account that joins with that name. Name the account with xuid:<XUID> instead.", "names", ops)
		}
		if len(members) > 0 {
			a.logger().Warn("Whitelist entries held by name: bound to the first Xbox account that joins with that name.", "names", members)
		}
		return
	}
	n := 0
	count := func(e *entries) { n += e.accounts() }
	if a.operators != nil {
		a.operators.view(count)
	}
	if a.whitelist != nil {
		a.whitelist.view(count)
	}
	if a.bans != nil {
		a.bans.view(count)
	}
	if n > 0 {
		a.logger().Warn(fmt.Sprintf("%d entries of the lists name Xbox accounts: on this server, keyed by name, each matches the name its player last had, and is written back unchanged.", n))
	}
}

// adminAccess and underlyingWhitelist hand Config.New the lists of the
// admission, which the Server changes through its admin API.
func (a admission) adminAccess() (*operatorsList, *banList, *roleList) {
	return a.operators, a.bans, a.roles
}
func (a admission) underlyingWhitelist() *Whitelist { return a.whitelist }
func (a admission) knownAccess() *knownPlayers      { return a.known }

var _ Allower = admission{}
