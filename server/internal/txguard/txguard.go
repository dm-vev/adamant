package txguard

import "github.com/df-mc/dragonfly/server/world"

const ClosedPanicMessage = "world.Tx: use of transaction after transaction finishes is not permitted"

func Run(tx *world.Tx, fn func()) (ok bool) {
	return run(tx, fn)
}

func Value[T any](tx *world.Tx, fn func() T) (value T, ok bool) {
	ok = run(tx, func() {
		value = fn()
	})
	return
}

func run(tx *world.Tx, fn func()) (ok bool) {
	if tx == nil {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			if msg, str := r.(string); str && msg == ClosedPanicMessage {
				ok = false
				return
			}
			panic(r)
		}
	}()
	fn()
	return true
}
