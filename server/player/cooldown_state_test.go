package player

import (
	"maps"
	"math/rand/v2"
	"sync"
	"testing"
	"time"
)

// The bodies below are the ones HasCooldown, SetCooldown and expireCooldowns
// held before they moved to cooldownState (P3b), with the clock as an
// argument. The test runs both over the same random sequences and requires
// equal maps and answers after every step.

func oldHasCooldown(s *cooldownState, name string, now time.Time) bool {
	s.cooldownsMu.Lock()
	defer s.cooldownsMu.Unlock()

	otherTime, ok := s.cooldowns[name]
	if !ok {
		return false
	}
	if now.After(otherTime) {
		delete(s.cooldowns, name)
		return false
	}
	return true
}

func oldExpireCooldowns(s *cooldownState, now time.Time) {
	s.cooldownsMu.Lock()
	defer s.cooldownsMu.Unlock()
	for it, ti := range s.cooldowns {
		if now.After(ti) {
			delete(s.cooldowns, it)
		}
	}
}

func TestCooldownStateMatchesItsOldBodies(t *testing.T) {
	names := []string{"minecraft:ender_pearl", "minecraft:shield", "minecraft:goat_horn", "minecraft:chorus_fruit"}
	rng := rand.New(rand.NewPCG(3, 4))
	for run := 0; run < 200; run++ {
		got := cooldownState{cooldowns: map[string]time.Time{}}
		want := cooldownState{cooldowns: map[string]time.Time{}}
		for step := 0; step < 60; step++ {
			name := names[rng.IntN(len(names))]
			switch rng.IntN(3) {
			case 0:
				before := time.Now()
				got.cooldownSet(name, time.Duration(rng.IntN(40)-10)*time.Millisecond)
				// The set time is the clock of the call plus the duration:
				// copy it, as the old body took the same clock.
				want.cooldowns[name] = got.cooldowns[name]
				if got.cooldowns[name].Before(before.Add(-11 * time.Millisecond)) {
					t.Fatalf("step %d: cooldown set in the past: %v", step, got.cooldowns[name])
				}
			case 1:
				now := time.Now().Add(time.Duration(rng.IntN(30)-5) * time.Millisecond)
				if g, w := got.cooldownActive(name, now), oldHasCooldown(&want, name, now); g != w {
					t.Fatalf("step %d: active = %v, want %v", step, g, w)
				}
			case 2:
				now := time.Now().Add(time.Duration(rng.IntN(30)-5) * time.Millisecond)
				got.cooldownExpire(now)
				oldExpireCooldowns(&want, now)
			}
			if !maps.Equal(got.cooldowns, want.cooldowns) {
				t.Fatalf("step %d: cooldowns\n got %v\nwant %v", step, got.cooldowns, want.cooldowns)
			}
		}
	}
}

// The mutex still guards the map when the three run at once; go test -race
// reports it if it does not.
func TestCooldownStateIsGuarded(t *testing.T) {
	s := cooldownState{cooldowns: map[string]time.Time{}}
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				s.cooldownSet("minecraft:shield", time.Millisecond*time.Duration(i))
				s.cooldownActive("minecraft:shield", time.Now())
				s.cooldownExpire(time.Now())
			}
		}()
	}
	wg.Wait()
}
