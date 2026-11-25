package item

import (
	"slices"

	coreitem "github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// StackBuilder constructs an item.Stack fluently with custom name, lore,
// enchantments, durability damage, unbreakable flag, and custom NBT values.
type StackBuilder struct {
	stack coreitem.Stack
}

// Stack starts a fluent StackBuilder for it with optional initial count
// (defaults to 1).
func Stack(it world.Item, count ...int) *StackBuilder {
	n := 1
	if len(count) > 0 && count[0] > 0 {
		n = count[0]
	}
	return &StackBuilder{stack: coreitem.NewStack(it, n)}
}

// FromStack wraps an existing item.Stack in a fluent StackBuilder.
func FromStack(st coreitem.Stack) *StackBuilder {
	return &StackBuilder{stack: st}
}

// Count sets the stack count.
func (b *StackBuilder) Count(n int) *StackBuilder {
	if n > 0 && !b.stack.Empty() {
		b.stack = b.stack.Grow(n - b.stack.Count())
	}
	return b
}

// Name sets the stack's custom display name.
func (b *StackBuilder) Name(name string) *StackBuilder {
	b.stack = b.stack.WithCustomName(name)
	return b
}

// Lore replaces the stack's lore lines.
func (b *StackBuilder) Lore(lines ...string) *StackBuilder {
	b.stack = b.stack.WithLore(lines...)
	return b
}

// AddLore appends one or more lines to the stack's existing lore.
func (b *StackBuilder) AddLore(lines ...string) *StackBuilder {
	cur := slices.Clone(b.stack.Lore())
	cur = append(cur, lines...)
	b.stack = b.stack.WithLore(cur...)
	return b
}

// Enchant applies an enchantment at level to the stack.
func (b *StackBuilder) Enchant(t coreitem.EnchantmentType, level int) *StackBuilder {
	if t != nil && level > 0 {
		b.stack = b.stack.WithEnchantments(coreitem.NewEnchantment(t, level))
	}
	return b
}

// Damage applies d durability damage to the stack.
func (b *StackBuilder) Damage(d int) *StackBuilder {
	if d > 0 {
		b.stack = b.stack.Damage(d)
	}
	return b
}

// Unbreakable marks the stack as unbreakable.
func (b *StackBuilder) Unbreakable() *StackBuilder {
	b.stack = b.stack.AsUnbreakable()
	return b
}

// Value attaches a custom NBT key/value pair to the stack.
func (b *StackBuilder) Value(key string, val any) *StackBuilder {
	b.stack = b.stack.WithValue(key, val)
	return b
}

// Build returns the constructed item.Stack.
func (b *StackBuilder) Build() coreitem.Stack {
	if b == nil {
		return coreitem.Stack{}
	}
	return b.stack
}
