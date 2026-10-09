package javastructure

import "testing"

func TestJigsawSpaceBoundsAndTouchingFaces(t *testing.T) {
	space := newJigsawSpace(Box{Min: [3]int32{0, 0, 0}, Max: [3]int32{9, 9, 9}})
	space.reserve(Box{Min: [3]int32{2, 2, 2}, Max: [3]int32{4, 4, 4}})

	for _, candidate := range []Box{
		{Min: [3]int32{5, 2, 2}, Max: [3]int32{7, 4, 4}}, // face-touching is disjoint
		{Min: [3]int32{0, 0, 0}, Max: [3]int32{1, 9, 9}},
		{Min: [3]int32{0, 0, 0}, Max: [3]int32{9, 9, 1}},
	} {
		if !space.fits(candidate) {
			t.Errorf("fits(%+v) = false, want true", candidate)
		}
	}

	for _, candidate := range []Box{
		{Min: [3]int32{4, 3, 3}, Max: [3]int32{6, 6, 6}}, // overlaps reservation
		{Min: [3]int32{-1, 0, 0}, Max: [3]int32{0, 0, 0}},
		{Min: [3]int32{0, 0, 0}, Max: [3]int32{10, 0, 0}},
		{Min: [3]int32{2, 0, 0}, Max: [3]int32{1, 0, 0}}, // invalid box
	} {
		if space.fits(candidate) {
			t.Errorf("fits(%+v) = true, want false", candidate)
		}
	}
}

func TestJigsawSpaceReservationsLeaveDisjointRegions(t *testing.T) {
	space := newJigsawSpace(Box{Min: [3]int32{0, 0, 0}, Max: [3]int32{9, 9, 9}})
	space.reserve(Box{Min: [3]int32{0, 0, 0}, Max: [3]int32{4, 9, 9}})
	space.reserve(Box{Min: [3]int32{5, 0, 0}, Max: [3]int32{9, 4, 9}})

	if !space.fits(Box{Min: [3]int32{5, 5, 0}, Max: [3]int32{9, 9, 9}}) {
		t.Fatal("disjoint upper-right region should remain available")
	}
	for _, candidate := range []Box{
		{Min: [3]int32{4, 5, 5}, Max: [3]int32{5, 6, 6}},
		{Min: [3]int32{6, 4, 5}, Max: [3]int32{7, 5, 6}},
	} {
		if space.fits(candidate) {
			t.Errorf("reserved overlap %+v unexpectedly fits", candidate)
		}
	}
}

func TestJigsawSpaceHandlesExtremeCoordinates(t *testing.T) {
	space := newJigsawSpace(Box{
		Min: [3]int32{-2_147_483_648, 1_500_000_000, -2_000_000_000},
		Max: [3]int32{-2_147_483_640, 1_500_000_010, -1_999_999_990},
	})
	space.reserve(Box{
		Min: [3]int32{-2_147_483_646, 1_500_000_002, -1_999_999_998},
		Max: [3]int32{-2_147_483_644, 1_500_000_004, -1_999_999_996},
	})
	if !space.fits(Box{
		Min: [3]int32{-2_147_483_648, 1_500_000_000, -2_000_000_000},
		Max: [3]int32{-2_147_483_647, 1_500_000_010, -1_999_999_990},
	}) {
		t.Fatal("large negative-coordinate region should fit")
	}
	if space.fits(Box{
		Min: [3]int32{-2_147_483_645, 1_500_000_003, -1_999_999_997},
		Max: [3]int32{-2_147_483_645, 1_500_000_003, -1_999_999_997},
	}) {
		t.Fatal("overlap at extreme coordinates should not fit")
	}
}

func TestJigsawSpaceContextsAreIndependent(t *testing.T) {
	bounds := Box{Min: [3]int32{0, 0, 0}, Max: [3]int32{9, 9, 9}}
	parent := newJigsawSpace(bounds)
	child := newJigsawSpace(bounds)
	parent.reserve(Box{Min: [3]int32{0, 0, 0}, Max: [3]int32{4, 9, 9}})
	child.reserve(Box{Min: [3]int32{5, 0, 0}, Max: [3]int32{9, 4, 9}})

	left := Box{Min: [3]int32{0, 0, 0}, Max: [3]int32{4, 9, 9}}
	right := Box{Min: [3]int32{5, 0, 0}, Max: [3]int32{9, 4, 9}}
	if parent.fits(left) || !parent.fits(right) {
		t.Fatal("parent context did not retain only its own reservation")
	}
	if !child.fits(left) || child.fits(right) {
		t.Fatal("nested context did not retain only its own reservation")
	}
}
