package overworld

import (
	"os"
	"testing"

	"github.com/dm-vev/adamant/internal/testfixture"
)

// TestMain fails the tests when code they run recovers a panic that none of
// them expects, as a transaction that panics does: the test would stop there
// and still pass.
func TestMain(m *testing.M) {
	os.Exit(testfixture.RunCheckingPanics(m))
}
