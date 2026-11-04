package session

import "github.com/df-mc/dragonfly/server/item"

// StackNetworkID returns the stack network ID the session sends clients of a
// stack, for the tests of other packages that name stacks in item stack
// requests.
func StackNetworkID(s item.Stack) int32 { return item_id(s) }
