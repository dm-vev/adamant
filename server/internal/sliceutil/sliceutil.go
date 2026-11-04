package sliceutil

// Convert converts a slice of type B to a slice of type A. Convert panics if B
// cannot be type asserted to type A.
func Convert[A, B any, S ~[]B](v S) []A {
	a := make([]A, len(v))
	for i, b := range v {
		a[i] = (any)(b).(A)
	}
	return a
}

// Filter iterates over elements of collection, returning an array of all
// elements function c returns true for.
func Filter[E any](s []E, c func(E) bool) []E {
	a := make([]E, 0, len(s))
	for _, e := range s {
		if c(e) {
			a = append(a, e)
		}
	}
	return a
}
