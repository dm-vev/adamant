package server

import (
	"errors"
	"fmt"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// dataKey returns the UUID the server keeps the data of the player of an
// identity by, its record and its spawn points: on a server keyed by
// XUID, the UUID of the login, which Xbox Live signs and which follows
// from the account; on one keyed by name, the UUID of the name,
// identity.OfflineUUID, whatever UUID the client sends. A client without
// Xbox Live chooses its UUID, may send another at each login, or that of
// another player.
func (srv *Server) dataKey(id client.Identity) uuid.UUID {
	if srv.IdentityKey() == identity.XUID {
		return id.UUID
	}
	return identity.OfflineUUID(id.Name)
}

// online returns the handle of the player online that the identity passed
// logs in as: the player of its UUID, which the server and its clients tell
// players apart by; on a server keyed by name, the player of its name, in
// any case; on one keyed by XUID, the player of its account. One identity
// is online once.
func (srv *Server) online(id client.Identity) (*world.EntityHandle, bool) {
	if h, ok := srv.Player(id.UUID); ok {
		return h, true
	}
	if srv.IdentityKey() == identity.XUID {
		if k, ok := accountKey(id); ok {
			x, _ := k.XUID()
			return srv.players.byXUID(x)
		}
		return nil, false
	}
	if p, ok := srv.players.named(id.Name); ok {
		return p.handle, true
	}
	return nil, false
}

// errNoRecord is the error of a load that found no record of the player.
var errNoRecord = errors.New("no record")

// loadPlayer loads the record of the player of the identity passed, by its
// data key. On a server keyed by name with no record by the name, it
// loads the record of the UUID the client sent, as the server kept the
// records before it keyed them by name, if that record is of the name:
// a client that sends the UUID of another player does not get its data. It
// reports whether it took the record of the UUID of the client.
func (srv *Server) loadPlayer(id client.Identity, dim func(world.Dimension) *world.World) (player.Config, *world.World, bool, error) {
	key := srv.dataKey(id)
	d, w, err := srv.conf.PlayerProvider.Load(key, dim)
	if err == nil || key == id.UUID {
		return d, w, false, err
	}
	old, ow, oerr := srv.conf.PlayerProvider.Load(id.UUID, dim)
	if oerr != nil {
		return d, w, false, err
	}
	if identity.Normalize(old.Name) != identity.Normalize(id.Name) {
		srv.conf.Log.Info(fmt.Sprintf("The client of %s sent the UUID of the data of %s: not loaded.", id.Name, old.Name), "client-uuid", id.UUID)
		return player.Config{}, nil, false, errNoRecord
	}
	srv.conf.Log.Info(fmt.Sprintf("Moved the data of %s from the UUID its client sent.", identity.Normalize(id.Name)), "uuid", key, "client-uuid", id.UUID)
	return old, ow, true, nil
}

// moveSpawn copies the spawn point the world keeps by the UUID the client
// of a player sent to its data key, unless it has one there, as the record
// of the player moved.
func (srv *Server) moveSpawn(id client.Identity) {
	key, p := srv.dataKey(id), srv.conf.WorldProvider
	if _, ok, err := p.LoadPlayerSpawnPosition(key); ok || err != nil {
		return
	}
	pos, ok, err := p.LoadPlayerSpawnPosition(id.UUID)
	if !ok || err != nil {
		return
	}
	srv.world.SetPlayerSpawn(key, pos)
}
