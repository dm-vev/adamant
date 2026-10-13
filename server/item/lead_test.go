package item_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type leadUser struct {
	h         *world.EntityHandle
	main, off item.Stack
}

func (u *leadUser) H() *world.EntityHandle              { return u.h }
func (*leadUser) Position() mgl64.Vec3                  { return mgl64.Vec3{} }
func (*leadUser) Rotation() cube.Rotation               { return cube.Rotation{} }
func (*leadUser) Close() error                          { return nil }
func (u *leadUser) HeldItems() (item.Stack, item.Stack) { return u.main, u.off }
func (u *leadUser) SetHeldItems(main, off item.Stack)   { u.main, u.off = main, off }
func (*leadUser) UsingItem() bool                       { return false }
func (*leadUser) ReleaseItem()                          {}
func (*leadUser) UseItem()                              {}

func TestLeadUseOnMobConsumesOneLeadAndSetsHolder(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	mobHandle := entity.NewCow(world.EntitySpawnOpts{})
	user := &leadUser{h: world.NewEntity(entity.CowType, entity.CowConfig{})}
	<-w.Exec(func(tx *world.Tx) {
		mob := tx.AddEntity(mobHandle)
		ctx := item.UseContext{}
		if !(item.Lead{}).UseOnEntity(mob, tx, user, &ctx) {
			t.Fatal("using a lead on a cow was rejected")
		}
		if ctx.CountSub != 1 {
			t.Fatalf("lead count subtraction = %d, want 1", ctx.CountSub)
		}
		if got := mob.(*entity.Cow).HolderLeashID(); got != user.H().UniqueID() {
			t.Fatalf("cow leash holder = %d, want %d", got, user.H().UniqueID())
		}
	})
}
