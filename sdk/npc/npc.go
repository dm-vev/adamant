// Package npc provides skinned human NPCs rendered natively as players (without
// polluting the tab list) with head tracking toward nearby players, equipment,
// interact/attack callbacks, and per-player visibility.
package npc

import (
	"math"
	"sync"

	"github.com/dm-vev/adamant/sdk/event"
	"github.com/dm-vev/adamant/sdk/plugin"
	"github.com/dm-vev/adamant/sdk/schedule"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/player/skin"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Option configures a Human NPC spawned by Spawn.
type Option func(*config)

type config struct {
	name       string
	scoreTag   string
	pos        mgl64.Vec3
	rot        cube.Rotation
	sk         skin.Skin
	hasSkin    bool
	mainHand   item.Stack
	offHand    item.Stack
	helmet     item.Stack
	chestplate item.Stack
	leggings   item.Stack
	boots      item.Stack
	scale      float64
	immobile   bool
	lookRadius float64
	onInteract func(p *player.Player, tx *world.Tx)
	onAttack   func(p *player.Player, tx *world.Tx)
}

// Skin sets the human skin rendered for the NPC.
func Skin(sk skin.Skin) Option { return func(c *config) { c.sk = sk; c.hasSkin = true } }

// Position sets the spawn position of the NPC.
func Position(pos mgl64.Vec3) Option { return func(c *config) { c.pos = pos } }

// Rotation sets the initial yaw/pitch rotation of the NPC.
func Rotation(rot cube.Rotation) Option { return func(c *config) { c.rot = rot } }

// ScoreTag sets the secondary line displayed below the NPC's nametag.
func ScoreTag(tag string) Option { return func(c *config) { c.scoreTag = tag } }

// HeldItems sets the items held in the NPC's main hand and off-hand.
func HeldItems(main, off item.Stack) Option {
	return func(c *config) { c.mainHand = main; c.offHand = off }
}

// Armour sets the armour pieces worn by the NPC.
func Armour(helmet, chestplate, leggings, boots item.Stack) Option {
	return func(c *config) {
		c.helmet = helmet
		c.chestplate = chestplate
		c.leggings = leggings
		c.boots = boots
	}
}

// Scale sets the visual scale of the NPC (defaults to 1.0).
func Scale(s float64) Option { return func(c *config) { c.scale = s } }

// Mobile allows the NPC to move rather than remaining frozen in place.
func Mobile() Option { return func(c *config) { c.immobile = false } }

// LookAtPlayers turns the NPC to face the nearest real player within radius
// blocks each tick.
func LookAtPlayers(radius float64) Option { return func(c *config) { c.lookRadius = radius } }

// OnInteract runs when a player right-clicks / interacts with the NPC.
func OnInteract(fn func(p *player.Player, tx *world.Tx)) Option {
	return func(c *config) { c.onInteract = fn }
}

// OnAttack runs when a player left-clicks / punches the NPC.
func OnAttack(fn func(p *player.Player, tx *world.Tx)) Option {
	return func(c *config) { c.onAttack = fn }
}

// NPC represents a live skinned human NPC spawned in a world.
type NPC struct {
	handle  *world.EntityHandle
	world   *world.World
	cleanup []func()
	once    sync.Once
}

// Spawn creates and spawns a skinned human NPC into tx. When ctx is non-nil,
// its event listeners, head-tracking system, and entity are cleaned up
// automatically when the plugin disables.
func Spawn(ctx *plugin.Context, tx *world.Tx, name string, opts ...Option) *NPC {
	if tx == nil || tx.Closed() {
		return nil
	}
	cfg := config{
		name:     name,
		immobile: true,
		scale:    1,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	sk := cfg.sk
	if !cfg.hasSkin {
		sk = skin.New(64, 64)
	}
	id := uuid.New()
	pConf := player.Config{
		UUID:     id,
		Name:     cfg.name,
		Position: cfg.pos,
		Rotation: cfg.rot,
		Skin:     sk,
		GameMode: world.GameModeSurvival,
	}
	h := world.EntitySpawnOpts{
		Position: cfg.pos,
		Rotation: cfg.rot,
		ID:       id,
		NameTag:  cfg.name,
	}.New(player.Type, pConf)

	ent := tx.AddEntity(h).(*player.Player)
	if cfg.immobile {
		ent.SetMobile()
		ent.SetImmobile()
	}
	if cfg.scale > 0 && cfg.scale != 1 {
		ent.SetScale(cfg.scale)
	}
	if cfg.scoreTag != "" {
		ent.SetScoreTag(cfg.scoreTag)
	}
	if !cfg.mainHand.Empty() || !cfg.offHand.Empty() {
		ent.SetHeldItems(cfg.mainHand, cfg.offHand)
	}
	if !cfg.helmet.Empty() {
		ent.Armour().SetHelmet(cfg.helmet)
	}
	if !cfg.chestplate.Empty() {
		ent.Armour().SetChestplate(cfg.chestplate)
	}
	if !cfg.leggings.Empty() {
		ent.Armour().SetLeggings(cfg.leggings)
	}
	if !cfg.boots.Empty() {
		ent.Armour().SetBoots(cfg.boots)
	}

	w := tx.World()
	n := &NPC{handle: ent.H(), world: w}

	// Always protect NPCs from player damage and dispatch OnAttack.
	subAtk := event.OnWorld[player.AttackEntityEvent](ctx, w, event.Normal, func(ev *player.AttackEntityEvent) {
		if ev.Target == nil || ev.Target.H() != n.handle {
			return
		}
		ev.Cancel()
		if cfg.onAttack != nil {
			cfg.onAttack(ev.Player, ev.Tx)
		}
	})
	n.cleanup = append(n.cleanup, subAtk.Unsubscribe)

	if cfg.onInteract != nil {
		subInt := event.OnWorld[player.ItemUseOnEntityEvent](ctx, w, event.Normal, func(ev *player.ItemUseOnEntityEvent) {
			if ev.Entity == nil || ev.Entity.H() != n.handle {
				return
			}
			ev.Cancel()
			cfg.onInteract(ev.Player, ev.Tx)
		})
		n.cleanup = append(n.cleanup, subInt.Unsubscribe)
	}

	if cfg.lookRadius > 0 {
		radiusSq := cfg.lookRadius * cfg.lookRadius
		rem := schedule.System(ctx, w, schedule.PostEntities, func(tickTx *world.Tx, _ int64) {
			raw, ok := n.handle.Entity(tickTx)
			if !ok {
				return
			}
			self, ok := raw.(*player.Player)
			if !ok {
				return
			}
			pos := self.Position()
			box := cube.Box(
				pos[0]-cfg.lookRadius, pos[1]-cfg.lookRadius, pos[2]-cfg.lookRadius,
				pos[0]+cfg.lookRadius, pos[1]+cfg.lookRadius, pos[2]+cfg.lookRadius,
			)
			var nearest *player.Player
			bestDist := radiusSq
			for other := range tickTx.EntitiesWithin(box) {
				if other.H() == n.handle {
					continue
				}
				pl, ok := other.(*player.Player)
				if !ok {
					continue
				}
				d := pl.Position().Sub(pos).LenSqr()
				if d < bestDist {
					bestDist = d
					nearest = pl
				}
			}
			if nearest == nil {
				return
			}
			delta := nearest.Position().Sub(pos)
			horiz := math.Hypot(delta[0], delta[2])
			targetYaw := math.Atan2(-delta[0], delta[2]) * 180 / math.Pi
			targetPitch := -math.Atan2(delta[1], horiz) * 180 / math.Pi
			cur := self.Rotation()
			self.Move(mgl64.Vec3{}, targetYaw-cur.Yaw(), targetPitch-cur.Pitch())
		})
		n.cleanup = append(n.cleanup, rem)
	}

	if ctx != nil {
		ctx.OnDisable(func() {
			n.stopHooks()
			if w != nil {
				w.Exec(func(closeTx *world.Tx) { n.Close(closeTx) })
			}
		})
	}
	return n
}

func (n *NPC) stopHooks() {
	n.once.Do(func() {
		for _, fn := range n.cleanup {
			if fn != nil {
				fn()
			}
		}
		n.cleanup = nil
	})
}

// Handle returns the underlying *world.EntityHandle for the NPC.
func (n *NPC) Handle() *world.EntityHandle {
	if n == nil {
		return nil
	}
	return n.handle
}

// Player resolves the live *player.Player backing the NPC inside tx.
func (n *NPC) Player(tx *world.Tx) (*player.Player, bool) {
	if n == nil || n.handle == nil || tx == nil || tx.Closed() {
		return nil, false
	}
	ent, ok := n.handle.Entity(tx)
	if !ok {
		return nil, false
	}
	p, ok := ent.(*player.Player)
	return p, ok
}

// Teleport moves the NPC to pos inside tx.
func (n *NPC) Teleport(tx *world.Tx, pos mgl64.Vec3) {
	if p, ok := n.Player(tx); ok {
		p.Teleport(pos)
	}
}

// SetNameTag updates the NPC's floating nametag inside tx.
func (n *NPC) SetNameTag(tx *world.Tx, name string) {
	if p, ok := n.Player(tx); ok {
		p.SetNameTag(name)
	}
}

// SetScoreTag updates the NPC's secondary line below its nametag inside tx.
func (n *NPC) SetScoreTag(tx *world.Tx, a ...any) {
	if p, ok := n.Player(tx); ok {
		p.SetScoreTag(a...)
	}
}

// SetSkin updates the NPC's skin inside tx.
func (n *NPC) SetSkin(tx *world.Tx, sk skin.Skin) {
	if p, ok := n.Player(tx); ok {
		p.SetSkin(sk)
	}
}

// HideFrom hides the NPC exclusively from viewer in tx.
func (n *NPC) HideFrom(viewer *player.Player, tx *world.Tx) {
	if viewer == nil {
		return
	}
	if p, ok := n.Player(tx); ok {
		viewer.HideEntity(p)
	}
}

// ShowTo reveals the NPC exclusively to viewer in tx.
func (n *NPC) ShowTo(viewer *player.Player, tx *world.Tx) {
	if viewer == nil {
		return
	}
	if p, ok := n.Player(tx); ok {
		viewer.ShowEntity(p)
	}
}

// Close detaches all hooks and removes the NPC from tx.
func (n *NPC) Close(tx *world.Tx) {
	if n == nil {
		return
	}
	n.stopHooks()
	if n.handle != nil {
		if tx != nil && !tx.Closed() {
			if ent, ok := n.handle.Entity(tx); ok {
				tx.RemoveEntity(ent)
			}
		}
		_ = n.handle.Close()
	}
}
