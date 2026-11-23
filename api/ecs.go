package api

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/ecs"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// ComponentKey is a typed handle for a component slot in an ECS Bag.
type ComponentKey[T any] = ecs.Key[T]

// ComponentOption configures a ComponentKey registered via NewComponent.
type ComponentOption[T any] = ecs.Option[T]

// ComponentBag stores typed ECS components in a dense slot slice.
type ComponentBag = ecs.Bag

// ComponentHolder is implemented by types that own an *ecs.Bag, including
// *entity.Ent, *world.EntityHandle, *player.Player, *world.World, and
// *world.Column.
type ComponentHolder interface {
	Components() *ecs.Bag
}

// NewComponent registers or looks up a typed ECS component key by name.
func NewComponent[T any](name string, opts ...ComponentOption[T]) ComponentKey[T] {
	return ecs.NewKey[T](name, opts...)
}

// WithComponentCodec attaches NBT persistence callbacks to a ComponentKey.
func WithComponentCodec[T any](encode func(T) map[string]any, decode func(map[string]any) (T, bool)) ComponentOption[T] {
	return ecs.WithCodec[T](encode, decode)
}

func resolveBag(target any) (*ecs.Bag, error) {
	if target == nil {
		return nil, nil
	}
	switch v := target.(type) {
	case *ecs.Bag:
		return v, nil
	case ComponentHolder:
		return v.Components(), nil
	case world.Entity:
		if v == nil || v.H() == nil {
			return nil, nil
		}
		return v.H().Components(), nil
	default:
		return nil, fmt.Errorf("api: unsupported ECS target %T (expected *ecs.Bag or ComponentHolder)", target)
	}
}

// Get returns the component stored under k on target (*ecs.Bag, *entity.Ent,
// *world.EntityHandle, *player.Player, *world.World, *world.Column, or any
// world.Entity), or the zero value and false if absent or target is nil.
func Get[T any](target any, k ComponentKey[T]) (T, bool) {
	var zero T
	bag, err := resolveBag(target)
	if err != nil || bag == nil {
		return zero, false
	}
	return ecs.Get(bag, k)
}

// Set stores v under k on target (*ecs.Bag, *entity.Ent, *world.EntityHandle,
// *player.Player, *world.World, *world.Column, or any world.Entity).
func Set[T any](target any, k ComponentKey[T], v T) {
	bag, err := resolveBag(target)
	if err != nil {
		panic(err.Error())
	}
	if bag == nil {
		panic("api: Set on nil ECS bag")
	}
	ecs.Set(bag, k, v)
}

// Update invokes fn on a pointer to the component stored under k on target,
// initializing it to T's zero value if absent.
func Update[T any](target any, k ComponentKey[T], fn func(*T)) {
	bag, err := resolveBag(target)
	if err != nil {
		panic(err.Error())
	}
	if bag == nil {
		panic("api: Update on nil ECS bag")
	}
	ecs.Update(bag, k, fn)
}

// Delete removes the component stored under k from target, if present.
func Delete[T any](target any, k ComponentKey[T]) {
	bag, err := resolveBag(target)
	if err != nil || bag == nil {
		return
	}
	ecs.Delete(bag, k)
}

// Has reports whether a component is stored under k on target.
func Has[T any](target any, k ComponentKey[T]) bool {
	bag, err := resolveBag(target)
	if err != nil || bag == nil {
		return false
	}
	return ecs.Has(bag, k)
}

// GetBag returns the component stored under k directly from an *ecs.Bag.
func GetBag[T any](b *ecs.Bag, k ComponentKey[T]) (T, bool) { return ecs.Get(b, k) }

// SetBag stores v under k directly in an *ecs.Bag.
func SetBag[T any](b *ecs.Bag, k ComponentKey[T], v T) { ecs.Set(b, k, v) }

// UpdateBag updates the component stored under k directly in an *ecs.Bag.
func UpdateBag[T any](b *ecs.Bag, k ComponentKey[T], fn func(*T)) { ecs.Update(b, k, fn) }

// DeleteBag removes the component stored under k directly from an *ecs.Bag.
func DeleteBag[T any](b *ecs.Bag, k ComponentKey[T]) { ecs.Delete(b, k) }

// HasBag reports whether k is stored directly in an *ecs.Bag.
func HasBag[T any](b *ecs.Bag, k ComponentKey[T]) bool { return ecs.Has(b, k) }

// EntityComponents returns the ECS component bag attached to entity e, or nil.
func EntityComponents(e world.Entity) *ecs.Bag {
	if e == nil {
		return nil
	}
	if holder, ok := e.(ComponentHolder); ok {
		return holder.Components()
	}
	if h := e.H(); h != nil {
		return h.Components()
	}
	return nil
}

// HandleComponents returns the ECS component bag attached to handle h, or nil.
func HandleComponents(h *world.EntityHandle) *ecs.Bag {
	if h == nil {
		return nil
	}
	return h.Components()
}

// PlayerComponents returns the ECS component bag attached to player p, or nil.
func PlayerComponents(p *player.Player) *ecs.Bag {
	if p == nil {
		return nil
	}
	return p.Components()
}

// ChunkComponents returns the ECS component bag for the chunk at pos in tx, or nil.
func ChunkComponents(tx *world.Tx, pos world.ChunkPos) *ecs.Bag {
	if tx == nil || tx.Closed() {
		return nil
	}
	return tx.ChunkComponents(pos)
}

// ColumnComponents returns the ECS component bag attached to column c, or nil.
func ColumnComponents(c *world.Column) *ecs.Bag {
	if c == nil {
		return nil
	}
	return c.Components()
}

// WorldComponents returns the ECS component bag attached to world w, or nil.
func WorldComponents(w *world.World) *ecs.Bag {
	if w == nil {
		return nil
	}
	return w.Components()
}
