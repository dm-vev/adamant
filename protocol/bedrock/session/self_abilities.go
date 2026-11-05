package session

import (
	"fmt"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// SendSpeed sends the speed of the player in an UpdateAttributes packet, so that it is updated client-side.
func (s *Session) SendSpeed(speed float64) {
	s.writePacket(&mc.UpdateAttributesPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []mc.Attribute{{
			Name:       "minecraft:movement",
			Value:      float32(speed),
			Max:        math.MaxFloat32,
			DefaultMax: math.MaxFloat32,
			Default:    0.1,
		}},
	})
}

// SendFood ...
func (s *Session) SendFood(food int, saturation, exhaustion float64) {
	s.writePacket(&mc.UpdateAttributesPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []mc.Attribute{
			{
				Name:       "minecraft:player.hunger",
				Value:      float32(food),
				Max:        20,
				DefaultMax: 20,
				Default:    20,
			},
			{
				Name:       "minecraft:player.saturation",
				Value:      float32(saturation),
				Max:        20,
				DefaultMax: 20,
				Default:    20,
			},
			{
				Name:       "minecraft:player.exhaustion",
				Value:      float32(exhaustion),
				Max:        5,
				DefaultMax: 5,
			},
		},
	})
}

// SendGameMode sends the game mode of the Controllable entity of the session to the client. It makes sure the right
// flags are set to create the full game mode.
func (s *Session) SendGameMode(c Controllable) {
	if s == Nop {
		return
	}
	s.writePacket(&mc.SetPlayerGameTypePacket{GameType: GameTypeFromMode(c.GameMode())})
	s.resendAbilities(c)
}

// resendAbilities queues an ability update after a game mode update. Bedrock ignores abilities sent immediately
// after the game type, so the update runs later on the controllable's world owner.
func (s *Session) resendAbilities(c Controllable) {
	delay := s.abilityResendDelay
	if delay <= 0 {
		delay = defaultAbilityResendDelay
	}

	s.abilityResendMu.Lock()
	if s.closing.Load() {
		s.abilityResendMu.Unlock()
		return
	}
	s.abilityResendGen++
	generation := s.abilityResendGen
	previous := s.abilityResend
	s.abilityResend = c.H().DoAfter(delay, func(_ *world.Tx, _ world.Entity) {
		s.abilityResendMu.Lock()
		if s.closing.Load() || s.abilityResendGen != generation {
			s.abilityResendMu.Unlock()
			return
		}
		s.abilityResend = nil
		s.abilityResendMu.Unlock()
		s.SendAbilities(c)
	})
	s.abilityResendMu.Unlock()
	if previous != nil {
		previous.Cancel()
	}
}

// SendAbilities sends the abilities of the Controllable entity of the session to the client.
func (s *Session) SendAbilities(c Controllable) {
	if s == Nop {
		return
	}
	if !c.GameMode().HasCollision() {
		// Spectator mode needs a teleport before the ability update on the
		// owning client, then flight may schedule a further ability resend.
		defer c.StartFlying()
		s.ViewEntityTeleport(c, c.Position())
	}
	data := s.abilityData(c, c.H().UniqueID())
	s.abilities.Store(&data)
	self := data
	self.EntityUniqueID = selfEntityRuntimeID
	s.writePacket(&mc.UpdateAbilitiesPacket{AbilityData: self})
}

// SendPermissions refreshes abilities for this player and the other clients
// that have the player in their player list, then refreshes command access.
func (s *Session) SendPermissions(c Controllable) {
	if s == Nop {
		return
	}
	s.SendAbilities(c)
	s.commandsDirty.Store(true)
	data := s.abilityData(c, c.H().UniqueID())
	s.abilities.Store(&data)
	s.sendAbilities(data)
}

// abilityData builds abilities without changing player state, so the same
// permissions can be included in AddPlayer and updates for other clients.
func (s *Session) abilityData(c Controllable, uniqueID int64) mc.AbilityData {
	mode, abilities := c.GameMode(), uint32(0)
	if mode.AllowsFlying() {
		abilities |= mc.AbilityMayFly
		if c.Flying() {
			abilities |= mc.AbilityFlying
		}
	}
	if !mode.HasCollision() {
		abilities |= mc.AbilityNoClip
	}
	if !mode.AllowsTakingDamage() {
		abilities |= mc.AbilityInvulnerable
	}
	if mode.CreativeInventory() {
		abilities |= mc.AbilityInstantBuild
	}
	perms := client.MemberPermissions()
	if s.conf.Permissions != nil {
		perms = s.conf.Permissions(c.UUID())
	}
	abilities |= roleAbilities(mode, perms)
	return mc.AbilityData{
		EntityUniqueID:     uniqueID,
		PlayerPermissions:  permissionLevel(perms.Role),
		CommandPermissions: commandLevel(perms.Level),
		Layers: []mc.AbilityLayer{
			{
				Type:             mc.AbilityLayerTypeBase,
				Abilities:        mc.AbilityCount - 1,
				Values:           abilities,
				FlySpeed:         float32(c.FlightSpeed()),
				VerticalFlySpeed: float32(c.VerticalFlightSpeed()),
				WalkSpeed:        mc.AbilityBaseWalkSpeed,
			},
		},
	}
}

// SendHealth sends the health and max health to the player.
func (s *Session) SendHealth(health, max, absorption float64) {
	if health <= 0 {
		s.resetInventory("death")
	}
	s.writePacket(&mc.UpdateAttributesPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []mc.Attribute{{
			Name:       "minecraft:health",
			Value:      float32(math.Ceil(health)),
			Max:        float32(math.Ceil(max)),
			DefaultMax: 20,
			Default:    20,
		}, {
			Name:       "minecraft:absorption",
			Value:      float32(math.Ceil(absorption)),
			Max:        float32(math.MaxFloat32),
			DefaultMax: float32(math.MaxFloat32),
		}},
	})
}

// SendEffect sends an effect newly added to the player.
func (s *Session) SendEffect(e effect.Effect) {
	s.sendEffect(e, mc.MobEffectAdd)
}

// SendEffectModification sends an effect of the player that replaced one of
// the same type, which BDS sends as a modification rather than a new effect.
func (s *Session) SendEffectModification(e effect.Effect) {
	s.sendEffect(e, mc.MobEffectModify)
}

func (s *Session) sendEffect(e effect.Effect, op byte) {
	id, _ := effect.ID(e.Type())
	dur := e.Duration() / (time.Second / 20)
	if e.Infinite() {
		dur = -1
	}
	s.writePacket(&mc.MobEffectPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Operation:       op,
		EffectType:      int32(id),
		Amplifier:       int32(e.Level() - 1),
		Particles:       !e.ParticlesHidden(),
		Duration:        int32(dur),
		Ambient:         e.Ambient(),
	})
}

// SendEffectRemoval sends the removal of an effect passed.
func (s *Session) SendEffectRemoval(e effect.Type) {
	id, ok := effect.ID(e)
	if !ok {
		panic(fmt.Sprintf("unregistered effect type %T", e))
	}
	s.writePacket(&mc.MobEffectPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Operation:       mc.MobEffectRemove,
		EffectType:      int32(id),
	})
}

// SendExperience sends the experience level and progress from the given experience manager to the player.
func (s *Session) SendExperience(level int, progress float64) {
	s.writePacket(&mc.UpdateAttributesPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []mc.Attribute{
			{
				Name:       "minecraft:player.level",
				Value:      float32(level),
				Max:        float32(math.MaxInt32),
				DefaultMax: float32(math.MaxInt32),
			},
			{
				Name:       "minecraft:player.experience",
				Value:      float32(progress),
				Max:        1,
				DefaultMax: 1,
			},
		},
	})
}

// SendChargeItemComplete sends a packet to indicate that the item charging process has been completed.
func (s *Session) SendChargeItemComplete() {
	s.writePacket(&mc.ActorEventPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		EventType:       mc.ActorEventFinishedChargingItem,
	})
}

// GameTypeFromMode returns the protocol game type ID for mode.
func GameTypeFromMode(mode world.GameMode) int32 {
	if mode.AllowsFlying() && mode.CreativeInventory() {
		return mc.GameTypeCreative
	}
	if !mode.Visible() && !mode.HasCollision() {
		return mc.GameTypeSpectator
	}
	if !mode.AllowsEditing() {
		return mc.GameTypeAdventure
	}
	return mc.GameTypeSurvival
}
