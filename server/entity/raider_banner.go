package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// obtainRaidLeaderBannerGoal ports Raider.ObtainRaidLeaderBannerGoal: a
// raider of a wave without a captain goes for an ominous banner it can
// reach, giving up on unreachable banners for half a minute.
type obtainRaidLeaderBannerGoal struct {
	goalFlags
	path        *mobPath
	banner      *world.EntityHandle
	unreachable map[*world.EntityHandle]int64
}

func (g *obtainRaidLeaderBannerGoal) CanUse(m *Mob) bool {
	if raiderCannotPickUpBanner(m) {
		return false
	}
	now := m.tx.CurrentTick()
	kept := map[*world.EntityHandle]int64{}
	fr := m.AI.FollowRange
	box := m.BBox().GrowVec3([3]float64{fr, 8, fr})
	for e := range m.tx.EntitiesWithin(box) {
		stack, ok := pickableItem(e)
		if !ok || !ominousBanner(stack) {
			continue
		}
		if until, ok := g.unreachable[e.H()]; ok && now < until {
			kept[e.H()] = until
			continue
		}
		if path := m.AI.nav.CreatePathToEntity(m, e, 1); path != nil && path.CanReach() {
			g.path, g.banner = path, e.H()
			return true
		}
		kept[e.H()] = now + 600
	}
	g.unreachable = kept
	return false
}

func (g *obtainRaidLeaderBannerGoal) CanContinueToUse(m *Mob) bool {
	if g.banner == nil || g.path == nil {
		return false
	}
	if _, ok := g.banner.Entity(m.tx); !ok {
		return false
	}
	return !g.path.Done() && !raiderCannotPickUpBanner(m)
}

func (g *obtainRaidLeaderBannerGoal) Start(m *Mob) { m.AI.nav.MoveTo(m, g.path, 1.15) }

func (g *obtainRaidLeaderBannerGoal) Stop(*Mob) { g.path, g.banner = nil, nil }

func (g *obtainRaidLeaderBannerGoal) Tick(m *Mob) {
	if g.banner == nil {
		return
	}
	e, ok := g.banner.Entity(m.tx)
	if ok && e.Position().Sub(m.data.Pos).LenSqr() < 1.414*1.414 {
		raiderPickUpItem(m, e)
	}
}

// ominousBanner reports whether a stack is the ominous banner.
func ominousBanner(s item.Stack) bool {
	return !s.Empty() && s.Comparable(ominousBannerStack())
}

// raiderCanBeLeader ports Raider.canBeLeader: ravagers and witches cannot.
func raiderCanBeLeader(m *Mob) bool {
	switch m.H().Type() {
	case RavagerType, WitchType:
		return false
	}
	return true
}

// raiderCannotPickUpBanner ports ObtainRaidLeaderBannerGoal.cannotPickUpBanner.
func raiderCannotPickUpBanner(m *Mob) bool {
	r, ok := m.Behaviour().(interface{ raider() *raiderState })
	if !ok || !raiderCanBeLeader(m) {
		return true
	}
	raid := r.raider().activeRaid(m)
	if raid == nil {
		return true
	}
	if h, ok := equipmentHolderOf(m.Self); ok && ominousBanner(*h.equipment(slotHead)) {
		return true
	}
	leader, ok := m.tx.RaidLeader(raid, raiderWave(m))
	return ok && entityAlive(leader)
}

// raiderWave returns the wave of the raid a raider joined.
func raiderWave(m *Mob) int {
	if b, ok := m.Behaviour().(*hostileState); ok {
		return b.raidWave
	}
	if b, ok := m.Behaviour().(*WitchBehaviour); ok {
		return b.raidWave
	}
	return 0
}

// raiderPickUpItem ports Raider.pickUpItem for the ominous banner: a raider
// of a wave without a captain puts it on and becomes the captain, maybe
// dropping what it wore.
func raiderPickUpItem(m *Mob, e world.Entity) {
	stack, ok := pickableItem(e)
	b, isHostile := m.Behaviour().(*hostileState)
	if !ok || !isHostile {
		return
	}
	raid := b.activeRaid(m)
	if raid == nil || !ominousBanner(stack) {
		return
	}
	if _, hasLeader := m.tx.RaidLeader(raid, b.raidWave); hasLeader {
		return
	}
	if current := b.head; !current.Empty() && float32(math.Max(float64(m.Random().NextFloat())-.1, 0)) < b.dropChance(slotHead) {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.data.Pos}, current))
	}
	if c, ok := m.Self.(Collector); ok {
		viewers := m.tx.Viewers(e.Position())
		for _, v := range viewers {
			v.ViewEntityAction(e, PickedUpAction{Collector: c})
		}
		m.tx.ReleaseViewers(viewers)
	}
	takeItemEntity(m, e, stack.Count())
	// Raid.setLeader puts the banner on, sure to drop.
	m.tx.SetRaidLeader(raid, b.raidWave, m.Self)
	b.head, b.headDropGuaranteed = ominousBannerStack(), true
	b.patrolLeader = true
	viewEquipment(m.tx, m.Self)
}
