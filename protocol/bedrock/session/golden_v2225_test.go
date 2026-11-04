package session_test

import (
	"os"
	"os/exec"
	"testing"
)

// scenariosOf2225 are the golden scenarios of the session that
// TestGoldenScenariosOf2225 records for a client of 2225.
var scenariosOf2225 = []string{
	"TestGoldenJoin", "TestGoldenChunks", "TestGoldenWorld", "TestGoldenPlayerState", "TestGoldenText",
	"TestGoldenInventory", "TestGoldenMenus", "TestGoldenMultiCraft", "TestGoldenObserved", "TestGoldenRefusals",
}

// TestGoldenScenariosOf2225 runs the golden scenarios of the session for a
// client of 2225 under the primary 2193, each in a process of its own
// whose goldenProtocol is 2225, against testdata/golden/v2225: the same
// scenes, the session's packets as they go to such a client, of the model
// as they are, with 2225's tables of content.
func TestGoldenScenariosOf2225(t *testing.T) {
	if os.Getenv("GOLDEN_CHILD") != "" {
		return
	}
	readGoldenSnapshots(t)
	for _, name := range scenariosOf2225 {
		t.Run(name, func(t *testing.T) {
			args := []string{"-test.run=^" + name + "$", "-test.count=1", "-test.timeout=5m"}
			if *updateSnapshots {
				args = append(args, "-update-snapshots")
			}
			cmd := exec.Command(os.Args[0], args...)
			cmd.Env = append(os.Environ(), "GOLDEN_CHILD="+name, "GOLDEN_PROTOCOL=2225")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s for 2225 in a process of its own: %v\n%s", name, err, out)
			}
		})
	}
}
