package player

import (
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// The bodies below are the ones the methods of Player held before they moved
// to itemUseState (P3a), over the state alone and the held slot as an
// argument. The test runs both over the same random sequences of use and
// requires equal states and answers after every step.

func oldBegin(s *itemUseState, stack item.Stack, slot uint32) {
	s.usingItem = true
	s.usingSince = time.Now()
	s.usingTicks = 0
	s.usingStack = stack
	if _, ok := stack.Item().(item.KineticWeapon); ok {
		if s.kineticRecent == nil {
			s.kineticRecent = make(map[*world.EntityHandle]int64)
		}
		clear(s.kineticRecent)
	}
	s.usingSlot = slot
	s.itemUseRecentlyCompleted = false
}

func oldCancel(s *itemUseState) {
	if !s.usingItem {
		return
	}
	s.usingItem = false
	s.usingTicks = 0
	s.usingStack = item.Stack{}
	clear(s.kineticRecent)
}

func oldComplete(s *itemUseState, held item.Stack, slot uint32) {
	s.usingItem = false
	s.usingTicks = 0
	s.usingStack = item.Stack{}
	clear(s.kineticRecent)
	s.itemUseCompleteTick = s.itemUseTick
	s.itemUseCompletedStack = held
	s.itemUseCompletedSlot = slot
	s.itemUseRecentlyCompleted = true
}

func oldMatches(s *itemUseState, slot uint32, stack item.Stack) bool {
	return s.usingSlot == slot && s.usingStack.Equal(stack)
}

func oldRecentlyCompleted(s *itemUseState, slot uint32, stack item.Stack) bool {
	if !s.itemUseRecentlyCompleted {
		return false
	}
	if s.itemUseCompletedSlot != slot || !s.itemUseCompletedStack.Equal(stack) {
		s.itemUseRecentlyCompleted = false
		return false
	}
	if ticks := s.itemUseTick - s.itemUseCompleteTick; ticks >= 0 && ticks <= 2 {
		return true
	}
	s.itemUseRecentlyCompleted = false
	return false
}

func oldElapsed(s *itemUseState, since time.Time) time.Duration {
	return max(since.Sub(s.usingSince)+time.Second/20, time.Duration(s.usingTicks)*time.Second/20)
}

func oldTick(s *itemUseState, current int64) {
	s.itemUseTick = current
	s.usingTicks++
}

func TestItemUseStateMatchesItsOldBodies(t *testing.T) {
	stacks := []item.Stack{
		{},
		item.NewStack(item.Bow{}, 1),
		item.NewStack(item.Arrow{}, 3),
		item.NewStack(item.Crossbow{}, 1),
		item.NewStack(item.Spear{Tier: item.ToolTierWood}, 1),
		item.NewStack(item.Spear{Tier: item.ToolTierIron}, 1),
	}
	rng := rand.New(rand.NewPCG(1, 2))
	same := func(step int, a, b *itemUseState) {
		t.Helper()
		x, y := *a, *b
		x.usingSince, y.usingSince = time.Time{}, time.Time{}
		// A map made empty and one never made are the same to every reader.
		if len(x.kineticRecent) == 0 {
			x.kineticRecent = nil
		}
		if len(y.kineticRecent) == 0 {
			y.kineticRecent = nil
		}
		if !reflect.DeepEqual(x, y) {
			t.Fatalf("step %d: state\n got %+v\nwant %+v", step, x, y)
		}
	}
	for run := 0; run < 200; run++ {
		var got, want itemUseState
		var tick int64
		for step := 0; step < 60; step++ {
			stack, slot := stacks[rng.IntN(len(stacks))], uint32(rng.IntN(3))
			switch rng.IntN(7) {
			case 0:
				got.useBegin(stack, slot)
				oldBegin(&want, stack, slot)
				if _, ok := stack.Item().(item.KineticWeapon); ok {
					got.kineticRecent[nil], want.kineticRecent[nil] = 1, 1
				}
			case 1:
				if got.usingItem {
					got.useFinish()
				}
				oldCancel(&want)
			case 2:
				got.useFinish()
				got.useMarkCompleted(stack, slot)
				oldComplete(&want, stack, slot)
			case 3:
				if g, w := got.useMatches(slot, stack), oldMatches(&want, slot, stack); g != w {
					t.Fatalf("step %d: matches = %v, want %v", step, g, w)
				}
			case 4:
				if g, w := got.useRecentlyCompleted(slot, stack), oldRecentlyCompleted(&want, slot, stack); g != w {
					t.Fatalf("step %d: recentlyCompleted = %v, want %v", step, g, w)
				}
			case 5:
				tick += int64(rng.IntN(4))
				got.useCountTick(tick)
				oldTick(&want, tick)
			case 6:
				now := time.Now().Add(time.Duration(rng.IntN(3000)) * time.Millisecond)
				// elapsed reads the clock itself, so both sides are given
				// the same bounds: the answer must lie where the old one does.
				lo, hi := oldElapsed(&want, time.Now()), oldElapsed(&want, now)
				if g := got.useElapsed(); g < lo || g > hi+time.Second {
					t.Fatalf("step %d: elapsed = %v, want between %v and %v", step, g, lo, hi+time.Second)
				}
			}
			same(step, &got, &want)
		}
	}
}
