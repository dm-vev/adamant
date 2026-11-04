package server

import (
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/google/uuid"
)

// TestNPCNamedLikeAnOperatorIsNot checks that the operator check of the
// commands, the game master blocks and the abilities goes by the player
// online, not by a name: an entity of a plugin named after an operator is
// none, nor is the operator once offline, while the operator online is, in
// each mode.
func TestNPCNamedLikeAnOperatorIsNot(t *testing.T) {
	byName := adminConfig(t, filepath.Join(t.TempDir(), "white-list.txt"), false)
	byXUID, _ := xuidConfig(t, filepath.Join(t.TempDir(), "white-list.txt"), false)
	for _, tc := range []struct {
		mode string
		srv  *Server
		id   client.Identity
	}{
		{"name", byName, client.Identity{Name: "Steve", UUID: uuid.New()}},
		{"xuid", byXUID, account("Steve", steveXUID)},
	} {
		if _, err := tc.srv.Op("Steve"); err != nil {
			t.Fatal(err)
		}
		if tc.mode == "xuid" {
			if _, ok := tc.srv.conf.Allower.Allow(nil, tc.id, client.Profile{}); !ok {
				t.Fatal("Steve is refused")
			}
		}
		if tc.srv.PlayerIsOperator(tc.id.UUID) {
			t.Errorf("%s: Steve is an operator before he is online", tc.mode)
		}
		online(tc.srv, tc.id)
		if !tc.srv.PlayerIsOperator(tc.id.UUID) {
			t.Errorf("%s: Steve online is not an operator", tc.mode)
		}
		if tc.srv.PlayerIsOperator(uuid.New()) {
			t.Errorf("%s: an entity named Steve that is no player online is an operator", tc.mode)
		}
		tc.srv.players.remove(tc.id.UUID)
		if tc.srv.PlayerIsOperator(tc.id.UUID) {
			t.Errorf("%s: Steve is an operator once offline", tc.mode)
		}
	}
}
