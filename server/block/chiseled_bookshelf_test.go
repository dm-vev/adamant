package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type chiseledBookshelfUser struct {
	pistonTestUser
	held item.Stack
	inv *inventory.Inventory
}

func (u *chiseledBookshelfUser) HeldItems() (item.Stack, item.Stack) { return u.held, item.Stack{} }
func (u *chiseledBookshelfUser) Inventory() *inventory.Inventory { return u.inv }

func TestChiseledBookshelfSlotsAndActorRoundTrip(t *testing.T) {
	w := world.Config{Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()
	pos := cube.Pos{0, 64, 0}
	user := &chiseledBookshelfUser{held: item.NewStack(item.EnchantedBook{}, 1), inv: inventory.New(36, nil)}
	ctx := &item.UseContext{}
	done := w.Exec(func(tx *world.Tx) {
		b := NewChiseledBookshelf()
		b.Facing = cube.North
		tx.SetBlock(pos, b, nil)
		// North-facing shelves map the right-most top face position to slot 2.
		if !b.ActivateAt(pos, cube.FaceNorth, mgl64.Vec3{0.1, 0.8, 0.5}, tx, user, ctx) {
			t.Fatal("expected book insertion")
		}
		got := tx.Block(pos).(ChiseledBookshelf)
		if got.Items[2].Item() != (item.EnchantedBook{}) || got.Items[2].Count() != 1 || got.StoredMask != 4 {
			t.Fatalf("inserted state = item %T count %d mask %d", got.Items[2].Item(), got.Items[2].Count(), got.StoredMask)
		}
		if got.ComparatorOutput(tx, pos) != 3 || ctx.CountSub != 1 {
			t.Fatalf("comparator=%d countSub=%d", got.ComparatorOutput(tx, pos), ctx.CountSub)
		}
		got.Items[0] = item.NewStack(item.Book{}, 1)
		got.Items[5] = item.NewStack(item.WrittenBook{Title: "Independent"}, 1)
		got.StoredMask |= 1 | 32
		restored := got.DecodeNBT(got.EncodeNBT()).(ChiseledBookshelf)
		written, ok := restored.Items[5].Item().(item.WrittenBook)
		if !ok || restored.Items[0].Item() != (item.Book{}) || restored.Items[2].Item() != (item.EnchantedBook{}) || written.Title != "Independent" || restored.LastInteractedSlot != 2 {
			t.Fatalf("actor round trip lost independent slots: %+v", restored)
		}
		user.held = item.Stack{}
		if !got.ActivateAt(pos, cube.FaceNorth, mgl64.Vec3{0.1, 0.8, 0.5}, tx, user, &item.UseContext{}) {
			t.Fatal("expected empty-hand retrieval")
		}
		removed := tx.Block(pos).(ChiseledBookshelf)
		if !removed.Items[2].Empty() || removed.ComparatorOutput(tx, pos) != 3 {
			t.Fatalf("retrieved state has slot=%v comparator=%d", removed.Items[2], removed.ComparatorOutput(tx, pos))
		}
		if stack, _ := user.inv.Item(0); stack.Item() != (item.EnchantedBook{}) || stack.Count() != 1 {
			t.Fatalf("retrieved inventory item = %T x%d", stack.Item(), stack.Count())
		}
	})
	<-done
}
