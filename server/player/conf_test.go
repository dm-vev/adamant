package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world"
)

func TestConfigPortalTravelCooldown(t *testing.T) {
	data := &world.EntityData{}
	Config{}.Apply(data)
	if got := data.Data.(*playerData).portalTravel.Cooldown; got != 15*time.Second {
		t.Fatalf("unexpected portal cooldown: %v", got)
	}
}
