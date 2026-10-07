package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// MerchantOffer is one durable villager trade. Input and SecondaryInput are
// the required item stacks, Output is the result, Uses and MaxUses control
// exhaustion, and Demand/SpecialPrice carry Java's price modifiers.
type MerchantOffer struct {
	Input          item.Stack
	SecondaryInput item.Stack
	Output         item.Stack
	Uses           int
	MaxUses        int
	XP             int
	RewardXP       bool
	Demand         int
	SpecialPrice   int
	Tier            int
	PriceMultiplier float32
}

// Merchant exposes the state required by a trading UI. Implementations must
// apply trade side effects only from CompleteMerchantTrade, after the session
// has validated and reserved all inventory transfers for the same offer.
type Merchant interface {
	MerchantName() string
	MerchantProfession() string
	MerchantLevel() int
	MerchantOffers() ([]MerchantOffer, error)
	MerchantOffersFor(user world.Entity) ([]MerchantOffer, error)
	CompleteMerchantTrade(tx *world.Tx, user world.Entity, offer int) (bool, error)
}
