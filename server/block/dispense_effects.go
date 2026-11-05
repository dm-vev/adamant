package block

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// dispenseClick is the click that ends a dispense for the clients that see
// it. Java's DefaultDispenseItemBehavior.playSound sends level event 1000,
// OptionalDispenseItemBehavior 1000 or 1001 and ProjectileDispenseBehavior
// the event of its DispenseConfig; what Bedrock Edition sends in their place
// was read from BDS 1.26.61.1 (net/bedrock/client/testdata/bds_actors/README.md).
type dispenseClick uint8

const (
	// clickEjected is the click of an item that was ejected: block.click
	// at the launch point, with the block there.
	clickEjected dispenseClick = iota
	// clickDone is the click of a behaviour that did what its item does:
	// block.click at the launch point, with no block.
	clickDone
	// clickFailed is the click of an OptionalDispenseItemBehavior that
	// failed: block.click.fail at the launch point.
	clickFailed
	// clickSilent is no click at all: BDS plays none for shears, brushes and
	// shulker boxes.
	clickSilent
)

// dispenseEffects are the effects of a dispense that the clients see and hear:
// an optional launch sound, the smoke of level event 2000 at the launch
// point, and the click.
type dispenseEffects struct {
	// launch is played before the smoke: the level event of a projectile.
	launch world.Sound
	// click is the click played after the smoke.
	click dispenseClick
	// clickAt, if set, is where the click plays instead of the launch point.
	clickAt *mgl64.Vec3
}

// dispensed is what a behaviour did with the stack of its slot.
type dispensed struct {
	// left is the stack the slot holds afterwards.
	left item.Stack
	// insert is a remainder that goes into the dispenser if it fits
	// (DefaultDispenseItemBehavior.consumeWithRemainder), and is ejected
	// otherwise.
	insert item.Stack
	// unchanged is whether the slot is left as it was, and comparators are
	// not told.
	unchanged bool
	// effects are what the clients see and hear.
	effects dispenseEffects
}

// done is the result of a behaviour that used one item of the stack and did
// what the item does.
func (src dispenseSource) done() dispensed {
	return dispensed{left: src.stack.Grow(-1), effects: dispenseEffects{click: clickDone}}
}

// failed is the result of an OptionalDispenseItemBehavior that could not do
// what its item does: the stack stays and the dispenser clicks.
func (src dispenseSource) failed() dispensed {
	return dispensed{left: src.stack, unchanged: true, effects: dispenseEffects{click: clickFailed}}
}

// withRemainder is the result of a behaviour that used one item of the stack
// and made another, as DefaultDispenseItemBehavior.consumeWithRemainder: the
// item made takes the slot of the last item, and goes into the dispenser, or
// out of it, otherwise.
func (src dispenseSource) withRemainder(remainder item.Stack) dispensed {
	left := src.stack.Grow(-1)
	if left.Empty() {
		return dispensed{left: remainder, effects: dispenseEffects{click: clickDone}}
	}
	return dispensed{left: left, insert: remainder, effects: dispenseEffects{click: clickDone}}
}

// launchPoint returns where the clients see the dispenser launch: the point
// the smoke and the click of an ejected item are at.
func (src dispenseSource) launchPoint() mgl64.Vec3 {
	return nativeItemEjectionLaunch(src.pos, src.d.Facing)
}

// play shows the clients the effects of a dispense.
func (src dispenseSource) play(fx dispenseEffects) {
	launch := src.launchPoint()
	if fx.launch != nil {
		src.tx.PlaySound(launch, fx.launch)
	}
	src.tx.AddParticle(launch, particle.DispenserSmoke{Face: src.d.Facing})
	at := launch
	if fx.clickAt != nil {
		at = *fx.clickAt
	}
	switch fx.click {
	case clickEjected:
		playContainerSound(at, src.tx, "block.click")
	case clickDone:
		src.tx.PlaySound(at, sound.Event{Name: "block.click"})
	case clickFailed:
		src.tx.PlaySound(at, sound.Event{Name: "block.click.fail"})
	}
}

// insertItem puts the stack into the dispenser and returns what did not fit, as
// DispenserBlockEntity.insertItem: it fills the first slots that are empty or
// hold the same item, up to the maximum count of the stack.
func (d Dispenser) insertItem(stack item.Stack) item.Stack {
	for slot := 0; slot < d.inventory.Size() && !stack.Empty(); slot++ {
		target, _ := d.inventory.Item(slot)
		if !target.Empty() && !target.Comparable(stack) {
			continue
		}
		transfer := min(stack.Count(), stack.MaxCount()-target.Count())
		if transfer <= 0 {
			continue
		}
		if target.Empty() {
			target = stack.Grow(transfer - stack.Count())
		} else {
			target = target.Grow(transfer)
		}
		_ = d.inventory.SetItem(slot, target)
		stack = stack.Grow(-transfer)
	}
	return stack
}

// store puts the result of a behaviour into the slot it dispensed from, and
// the remainder it made into the dispenser or out of it.
func (src dispenseSource) store(slot int, res dispensed) {
	if res.unchanged {
		return
	}
	_ = src.d.inventory.SetItem(slot, res.left)
	if !res.insert.Empty() {
		if rest := src.d.insertItem(res.insert); !rest.Empty() {
			// addToInventoryOrDispense: the item that did not fit is spawned
			// with the default sound and animation.
			ejectContainerItem(src.pos, src.d.Facing, src.tx, rest)
			src.play(dispenseEffects{click: clickEjected})
		}
	}
	notifyComparatorUpdate(src.pos, src.tx)
}
