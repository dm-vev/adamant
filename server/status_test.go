package server

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/player"
)

// TestServerStatus checks the Status and the PlayerNames of a server with
// two players, with the default status and with one that sets every field.
func TestServerStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		provider StatusProvider
		want     Status
	}{
		"default": {nil, Status{Name: "Status Test", PlayerCount: 2, MaxPlayers: 3}},
		"custom":  {customStatus{}, Status{Name: "Name", SubName: "Sub", PlayerCount: 3, MaxPlayers: 5}},
	} {
		t.Run(name, func(t *testing.T) {
			jt := newJoinTest(t, player.NopProvider{}, Config{Name: "Status Test", StatusProvider: tc.provider})
			jt.start()
			jt.join(t, joinLogin("Bob", ""))
			jt.join(t, joinLogin("Alice", ""))
			if got := jt.srv.Status(); got != tc.want {
				t.Errorf("Status() = %+v, want %+v", got, tc.want)
			}
			names := jt.srv.PlayerNames()
			slices.Sort(names)
			if !slices.Equal(names, []string{"Alice", "Bob"}) {
				t.Errorf("PlayerNames() = %v, want Alice and Bob", names)
			}
		})
	}
}

// customStatus is a StatusProvider that sets every field.
type customStatus struct{}

func (customStatus) ServerStatus(playerCount, maxPlayers int) Status {
	return Status{Name: "Name", SubName: "Sub", PlayerCount: playerCount + 1, MaxPlayers: maxPlayers + 2}
}
