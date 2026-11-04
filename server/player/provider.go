package player

import (
	"errors"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"io"
)

// Provider represents a value that may provide data to a Player value. It usually does the reading and
// writing of the player data so that the Player may use it.
type Provider interface {
	// Save is called when the player leaves the server. The Data of the player is passed.
	Save(uuid uuid.UUID, data Config, w *world.World) error
	// Load is called when the player joins and passes the UUID of the player.
	// It expects to the player data, and an error that is nil if the player data could be found. If non-nil, the player
	// will use default values, and you can use an empty Data struct.
	Load(uuid uuid.UUID, world func(world.Dimension) *world.World) (Config, *world.World, error)
	// Closer is used on server close when the server calls Provider.Close() and is used to safely close the Provider.
	io.Closer
}

// Lister is a Provider that can list the players it holds records of: the
// UUID each is keyed by, and the name and the XUID the record holds. The
// XUID of a record saved on a server with Xbox Live sign-in off is what the
// client claimed.
type Lister interface {
	// Players calls yield for each record, until yield returns false.
	Players(yield func(id uuid.UUID, name, xuid string) bool) error
}

// Compile time check to make sure NopProvider implements Provider.
var _ Provider = (*NopProvider)(nil)

// NopProvider is a player data provider that won't store any data and instead always return default values
type NopProvider struct{}

func (NopProvider) Save(uuid.UUID, Config, *world.World) error { return nil }
func (NopProvider) Load(uuid.UUID, func(world.Dimension) *world.World) (Config, *world.World, error) {
	return Config{}, nil, errors.New("")
}
func (NopProvider) Close() error { return nil }
