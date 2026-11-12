package session

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/internal/testfixture"
)

// TestMain fails the tests when code they run recovers a panic that none of
// them expects, as a transaction that panics does: the test would stop there
// and still pass.
func TestMain(m *testing.M) {
	os.Exit(testfixture.RunCheckingPanics(m, encoderPanicOfTest))
}

// encoderPanicOfTest allows the panic of the encoder of a game rule that has
// no type, which TestSessionDropsPacketThatDoesNotEncode queues to see the
// session drop a packet that does not encode.
func encoderPanicOfTest(r faultcheck.Recovered) bool {
	return r.Site == "session packet encode" && strings.HasSuffix(fmt.Sprint(r.Value), "game rule value of no type")
}
