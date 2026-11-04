package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/google/uuid"
)

// backupSuffix ends the name of the copy a list file is kept as before the
// first start of a server keyed by XUID moves its names to accounts.
const backupSuffix = ".before-xuid"

// migratedList is a list the move to accounts goes through.
type migratedList interface {
	update(f func(e *entries) bool) error
	view(f func(e *entries))
	path() string
}

// moveToAccounts runs at the first start of a server keyed by XUID, which
// the lack of known-players.json tells: it seeds the known players with
// the saved records of the players that hold an XUID, and moves each name
// on the lists that exactly one of them gives to an account to the entry
// of that account. A name two records give to two accounts, and a name no
// record gives, stay held by name until an account joins with it. Each
// list file that holds a name is kept first as <file>.before-xuid, unless
// such a copy exists; the lists are written, then known-players.json,
// which marks the move done.
func moveToAccounts(a admission, lister player.Lister) error {
	type record struct {
		xuid, name string
		id         uuid.UUID
	}
	var records []record
	accounts := map[string][]string{}
	// display holds, for each name normalised, the name as a record holds
	// it: the entry of the account is shown so.
	display := map[string]string{}
	if lister != nil {
		err := lister.Players(func(id uuid.UUID, name, xuid string) bool {
			k, err := identity.OfXUID(xuid)
			if err != nil {
				return true
			}
			x, _ := k.XUID()
			records = append(records, record{xuid: x, name: strings.TrimSpace(name), id: id})
			if n := identity.Normalize(name); n != "" && !slices.Contains(accounts[n], x) {
				accounts[n] = append(accounts[n], x)
				display[n] = strings.TrimSpace(name)
			}
			return true
		})
		if err != nil {
			return fmt.Errorf("list the saved players: %w", err)
		}
	}
	for _, r := range records {
		a.known.seed(knownPlayer{XUID: r.xuid, Name: r.name, UUID: r.id}, len(accounts[identity.Normalize(r.name)]) == 1)
	}

	type list struct {
		what string
		list migratedList
	}
	var lists []list
	if a.operators != nil {
		lists = append(lists, list{"operators", a.operators})
	}
	if a.whitelist != nil {
		lists = append(lists, list{"whitelist", a.whitelist})
	}
	if a.bans != nil {
		lists = append(lists, list{"bans", a.bans})
	}
	var total, moved int
	var held []string
	for _, l := range lists {
		var names []string
		l.list.view(func(e *entries) { names = e.pending() })
		if len(names) == 0 {
			continue
		}
		if err := backup(l.list.path()); err != nil {
			return err
		}
		err := l.list.update(func(e *entries) bool {
			changed := false
			for _, n := range names {
				total++
				xs := accounts[n]
				switch len(xs) {
				case 1:
					// The name of the record, not the name the account had
					// last: an account two records hold under two names has
					// both.
					k, _ := identity.OfXUID(xs[0])
					e.bind(k, display[n])
					changed = true
					moved++
				case 0:
					held = append(held, n+" ("+l.what+")")
				default:
					held = append(held, n+" ("+l.what+")")
					a.logger().Warn(fmt.Sprintf("The name %q on the %s is that of %d saved players: it stays held by name until one joins with it.", n, l.what, len(xs)), "accounts", "xuid:"+strings.Join(xs, ", xuid:"))
				}
			}
			return changed
		})
		if err != nil {
			return err
		}
	}
	if err := a.known.write(); err != nil {
		return err
	}
	if total == 0 {
		return nil
	}
	msg := fmt.Sprintf("Moved %d of %d names on the lists to Xbox accounts", moved, total)
	if len(held) > 0 {
		msg += fmt.Sprintf("; %d stay held by name until they join: %s", len(held), strings.Join(held, ", "))
	}
	a.logger().Info(msg + ".")
	if moved > 0 {
		a.logger().Warn("The names were moved to the accounts by the XUIDs saved with the data of the players. If this server ever ran with AuthEnabled = false, those XUIDs are what clients claimed: review the lists. The files before the move are kept as *" + backupSuffix + ".")
	}
	return nil
}

// backup copies the file at path to path+".before-xuid", unless that file
// exists or path does not.
func backup(path string) error {
	dst := path + backupSuffix
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	src, err := os.Open(path)
	if errNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("keep %s: %w", path, err)
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("keep %s: %w", path, err)
	}
	if err := writeFileAtomic(dst, data, "copy of "+path); err != nil {
		return errors.Unwrap(err)
	}
	return nil
}
