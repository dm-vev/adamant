package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

func TestMerchantOfferCostA(t *testing.T) {
	tests := []struct {
		name                   string
		count, demand, special int
		multiplier             float32
		want                   int
	}{
		{"base price", 10, 0, 0, 0.05, 10},
		{"demand rounded down", 10, 5, 0, 0.05, 12},
		{"too small a demand", 1, 1, 0, 0.05, 1},
		{"negative demand ignored", 10, -5, 0, 0.05, 10},
		{"special price", 10, 0, -3, 0.05, 7},
		{"at least one", 10, 0, -20, 0.05, 1},
		{"at most a stack", 60, 100, 0, 0.2, 64},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			offer := MerchantOffer{Input: item.NewStack(item.Emerald{}, test.count), Demand: test.demand, SpecialPrice: test.special, PriceMultiplier: test.multiplier}
			got := offer.CostA()
			if got.Count() != test.want {
				t.Fatalf("CostA() has %v items, want %v", got.Count(), test.want)
			}
			if !got.Comparable(offer.Input) {
				t.Fatalf("CostA() = %v, not the input %v", got, offer.Input)
			}
		})
	}
}
