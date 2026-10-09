package javastructure

// jigsawSpace tracks the free child-placement region for one source piece.
// Bounds and reservations use inclusive integer block coordinates.
type jigsawSpace struct {
	bounds       Box
	reservations []Box
}

func newJigsawSpace(bounds Box) *jigsawSpace {
	return &jigsawSpace{bounds: bounds}
}

// fits reports whether the candidate's source-deflated shape is wholly inside
// the source bounds and intersects none of the un-deflated reservations.
func (s *jigsawSpace) fits(candidate Box) bool {
	if s == nil || !validJigsawBox(candidate) || !validJigsawBox(s.bounds) {
		return false
	}
	for axis := range 3 {
		if candidate.Min[axis] < s.bounds.Min[axis] || candidate.Max[axis] > s.bounds.Max[axis] {
			return false
		}
	}
	for _, reserved := range s.reservations {
		if jigsawBoxesOverlap(candidate, reserved) {
			return false
		}
	}
	return true
}

// reserve subtracts the candidate's un-deflated block box from this space.
// Callers reserve only accepted candidates.
func (s *jigsawSpace) reserve(candidate Box) {
	s.reservations = append(s.reservations, candidate)
}

func validJigsawBox(box Box) bool {
	for axis := range 3 {
		if box.Min[axis] > box.Max[axis] {
			return false
		}
	}
	return true
}

func jigsawBoxesOverlap(a, b Box) bool {
	for axis := range 3 {
		if a.Max[axis] < b.Min[axis] || b.Max[axis] < a.Min[axis] {
			return false
		}
	}
	return true
}

func jigsawBoxContains(box Box, pos [3]int32) bool {
	for axis := range 3 {
		if pos[axis] < box.Min[axis] || pos[axis] > box.Max[axis] {
			return false
		}
	}
	return true
}
