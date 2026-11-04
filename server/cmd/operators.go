package cmd

import (
	"sync/atomic"

	"github.com/google/uuid"
)

var operatorChecker atomic.Pointer[func(id uuid.UUID) bool]

// SetOperatorChecker installs the server's operator lookup, used by
// permission checks outside command execution (e.g. command block editing).
// It is passed the UUID of a player.
func SetOperatorChecker(f func(id uuid.UUID) bool) {
	operatorChecker.Store(&f)
}

// IsOperator reports whether the player of the UUID passed, a player
// online, has operator permissions: by its account or its name, as the
// server keys its players, never by the name of an entity that is no
// player online. It is false until the server installs an operator
// checker.
func IsOperator(id uuid.UUID) bool {
	f := operatorChecker.Load()
	return f != nil && (*f)(id)
}
