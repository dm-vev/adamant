package leveldat

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestPositionTrackingSettingsRoundTrip(t *testing.T) {
	want := world.PositionTrackingData{
		Next: 91,
		Entries: []world.PositionTrackingEntry{
			{Handle: 7, Position: cube.Pos{1, 2, 3}, Dimension: 0, Active: true},
			{Handle: 91, Position: cube.Pos{-4, 70, 8}, Dimension: 1029, Active: false},
		},
	}
	settings := &world.Settings{}
	settings.LoadPositionTrackingData(want)
	data := &Data{}
	data.PutSettings(settings)
	got := data.Settings().PositionTrackingData()
	if got.Next != want.Next || len(got.Entries) != len(want.Entries) {
		t.Fatalf("tracking data = %#v, want %#v", got, want)
	}
	entries := make(map[int32]world.PositionTrackingEntry, len(got.Entries))
	for _, entry := range got.Entries {
		entries[entry.Handle] = entry
	}
	for _, entry := range want.Entries {
		if gotEntry := entries[entry.Handle]; gotEntry != entry {
			t.Fatalf("entry %d = %#v, want %#v", entry.Handle, gotEntry, entry)
		}
	}
}
