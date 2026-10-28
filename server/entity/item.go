package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math/rand/v2"
	"time"
)

// NewItem creates a new item entity using the item stack passed. The item
// entity will be positioned at the position passed. If the stack's count
// exceeds its max count, the count of the stack will be changed to the
// maximum.
func NewItem(opts world.EntitySpawnOpts, i item.Stack) *world.EntityHandle {
	conf := itemConf
	conf.Item = i
	return opts.New(ItemType, conf)
}

// NewItemDrop creates ordinary loot with the native upward toss and random
// horizontal spread. NewItem preserves explicitly supplied motion instead.
func NewItemDrop(opts world.EntitySpawnOpts, i item.Stack) *world.EntityHandle {
	opts.Velocity = mgl64.Vec3{rand.Float64()*0.2 - 0.1, 0.2, rand.Float64()*0.2 - 0.1}
	return NewItem(opts, i)
}

// NewItemPickupDelay creates a new item entity containing item stack i. A
// delay may be specified which defines for how long the item stack cannot be
// picked up from the ground.
func NewItemPickupDelay(opts world.EntitySpawnOpts, i item.Stack, delay time.Duration) *world.EntityHandle {
	conf := itemConf
	conf.Item = i
	conf.PickupDelay = delay
	return opts.New(ItemType, conf)
}

var itemConf = ItemBehaviourConfig{
	Gravity: 0.04,
	Drag:    0.02,
}

// ItemType is a world.EntityType implementation for Item.
var ItemType itemType

type itemType struct{}

func (t itemType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	e := &Ent{tx: tx, handle: handle, data: data}
	if behaviour, ok := data.Data.(*ItemBehaviour); ok && !behaviour.collisionInitialised {
		behaviour.collisionInitialised = true
		// The native initial zero move seeds penetration state while the
		// spawn API preserves the requested item origin. OnGround starts set.
		behaviour.passive.mc.CheckCollision(tx, e, data.Pos, mgl64.Vec3{})
		behaviour.passive.mc.nativeBoxSet = false
		behaviour.passive.mc.onGround = true
		data.OnGround = true
	}
	return e
}

func (itemType) EncodeEntity() string { return "minecraft:item" }

// NetworkOffset is the native ItemActor height offset: half its 0.25-block
// collision height. The client adds visual bobbing independently of this origin.
func (itemType) NetworkOffset() float64 { return 0.125 }

// CollisionWidth returns the stored physical width, independently of rounding
// of the translated AABB bounds at large world coordinates.
func (itemType) CollisionWidth() float64 { return .25 }
func (itemType) BBox(e world.Entity) cube.BBox {
	if ent, ok := e.(*Ent); ok {
		if b, ok := ent.data.Data.(*ItemBehaviour); ok {
			c := b.passive.mc
			if c.nativeBoxSet && ent.Position() == c.nativeBoxPosition {
				lo, hi := c.nativeBox.Min(), c.nativeBox.Max()
				return cube.Box(float64(lo[0]), float64(lo[1]), float64(lo[2]), float64(hi[0]), float64(hi[1]), float64(hi[2])).Translate(ent.Position().Mul(-1))
			}
		}
	}
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}

func (itemType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := itemConf
	conf.Item = item.MapNBT(m, "Item")
	conf.PickupDelay = time.Duration(nbtconv.Int64(m, "PickupDelay")) * (time.Second / 20)

	data.Data = conf.New(world.BlockRegistryFromNBT(m))
}

func (itemType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*ItemBehaviour)
	return map[string]any{
		"Health":      int16(5),
		"PickupDelay": int64(b.pickupDelay / (time.Second / 20)),
		"Item":        item.WriteNBT(b.Item(), true),
	}
}
