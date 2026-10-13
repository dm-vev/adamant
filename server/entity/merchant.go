package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// MerchantOffer is one durable villager trade. Input and SecondaryInput are
// the required item stacks, Output is the result, Uses and MaxUses control
// exhaustion, and Demand/SpecialPrice carry Java's price modifiers.
type MerchantOffer struct {
	Input           item.Stack
	SecondaryInput  item.Stack
	Output          item.Stack
	Uses            int
	MaxUses         int
	XP              int
	RewardXP        bool
	Demand          int
	SpecialPrice    int
	Tier            int
	PriceMultiplier float32
}

// Merchant exposes the state required by a trading UI. Implementations must
// apply trade side effects only from CompleteMerchantTrade, after the session
// has validated and reserved all inventory transfers for the same offer.
type Merchant interface {
	// MerchantName is the title of the trade screen, usually a Bedrock
	// translation key such as entity.villager.farmer.
	MerchantName() string
	MerchantProfession() string
	MerchantLevel() int
	MerchantOffers() ([]MerchantOffer, error)
	MerchantOffersFor(user world.Entity) ([]MerchantOffer, error)
	CompleteMerchantTrade(tx *world.Tx, user world.Entity, offer int) (bool, error)
}

// TradingMerchant is a Merchant that serves one player at a time, like Java's
// AbstractVillager trading player.
type TradingMerchant interface {
	Merchant
	// StartTrading claims the merchant for user and reports whether it was
	// free to trade with them.
	StartTrading(user *world.EntityHandle) bool
	// StopTrading releases the merchant if user is trading with it.
	StopTrading(user *world.EntityHandle)
}

// tradingPlayer tracks the player trading with a merchant. Java's
// TradeWithPlayerGoal ends the trade once the player is gone or more than
// four blocks away.
type tradingPlayer struct {
	user *world.EntityHandle
}

func (t *tradingPlayer) start(user *world.EntityHandle) bool {
	if t.user != nil && t.user != user {
		return false
	}
	t.user = user
	return true
}

func (t *tradingPlayer) stop(user *world.EntityHandle) {
	if t.user == user {
		t.user = nil
	}
}

// tick returns the trading player, ending the trade when it is no longer in
// range of the merchant at pos.
func (t *tradingPlayer) tick(tx *world.Tx, pos mgl64.Vec3) (world.Entity, bool) {
	if t.user == nil {
		return nil, false
	}
	user, ok := t.user.Entity(tx)
	if ok {
		if l, living := user.(Living); living && l.Dead() {
			ok = false
		}
	}
	if !ok || user.Position().Sub(pos).LenSqr() > 16 {
		t.user = nil
		return nil, false
	}
	return user, true
}

// faceTowards returns rot turned to look from pos at target.
func faceTowards(rot cube.Rotation, pos, target mgl64.Vec3) cube.Rotation {
	d := target.Sub(pos)
	horizontal := math.Hypot(d[0], d[2])
	if horizontal == 0 && d[1] == 0 {
		return rot
	}
	return cube.Rotation{math.Atan2(-d[0], d[2]) * 180 / math.Pi, -math.Atan2(d[1], horizontal) * 180 / math.Pi}
}
