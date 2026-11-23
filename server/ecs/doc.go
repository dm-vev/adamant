// Package ecs provides a zero-allocation-when-empty, dense-indexed Entity
// Component System (ECS) component store.
//
// Typed keys are registered once via NewKey and assigned monotonic dense
// uint16 identifiers starting at 1 (with 0 reserved for the invalid zero Key).
// Bags store component values in a flat slot slice indexed directly by key ID
// without reflection or map hashing on Get, Set, Update, Has, or Delete.
//
// Keys may optionally attach an NBT codec via WithCodec for persistent
// serialization through Bag.EncodeNBT and Bag.DecodeNBT.
package ecs
