package world

import "testing"

func TestWardenSpawnTrackerTickDecayAndCodec(t *testing.T) {
	tracker := &WardenSpawnTracker{TicksSinceLastWarning: 11999, WarningLevel: 3, CooldownTicks: 1}
	tracker.Tick()
	if tracker.TicksSinceLastWarning != 12000 || tracker.WarningLevel != 3 || tracker.CooldownTicks != 0 {
		t.Fatalf("tracker at 12000 ticks = %+v, want warning unchanged and cooldown elapsed", tracker)
	}
	tracker.Tick()
	if tracker.TicksSinceLastWarning != 0 || tracker.WarningLevel != 2 {
		t.Fatalf("tracker after decay = %+v, want timer reset and one warning removed", tracker)
	}
	decoded := DecodeWardenSpawnTracker(tracker.EncodeNBT())
	if *decoded != *tracker {
		t.Fatalf("tracker codec round trip = %+v, want %+v", decoded, tracker)
	}
	decoded.SetWarningLevel(9)
	if decoded.WarningLevel != 4 {
		t.Fatalf("warning level after clamp = %d, want 4", decoded.WarningLevel)
	}
}
