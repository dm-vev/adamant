package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestCommandBlockMinecartNBTRoundTrip(t *testing.T) {
	input := map[string]any{
		"Command": "say minecart", "CustomName": "rail", "LastOutput": "ok", "TrackOutput": uint8(1),
		"SuccessCount": int32(3), "powered": uint8(1), "conditionMet": uint8(1), "TickDelay": int32(6),
		"ExecuteOnFirstTick": uint8(1), "LPCommandMode": int32(2), "LPCondionalMode": uint8(1),
		"LPRedstoneMode": uint8(1), "auto": uint8(1), "Version": int32(26),
	}
	data := &world.EntityData{}
	CommandBlockMinecartType.DecodeNBT(input, data)
	got := CommandBlockMinecartType.EncodeNBT(data)
	for key, want := range input {
		if got[key] != want {
			t.Errorf("%s = %#v, want %#v", key, got[key], want)
		}
	}
}
