package block

import (
	"reflect"

	"github.com/df-mc/dragonfly/server/world"
)

// HasDispenseBehaviour reports whether a dispenser has a behaviour of its own
// for the item, rather than ejecting it.
func HasDispenseBehaviour(it world.Item) bool {
	_, ok := dispenseBehaviours[reflect.TypeOf(it)]
	return ok
}
