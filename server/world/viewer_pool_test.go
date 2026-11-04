package world

import "testing"

// BenchmarkViewerSlicePool borrows the viewers of a world and hands them
// back, as every broadcast to its viewers does.
func BenchmarkViewerSlicePool(b *testing.B) {
	w := &World{}
	b.ReportAllocs()
	for b.Loop() {
		viewers, _ := w.allViewers()
		w.releaseViewers(viewers)
	}
}
