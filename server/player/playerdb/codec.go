package playerdb

import (
	"encoding/json"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// Encode encodes the data of a player in the world w as the record a
// Provider stores: the JSON of its fields. Other stores, as the world
// database of mcdb, keep the same records.
func Encode(d player.Config, w *world.World) ([]byte, error) {
	return json.Marshal(toJson(d, w))
}

// Decode decodes a record Encode returned, of the player keyed id: the
// player's data, and the world it is in, which world returns for the
// dimension of the record. A record without a UUID is of the player id.
func Decode(record []byte, id uuid.UUID, world func(world.Dimension) *world.World) (player.Config, *world.World, error) {
	var d jsonData
	if err := json.Unmarshal(record, &d); err != nil {
		return player.Config{}, nil, err
	}
	conf, w := fromJson(d, world, id)
	return conf, w, nil
}

// Identity decodes only the name and the XUID a record Encode returned
// holds, as Provider.Players lists them.
func Identity(record []byte) (name, xuid string, err error) {
	var d struct{ XUID, Username string }
	if err := json.Unmarshal(record, &d); err != nil {
		return "", "", err
	}
	return d.Username, d.XUID, nil
}
