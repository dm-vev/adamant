package v2225

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestGophertunnelSpeaks2225 checks the module pins a gophertunnel that
// speaks protocol 2225.
func TestGophertunnelSpeaks2225(t *testing.T) {
	if protocol.CurrentProtocol != 2225 || protocol.CurrentVersion != "1.26.60" {
		t.Fatalf("gophertunnel speaks %d (%s), not 2225", protocol.CurrentProtocol, protocol.CurrentVersion)
	}
}
