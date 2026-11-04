package menu

import "errors"

// The errors below are reasons an operation fails for that a protocol tells
// its client apart, as Bedrock tells them by the status of the response
// that refuses a request. Operations wrap them; errors.Is finds them.
var (
	// ErrSameSlot is the error of Move and Swap for a source and a
	// destination that are one slot.
	ErrSameSlot = errors.New("source and destination are the same slot")
)
