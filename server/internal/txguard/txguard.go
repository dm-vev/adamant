package txguard

import "github.com/df-mc/dragonfly/server/world"

const ClosedPanicMessage = "world.Tx: use of transaction after transaction finishes is not permitted"

// Run calls fn if tx is non-nil and open, returning whether fn ran.
func Run(tx *world.Tx, fn func()) bool {
	if tx == nil || tx.Closed() {
		return false
	}
	fn()
	return true
}

// Value calls fn if tx is non-nil and open, returning its result and whether fn ran.
func Value[T any](tx *world.Tx, fn func() T) (T, bool) {
	if tx == nil || tx.Closed() {
		var zero T
		return zero, false
	}
	return fn(), true
}
