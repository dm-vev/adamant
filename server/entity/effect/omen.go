package effect

import (
	"image/color"
	"time"

	"github.com/df-mc/dragonfly/server/world"
)

// BadOmen is applied by ominous bottles and transforms into RaidOmen when its
// holder enters a village where a raid can begin.
var BadOmen badOmen

type badOmen struct{ nopLasting }

// RGBA returns the Java Bad Omen effect colour.
func (badOmen) RGBA() color.RGBA { return color.RGBA{R: 0x0b, G: 0x61, B: 0x38, A: 0xff} }

// Apply is unused: Bad Omen needs the active world transaction to detect a
// village and is applied through ApplyWithTx by the entity effect manager.
func (badOmen) Apply(world.Entity, Effect) {}

// ApplyWithTx asks the entity to activate its raid omen. The entity owns
// player eligibility, village detection, raid position, and the transition to
// RaidOmen because those depend on world-specific raid state.
func (badOmen) ApplyWithTx(e world.Entity, eff Effect, tx *world.Tx) {
	if target, ok := e.(interface{ TryActivateBadOmen(*world.Tx, int) bool }); ok && target.TryActivateBadOmen(tx, eff.Level()-1) {
		if remover, ok := e.(interface{ RemoveEffect(Type) }); ok {
			remover.RemoveEffect(BadOmen)
		}
	}
}

// RaidOmen is the short-lived transition effect that starts or extends the
// raid at the position recorded when BadOmen was converted.
var RaidOmen raidOmen

type raidOmen struct{ nopLasting }

// RGBA returns the Java Raid Omen effect colour.
func (raidOmen) RGBA() color.RGBA { return color.RGBA{R: 0xde, G: 0x40, B: 0x58, A: 0xff} }

// Apply is unused: Raid Omen starts a raid at expiry and requires a world
// transaction, so it is applied through ApplyWithTx.
func (raidOmen) Apply(world.Entity, Effect) {}

// ApplyWithTx starts a raid when the transition effect reaches its last tick.
func (raidOmen) ApplyWithTx(e world.Entity, eff Effect, tx *world.Tx) {
	if eff.Duration() > time.Second/20 {
		return
	}
	if target, ok := e.(interface{ ActivateRaidOmen(*world.Tx) bool }); ok && target.ActivateRaidOmen(tx) {
		if remover, ok := e.(interface{ RemoveEffect(Type) }); ok {
			remover.RemoveEffect(RaidOmen)
		}
	}
}
// TrialOmen is applied by trial spawners to begin an ominous trial.
var TrialOmen trialOmen

type trialOmen struct{ nopLasting }

// RGBA returns the Java Trial Omen effect colour.
func (trialOmen) RGBA() color.RGBA { return color.RGBA{R: 0x16, G: 0xa6, B: 0xa6, A: 0xff} }

func (trialOmen) Apply(world.Entity, Effect) {}
// HeroOfTheVillage is granted to players after they successfully defend a
// village from a raid.
var HeroOfTheVillage heroOfTheVillage

type heroOfTheVillage struct{ nopLasting }

// RGBA returns the Java Hero of the Village effect colour.
func (heroOfTheVillage) RGBA() color.RGBA {
	return color.RGBA{R: 0x44, G: 0xff, B: 0x44, A: 0xff}
}

func (heroOfTheVillage) Apply(world.Entity, Effect) {}

var _ LastingType = heroOfTheVillage{}

var _ LastingType = trialOmen{}

var _ LastingType = badOmen{}
var _ LastingType = raidOmen{}
var _ interface {
	ApplyWithTx(world.Entity, Effect, *world.Tx)
} = badOmen{}
var _ interface {
	ApplyWithTx(world.Entity, Effect, *world.Tx)
} = raidOmen{}

// RaidOmenDuration is the duration before a raid omen starts its raid.
const RaidOmenDuration = 30 * time.Second
