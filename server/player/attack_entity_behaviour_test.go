package player

import (
	"github.com/google/uuid"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// attackCase is one attack of TestAttackEntityBehaviour: the player holds held and has charged for charge
// ticks, spawn makes its target 2 blocks ahead, and edit changes the player before it attacks.
type attackCase struct {
	name   string
	held   item.Stack
	charge int
	spawn  func(opts world.EntitySpawnOpts) *world.EntityHandle
	edit   func(p *Player, h *traceHandler)
}

func playerTarget(opts world.EntitySpawnOpts) *world.EntityHandle {
	opts.ID = uuid.New()
	return opts.New(Type, Config{Position: opts.Position, GameMode: world.GameModeSurvival, Name: "Target", Log: slog.New(slog.DiscardHandler)})
}

func cowTarget(opts world.EntitySpawnOpts) *world.EntityHandle { return entity.NewCow(opts) }

// TestAttackEntityBehaviour pins what Player.AttackEntity does over a table of attacks: its result, the
// events it fires in order, the calls a viewer and the client of the player see, the health and velocity of
// the target and of three bystanders (one beside it, one beyond and one aside), and what the attacker is left
// with. The transcript of testdata/behaviour/attack_entity.txt was recorded before the method was split.
func TestAttackEntityBehaviour(t *testing.T) {
	sword := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
	axe := item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1)
	mace := item.NewStack(item.Mace{}, 1)
	ench := func(s item.Stack, e ...item.Enchantment) item.Stack { return s.WithEnchantments(e...) }
	fall := func(d float64) func(p *Player, h *traceHandler) {
		return func(p *Player, h *traceHandler) { p.fallDistance, p.onGround = d, false }
	}
	cases := []attackCase{
		{name: "fist", held: item.Stack{}},
		{name: "sword uncharged", held: sword},
		{name: "sword charged sweeps", held: sword, charge: 12},
		{name: "sword charged sprinting", held: sword, charge: 12, edit: func(p *Player, h *traceHandler) { p.sprinting = true }},
		{name: "sword critical", held: sword, charge: 12, edit: fall(1.5)},
		{name: "sword critical uncharged", held: sword, edit: fall(1.5)},
		{name: "sword critical blind", held: sword, charge: 12, edit: func(p *Player, h *traceHandler) {
			fall(1.5)(p, h)
			p.AddEffect(effect.New(effect.Blindness, 1, 1<<30))
		}},
		{name: "sword critical slow falling", held: sword, charge: 12, edit: func(p *Player, h *traceHandler) {
			fall(1.5)(p, h)
			p.AddEffect(effect.New(effect.SlowFalling, 1, 1<<30))
		}},
		{name: "sword critical flying", held: sword, charge: 12, edit: func(p *Player, h *traceHandler) {
			fall(1.5)(p, h)
			p.flying = true
		}},
		{name: "sharpness", held: ench(sword, item.NewEnchantment(enchantment.Sharpness, 3)), charge: 12},
		{name: "sharpness critical", held: ench(sword, item.NewEnchantment(enchantment.Sharpness, 5)), charge: 12, edit: fall(2)},
		{name: "knockback", held: ench(sword, item.NewEnchantment(enchantment.Knockback, 2)), charge: 12},
		{name: "fire aspect", held: ench(sword, item.NewEnchantment(enchantment.FireAspect, 2)), charge: 12},
		{name: "sweeping edge", held: ench(sword, item.NewEnchantment(enchantment.SweepingEdge, 3)), charge: 12},
		{name: "smite on undead", held: ench(sword, item.NewEnchantment(enchantment.Smite, 5)), charge: 12,
			spawn: entity.NewZombie},
		{name: "bane on spider", held: ench(sword, item.NewEnchantment(enchantment.BaneOfArthropods, 5)), charge: 12,
			spawn: entity.NewSpider},
		{name: "kill with sharpness", held: ench(sword, item.NewEnchantment(enchantment.Sharpness, 40)), charge: 12},
		{name: "axe", held: axe, charge: 12},
		{name: "strength", held: sword, charge: 12, edit: func(p *Player, h *traceHandler) { p.AddEffect(effect.New(effect.Strength, 2, 1<<30)) }},
		{name: "weakness", held: sword, charge: 12, edit: func(p *Player, h *traceHandler) { p.AddEffect(effect.New(effect.Weakness, 2, 1<<30)) }},
		{name: "damaged sword", held: sword.Damage(100), charge: 12},
		{name: "mace no fall", held: mace, charge: 12},
		{name: "mace smash", held: mace, charge: 12, edit: fall(6)},
		{name: "mace smash gliding", held: mace, charge: 12, edit: func(p *Player, h *traceHandler) {
			fall(6)(p, h)
			p.gliding = true
		}},
		{name: "mace smash density breach", held: ench(mace, item.NewEnchantment(enchantment.Density, 3), item.NewEnchantment(enchantment.Breach, 2)),
			charge: 12, edit: fall(6)},
		{name: "mace smash wind burst", held: ench(mace, item.NewEnchantment(enchantment.WindBurst, 2)), charge: 12, edit: fall(6)},
		{name: "spear", held: item.NewStack(item.Spear{Tier: item.ToolTierIron}, 1), charge: 12},
		{name: "event cancelled", held: sword, charge: 12, edit: func(p *Player, h *traceHandler) { h.cancel["AttackEntity"] = true }},
		{name: "event changes", held: sword, charge: 12, edit: func(p *Player, h *traceHandler) {
			fall(1.5)(p, h)
			h.attackForce, h.attackHeight, h.noCritical = 1.2, 0.5, true
		}},
		{name: "creative", held: sword, charge: 12, edit: func(p *Player, h *traceHandler) { p.SetGameMode(world.GameModeCreative) }},
		{name: "player", held: sword, charge: 12, spawn: playerTarget},
		{name: "player critical", held: sword, charge: 12, spawn: playerTarget, edit: fall(1.5)},
		{name: "player pvp off", held: sword, charge: 12, spawn: playerTarget, edit: func(p *Player, h *traceHandler) { p.tx.World().SetGameRule("pvp", false) }},
		{name: "armour stand", held: sword, charge: 12,
			spawn: entity.NewArmourStand},
		{name: "boat", held: sword, charge: 12,
			spawn: func(o world.EntitySpawnOpts) *world.EntityHandle { return entity.NewBoat(o, 0) }},
		{name: "boat fist", held: item.Stack{},
			spawn: func(o world.EntitySpawnOpts) *world.EntityHandle { return entity.NewBoat(o, 0) }},
		{name: "out of reach", held: sword, charge: 12, edit: func(p *Player, h *traceHandler) {
			p.tx.World().SetGameRule("pvp", true)
			p.data.Pos = mgl64.Vec3{-20, 100, 0.5}
		}},
	}
	tr := &transcript{}
	for _, c := range cases {
		runAttackCase(t, tr, c)
	}
	checkTranscript(t, "attack_entity", tr)
}

func runAttackCase(t *testing.T, tr *transcript, c attackCase) {
	t.Helper()
	w, viewer := clientTestWorld(t)
	r := clienttest.New()
	pos := mgl64.Vec3{0.5, 100, 0.5}
	withCheckedPlayer(t, w, r, anticheat.Default(), pos, func(tx *world.Tx, p *Player) {
		h := &traceHandler{tr: tr, cancel: map[string]bool{}}
		p.Handle(h)
		spawn := c.spawn
		if spawn == nil {
			spawn = cowTarget
		}
		target := tx.AddEntity(spawn(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 100, 0.5}}))
		var bystanders []world.Entity
		for _, bp := range []mgl64.Vec3{{2.5, 100, 1.4}, {3.7, 100, 0.5}, {0.5, 100, 2.9}} {
			bystanders = append(bystanders, tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: bp})))
		}
		p.data.Rot = cube.Rotation{-90, 0}
		r.SetHandlingInput(true)
		p.SetHeldItems(c.held, item.Stack{})
		for range c.charge {
			p.tickAttackStrength()
		}
		p.onGround = true
		if c.edit != nil {
			c.edit(p, h)
		}
		tr.linef("== %s", c.name)
		viewer.Reset()
		r.Reset()
		result := p.AttackEntity(target)
		tr.linef("result %v", result)
		tr.calls("viewer", viewer)
		tr.calls("client", r)
		report := func(name string, e world.Entity) {
			if l, ok := e.(entity.Living); ok {
				tr.linef("%s health=%.4f dead=%v closed=%v velocity=%s fire=%v", name, l.Health(), l.Dead(), e.H().Closed(), describeArg(l.Velocity()), onFire(e))
			} else {
				tr.linef("%s closed=%v", name, e.H().Closed())
			}
		}
		report("target", target)
		names := []string{"beside", "beyond", "aside"}
		for i, b := range bystanders {
			report(names[i], b)
		}
		held, _ := p.HeldItems()
		name, meta := "empty", int16(0)
		if !held.Empty() {
			name, meta = held.Item().EncodeItem()
		}
		tr.linef("player held=%s/%d count=%d durability=%d exhaustion=%.4f strength=%d fall=%.4f velocity=%s lastHurtMob=%v",
			name, meta, held.Count(), held.Durability(), p.hunger.exhaustionLevel, p.attackStrengthTicks, p.fallDistance, describeArg(p.Velocity()), lastHurtMobType(p))
	})
}

func onFire(e world.Entity) bool {
	if f, ok := e.(entity.Flammable); ok {
		return f.OnFireDuration() > 0
	}
	return false
}

func lastHurtMobType(p *Player) string {
	e, _ := p.LastHurtMob()
	if e == nil {
		return "none"
	}
	return e.H().Type().EncodeEntity()
}
