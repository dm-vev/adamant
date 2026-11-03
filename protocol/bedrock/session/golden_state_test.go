package session_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/world"
)

// TestGoldenPlayerState records the state of the player a client is sent:
// health, food and experience, effects added, changed and removed, game
// modes with their abilities, flight, speed, input locks, the held slot,
// the spawn point, death and respawn, coordinates and instant respawn, and
// a transfer to another server.
func TestGoldenPlayerState(t *testing.T) {
	if isolated(t) {
		return
	}
	s := newScenario(t, "player_state", scenarioConfig{})

	s.step("health: hurt, healed, maximum and absorption", func(_ *world.Tx, p *player.Player) {
		p.Hurt(5, entity.FreezeDamageSource{})
		p.Heal(2, effect.InstantHealingSource{})
		p.SetMaxHealth(30)
		p.SetAbsorption(4)
	})
	s.step("food: set, added, saturated, exhausted", func(_ *world.Tx, p *player.Player) {
		p.SetFood(10)
		p.AddFood(3)
		p.Saturate(2, 4.5)
		p.Exhaust(1.5)
	})
	s.step("experience: added, level and progress set, removed", func(_ *world.Tx, p *player.Player) {
		p.AddExperience(30)
		p.SetExperienceLevel(5)
		p.SetExperienceProgress(0.5)
		p.RemoveExperience(10)
	})
	s.step("effects added", func(_ *world.Tx, p *player.Player) {
		p.AddEffect(effect.New(effect.Speed, 1, 30*time.Second))
		p.AddEffect(effect.NewAmbient(effect.Haste, 2, 10*time.Second))
		p.AddEffect(effect.NewInfinite(effect.NightVision, 1))
	})
	s.step("effect modified", func(_ *world.Tx, p *player.Player) {
		p.AddEffect(effect.New(effect.Speed, 3, 60*time.Second))
	})
	s.tick("effects tick", 1)
	s.step("effects removed", func(_ *world.Tx, p *player.Player) {
		p.RemoveEffect(effect.Speed)
		p.RemoveEffect(effect.NightVision)
	})
	s.gameModeStep("game mode: creative", 1, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeCreative)
	})
	s.step("flight speeds", func(_ *world.Tx, p *player.Player) {
		p.SetFlightSpeed(0.1)
		p.SetVerticalFlightSpeed(2)
	})
	s.gameModeStep("flight started", 1, func(_ *world.Tx, p *player.Player) {
		p.StartFlying()
	})
	s.gameModeStep("flight stopped", 1, func(_ *world.Tx, p *player.Player) {
		p.StopFlying()
	})
	s.gameModeStep("game mode: adventure", 1, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeAdventure)
	})
	// In spectator mode the abilities sent again make the player fly, which
	// sends them once more.
	s.gameModeStep("game mode: spectator", 2, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeSpectator)
	})
	s.gameModeStep("game mode: survival", 1, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeSurvival)
	})
	s.step("permissions refreshed", func(_ *world.Tx, p *player.Player) {
		p.RefreshPermissions()
	})
	s.step("speed", func(_ *world.Tx, p *player.Player) {
		p.SetSpeed(0.2)
	})
	s.step("input locks", func(_ *world.Tx, p *player.Player) {
		p.LockInput(input.Movement())
		p.LockInput(input.Camera())
		p.UnlockInput(input.Movement())
	})
	s.step("input locks cleared", func(_ *world.Tx, p *player.Player) {
		p.ClearInputLocks()
	})
	s.step("immobile and mobile", func(_ *world.Tx, p *player.Player) {
		p.SetImmobile()
		p.SetMobile()
	})
	s.step("held slot", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(3); err != nil {
			t.Error(err)
		}
	})
	s.step("respawn point", func(_ *world.Tx, p *player.Player) {
		p.SetRespawnPosition(cube.Pos{5, -60, 5}, world.Overworld)
	})
	s.step("coordinates shown and hidden, instant respawn", func(_ *world.Tx, p *player.Player) {
		p.ShowCoordinates()
		p.HideCoordinates()
		p.EnableInstantRespawn()
		p.DisableInstantRespawn()
	})
	s.step("experience cleared", func(_ *world.Tx, p *player.Player) {
		// A player that dies drops its experience as orbs that fly off at
		// random.
		p.SetExperienceLevel(0)
		p.SetExperienceProgress(0)
	})
	s.step("death", func(_ *world.Tx, p *player.Player) {
		p.Hurt(1000, entity.FreezeDamageSource{})
	})
	s.tick("after death", 2)
	s.step("respawn", func(_ *world.Tx, p *player.Player) {
		p.Respawn()
	})
	s.tick("after respawn", 2)
	s.step("transfer", func(_ *world.Tx, p *player.Player) {
		if err := p.Transfer("127.0.0.1:19133"); err != nil {
			t.Error(err)
		}
	})
	s.snapshot()
}
