package item

import (
	"strings"
	"testing"
)

func TestValidText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		s    string
		max  int
		want bool
	}{
		{strings.Repeat("a", 512), 512, true},
		{strings.Repeat("a", 513), 512, false},
		// Characters count, not bytes.
		{strings.Repeat("ж", 512), 512, true},
		{strings.Repeat("ж", 513), 512, false},
		{"a\xffb", 512, false},
		{"", 32, true},
	} {
		if got := ValidText(tc.s, tc.max); got != tc.want {
			t.Errorf("%d bytes of max %d: %v, want %v", len(tc.s), tc.max, got, tc.want)
		}
	}
}
