package entity

import "testing"

func TestFloorDiv(t *testing.T) {
	for _, test := range []struct {
		n, want int
	}{
		{n: 15, want: 1},
		{n: 16, want: 2},
		{n: -1, want: -1},
		{n: -15, want: -2},
		{n: -16, want: -2},
	} {
		if got := floorDiv(test.n, 8); got != test.want {
			t.Errorf("floorDiv(%d, 8) = %d, want %d", test.n, got, test.want)
		}
	}
}
