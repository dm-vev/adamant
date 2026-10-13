package cmd

import "sync/atomic"

var operatorChecker atomic.Pointer[func(name string) bool]

// SetOperatorChecker installs the server's operator lookup, used by
// permission checks outside command execution (e.g. command block editing).
func SetOperatorChecker(f func(name string) bool) {
	operatorChecker.Store(&f)
}

// IsOperator reports whether the named player has operator permissions. It is
// false until the server installs an operator checker.
func IsOperator(name string) bool {
	f := operatorChecker.Load()
	return f != nil && (*f)(name)
}
