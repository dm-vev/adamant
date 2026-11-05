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

// TestGoldenScenariosOfPrimary2225 runs the golden scenarios of
// scenariosOf2225 under the primary 2225, for a client of 2193 and one of
// 2225, each in a process of its own, against
// testdata/golden/primary2225/v2193 and v2225: the session speaks 2225 and
// uses its features, as PrimaryProtocol = "1.26.60" has it.
func TestGoldenScenariosOfPrimary2225(t *testing.T) {
	if os.Getenv("GOLDEN_CHILD") != "" {
		return
	}
	readGoldenSnapshots(t)
	for _, client := range []string{"2193", "2225"} {
		for _, name := range scenariosOf2225 {
			t.Run(client+"/"+name, func(t *testing.T) {
				args := []string{"-test.run=^" + name + "$", "-test.count=1", "-test.timeout=5m"}
				if *updateSnapshots {
					args = append(args, "-update-snapshots")
				}
				cmd := exec.Command(os.Args[0], args...)
				cmd.Env = append(os.Environ(), "GOLDEN_CHILD="+name, "GOLDEN_PRIMARY=2225")
				if client == "2225" {
					cmd.Env = append(cmd.Env, "GOLDEN_PROTOCOL=2225")
				}
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s for %s under the primary 2225 in a process of its own: %v\n%s", name, client, err, out)
				}
			})
		}
	}
}

// serverScenariosOfPrimary2225 are the golden scenarios of a server.Server
// that TestGoldenServerScenariosOfPrimary2225 records.
var serverScenariosOfPrimary2225 = []string{"TestGoldenServerJoin", "TestGoldenServerLogin", "TestGoldenServerOperator"}

// TestGoldenServerScenariosOfPrimary2225 runs the golden scenarios of a
// server.Server of serverScenariosOfPrimary2225 under the default config,
// which allows both protocols and speaks the newest, 2225, for a client of
// 2193 and one of 2225, each in a process of its own, against
// testdata/golden/primary2225/v2193 and v2225: a member joins, the logins
// are refused or taken, and an operator joins, each with the commands of its
// level.
func TestGoldenServerScenariosOfPrimary2225(t *testing.T) {
	if os.Getenv("GOLDEN_CHILD") != "" {
		return
	}
	readGoldenSnapshots(t)
	for _, client := range []string{"2193", "2225"} {
		for _, name := range serverScenariosOfPrimary2225 {
			t.Run(client+"/"+name, func(t *testing.T) {
				args := []string{"-test.run=^" + name + "$", "-test.count=1", "-test.timeout=5m"}
				if *updateSnapshots {
					args = append(args, "-update-snapshots")
				}
				cmd := exec.Command(os.Args[0], args...)
				cmd.Env = append(os.Environ(), "GOLDEN_CHILD="+name, "GOLDEN_PRIMARY=2225")
				if client == "2225" {
					cmd.Env = append(cmd.Env, "GOLDEN_PROTOCOL=2225")
				}
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s for %s under the default config in a process of its own: %v\n%s", name, client, err, out)
				}
			})
		}
	}
}
