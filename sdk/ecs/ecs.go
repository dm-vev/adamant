// Package ecs provides typed, zero-allocation ECS component keys and accessors
// with optional NBT persistence across entities, players, chunks, and worlds.
package ecs

import (
	"fmt"

	core "github.com/df-mc/dragonfly/server/ecs"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Key is a typed handle for a component slot in an ECS Bag.
type Key[T any] = core.Key[T]

// Option configures a Key registered via NewKey.
type Option[T any] = core.Option[T]

// Bag stores typed ECS components in a dense slot slice.
type Bag = core.Bag

// Holder is implemented by types that own a *Bag (*entity.Ent,
// *world.EntityHandle, *player.Player, *world.World, *world.Column).
type Holder interface {
	Components() *Bag
}

// NewKey registers or looks up a typed ECS component key by name.
func NewKey[T any](name string, opts ...Option[T]) Key[T] {
	return core.NewKey[T](name, opts...)
}

// Codec attaches NBT encode/decode callbacks to a Key so the component persists
// automatically on entities, players, chunks, and worlds.
func Codec[T any](encode func(T) map[string]any, decode func(map[string]any) (T, bool)) Option[T] {
	return core.WithCodec[T](encode, decode)
}

func resolveBag(target any) (*Bag, error) {
	if target == nil {
		return nil, nil
	}
	switch v := target.(type) {
	case *Bag:
		return v, nil
	case Holder:
		return v.Components(), nil
	case world.Entity:
		if v == nil || v.H() == nil {
			return nil, nil
		}
		return v.H().Components(), nil
	default:
		return nil, fmt.Errorf("sdk/ecs: unsupported target %T (expected *Bag or Holder)", target)
	}
}

// Get returns the component stored under k on target, or zero and false.
func Get[T any](target any, k Key[T]) (T, bool) {
	var zero T
	bag, err := resolveBag(target)
	if err != nil || bag == nil {
		return zero, false
	}
	return core.Get(bag, k)
}

// Set stores v under k on target.
func Set[T any](target any, k Key[T], v T) {
	bag, err := resolveBag(target)
	if err != nil {
		panic(err.Error())
	}
	if bag == nil {
		panic("sdk/ecs: Set on nil bag")
	}
	core.Set(bag, k, v)
}

// Update invokes fn on a pointer to the component stored under k on target,
// initializing it to T's zero value if absent.
func Update[T any](target any, k Key[T], fn func(*T)) {
	bag, err := resolveBag(target)
	if err != nil {
		panic(err.Error())
	}
	if bag == nil {
		panic("sdk/ecs: Update on nil bag")
	}
	core.Update(bag, k, fn)
}

// Delete removes the component stored under k from target.
func Delete[T any](target any, k Key[T]) {
	bag, err := resolveBag(target)
	if err != nil || bag == nil {
		return
	}
	core.Delete(bag, k)
}

// Has reports whether k is stored on target.
func Has[T any](target any, k Key[T]) bool {
	bag, err := resolveBag(target)
	if err != nil || bag == nil {
		return false
	}
	return core.Has(bag, k)
}

// GetBag returns the component stored under k directly from b.
func GetBag[T any](b *Bag, k Key[T]) (T, bool) { return core.Get(b, k) }

// SetBag stores v under k directly in b.
func SetBag[T any](b *Bag, k Key[T], v T) { core.Set(b, k, v) }

// UpdateBag updates the component stored under k directly in b.
func UpdateBag[T any](b *Bag, k Key[T], fn func(*T)) { core.Update(b, k, fn) }

// DeleteBag removes k directly from b.
func DeleteBag[T any](b *Bag, k Key[T]) { core.Delete(b, k) }

// HasBag reports whether k is stored directly in b.
func HasBag[T any](b *Bag, k Key[T]) bool { return core.Has(b, k) }

// OfEntity returns the ECS bag attached to entity e, or nil.
func OfEntity(e world.Entity) *Bag {
	if e == nil {
		return nil
	}
	if h, ok := e.(Holder); ok {
		return h.Components()
	}
	if h := e.H(); h != nil {
		return h.Components()
	}
	return nil
}

// OfHandle returns the ECS bag attached to handle h, or nil.
func OfHandle(h *world.EntityHandle) *Bag {
	if h == nil {
		return nil
	}
	return h.Components()
}

// OfPlayer returns the ECS bag attached to player p, or nil.
func OfPlayer(p *player.Player) *Bag {
	if p == nil {
		return nil
	}
	return p.Components()
}

// OfChunk returns the ECS bag for the chunk at pos in tx, or nil.
func OfChunk(tx *world.Tx, pos world.ChunkPos) *Bag {
	if tx == nil || tx.Closed() {
		return nil
	}
	return tx.ChunkComponents(pos)
}

// OfColumn returns the ECS bag attached to column c, or nil.
func OfColumn(c *world.Column) *Bag {
	if c == nil {
		return nil
	}
	return c.Components()
}

// OfWorld returns the ECS bag attached to world w, or nil.
func OfWorld(w *world.World) *Bag {
	if w == nil {
		return nil
	}
	return w.Components()
}
