package builtin

import (
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/world"
)

// statusTestAdapter is a server with no players.
type statusTestAdapter struct{ serverAdapter }

func (statusTestAdapter) StartTime() time.Time          { return time.Now() }
func (statusTestAdapter) Online() []server.OnlinePlayer { return nil }
func (statusTestAdapter) MaxPlayerCount() int           { return 0 }

// TestStatusTellsTheLiveHeap checks that the memory line of /status tells the
// heap the last collection found reachable beside the heap used, which
// includes garbage yet to be collected and so reads up to twice the load.
func TestStatusTellsTheLiveHeap(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	var lines []string
	<-w.Exec(func(tx *world.Tx) {
		for _, m := range runTestCommand(newStatusCommand(statusTestAdapter{}), "", tx).Messages() {
			lines = append(lines, m.String())
		}
	})
	for _, l := range lines {
		if strings.HasPrefix(l, "Memory: ") {
			if !strings.Contains(l, "of it live after the last GC") {
				t.Errorf("the memory line is %q, want the live heap in it", l)
			}
			return
		}
	}
	t.Errorf("no memory line in the status: %q", lines)
}
