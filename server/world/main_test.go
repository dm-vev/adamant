package world

import (
	"os"
	"testing"
)

// TestMain makes every wait for a world owner from a world owner end the
// tests with its stack (PanicOnOwnerWait), so that none hides behind a task
// that recovers it.
func TestMain(m *testing.M) {
	PanicOnOwnerWait = true
	os.Exit(m.Run())
}
