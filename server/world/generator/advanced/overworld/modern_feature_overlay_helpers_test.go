package overworld

import "github.com/df-mc/dragonfly/server/world"

// Only tests use these helpers of modern_feature_overlay.go, so they live with the tests.

// all yields every position with an index and its index plus one.
func (x *posIndex) all(yield func(featurePos, uint32) bool) {
	for key, page := range x.pages {
		sx := int32(uint32(key)<<10) >> 10 << 4
		sz := int32(uint32(key>>22)<<10) >> 10 << 4
		sy := int32(uint32(key>>44)<<12) >> 12 << 4
		for i, index := range page {
			if index == 0 {
				continue
			}
			if !yield(featurePos{sx + int32(i&15), sy + int32(i>>8), sz + int32(i>>4&15)}, index) {
				return
			}
		}
	}
}

// len returns the number of positions written.
func (o *featureOverlay) len() int {
	return len(o.states)
}

// all yields every position written and its state.
func (o *featureOverlay) all(yield func(featurePos, world.NativeState) bool) {
	for p, i := range o.index.all {
		if !yield(p, o.states[i-1]) {
			return
		}
	}
}
