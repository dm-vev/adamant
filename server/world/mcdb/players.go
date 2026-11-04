package mcdb

import (
	"strings"

	"github.com/google/uuid"
)

// keyPlayerPrefix prefixes the records of players the DB holds: the key of
// the record of a player is the prefix and the UUID the server keeps its
// data by, as text (adamant:player:6d1c62f5-1e30-4a4b-9a43-6f4b8bd0a7a1).
// The DB holds a record as it is given: playerdb.WorldProvider stores the
// JSON of playerdb.
const keyPlayerPrefix = "adamant:player:"

func playerKey(id uuid.UUID) []byte { return []byte(keyPlayerPrefix + id.String()) }

// LoadPlayer returns the record of the player keyed id, as the commit open
// leaves it, if any. If the DB holds none, errors.Is(err,
// leveldb.ErrNotFound) is true.
func (db *DB) LoadPlayer(id uuid.UUID) ([]byte, error) {
	var g getter = ldbGetter{db}
	db.cmu.Lock()
	if c := db.open; c != nil {
		g = c
	}
	db.cmu.Unlock()
	return g.get(playerKey(id))
}

// SavePlayer saves record as the record of the player keyed id: into the
// commit open, if any (DB.BeginCommit), which a checkpoint saves its
// players in, and at once otherwise. A save waits for a commit being
// written. A record saved into a commit that then fails, or is discarded,
// is written again at once.
func (db *DB) SavePlayer(id uuid.UUID, record []byte) error {
	key := playerKey(id)
	db.cmu.Lock()
	defer db.cmu.Unlock()
	if c := db.open; c != nil {
		c.put(key, record)
		c.mu.Lock()
		c.players[string(key)] = true
		c.mu.Unlock()
		return nil
	}
	return db.putDirect(key, record)
}

// PlayerRecords calls yield with the key and the record of each player the
// DB holds a record of, until yield returns false.
func (db *DB) PlayerRecords(yield func(id uuid.UUID, record []byte) bool) error {
	var s store = db.direct()
	db.cmu.Lock()
	if c := db.open; c != nil {
		s = c
	}
	db.cmu.Unlock()
	var keys [][]byte
	if err := s.keys([]byte(keyPlayerPrefix), func(key []byte) { keys = append(keys, key) }); err != nil {
		return err
	}
	for _, key := range keys {
		id, err := uuid.Parse(strings.TrimPrefix(string(key), keyPlayerPrefix))
		if err != nil {
			continue
		}
		record, err := s.get(key)
		if err != nil {
			continue
		}
		if !yield(id, record) {
			break
		}
	}
	return nil
}
