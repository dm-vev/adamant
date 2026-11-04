package httpauth

import (
	"net/http/httptest"
	"testing"
)

// TestMatchComparesEveryToken checks that Match finds the token of a hash,
// compares every hash whether or not it finds one, and finds none of
// another token.
func TestMatchComparesEveryToken(t *testing.T) {
	calls := 0
	old := compare
	compare = func(a, b []byte) int { calls++; return old(a, b) }
	t.Cleanup(func() { compare = old })
	tokens := []Hash{HashOf("a"), HashOf("b"), HashOf("c")}
	for _, tc := range []struct {
		token string
		want  int
		ok    bool
	}{{"a", 0, true}, {"c", 2, true}, {"d", -1, false}, {"", -1, false}} {
		calls = 0
		i, ok := Match(tc.token, tokens)
		if i != tc.want || ok != tc.ok || calls != len(tokens) {
			t.Errorf("Match(%q) = %d, %v after %d comparisons, want %d, %v after 3", tc.token, i, ok, calls, tc.want, tc.ok)
		}
	}
}

// TestBearer checks that only "Authorization: Bearer <token>" is read.
func TestBearer(t *testing.T) {
	for header, want := range map[string]string{"Bearer abc": "abc", "bearer abc": "", "Basic abc": "", "": "", "Bearer ": ""} {
		r := httptest.NewRequest("GET", "/v1/status?token=abc", nil)
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		got, ok := Bearer(r)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q, %v, want %q", header, got, ok, want)
		}
	}
}
