package block

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// TestSmelterActorsUseBDSBurnTimes checks furnaces, blast furnaces and
// smokers write their fuel as BDS does: BurnTime the ticks left,
// BurnDuration the ticks the burning fuel lasts in all and no MaxTime, and
// that they load BDS actors and actors earlier versions saved.
func TestSmelterActorsUseBDSBurnTimes(t *testing.T) {
	const tick = time.Millisecond * 50
	blocks := []interface {
		EncodeNBT() map[string]any
		DecodeNBT(map[string]any) any
	}{NewFurnace(cube.North), NewBlastFurnace(cube.North), NewSmoker(cube.North)}
	for _, b := range blocks {
		// Coal burned for 22 ticks, as a BDS furnace sent it.
		s := smelterOf(b)
		s.setDurations(1578*tick, 1600*tick, 23*tick)
		data := b.EncodeNBT()
		if data["BurnTime"] != int16(1578) || data["BurnDuration"] != int16(1600) || data["CookTime"] != int16(23) {
			t.Fatalf("%T burn times = %v/%v/%v, want BDS's 1578/1600/23", b, data["BurnTime"], data["BurnDuration"], data["CookTime"])
		}
		if _, ok := data["MaxTime"]; ok {
			t.Fatalf("%T writes MaxTime, which BDS does not", b)
		}

		for _, load := range []struct {
			name string
			data map[string]any
			want time.Duration
		}{
			// A stick lasts 100 ticks, less than an item takes to cook.
			{"BDS", map[string]any{"BurnTime": int16(80), "BurnDuration": int16(100), "CookTime": int16(20)}, 100 * tick},
			{"legacy", map[string]any{"BurnTime": int16(800), "BurnDuration": int16(100), "MaxTime": int16(1600), "CookTime": int16(20)}, 1600 * tick},
		} {
			_, maximum, _ := smelterOf(b.DecodeNBT(load.data)).Durations()
			if maximum != load.want {
				t.Fatalf("%T loaded from a %s actor burns for %v in all, want %v", b, load.name, maximum, load.want)
			}
		}
	}
}

func smelterOf(b any) *smelter {
	switch b := b.(type) {
	case Furnace:
		return b.smelter
	case BlastFurnace:
		return b.smelter
	case Smoker:
		return b.smelter
	}
	panic("not a smelter")
}
