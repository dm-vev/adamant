package server_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/text"
)

// TestBanKickCarriesReason checks that a player online who is banned is
// kicked with the text it would be refused with at login, the reason
// included, where the kick named no reason before.
func TestBanKickCarriesReason(t *testing.T) {
	a := newAuditTest(t)
	_, j := a.join(t, "Griefer")
	if ok, err := a.srv.Ban("Griefer", server.BanOptions{Reason: "griefing"}); ok != server.BanAdded || err != nil {
		t.Fatalf("Ban = %v, %v", ok, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if calls := j.Recorder().CallsTo("Disconnect"); len(calls) > 0 {
			if got := calls[0].Args[0].(text.Text).Plain(); got != "You are banned from this server. Reason: griefing" {
				t.Errorf("kicked with %q", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the banned player was not kicked")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
