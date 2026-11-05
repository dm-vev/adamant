package server

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/version"
)

// versionListener is a Listener that tells the versions of its clients.
type versionListener struct {
	Listener
	primary  string
	joinable []string
}

func (l versionListener) GameVersions() (string, []string) { return l.primary, l.joinable }

// TestGameVersions checks what the server tells of the versions it speaks:
// the release it implements for no listener that knows, and the primary and
// the clients of the first listener that does.
func TestGameVersions(t *testing.T) {
	srv := &Server{}
	if primary, joinable := srv.GameVersions(); primary != version.Minecraft || joinable != nil {
		t.Errorf("GameVersions() of a server with no listener = %q, %v, want %q, none", primary, joinable, version.Minecraft)
	}
	if got, want := srv.versionDescription(), version.Minecraft; got != want {
		t.Errorf("versionDescription() = %q, want %q", got, want)
	}

	srv.listeners = []Listener{nil, versionListener{primary: "1.26.60", joinable: []string{"26.50–26.52", "26.60–26.61"}}}
	if primary, joinable := srv.GameVersions(); primary != "1.26.60" || !slices.Equal(joinable, []string{"26.50–26.52", "26.60–26.61"}) {
		t.Errorf("GameVersions() = %q, %v", primary, joinable)
	}
	if got, want := srv.versionDescription(), "1.26.60; clients 26.50–26.52, 26.60–26.61"; got != want {
		t.Errorf("versionDescription() = %q, want %q", got, want)
	}
}
