package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
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
			if got := calls[0].Args[0].(text.Text).Plain(); got != "You are banned from this server.\nReason: griefing" {
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

// TestBanIPOfPlayerOnline checks /ban-ip of a player online: the address
// of the player is banned, the output names every player online from it,
// and each is kicked.
func TestBanIPOfPlayerOnline(t *testing.T) {
	a := newAuditTest(t)
	_, alt1 := a.join(t, "Alt1")
	_, alt2 := a.join(t, "Alt2")
	out := &cmd.Output{}
	<-a.srv.World().Exec(func(tx *world.Tx) { cmd.ExecuteLine(consoleLike{out}, "/ban-ip Alt1 alts", tx, nil) })
	var lines []string
	for _, m := range out.Messages() {
		lines = append(lines, m.String())
	}
	if got := strings.Join(lines, "\n"); got != "Banned IP 203.0.113.9: alts\nThis ban affects 2 player(s): Alt1, Alt2" {
		t.Errorf("/ban-ip Alt1: %q, errors %v", got, out.Errors())
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(alt1.Recorder().CallsTo("Disconnect")) == 0 || len(alt2.Recorder().CallsTo("Disconnect")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the players of the address were not kicked")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
