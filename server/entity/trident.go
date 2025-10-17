package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"time"
)

const (
	tridentBaseDamage       = 8.0
	loyaltyReturnDelay      = time.Second
	loyaltyCollectionRadius = 1.5
)

// NewTrident creates a new thrown trident entity using the provided configuration values.
func NewTrident(opts world.EntitySpawnOpts, owner world.Entity, stack item.Stack, loyaltyLevel, impalingLevel int, channeling bool) *world.EntityHandle {
	conf := tridentConf
	if owner != nil {
		conf.Owner = owner.H()
	}
	conf.PickupItem = stack
	conf.DisablePickup = stack.Empty()
	trident := TridentConfig{
		Projectile: conf,
		Item:       stack,
		Loyalty:    loyaltyLevel,
		Impaling:   impalingLevel,
		Channeling: channeling,
	}
	return opts.New(TridentType, trident)
}

var tridentConf = ProjectileBehaviourConfig{
	Gravity:               0.05,
	Drag:                  0.01,
	Damage:                tridentBaseDamage,
	Sound:                 sound.TridentHit{},
	SurviveBlockCollision: true,
}

// TridentConfig holds the configuration for a thrown trident entity.
type TridentConfig struct {
	Projectile ProjectileBehaviourConfig
	Item       item.Stack
	Loyalty    int
	Impaling   int
	Channeling bool
	Returning  bool
}

// Apply applies the configuration to the entity data.
func (conf TridentConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a new TridentBehaviour from the configuration.
func (conf TridentConfig) New() *TridentBehaviour {
	behaviour := &TridentBehaviour{
		item:       conf.Item,
		loyalty:    conf.Loyalty,
		impaling:   conf.Impaling,
		channeling: conf.Channeling,
		returning:  conf.Returning,
	}
	projectile := conf.Projectile
	projectile.Hit = behaviour.hit
	behaviour.ProjectileBehaviour = projectile.New()
	behaviour.item = behaviour.ProjectileBehaviour.conf.PickupItem
	return behaviour
}

// TridentBehaviour implements the behaviour for thrown trident entities.
type TridentBehaviour struct {
	*ProjectileBehaviour

	item       item.Stack
	loyalty    int
	impaling   int
	channeling bool
	returning  bool
}

// Tick progresses the trident's behaviour.
func (b *TridentBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.loyalty > 0 && !b.returning {
		if b.ProjectileBehaviour.collided || e.Age() >= loyaltyReturnDelay {
			b.startReturn()
		}
	}

	if b.returning {
		b.ProjectileBehaviour.collided = false
		b.ProjectileBehaviour.collisionPos = cube.Pos{}
		b.ProjectileBehaviour.conf.DisablePickup = true

		if owner, ok := b.ProjectileBehaviour.conf.Owner.Entity(tx); ok {
			target := EyePosition(owner)
			delta := target.Sub(e.Position())
			dist := delta.Len()
			if dist > 0 {
				speed := enchantment.Loyalty.ReturnSpeed(b.loyalty)
				e.SetVelocity(delta.Normalize().Mul(speed))
			}
			if dist <= loyaltyCollectionRadius {
				if collector, ok := owner.(Collector); ok {
					if b.item.Empty() {
						tx.PlaySound(owner.Position(), sound.TridentReturn{})
						_ = e.CloseIn(tx)
						return nil
					}
					if n, ok := collector.Collect(b.item); ok && n > 0 {
						viewers := tx.Viewers(owner.Position())
						for _, viewer := range viewers {
							viewer.ViewEntityAction(e, PickedUpAction{Collector: collector})
						}
						tx.ReleaseViewers(viewers)
						tx.PlaySound(owner.Position(), sound.TridentReturn{})
						_ = e.CloseIn(tx)
						return nil
					}
				}
				if !b.item.Empty() {
					tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: target}, b.item))
				}
				tx.PlaySound(target, sound.TridentReturn{})
				_ = e.CloseIn(tx)
				return nil
			}
		} else {
			b.returning = false
			b.ProjectileBehaviour.conf.DisablePickup = b.item.Empty()
		}
	}

	movement := b.ProjectileBehaviour.Tick(e, tx)
	if b.returning {
		b.ProjectileBehaviour.close = false
	}
	return movement
}

func (b *TridentBehaviour) hit(e *Ent, tx *world.Tx, result trace.Result) {
	switch r := result.(type) {
	case trace.EntityResult:
		if living, ok := r.Entity().(Living); ok {
			if b.impaling > 0 {
				if bonus := enchantment.Impaling.Damage(b.impaling, r.Entity(), tx); bonus > 0 {
					owner, _ := b.ProjectileBehaviour.conf.Owner.Entity(tx)
					living.Hurt(bonus, ProjectileDamageSource{Projectile: e, Owner: owner})
				}
			}
		}
		b.startReturn()
	case trace.BlockResult:
		if b.channeling && tx.ThunderingAt(r.BlockPosition()) {
			if create := tx.World().EntityRegistry().Config().Lightning; create != nil {
				tx.AddEntity(create(world.EntitySpawnOpts{Position: r.Position()}))
			}
			tx.PlaySound(r.Position(), sound.TridentChanneling{})
		}
		b.startReturn()
	}
}

func (b *TridentBehaviour) startReturn() {
	if b.loyalty <= 0 {
		return
	}
	b.returning = true
	b.ProjectileBehaviour.close = false
	b.ProjectileBehaviour.collided = false
	b.ProjectileBehaviour.collisionPos = cube.Pos{}
}

// TridentType is a world.EntityType implementation for thrown tridents.
var TridentType tridentType

type tridentType struct{}

func (t tridentType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (t tridentType) EncodeEntity() string { return "minecraft:thrown_trident" }

func (t tridentType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.25, 0, -0.25, 0.25, 0.35, 0.25)
}

func (t tridentType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := tridentConf
	conf.DisablePickup = !nbtconv.Bool(m, "player")
	conf.PickupItem = nbtconv.MapItem(m, "Item")
	conf.CollisionPosition = nbtconv.Pos(m, "StuckToBlockPos")
	trident := TridentConfig{
		Projectile: conf,
		Item:       conf.PickupItem,
		Loyalty:    int(nbtconv.Int32(m, "loyalty")),
		Impaling:   int(nbtconv.Int32(m, "impaling")),
		Channeling: nbtconv.Bool(m, "channeling"),
		Returning:  nbtconv.Bool(m, "returning"),
	}
	data.Data = trident.New()
}

func (t tridentType) EncodeNBT(data *world.EntityData) map[string]any {
	behaviour := data.Data.(*TridentBehaviour)
	m := map[string]any{
		"Damage":     float32(behaviour.ProjectileBehaviour.conf.Damage),
		"player":     boolByte(!behaviour.ProjectileBehaviour.conf.DisablePickup),
		"loyalty":    int32(behaviour.loyalty),
		"impaling":   int32(behaviour.impaling),
		"channeling": boolByte(behaviour.channeling),
		"returning":  boolByte(behaviour.returning),
	}
	if !behaviour.item.Empty() {
		m["Item"] = nbtconv.WriteItem(behaviour.item, true)
	}
	if behaviour.collided {
		m["StuckToBlockPos"] = nbtconv.PosToInt32Slice(behaviour.collisionPos)
	}
	return m
}
