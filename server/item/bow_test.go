package item_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	_ "github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestBowAndCrossbowArrowLaunch(t *testing.T) {
	tests := []struct {
		name         string
		duration     time.Duration
		crossbow     bool
		powerLevel   int
		wantVelocity float64
		wantCritical bool
	}{
		{name: "uncharged bow", duration: 150 * time.Millisecond, wantVelocity: 0.5375},
		{name: "full bow", duration: time.Second, wantVelocity: 5, wantCritical: true},
		{name: "power bow", duration: time.Second, powerLevel: 3, wantVelocity: 5, wantCritical: true},
		{name: "crossbow", crossbow: true, wantVelocity: 3.15, wantCritical: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var opts world.EntitySpawnOpts
			var conf world.ArrowSpawnConfig
			registry := world.EntityRegistryConfig{Arrow: func(gotOpts world.EntitySpawnOpts, gotConf world.ArrowSpawnConfig) *world.EntityHandle {
				opts, conf = gotOpts, gotConf
				return gotOpts.New(arrowTestEntityType{}, arrowTestEntityConfig{})
			}}.New([]world.EntityType{arrowTestEntityType{}})
			w := world.Config{Synchronous: true, Entities: registry}.New()
			defer w.Close()

			releaser := &arrowTestReleaser{rotation: cube.Rotation{90, 0}}
			ctx := &item.UseContext{FirstFunc: func(match func(item.Stack) bool) (item.Stack, bool) {
				arrow := item.NewStack(item.Arrow{}, 1)
				return arrow, match(arrow)
			}}
			w.Do(func(tx *world.Tx) {
				if test.crossbow {
					crossbow := item.Crossbow{Item: item.NewStack(item.Arrow{}, 1)}
					releaser.held = item.NewStack(crossbow, 1)
					if !crossbow.ReleaseCharge(releaser, tx, ctx) {
						t.Fatal("charged crossbow did not fire")
					}
					return
				}
				releaser.held = item.NewStack(item.Bow{}, 1)
				if test.powerLevel > 0 {
					releaser.held = releaser.held.WithEnchantments(item.NewEnchantment(enchantment.Power, test.powerLevel))
				}
				item.Bow{}.Release(releaser, tx, ctx, test.duration)
			})

			if got := opts.Velocity.Len(); !mgl64.FloatEqualThreshold(got, test.wantVelocity, 1e-9) {
				t.Fatalf("velocity = %v, want %v", got, test.wantVelocity)
			}
			if conf.Damage != 1 || conf.PowerLevel != test.powerLevel || conf.Critical != test.wantCritical {
				t.Fatalf("arrow config = {damage: %v, power: %v, critical: %v}, want {damage: 1, power: %v, critical: %v}", conf.Damage, conf.PowerLevel, conf.Critical, test.powerLevel, test.wantCritical)
			}
		})
	}
}

type arrowTestReleaser struct {
	rotation cube.Rotation
	held     item.Stack
}

func (*arrowTestReleaser) Close() error                          { return nil }
func (*arrowTestReleaser) H() *world.EntityHandle                { return nil }
func (*arrowTestReleaser) Position() mgl64.Vec3                  { return mgl64.Vec3{} }
func (r *arrowTestReleaser) Rotation() cube.Rotation             { return r.rotation }
func (r *arrowTestReleaser) HeldItems() (item.Stack, item.Stack) { return r.held, item.Stack{} }
func (r *arrowTestReleaser) SetHeldItems(main, _ item.Stack)     { r.held = main }
func (*arrowTestReleaser) UsingItem() bool                       { return false }
func (*arrowTestReleaser) ReleaseItem()                          {}
func (*arrowTestReleaser) UseItem()                              {}
func (*arrowTestReleaser) GameMode() world.GameMode              { return world.GameModeSurvival }
func (*arrowTestReleaser) PlaySound(world.Sound)                 {}

type arrowTestEntityConfig struct{}

func (arrowTestEntityConfig) Apply(*world.EntityData) {}

type arrowTestEntityType struct{}

func (arrowTestEntityType) Open(_ *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return arrowTestEntity{handle: handle, data: data}
}
func (arrowTestEntityType) EncodeEntity() string { return "test:arrow" }
func (arrowTestEntityType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, -0.125, -0.125, 0.125, 0.125, 0.125)
}
func (arrowTestEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (arrowTestEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type arrowTestEntity struct {
	handle *world.EntityHandle
	data   *world.EntityData
}

func (arrowTestEntity) Close() error              { return nil }
func (e arrowTestEntity) H() *world.EntityHandle  { return e.handle }
func (e arrowTestEntity) Position() mgl64.Vec3    { return e.data.Pos }
func (e arrowTestEntity) Rotation() cube.Rotation { return e.data.Rot }
