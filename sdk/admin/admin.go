// Package admin provides server moderation helpers (bans, IP bans, operators,
// whitelist, save checkpoints) and offline player data access via the server's
// configured player.Provider.
package admin

import (
	"errors"

	"github.com/dm-vev/adamant/server"
	"github.com/dm-vev/adamant/server/item/inventory"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/google/uuid"
)

// BanOptions configures an expiring or attributed player/IP ban.
type BanOptions = server.BanOptions

// BanInfo describes an active ban entry.
type BanInfo = server.BanInfo

// BanChange describes the result of a Ban or BanAddress operation.
type BanChange = server.BanChange

// BanPlayer bans name with reason, kicking them if currently online.
func BanPlayer(srv *server.Server, name, reason string) (bool, error) {
	if srv == nil {
		return false, errors.New("sdk/admin: nil Server")
	}
	return srv.BanPlayer(name, reason)
}

// Ban bans name with full BanOptions (reason, source, expiry).
func Ban(srv *server.Server, name string, opts BanOptions) (BanChange, error) {
	if srv == nil {
		var zero BanChange
		return zero, errors.New("sdk/admin: nil Server")
	}
	return srv.Ban(name, opts)
}

// PardonPlayer lifts an active player ban for name.
func PardonPlayer(srv *server.Server, name string) (bool, error) {
	if srv == nil {
		return false, errors.New("sdk/admin: nil Server")
	}
	return srv.PardonPlayer(name)
}

// BanIP bans an IP address with reason.
func BanIP(srv *server.Server, ip, reason string) (bool, error) {
	if srv == nil {
		return false, errors.New("sdk/admin: nil Server")
	}
	return srv.BanIP(ip, reason)
}

// BanAddress bans an IP address with full BanOptions.
func BanAddress(srv *server.Server, ip string, opts BanOptions) (BanChange, error) {
	if srv == nil {
		var zero BanChange
		return zero, errors.New("sdk/admin: nil Server")
	}
	return srv.BanAddress(ip, opts)
}

// PardonIP lifts an active IP ban.
func PardonIP(srv *server.Server, ip string) (bool, error) {
	if srv == nil {
		return false, errors.New("sdk/admin: nil Server")
	}
	return srv.PardonIP(ip)
}

// BanList returns all active player bans (when ips is false) or IP bans (when ips is true).
func BanList(srv *server.Server, ips bool) ([]BanInfo, error) {
	if srv == nil {
		return nil, errors.New("sdk/admin: nil Server")
	}
	return srv.BanList(ips)
}

// Banned reports whether addr or name is currently banned, returning the reason.
func Banned(srv *server.Server, addr, name string) (string, bool) {
	if srv == nil {
		return "", false
	}
	return srv.Banned(addr, name)
}

// Op grants operator status to name.
func Op(srv *server.Server, name string) (bool, error) {
	if srv == nil {
		return false, errors.New("sdk/admin: nil Server")
	}
	return srv.Op(name)
}

// Deop revokes operator status from name.
func Deop(srv *server.Server, name string) (bool, error) {
	if srv == nil {
		return false, errors.New("sdk/admin: nil Server")
	}
	return srv.Deop(name)
}

// IsOperator reports whether name is an operator.
func IsOperator(srv *server.Server, name string) bool {
	return srv != nil && srv.IsOperator(name)
}

// Operators returns all configured operator entries.
func Operators(srv *server.Server) ([]string, error) {
	if srv == nil {
		return nil, errors.New("sdk/admin: nil Server")
	}
	return srv.OperatorEntries()
}

// WhitelistEnabled reports whether the server whitelist is currently active.
func WhitelistEnabled(srv *server.Server) bool {
	return srv != nil && srv.WhitelistEnabled()
}

// SetWhitelist enables or disables the server whitelist.
func SetWhitelist(srv *server.Server, enabled bool) error {
	if srv == nil {
		return errors.New("sdk/admin: nil Server")
	}
	return srv.WhitelistSetEnabled(enabled)
}

// WhitelistAdd adds name to the server whitelist.
func WhitelistAdd(srv *server.Server, name string) (bool, error) {
	if srv == nil {
		return false, errors.New("sdk/admin: nil Server")
	}
	return srv.WhitelistAdd(name)
}

// WhitelistRemove removes name from the server whitelist.
func WhitelistRemove(srv *server.Server, name string) (bool, error) {
	if srv == nil {
		return false, errors.New("sdk/admin: nil Server")
	}
	return srv.WhitelistRemove(name)
}

// WhitelistEntries returns all names currently on the whitelist.
func WhitelistEntries(srv *server.Server) ([]string, error) {
	if srv == nil {
		return nil, errors.New("sdk/admin: nil Server")
	}
	return srv.WhitelistEntries()
}

// SaveAll triggers a consistent checkpoint save across all worlds and players.
func SaveAll(srv *server.Server) error {
	if srv == nil {
		return errors.New("sdk/admin: nil Server")
	}
	return srv.SaveAll()
}

func defaultWorldLookup(srv *server.Server) func(world.Dimension) *world.World {
	return func(dim world.Dimension) *world.World {
		if srv == nil {
			return nil
		}
		switch dim {
		case world.Nether:
			return srv.Nether()
		case world.End:
			return srv.End()
		default:
			return srv.World()
		}
	}
}

// LoadOffline loads saved player.Config data for an offline player UUID from
// srv's PlayerProvider.
func LoadOffline(srv *server.Server, id uuid.UUID) (player.Config, *world.World, error) {
	if srv == nil || srv.PlayerProvider() == nil {
		return player.Config{}, nil, errors.New("sdk/admin: server has no PlayerProvider")
	}
	return srv.PlayerProvider().Load(id, defaultWorldLookup(srv))
}

// SaveOffline writes player.Config data for an offline player UUID back to
// srv's PlayerProvider. Nil inventories on data are initialized to empty
// defaults so callers do not need to allocate them manually.
func SaveOffline(srv *server.Server, id uuid.UUID, data player.Config, w *world.World) error {
	if srv == nil || srv.PlayerProvider() == nil {
		return errors.New("sdk/admin: server has no PlayerProvider")
	}
	if w == nil {
		w = srv.World()
	}
	if data.Inventory == nil {
		data.Inventory = inventory.New(36, nil)
	}
	if data.OffHand == nil {
		data.OffHand = inventory.New(1, nil)
	}
	if data.Armour == nil {
		data.Armour = inventory.NewArmour(nil)
	}
	if data.EnderChestInventory == nil {
		data.EnderChestInventory = inventory.New(27, nil)
	}
	return srv.PlayerProvider().Save(id, data, w)
}

// EditOffline loads an offline player's saved data (inventory, position, ECS
// components, etc.), runs fn to mutate it, and saves it back if fn returns nil.
func EditOffline(srv *server.Server, id uuid.UUID, fn func(data *player.Config, w **world.World) error) error {
	if fn == nil {
		return errors.New("sdk/admin: nil edit function")
	}
	data, w, err := LoadOffline(srv, id)
	if err != nil {
		return err
	}
	if err := fn(&data, &w); err != nil {
		return err
	}
	return SaveOffline(srv, id, data, w)
}
