package playerdb

import (
	"errors"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/google/uuid"
)

// Records is a database that keeps the records of players beside a world,
// as mcdb.DB keeps them under adamant:player:<UUID>.
type Records interface {
	// LoadPlayer returns the record of the player keyed id, or an error
	// satisfying errors.Is(err, leveldb.ErrNotFound) if there is none.
	LoadPlayer(id uuid.UUID) ([]byte, error)
	// SavePlayer saves the record of the player keyed id.
	SavePlayer(id uuid.UUID, record []byte) error
	// PlayerRecords calls yield with each record, until yield returns
	// false.
	PlayerRecords(yield func(id uuid.UUID, record []byte) bool) error
}

// WorldProvider is a player.Provider and player.Lister that keeps the
// records of players, as Provider encodes them, in the database of their
// world, so that the players are saved with the world they play in. A
// WorldProvider with a fallback reads a record the database does not hold
// from it: the provider the records were kept by before, the players folder
// of a Provider opened with OpenReadOnly. The next save of the player
// writes the record into the database, and the fallback is never written.
type WorldProvider struct {
	db       Records
	fallback player.Provider
}

var (
	_ player.Provider = (*WorldProvider)(nil)
	_ player.Lister   = (*WorldProvider)(nil)
)

// NewWorldProvider returns the WorldProvider that keeps the records of
// players in db. fallback, if not nil, is read for a player without a
// record in db; Close closes it, but not db, which its worlds close.
func NewWorldProvider(db Records, fallback player.Provider) *WorldProvider {
	return &WorldProvider{db: db, fallback: fallback}
}

// Save saves the record of the player keyed id, in the world w, to the
// database.
func (p *WorldProvider) Save(id uuid.UUID, d player.Config, w *world.World) error {
	b, err := Encode(d, w)
	if err != nil {
		return err
	}
	return p.db.SavePlayer(id, b)
}

// Load loads the record of the player keyed id from the database, or else
// from the fallback. It returns an error satisfying errors.Is(err,
// leveldb.ErrNotFound) if neither holds one.
func (p *WorldProvider) Load(id uuid.UUID, world func(world.Dimension) *world.World) (player.Config, *world.World, error) {
	b, err := p.db.LoadPlayer(id)
	switch {
	case err == nil:
		return Decode(b, id, world)
	case !errors.Is(err, leveldb.ErrNotFound) || p.fallback == nil:
		return player.Config{}, nil, err
	}
	return p.fallback.Load(id, world)
}

// Players calls yield with the UUID, the name and the XUID of each record,
// those of the database first, then those of the fallback, if it is a
// player.Lister, without a record in the database, until yield returns
// false. A record that does not decode is skipped.
func (p *WorldProvider) Players(yield func(id uuid.UUID, name, xuid string) bool) error {
	held := map[uuid.UUID]bool{}
	stopped := false
	err := p.db.PlayerRecords(func(id uuid.UUID, record []byte) bool {
		name, xuid, err := Identity(record)
		if err != nil {
			return true
		}
		held[id] = true
		stopped = !yield(id, name, xuid)
		return !stopped
	})
	if err != nil || stopped {
		return err
	}
	lister, ok := p.fallback.(player.Lister)
	if !ok {
		return nil
	}
	return lister.Players(func(id uuid.UUID, name, xuid string) bool {
		return held[id] || yield(id, name, xuid)
	})
}

// Records returns the database the provider keeps its records in.
func (p *WorldProvider) Records() Records { return p.db }

// Close closes the fallback, if any.
func (p *WorldProvider) Close() error {
	if p.fallback == nil {
		return nil
	}
	return p.fallback.Close()
}
