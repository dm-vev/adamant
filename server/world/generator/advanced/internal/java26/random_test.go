package java26

import "testing"

func TestLegacyRandomJava48Stream(t *testing.T) {
	r := NewLegacyRandom(0)
	want := []int32{-1155484576, -723955400}
	for i, w := range want {
		if got := r.NextInt32(); got != w {
			t.Fatalf("draw %d: got %d want %d", i, got, w)
		}
	}
}
func TestLegacyRandomBound(t *testing.T) {
	r := NewLegacyRandom(42)
	for range 1000 {
		v := r.NextInt(7)
		if v < 0 || v >= 7 {
			t.Fatalf("out of bound: %d", v)
		}
	}
}
func TestNoiseSamplingAllocationFree(t *testing.T) {
	r := NewLegacyRandom(123)
	n := NewNormalNoise(&r, -3, 1, 1, 1, 1)
	if allocs := testing.AllocsPerRun(100, func() { _ = n.Sample(1.25, -9.5, 31.75) }); allocs != 0 {
		t.Fatalf("sampling allocs: %v", allocs)
	}
}
