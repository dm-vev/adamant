package item_test

import (
	"testing"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

type mapUser struct{ pos mgl64.Vec3 }

func (*mapUser) Close() error                        { return nil }
func (*mapUser) H() *world.EntityHandle              { return nil }
func (u *mapUser) Position() mgl64.Vec3              { return u.pos }
func (*mapUser) Rotation() cube.Rotation             { return cube.Rotation{} }
func (*mapUser) HeldItems() (item.Stack, item.Stack) { return item.Stack{}, item.Stack{} }
func (*mapUser) SetHeldItems(item.Stack, item.Stack) {}
func (*mapUser) UsingItem() bool                     { return false }
func (*mapUser) ReleaseItem()                        {}
func (*mapUser) UseItem()                            {}

func TestEmptyMapDrawsGridAlignedMapAroundUser(t *testing.T) {
	db, err := (mcdb.Config{}).Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		ctx := &item.UseContext{}
		if !(item.EmptyMap{}).Use(tx, &mapUser{pos: mgl64.Vec3{100, 64, -20}}, ctx) {
			t.Fatal("empty map was not used")
		}
		filled, ok := ctx.NewItem.Item().(item.FilledMap)
		if ctx.CountSub != 1 || !ok || !filled.HasNativeID {
			t.Fatalf("use consumed %d and gave %#v", ctx.CountSub, ctx.NewItem.Item())
		}
		data, err := w.MapData(filled.NativeID)
		if err != nil {
			t.Fatal(err)
		}
		// Scale 0 maps cover 128 blocks on a grid offset by 64.
		if data.Scale != 0 || data.CenterX != 128 || data.CenterZ != 0 {
			t.Fatalf("map centred on %d, %d at scale %d", data.CenterX, data.CenterZ, data.Scale)
		}
	})
}

func TestFilledMapDecodesLegacyMapIDKey(t *testing.T) {
	t.Parallel()
	decoded := (item.FilledMap{}).DecodeNBT(map[string]any{
		"map_uuid":            int64(99),
		"adamant:java_map_id": int32(12),
	}).(item.FilledMap)
	if !decoded.HasCanonicalID || decoded.CanonicalID != 12 || !decoded.HasNativeID || decoded.NativeID != 99 {
		t.Fatalf("legacy DecodeNBT = %#v", decoded)
	}
	encoded := decoded.EncodeNBT()
	if encoded["adamant:map_id"] != int32(12) {
		t.Fatalf("re-encoded map ID = %#v, want 12 under adamant:map_id", encoded)
	}
	if _, legacy := encoded["adamant:java_map_id"]; legacy {
		t.Fatalf("re-encoded map kept legacy key: %#v", encoded)
	}
}
