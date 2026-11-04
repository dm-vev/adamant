package session_test

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// beaconRequestsScenario pays beacons of a pyramid of four levels and of
// one as TestGoldenMenuRequests compares them: a primary and a secondary
// effect, the same effect twice, no effects, and the payments the beacon
// refuses: a payment that is no payment, put in by the same request, which
// the refusal undoes; an unknown effect, an instant one and one no beacon
// gives; effects above the level of the pyramid and a secondary below
// level 4; and the beacon replaced under the open menu. A Destroy of the
// payment after a payment is ignored, after a refused one not reached.
// What each beacon has is recorded after each step that pays it.
func (s *scenario) beaconRequestsScenario() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	payment := func() v2193.StackRequestSlotInfo {
		return s.slot(v2193.ContainerBeaconPayment, uiBeaconPayment)
	}
	high, low := cube.Pos{2, -60, -2}, cube.Pos{-5, -60, 5}
	recordBeacon := func(pos cube.Pos) {
		s.step(fmt.Sprintf("the beacon at %v", pos), func(tx *world.Tx, _ *player.Player) {
			b, ok := tx.Block(pos).(block.Beacon)
			if !ok {
				fmt.Fprintf(&s.out, "no beacon: %T\n", tx.Block(pos))
				return
			}
			fmt.Fprintf(&s.out, "level %d, primary %T, secondary %T\n", b.Level(), b.Primary, b.Secondary)
		})
	}
	pay := func(heading string, pos cube.Pos, primary, secondary int32, accept bool) {
		s.t.Helper()
		actions := []v2193.StackRequestAction{
			&v2193.BeaconPaymentStackRequestAction{PrimaryEffect: primary, SecondaryEffect: secondary},
			&v2193.DestroyStackRequestAction{Count: 1, Source: payment()},
		}
		if accept {
			s.request(heading, nil, actions...)
		} else {
			s.refuse(heading, actions...)
		}
		recordBeacon(pos)
	}
	putIngot := func(from int) {
		s.t.Helper()
		s.request("an iron ingot put in the beacon", nil,
			&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, from), Destination: payment()})
	}

	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	s.give("iron ingots, an emerald and apples given",
		slotStack{0, item.NewStack(item.IronIngot{}, 16)},
		slotStack{1, item.NewStack(item.Emerald{}, 1)},
		slotStack{2, item.NewStack(item.Apple{}, 4)},
	)
	s.step("a beacon on four levels of iron and one on one level", func(tx *world.Tx, _ *player.Player) {
		for level := 1; level <= 4; level++ {
			for x := -level; x <= level; x++ {
				for z := -level; z <= level; z++ {
					tx.SetBlock(high.Add(cube.Pos{x, -level, z}), block.Iron{}, nil)
				}
			}
		}
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				tx.SetBlock(low.Add(cube.Pos{x, -1, z}), block.Iron{}, nil)
			}
		}
		tx.SetBlock(high, block.Beacon{}, nil)
		tx.SetBlock(low, block.Beacon{}, nil)
	})
	s.skip("the beacons find their pyramids", 80)
	recordBeacon(high)
	recordBeacon(low)

	s.clickBlock("the client opens the beacon of four levels", high)
	// The apples put in the payment slot by the request itself: the
	// refusal takes them out again.
	s.refuse("refused: an apple put in and paid for speed",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: payment()},
		&v2193.BeaconPaymentStackRequestAction{PrimaryEffect: 1},
		&v2193.DestroyStackRequestAction{Count: 1, Source: s.again(v2193.ContainerBeaconPayment, uiBeaconPayment)})
	recordBeacon(high)
	pay("refused: speed paid with nothing", high, 1, 0, false)
	putIngot(0)
	pay("refused: an unknown effect", high, 99, 0, false)
	pay("refused: a negative effect", high, -1, 0, false)
	pay("refused: instant health", high, 6, 0, false)
	pay("refused: slowness", high, 2, 0, false)
	pay("refused: speed and instant damage", high, 1, 7, false)
	pay("refused: speed and night vision", high, 1, 16, false)
	pay("speed and regeneration paid", high, 1, 10, true)
	putIngot(0)
	pay("strength twice paid", high, 5, 5, true)
	putIngot(0)
	pay("no effects paid: the beacon keeps its own", high, 0, 0, true)
	s.request("an emerald put in the beacon", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 1), Destination: payment()})
	pay("jump boost paid with an emerald", high, 8, 0, true)
	s.closeContainer("the client closes the beacon of four levels")

	s.clickBlock("the client opens the beacon of one level", low)
	putIngot(0)
	pay("refused: resistance of one level", low, 11, 0, false)
	pay("refused: strength of one level", low, 5, 0, false)
	pay("refused: regeneration of one level", low, 10, 0, false)
	pay("refused: haste and a secondary haste below level 4", low, 3, 3, false)
	pay("refused: no primary and a secondary below level 4", low, 0, 1, false)
	pay("haste paid", low, 3, 0, true)

	// The beacon replaced under the open menu: the payment slot is no
	// container of it any more.
	putIngot(0)
	s.place("the beacon replaced by stone", low, block.Stone{})
	pay("refused: speed with no beacon", low, 1, 0, false)
	s.place("a beacon put back", low, block.Beacon{})
	pay("refused: speed of a beacon put back, not yet of its pyramid", low, 1, 0, false)
	s.closeContainer("the client closes the beacon of one level")
	recordBeacon(high)
}
