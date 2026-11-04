// Package httpauth checks the bearer tokens of HTTP requests: in constant
// time, over SHA-256 hashes of a fixed length, comparing every token, so
// that the time of a check leaks neither the length of a token nor which
// token matched.
package httpauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// Hash is the SHA-256 hash of a token.
type Hash [sha256.Size]byte

// HashOf returns the hash of the token passed.
func HashOf(token string) Hash { return sha256.Sum256([]byte(token)) }

// Bearer returns the token of the Authorization header of r, "Bearer
// <token>", and reports whether it has one. Only the header is read: no
// query parameter, no cookie.
func Bearer(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token = strings.TrimSpace(token); !ok || token == "" {
		return "", false
	}
	return token, true
}

// compare compares two hashes in constant time; tests count its calls.
var compare = subtle.ConstantTimeCompare

// Match returns the index of the hash of tokens that the token presented
// hashes to, and reports whether one does. It compares the hash of the
// token with every hash, without stopping at a match.
func Match(presented string, tokens []Hash) (int, bool) {
	h := HashOf(presented)
	found := -1
	for i := range tokens {
		eq := compare(h[:], tokens[i][:])
		// Keep the first match, without branching on the comparison.
		take := subtle.ConstantTimeEq(int32(eq), 1) & subtle.ConstantTimeEq(int32(found), -1)
		found = subtle.ConstantTimeSelect(take, i, found)
	}
	return found, found >= 0
}
