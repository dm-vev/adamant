package java26

import (
	"encoding/json"
	"testing"
)

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

// These vectors were emitted by mapped Java 26.3 WorldgenRandom, wrapping
// XoroshiroRandomSource and LegacyRandomSource respectively.
func TestWorldgenRandomJavaVectors(t *testing.T) {
	tests := []struct {
		name                         string
		new                          func(int64) Random
		initialInt, initialUnbounded int32
		initialLong                  int64
		initialDouble                float64
		initialBool                  bool
		initialFloat                 float32
		decoration                   int64
		featureInt, featureUnbounded int32
		featureLong                  int64
		featureDouble                float64
		featureBool                  bool
		featureFloat                 float32
		forkLong                     int64
		gaussianA, gaussianB         float64
	}{
		{"xoroshiro", NewWorldgenRandom, 4, 2043596360, 6434035009442041789, 0x1.7ef88390fd92p-3, true, 0x1.f17dbcp-2,
			-2719146201160111891, 812116, 1566222763, 4441557957166825589, 0x1.c34d862c4081cp-1, false, 0x1.be893ap-1, 1700643124504176594, 0x1.922fa6a4a26c7p-2, -0x1.9da984b0ba598p0},
		{"legacy", NewWorldgenLegacyRandom, 16, -1016548095, 8429272609719263920, 0x1.8fe0ee3772df2p-2, true, 0x1.b3b3a4p-2,
			-2717992414025543991, 945682, 528419259, 6976842953715454339, 0x1.f41b533ba5c9fp-1, true, 0x1.56f764p-1, -8134942852329268515, 0x1.1dcfc42e20b1fp-1, 0x1.7392dda8be016p-4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.new(123456789)
			if got := r.NextInt(17); got != tc.initialInt {
				t.Fatalf("bounded int = %d, want %d", got, tc.initialInt)
			}
			if got := r.NextInt32(); got != tc.initialUnbounded {
				t.Fatalf("int = %d, want %d", got, tc.initialUnbounded)
			}
			if got := r.NextLong(); got != tc.initialLong {
				t.Fatalf("long = %d, want %d", got, tc.initialLong)
			}
			if got := r.NextDouble(); got != tc.initialDouble {
				t.Fatalf("double = %x, want %x", got, tc.initialDouble)
			}
			if got := r.NextBoolean(); got != tc.initialBool {
				t.Fatalf("boolean = %v, want %v", got, tc.initialBool)
			}
			if got := r.NextFloat(); got != tc.initialFloat {
				t.Fatalf("float = %x, want %x", got, tc.initialFloat)
			}
			decoration := r.SetDecorationSeed(987654321, -12345, 6789)
			if decoration != tc.decoration {
				t.Fatalf("decoration seed = %d, want %d", decoration, tc.decoration)
			}
			r.SetFeatureSeed(decoration, 31, 4)
			state := r.State()
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			var restoredState State
			if err := json.Unmarshal(encoded, &restoredState); err != nil {
				t.Fatal(err)
			}
			r = FromState(restoredState)
			if got := r.NextInt(1000003); got != tc.featureInt {
				t.Fatalf("feature bounded int = %d, want %d", got, tc.featureInt)
			}
			if got := r.NextInt32(); got != tc.featureUnbounded {
				t.Fatalf("feature int = %d, want %d", got, tc.featureUnbounded)
			}
			if got := r.NextLong(); got != tc.featureLong {
				t.Fatalf("feature long = %d, want %d", got, tc.featureLong)
			}
			if got := r.NextDouble(); got != tc.featureDouble {
				t.Fatalf("feature double = %x, want %x", got, tc.featureDouble)
			}
			if got := r.NextBoolean(); got != tc.featureBool {
				t.Fatalf("feature boolean = %v, want %v", got, tc.featureBool)
			}
			if got := r.NextFloat(); got != tc.featureFloat {
				t.Fatalf("feature float = %x, want %x", got, tc.featureFloat)
			}
			fork := r.ForkPositional().At(1, 2, 3)
			if got := fork.NextLong(); got != tc.forkLong {
				t.Fatalf("positional fork long = %d, want %d", got, tc.forkLong)
			}
			if got := r.NextGaussian(); got != tc.gaussianA {
				t.Fatalf("gaussian = %x, want %x", got, tc.gaussianA)
			}
			if got := r.NextGaussian(); got != tc.gaussianB {
				t.Fatalf("cached gaussian = %x, want %x", got, tc.gaussianB)
			}
		})
	}
}

func TestWorldgenRandomSeedPreservesGaussianCache(t *testing.T) {
	r := NewWorldgenRandom(123456789)
	want := []float64{-0x1.0b6667fa85107p0, -0x1.f0f01ef8f6e1ap-2, 0x1.f0fab789bbd06p-1, -0x1.2f2b5871d4b58p0}
	if got := r.NextGaussian(); got != want[0] {
		t.Fatalf("first gaussian = %x, want %x", got, want[0])
	}
	r.SetSeed(44)
	if got := r.NextGaussian(); got != want[1] {
		t.Fatalf("cached gaussian = %x, want %x", got, want[1])
	}
	if got := r.NextGaussian(); got != want[2] {
		t.Fatalf("gaussian after reseed = %x, want %x", got, want[2])
	}
	if got := r.NextGaussian(); got != want[3] {
		t.Fatalf("cached gaussian after reseed = %x, want %x", got, want[3])
	}
}

// Direct-source vectors guard the non-WorldgenRandom Xoroshiro API.
func TestDirectXoroshiroRandomJavaVectors(t *testing.T) {
	r := NewXoroshiroRandom(123456789)
	if got := r.NextInt(17); got != 7 {
		t.Fatalf("bounded int = %d, want 7", got)
	}
	if got := r.NextInt32(); got != 1667009048 {
		t.Fatalf("int = %d, want 1667009048", got)
	}
	if got := r.NextLong(); got != 6434035011650043425 {
		t.Fatalf("long = %d, want Java value", got)
	}
	if got := r.NextDouble(); got != 0x1.14709f7b1b33cp-1 {
		t.Fatalf("double = %x, want Java value", got)
	}
	if got := r.NextBoolean(); !got {
		t.Fatal("boolean = false, want true")
	}
	if got := r.NextFloat(); got != 0x1.90fd92p-1 {
		t.Fatalf("float = %x, want Java value", got)
	}
}
func TestNoiseSamplingAllocationFree(t *testing.T) {
	r := NewLegacyRandom(123)
	n := NewNormalNoise(&r, -3, 1, 1, 1, 1)
	if allocs := testing.AllocsPerRun(100, func() { _ = n.Sample(1.25, -9.5, 31.75) }); allocs != 0 {
		t.Fatalf("sampling allocs: %v", allocs)
	}
}
