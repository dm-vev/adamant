package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
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

func TestCommandBlockMinecartContainerNBTRoundTrip(t *testing.T) {
	data := &world.EntityData{}
	(CommandBlockMinecartConfig{SuccessCount: 7}).Apply(data)
	before := data.Data.(*commandBlockMinecartBehaviour)
	if err := before.inv.SetItem(0, item.NewStack(item.Diamond{}, 37)); err != nil {
		t.Fatal(err)
	}
	encoded := CommandBlockMinecartType.EncodeNBT(data)
	decoded := &world.EntityData{}
	CommandBlockMinecartType.DecodeNBT(encoded, decoded)
	after := decoded.Data.(*commandBlockMinecartBehaviour)
	stack, err := after.inv.Item(0)
	if err != nil || after.inv.Size() != 1 || stack.Count() != 37 || !stack.Comparable(item.NewStack(item.Diamond{}, 1)) || after.successCount != 7 {
		t.Fatalf("command-cart container was not preserved: stack=%v, success=%d, error=%v", stack, after.successCount, err)
	}
}
