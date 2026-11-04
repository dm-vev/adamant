package server

import (
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestMain makes every wait for a world owner from a world owner end the
// tests with its stack (world.PanicOnOwnerWait), so that none hides behind a
// task that recovers it.
func TestMain(m *testing.M) {
	world.PanicOnOwnerWait = true
	os.Exit(m.Run())
}
