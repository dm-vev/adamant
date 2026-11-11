package session

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSessionNBTPayloadsAreDeterministic encodes the payloads the session
// sends as NBT built from a map before many times: the encoder writes the
// keys of a map in random order, so the payloads are structs, and the bytes
// of each are the same every time and are pinned. The merchant offers hold
// no item stacks: those are the maps item.WriteNBT gives, which the encoder
// still writes in random order.
func TestSessionNBTPayloadsAreDeterministic(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		encode func(t *testing.T) []byte
		want   string
	}{
		{"sculk charge", func(*testing.T) []byte {
			return sculkChargePacket(mgl64.Vec3{1.5, 64, -2.5}, particle.SculkCharge{Charge: 3, Faces: 5}).SerialisedEventData
		}, "03017802030179800103017a05020663686172676503000206666163696e670500"},
		{"sculk charge pop", func(*testing.T) []byte {
			return sculkChargePacket(mgl64.Vec3{1.5, 64, -2.5}, particle.SculkChargePop{}).SerialisedEventData
		}, "03017802030179800103017a05"},
		{"sleeping players", func(*testing.T) []byte {
			return sleepingPlayersPacket(2, 5).SerialisedEventData
		}, "030b61626c65546f536c6565700a03146f766572776f726c64506c61796572436f756e740a0313736c656570696e67506c61796572436f756e7404"},
		{"merchant offers", func(t *testing.T) []byte {
			data, err := serializeMerchantOffers([]entity.MerchantOffer{
				{MaxUses: 16, Uses: 2, Tier: 1, XP: 5, Demand: 3, PriceMultiplier: .05},
				{RewardXP: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			return data
		}, "0a000907526563697065730a0403056e65744964020a0462757941000a0462757942000309627579436f756e7441000309627579436f756e7442000a0473656c6c000304757365730403076d617855736573200109726577617264457870000304746965720203097472616465724578700a051070726963654d756c7469706c69657241cdcc4c3d051070726963654d756c7469706c6965724200000000030664656d616e64060003056e65744964040a0462757941000a0462757942000309627579436f756e7441000309627579436f756e7442000a0473656c6c000304757365730003076d6178557365730001097265776172644578700103047469657200030974726164657245787000051070726963654d756c7469706c6965724100000000051070726963654d756c7469706c6965724200000000030664656d616e640000091354696572457870526571756972656d656e74730a0a030130000003013114000301328c0100030133ac0200030134f4030000"},
	} {
		t.Run(test.name, func(t *testing.T) {
			first := test.encode(t)
			for range 200 {
				if got := test.encode(t); !bytes.Equal(got, first) {
					t.Fatalf("bytes changed between encodes:\n%x\n%x", first, got)
				}
			}
			if got := hex.EncodeToString(first); got != test.want {
				t.Fatalf("bytes = %s, want %s", got, test.want)
			}
		})
	}
}
