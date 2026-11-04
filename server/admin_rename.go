package server

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/identity"
)

// followRename gives the entries the lists hold of the account of the key
// passed the name it joined with, writing each list that changes, and
// logs a rename: an Xbox account may change its gamertag, and the lists
// show the name it has. previous is the name the known players had for
// the account, "" if none; an entry that held another name counts too, and
// an entry of an account named by XUID alone takes its first name without
// a rename.
func (srv *Server) followRename(k identity.Key, name, previous string) {
	name = strings.TrimSpace(name)
	type list interface {
		update(f func(e *entries) bool) error
	}
	var lists []list
	if srv.operators != nil {
		lists = append(lists, srv.operators)
	}
	if srv.whitelist != nil {
		lists = append(lists, srv.whitelist)
	}
	if srv.bans != nil {
		lists = append(lists, srv.bans)
	}
	for _, l := range lists {
		err := l.update(func(e *entries) bool {
			en, ok := e.get(k)
			if !ok || en.name == name {
				return false
			}
			if previous == "" {
				previous = en.name
			}
			en.name = name
			e.put(en)
			return true
		})
		if err != nil {
			srv.conf.Log.Error("Write the list of a renamed account: "+err.Error(), "account", k.String())
		}
	}
	if previous != "" && previous != name {
		srv.conf.Log.Info(fmt.Sprintf("%s renamed: %s → %s.", k, previous, name))
	}
}
