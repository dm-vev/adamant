package server

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/identity"
)

// List names one of the lists of the server an admin changes.
type List uint8

const (
	// Operators is the list of the operators, ops.json.
	Operators List = iota
	// Whitelisted is the whitelist.
	Whitelisted
	// Banned is the list of the players banned, banned-players.json.
	Banned
)

// Found says how the server found the player an admin named.
type Found uint8

const (
	// FoundByXUID is a player named by "xuid:<XUID>".
	FoundByXUID Found = iota
	// FoundOnline is the player online with the name.
	FoundOnline
	// FoundOnList is the entry of the list whose player last had the name.
	FoundOnList
	// FoundKnown is the account that last joined with the name.
	FoundKnown
	// FoundPending is a name no account is known by on a server keyed by
	// XUID: the first account that joins with it is the player.
	FoundPending
	// FoundByName is a name, on a server keyed by name.
	FoundByName
)

// Target is a player an admin names, as the server resolved it.
type Target struct {
	// Key is what the list holds the player by.
	Key identity.Key
	// Name is the name to show: as the admin typed it on a server keyed by
	// name, the name the account last had otherwise, "" if none is known.
	Name string
	// Found says how the server found the player.
	Found Found
	// Pending reports that no account is known by Name yet, on a server
	// keyed by XUID: the first that joins with the name is the player.
	Pending bool
}

// String returns the player as the commands show it: "Steve
// (xuid:2535412345678901)", "xuid:2535412345678901" if no name is known,
// or the name.
func (t Target) String() string {
	if _, ok := t.Key.XUID(); ok {
		if t.Name == "" {
			return t.Key.String()
		}
		return t.Name + " (" + t.Key.String() + ")"
	}
	return t.Name
}

// Arg returns the target as the methods of the Server that take a player
// read it: "xuid:<XUID>" for an account, the name otherwise.
func (t Target) Arg() string {
	if _, ok := t.Key.XUID(); ok {
		return t.Key.String()
	}
	return t.Name
}

// errKeyedByName is the error of "xuid:<XUID>" added to a list of a server
// keyed by name.
var errKeyedByName = errors.New(`This server keys players by name (IdentityKey = "name"): name the player.`)

// ResolvePlayer resolves a player an admin names to add to a list, by
// "xuid:<XUID>" or by name. A name is, in order: the player online with
// the name, in any case; on a server keyed by XUID, the account that last
// joined with the name, or else the name held until an account joins with
// it (Pending); on a server keyed by name, the name. "xuid:<XUID>" is an
// error on a server keyed by name.
func (srv *Server) ResolvePlayer(s string) (Target, error) {
	k, err := identity.Parse(s)
	if err != nil {
		return Target{}, err
	}
	name := strings.TrimSpace(s)
	byXUID := srv.IdentityKey() == identity.XUID
	if x, ok := k.XUID(); ok {
		if !byXUID {
			return Target{}, errKeyedByName
		}
		return Target{Key: k, Name: srv.accountName(x), Found: FoundByXUID}, nil
	}
	if !byXUID {
		return Target{Key: k, Name: name, Found: FoundByName}, nil
	}
	if p, ok := srv.players.named(s); ok {
		if acc, ok := accountKey(p.id); ok {
			return Target{Key: acc, Name: p.id.Name, Found: FoundOnline}, nil
		}
	}
	if p, ok := srv.known.named(s); ok {
		acc, _ := identity.OfXUID(p.XUID)
		return Target{Key: acc, Name: p.Name, Found: FoundKnown}, nil
	}
	return Target{Key: k, Name: name, Found: FoundPending, Pending: true}, nil
}

// ResolveEntry resolves the entry of a list an admin names to remove, by
// "xuid:<XUID>" or by name. On a server keyed by XUID, a name is, in order:
// the account of the player online with the name, which must be on the
// list if another account the list holds had the name; the one account of
// the list whose player last had the name; the account that last joined
// with the name; the name held until an account joins with it. Several
// accounts of the list by the name are an error, which names each. On a
// server keyed by name, a name is the entry of the name, or of the account
// whose player last had it. If the list holds nothing of what the admin
// named, the Target names it and the removal finds nothing.
func (srv *Server) ResolveEntry(list List, s string) (Target, error) {
	k, err := identity.Parse(s)
	if err != nil {
		return Target{}, err
	}
	view, ok := srv.listView(list)
	if !ok {
		return Target{}, errAdminUnavailable
	}
	name := strings.TrimSpace(s)
	if x, ok := k.XUID(); ok {
		t := Target{Key: k, Name: srv.accountName(x), Found: FoundByXUID}
		view(func(e *entries) {
			if en, ok := e.get(k); ok && en.name != "" {
				t.Name = en.name
			}
		})
		return t, nil
	}
	if srv.IdentityKey() != identity.XUID {
		t := Target{Key: k, Name: name, Found: FoundByName}
		view(func(e *entries) {
			if en, ok := e.match(s); ok {
				t.Key = en.key
				if _, ok := en.key.XUID(); ok {
					t.Name, t.Found = en.name, FoundOnList
				}
			}
		})
		return t, nil
	}
	var t Target
	view(func(e *entries) {
		accounts := e.byName(s)
		if p, ok := srv.players.named(s); ok {
			if acc, ok := accountKey(p.id); ok {
				t = Target{Key: acc, Name: p.id.Name, Found: FoundOnline}
				switch {
				case e.has(acc):
				case len(accounts) > 0:
					// No silent fall-through to another account.
					err = fmt.Errorf("%s is not %s; %s: name one with xuid:<XUID>", t, listWhat(list), srv.describeAccounts("the list has", accounts))
				case e.has(k):
					// The name is held for the next account that joins with
					// it: the admin means that entry.
					t = Target{Key: k, Name: name, Found: FoundPending, Pending: true}
				}
				return
			}
		}
		switch {
		case len(accounts) == 1:
			t = Target{Key: accounts[0].key, Name: accounts[0].name, Found: FoundOnList}
		case len(accounts) > 1:
			err = fmt.Errorf("%s: name one with xuid:<XUID>", srv.describeAccounts("several entries of the list had the name "+name+":", accounts))
		default:
			t = Target{Key: k, Name: name, Found: FoundPending, Pending: true}
			if p, ok := srv.known.named(s); ok {
				if acc, _ := identity.OfXUID(p.XUID); e.has(acc) {
					t = Target{Key: acc, Name: p.Name, Found: FoundKnown}
				}
			}
		}
	})
	if err != nil {
		return Target{}, err
	}
	return t, nil
}

// describeAccounts returns the entries passed after the heading, each as a
// command shows it, with when the account was last seen, if known.
func (srv *Server) describeAccounts(heading string, accounts []entry) string {
	parts := make([]string, 0, len(accounts))
	for _, en := range accounts {
		desc := Target{Key: en.key, Name: en.name}.String()
		if x, _ := en.key.XUID(); x != "" {
			if p, ok := srv.known.account(x); ok && !p.LastSeen.IsZero() {
				desc += ", last seen " + p.LastSeen.Format(time.DateOnly)
			}
		}
		parts = append(parts, desc)
	}
	return heading + " " + strings.Join(parts, "; ")
}

// listWhat returns how the errors of removals name being on the list.
func listWhat(list List) string {
	switch list {
	case Operators:
		return "an operator"
	case Whitelisted:
		return "on the whitelist"
	default:
		return "banned"
	}
}

// listView returns the view of the list passed, false if the server has
// none.
func (srv *Server) listView(list List) (func(func(*entries)), bool) {
	switch list {
	case Operators:
		if srv.operators != nil {
			return srv.operators.view, true
		}
	case Whitelisted:
		if srv.whitelist != nil {
			return srv.whitelist.view, true
		}
	case Banned:
		if srv.bans != nil {
			return srv.bans.view, true
		}
	}
	return nil, false
}

// accountName returns the name the account of the XUID passed has online,
// or last joined with, "" if none is known.
func (srv *Server) accountName(xuid string) string {
	if h, ok := srv.players.byXUID(xuid); ok {
		srv.players.mu.RLock()
		defer srv.players.mu.RUnlock()
		for _, p := range srv.players.online {
			if p.handle == h {
				return p.id.Name
			}
		}
	}
	if p, ok := srv.known.account(xuid); ok {
		return p.Name
	}
	return ""
}

// entryStrings returns the entries of a list as the commands show them:
// "Steve (xuid:2535412345678901)" for an account, and on a server keyed by
// XUID "steve (held by name)" for a name held until an account joins with
// it; a name otherwise.
func (srv *Server) entryStrings(view func(func(*entries))) []string {
	byXUID := srv.IdentityKey() == identity.XUID
	var out []string
	view(func(e *entries) {
		out = make([]string, 0, len(e.order))
		for _, en := range e.order {
			s := Target{Key: en.key, Name: en.name}.String()
			if _, ok := en.key.XUID(); !ok && byXUID {
				s += " (held by name)"
			}
			out = append(out, s)
		}
	})
	return out
}

// KnownNames returns the names the accounts a server keyed by XUID has
// seen last joined with, sorted: the names the admin commands find
// accounts by. It is nil on a server keyed by name.
func (srv *Server) KnownNames() []string {
	return srv.known.names()
}
