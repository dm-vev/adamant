package player

import (
	"os"
	"testing"

	"github.com/df-mc/dragonfly/internal/testfixture"
	"github.com/df-mc/dragonfly/server/world"
)

// TestMain makes every wait for a world owner from a world owner end the
// tests with its stack (world.PanicOnOwnerWait), so that none hides behind a
// task that recovers it, and fails the tests when code they run recovers a
// panic that none of them expects: a test that panics in a transaction would
// stop there and still pass.
func TestMain(m *testing.M) {
	world.PanicOnOwnerWait = true
	os.Exit(testfixture.RunCheckingPanics(m))
}
