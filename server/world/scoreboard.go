package world

import (
	"encoding/json"
	"errors"

	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/goleveldb/leveldb"
)

const scoreboardNamespace = "scoreboard"

// Scoreboard returns the world's shared scoreboard model, loading it from the provider on first use.
func (w *World) Scoreboard() *scoreboard.Model {
	if w == nil {
		return scoreboard.NewModel()
	}
	w.scoreboardOnce.Do(func() {
		model := scoreboard.NewModel()
		data, err := w.LoadWorldData(scoreboardNamespace)
		if err == nil {
			if decodeErr := json.Unmarshal(data, model); decodeErr != nil {
				w.conf.Log.Error("load scoreboard: " + decodeErr.Error())
			}
		} else if !errors.Is(err, leveldb.ErrNotFound) {
			w.conf.Log.Error("load scoreboard: " + err.Error())
		}
		w.scoreboardModel = model
	})
	return w.scoreboardModel
}

// encodeScoreboard encodes the scoreboard of the world for a save, a new
// one if it was never used.
func (w *World) encodeScoreboard() ([]byte, error) {
	w.scoreboardOnce.Do(func() { w.scoreboardModel = scoreboard.NewModel() })
	return json.Marshal(w.scoreboardModel)
}
