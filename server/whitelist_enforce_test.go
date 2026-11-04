package server_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client/clienttest"
)

// kicked reports, within a few seconds, whether the client of j was kicked.
func kicked(j *clienttest.Join, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		if len(j.Recorder().CallsTo("Disconnect")) > 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestWhitelistEnforce checks [Whitelist] Enforce: turning the whitelist
// on kicks the players online it does not hold, but operators; reloading
// it kicks those it no longer holds; without Enforce, no one is kicked.
func TestWhitelistEnforce(t *testing.T) {
	a := newAuditTest(t, func(uc *server.UserConfig) { uc.Whitelist.Enforce = true })
	if _, err := a.srv.WhitelistAdd("Member"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.srv.Op("Op"); err != nil {
		t.Fatal(err)
	}
	_, member := a.join(t, "Member")
	_, op := a.join(t, "Op")
	_, stranger := a.join(t, "Stranger")
	if err := a.srv.WhitelistSetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !kicked(stranger, 10*time.Second) {
		t.Fatal("the stranger was not kicked as the whitelist turned on")
	}
	if kicked(member, 200*time.Millisecond) || kicked(op, 0) {
		t.Fatal("the member or the operator was kicked")
	}
	if _, err := a.srv.WhitelistRemove("Member"); err != nil {
		t.Fatal(err)
	}
	if err := a.srv.WhitelistReload(); err != nil {
		t.Fatal(err)
	}
	if !kicked(member, 10*time.Second) {
		t.Fatal("the member taken off the whitelist was not kicked as it reloaded")
	}

	b := newAuditTest(t)
	_, other := b.join(t, "Stranger")
	if err := b.srv.WhitelistSetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if kicked(other, 300*time.Millisecond) {
		t.Fatal("a server without Enforce kicked a player online")
	}
}
