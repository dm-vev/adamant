package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// hurtHit is one call of Player.Hurt of a hurtCase: the damage and the source the attacker makes, and whether
// the player is still immune from an earlier hit, as the wall clock decides it.
type hurtHit struct {
	dmg    float64
	src    func(attacker world.Entity) world.DamageSource
	immune bool
}

// hurtCase is one run of TestHurtBehaviour: a player that stands in a flat world, facing east, with a cow
// two blocks ahead as the attacker.
type hurtCase struct {
	name  string
	setup func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler)
	hits  []hurtHit
}

func attackBy(attacker world.Entity) world.DamageSource {
	return entity.AttackDamageSource{Attacker: attacker}
}

func plainSource(src world.DamageSource) func(world.Entity) world.DamageSource {
	return func(world.Entity) world.DamageSource { return src }
}

// TestHurtBehaviour pins what Player.Hurt does over a table of hits: the damage dealt and whether the player was
// vulnerable, the events in order, the calls its client and a viewer saw, and the state of the player and of the
// attacker after each hit, as recorded before the method was split.
func TestHurtBehaviour(t *testing.T) {
	iron := func(p *Player) {
		p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
		p.Armour().SetChestplate(item.NewStack(item.Chestplate{Tier: item.ArmourTierIron{}}, 1))
		p.Armour().SetLeggings(item.NewStack(item.Leggings{Tier: item.ArmourTierIron{}}, 1))
		p.Armour().SetBoots(item.NewStack(item.Boots{Tier: item.ArmourTierIron{}}, 1))
	}
	attack := func(dmg float64) hurtHit { return hurtHit{dmg: dmg, src: attackBy} }
	plain := func(dmg float64, src world.DamageSource) hurtHit { return hurtHit{dmg: dmg, src: plainSource(src)} }
	shield := func(p *Player) {
		p.SetHeldItems(item.NewStack(item.Shield{}, 1), item.Stack{})
		p.sneaking = true
	}
	rule := func(name string, v bool) func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
		return func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { tx.World().SetGameRule(name, v) }
	}
	cases := []hurtCase{
		{name: "attack", hits: []hurtHit{attack(5)}},
		{name: "attack twice", hits: []hurtHit{attack(5), {dmg: 7, src: attackBy, immune: true}, {dmg: 3, src: attackBy, immune: true}}},
		{name: "attack twice not immune", hits: []hurtHit{attack(5), attack(5)}},
		{name: "negative", hits: []hurtHit{attack(-3)}},
		{name: "zero", hits: []hurtHit{attack(0)}},
		{name: "armour", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { iron(p) }, hits: []hurtHit{attack(8)}},
		{name: "armour fire", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { iron(p) }, hits: []hurtHit{plain(3, block.FireDamageSource{})}},
		{name: "armour and protection", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			iron(p)
			p.Armour().SetHelmet(p.Armour().Helmet().WithEnchantments(item.NewEnchantment(enchantment.Protection, 4)))
			p.Armour().SetBoots(p.Armour().Boots().WithEnchantments(item.NewEnchantment(enchantment.Protection, 4)))
		}, hits: []hurtHit{attack(8)}},
		{name: "armour thorns", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			iron(p)
			p.Armour().SetChestplate(p.Armour().Chestplate().WithEnchantments(item.NewEnchantment(enchantment.Thorns, 30)))
		}, hits: []hurtHit{attack(2)}},
		{name: "armour ignored by void", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { iron(p) }, hits: []hurtHit{plain(4, entity.VoidDamageSource{})}},
		{name: "resistance", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.AddEffect(effect.New(effect.Resistance, 2, time.Minute))
		}, hits: []hurtHit{attack(10)}},
		{name: "fire resistance fire", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.AddEffect(effect.New(effect.FireResistance, 1, time.Minute))
		}, hits: []hurtHit{plain(2, block.FireDamageSource{}), plain(2, block.LavaDamageSource{}), attack(2)}},
		{name: "fire off", setup: rule("fire_damage", false), hits: []hurtHit{plain(2, block.FireDamageSource{}), attack(2)}},
		{name: "drowning off", setup: rule("drowning_damage", false), hits: []hurtHit{plain(2, entity.DrowningDamageSource{}), attack(2)}},
		{name: "fall off", setup: rule("fall_damage", false), hits: []hurtHit{plain(2, entity.FallDamageSource{}), plain(2, block.StalagmiteDamageSource{}), attack(2)}},
		{name: "freeze off", setup: rule("freeze_damage", false), hits: []hurtHit{plain(2, entity.FreezeDamageSource{}), attack(2)}},
		{name: "fall", hits: []hurtHit{plain(6, entity.FallDamageSource{})}},
		{name: "fire", hits: []hurtHit{plain(1, block.FireDamageSource{})}},
		{name: "drowning", hits: []hurtHit{plain(2, entity.DrowningDamageSource{})}},
		{name: "projectile", hits: []hurtHit{{dmg: 6, src: func(a world.Entity) world.DamageSource {
			return entity.ProjectileDamageSource{Projectile: a, Owner: a}
		}}}},
		{name: "frost walker magma", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.Armour().SetBoots(item.NewStack(item.Boots{Tier: item.ArmourTierIron{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.FrostWalker, 2)))
		}, hits: []hurtHit{plain(1, block.MagmaDamageSource{}), attack(1)}},
		{name: "creative", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { p.SetGameMode(world.GameModeCreative) }, hits: []hurtHit{attack(5)}},
		{name: "event cancelled", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { h.cancel["Hurt"] = true }, hits: []hurtHit{attack(5), attack(5)}},
		{name: "event scales", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { h.scaleHurt = 0.5 }, hits: []hurtHit{attack(8)}},
		{name: "absorption", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { p.SetAbsorption(4) }, hits: []hurtHit{attack(3), attack(6)}},
		{name: "absorption effect", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.AddEffect(effect.New(effect.Absorption, 1, time.Minute))
		}, hits: []hurtHit{attack(3), attack(3), attack(3)}},
		{name: "shield", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { shield(p) }, hits: []hurtHit{attack(2), attack(4)}},
		{name: "shield behind", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			shield(p)
			p.data.Rot = cube.Rotation{90, 0}
		}, hits: []hurtHit{attack(4)}},
		{name: "shield not sneaking", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			shield(p)
			p.sneaking = false
		}, hits: []hurtHit{attack(4)}},
		{name: "shield offhand", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.Stack{}, item.NewStack(item.Shield{}, 1))
			p.sneaking = true
		}, hits: []hurtHit{attack(6)}},
		{name: "shield and fall", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { shield(p) }, hits: []hurtHit{plain(4, entity.FallDamageSource{})}},
		{name: "shield and projectile", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { shield(p) }, hits: []hurtHit{{dmg: 4, src: func(a world.Entity) world.DamageSource {
			return entity.ProjectileDamageSource{Projectile: a, Owner: a}
		}}}},
		{name: "totem offhand", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), item.NewStack(item.Totem{}, 2))
		}, hits: []hurtHit{attack(30)}},
		{name: "totem main hand", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.NewStack(item.Totem{}, 1), item.Stack{})
		}, hits: []hurtHit{attack(30)}},
		{name: "totem and void", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.Stack{}, item.NewStack(item.Totem{}, 1))
		}, hits: []hurtHit{plain(30, entity.VoidDamageSource{})}},
		{name: "totem not needed", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.Stack{}, item.NewStack(item.Totem{}, 1))
		}, hits: []hurtHit{attack(5)}},
		{name: "kill", hits: []hurtHit{attack(30)}},
		{name: "kill by fall", hits: []hurtHit{plain(30, entity.FallDamageSource{})}},
		{name: "kill exact", hits: []hurtHit{attack(20)}},
		{name: "kill and hurt dead", hits: []hurtHit{attack(30), attack(1)}},
		{name: "kill in several hits", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { p.addHealth(-14) }, hits: []hurtHit{attack(3), attack(3)}},
		{name: "kill keeping inventory", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			tx.World().SetGameRule("keep_inventory", true)
			p.SetHeldItems(item.NewStack(item.Bread{}, 3), item.Stack{})
		}, hits: []hurtHit{attack(30)}},
		{name: "kill with items", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.NewStack(item.Bread{}, 3), item.Stack{})
			iron(p)
		}, hits: []hurtHit{attack(30)}},
		{name: "sleeping", setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			tx.World().SetTime(18000)
			sleepInBed(t, tx, p, block.Bed{Facing: cube.North})
		}, hits: []hurtHit{attack(2)}},
		{name: "exhaustion", hits: []hurtHit{attack(2), plain(2, entity.VoidDamageSource{})}},
	}
	tr := &transcript{}
	for _, c := range cases {
		runHurtCase(t, tr, c)
	}
	checkTranscript(t, "hurt", tr)
}

func runHurtCase(t *testing.T, tr *transcript, c hurtCase) {
	t.Helper()
	w := tickBenchmarkWorld(t)
	viewer := clienttest.New()
	loader := world.NewLoader(2, w, viewer)
	r := clienttest.New()
	start := mgl64.Vec3{0.5, tickBenchmarkGround, 0.5}
	handle := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
		Name: "victim", Position: start, GameMode: world.GameModeSurvival, Client: r,
	})
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, start)
		loader.Load(tx, 25)
		p := tx.AddEntity(handle).(*Player)
		th := &traceHandler{tr: tr, cancel: map[string]bool{}}
		p.Handle(th)
		attacker := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, tickBenchmarkGround, 0.5}}))
		p.data.Rot = cube.Rotation{-90, 0}
		tr.linef("== %s", c.name)
		if c.setup != nil {
			c.setup(t, tx, p, th)
		}
		viewer.Reset()
		r.Reset()
		tr.linef("start %s", digest(p))
		for i, hit := range c.hits {
			if hit.immune {
				p.immuneUntil = time.Now().Add(time.Hour)
			} else {
				p.immuneUntil = time.Time{}
			}
			dealt, vulnerable := p.Hurt(hit.dmg, hit.src(attacker))
			tr.linef("hit %d dmg=%.4f dealt=%.4f vulnerable=%v", i, hit.dmg, dealt, vulnerable)
			tr.linef("after %s lastDamage=%.4f lastHurtBy=%v dead=%v", digest(p), p.lastDamage, p.lastHurtBy != nil, p.Dead())
			tr.calls("viewer", viewer)
			tr.calls("client", r)
			cow := attacker.(entity.Living)
			tr.linef("attacker health=%.4f velocity=%s", cow.Health(), describeArg(cow.Velocity()))
		}
		_ = p.Close()
	})
	<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
}
