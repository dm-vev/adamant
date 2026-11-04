package item_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// TestGameCodeLogsThroughTheWorld checks that an item logs what goes wrong
// through the logger of its world, not through slog.Default().
func TestGameCodeLogsThroughTheWorld(t *testing.T) {
	var logged bytes.Buffer
	w := world.Config{Synchronous: true, Log: slog.New(slog.NewTextHandler(&logged, nil))}.New()
	defer w.Close()
	w.Do(func(tx *world.Tx) {
		if (item.SpawnEgg{Entity: "minecraft:nothing"}).Dispense(tx, cube.Pos{0, 64, 0}, cube.FaceUp) {
			t.Error("an egg of no entity spawned one")
		}
	})
	if !strings.Contains(logged.String(), `msg="spawn_egg: unknown entity" entity=minecraft:nothing`) {
		t.Errorf("the world logged %q", logged.String())
	}
}
