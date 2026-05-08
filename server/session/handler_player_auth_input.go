package session

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// PlayerAuthInputHandler handles the PlayerAuthInput packet.
type PlayerAuthInputHandler struct{}

// Handle ...
func (h PlayerAuthInputHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.PlayerAuthInput)
	if err := h.handleMovement(pk, s, tx, c); err != nil {
		return err
	}
	return h.handleActions(pk, s, tx, c)
}

// handleMovement handles the movement part of the packet.PlayerAuthInput.
func (h PlayerAuthInputHandler) handleMovement(pk *packet.PlayerAuthInput, s *Session, tx *world.Tx, c Controllable) error {
	yaw, pitch := c.Rotation().Elem()
	pos := c.Position()

	reference := []float64{pitch, yaw, yaw, pos[0], pos[1], pos[2]}
	for i, v := range [...]*float32{&pk.Pitch, &pk.Yaw, &pk.HeadYaw, &pk.Position[0], &pk.Position[1], &pk.Position[2]} {
		f := float64(*v)
		if math.IsNaN(f) || math.IsInf(f, 1) || math.IsInf(f, 0) {
			// Sometimes, the PlayerAuthInput packet is in fact sent with NaN/INF after being teleported (to another
			// world), see #425. For this reason, we don't actually return an error if this happens, because this will
			// result in the player being kicked. Just log it and replace the NaN value with the one we have tracked
			// server-side.
			s.conf.Log.Debug("process packet: PlayerAuthInput: found nan/inf values. assuming server-side values", "pos", fmt.Sprint(pk.Position), "yaw", pk.Yaw, "head-yaw", pk.HeadYaw, "pitch", pk.Pitch)
			*v = float32(reference[i])
		}
	}

	pk.Position = pk.Position.Sub(mgl32.Vec3{0, 1.62}) // Sub the base offset of players from the pos.

	newPos := vec32To64(pk.Position)
	deltaPos, deltaYaw, deltaPitch := newPos.Sub(pos), float64(pk.Yaw)-yaw, float64(pk.Pitch)-pitch

	if expected := s.teleportPos.Load(); expected != nil {
		if newPos.Sub(*expected).Len() > 1 {
			// The player has moved before it received the teleport packet. Ignore this movement entirely and
			// wait for the client to sync itself back to the server. Once we get a movement that is close
			// enough to the teleport position, we'll allow the player to move around again.
			return nil
		}
		s.teleportPos.Store(nil)
	}

	var ridingHandle *world.EntityHandle
	if rider, ok := c.(entity.Rider); ok {
		ridingHandle = rider.Riding()
	}
	if ridingHandle != nil {
		deltaPos = mgl64.Vec3{}
		if ridden, ok := ridingHandle.Entity(tx); ok {
			if input, ok := ridden.(entity.VehicleInput); ok {
				allowVehicleInput := true
				if controller, ok := ridden.(entity.VehicleController); ok {
					allowVehicleInput = controller.ControlsVehicle(c)
				}
				if allowVehicleInput {
					input.SetVehicleInput(float64(pk.MoveVector[0]), float64(pk.MoveVector[1]))
				}
			}
		}
	}
	if mgl64.FloatEqual(deltaPos.Len(), 0) && mgl64.FloatEqual(deltaYaw, 0) && mgl64.FloatEqual(deltaPitch, 0) {
		// The PlayerAuthInput packet is sent every tick, so don't do anything if the position and rotation
		// were unchanged.
		return nil
	}

	// Mark client-originated movement so we don't echo it back to the same client.
	s.moving.Store(true)
	c.Move(deltaPos, deltaYaw, deltaPitch)
	s.moving.Store(false)
	return nil
}

// handleActions handles the actions with the world that are present in the PlayerAuthInput packet.
func (h PlayerAuthInputHandler) handleActions(pk *packet.PlayerAuthInput, s *Session, tx *world.Tx, c Controllable) error {
	if pk.InputData.Load(packet.InputFlagPerformItemInteraction) {
		if err := h.handleUseItemData(pk.ItemInteractionData, s, c); err != nil {
			return err
		}
	}
	if pk.InputData.Load(packet.InputFlagPerformBlockActions) {
		if err := h.handleBlockActions(pk.BlockActions, s, tx, c); err != nil {
			return err
		}
	}
	h.handleInputFlags(pk.InputData, s, tx, c)

	if pk.InputData.Load(packet.InputFlagPerformItemStackRequest) {
		s.inTransaction.Store(true)
		defer s.inTransaction.Store(false)

		// As of 1.18 this is now used for sending item stack requests such as when mining a block.
		sh := s.handlers[packet.IDItemStackRequest].(*ItemStackRequestHandler)
		if err := sh.handleRequest(pk.ItemStackRequest, s, tx, c); err != nil {
			// Item stacks being out of sync isn't uncommon, so don't error. Just debug the error and let the
			// revert do its work.
			s.conf.Log.Debug("process packet: PlayerAuthInput: resolve item stack request: " + err.Error())
		}
	}
	return nil
}

// handleInputFlags handles the toggleable input flags set in a PlayerAuthInput packet.
func (h PlayerAuthInputHandler) handleInputFlags(flags protocol.Bitset, s *Session, tx *world.Tx, c Controllable) {
	dismountedByJump := h.tryDismount(flags, tx, c)
	if flags.Load(packet.InputFlagStartSprinting) {
		c.StartSprinting()
	}
	if flags.Load(packet.InputFlagStopSprinting) {
		c.StopSprinting()
	}
	if flags.Load(packet.InputFlagStartSneaking) {
		c.StartSneaking()
	}
	if flags.Load(packet.InputFlagStopSneaking) {
		c.StopSneaking()
	}
	if flags.Load(packet.InputFlagStartSwimming) {
		c.StartSwimming()
	}
	if flags.Load(packet.InputFlagStopSwimming) {
		c.StopSwimming()
	}
	if flags.Load(packet.InputFlagStartGliding) {
		c.StartGliding()
	}
	if flags.Load(packet.InputFlagStopGliding) {
		c.StopGliding()
	}
	if flags.Load(packet.InputFlagStartJumping) && !dismountedByJump {
		c.Jump()
	}
	if flags.Load(packet.InputFlagStartCrawling) {
		c.StartCrawling()
	}
	if flags.Load(packet.InputFlagStopCrawling) {
		c.StopCrawling()
	}
	if flags.Load(packet.InputFlagMissedSwing) {
		s.swingingArm.Store(true)
		defer s.swingingArm.Store(false)
		c.PunchAir()
	}
	if flags.Load(packet.InputFlagStartFlying) {
		if !c.GameMode().AllowsFlying() {
			s.conf.Log.Debug("process packet: PlayerAuthInput: flying flag enabled while unable to fly")
			s.SendAbilities(c)
		} else {
			c.StartFlying()
		}
	}
	if flags.Load(packet.InputFlagStopFlying) {
		c.StopFlying()
	}
}

func (h PlayerAuthInputHandler) tryDismount(flags protocol.Bitset, tx *world.Tx, c Controllable) bool {
	if !flags.Load(packet.InputFlagStartSneaking) && !flags.Load(packet.InputFlagStartJumping) {
		return false
	}
	rider, ok := c.(entity.Rider)
	if !ok {
		return false
	}
	handle := rider.Riding()
	if handle == nil {
		return false
	}
	dismounted := false
	if ridden, ok := handle.Entity(tx); ok {
		if dismountable, ok := ridden.(interface {
			Dismount(tx *world.Tx, passenger world.Entity)
		}); ok {
			dismountable.Dismount(tx, c)
			dismounted = true
		}
	}
	return dismounted && flags.Load(packet.InputFlagStartJumping)
}

// handleUseItemData handles the protocol.UseItemTransactionData found in a packet.PlayerAuthInput.
func (h PlayerAuthInputHandler) handleUseItemData(data protocol.UseItemTransactionData, s *Session, c Controllable) error {
	s.swingingArm.Store(true)
	defer s.swingingArm.Store(false)

	held, _ := c.HeldItems()
	if !held.Equal(stackToItem(s.br, data.HeldItem.Stack)) {
		s.conf.Log.Debug("process packet: PlayerAuthInput: UseItemTransaction: mismatch between actual held item and client held item")
		return nil
	}
	pos := cube.Pos{int(data.BlockPosition[0]), int(data.BlockPosition[1]), int(data.BlockPosition[2])}

	// Seems like this is only used for breaking blocks at the moment.
	switch data.ActionType {
	case protocol.UseItemActionBreakBlock:
		c.BreakBlock(pos)
	default:
		return fmt.Errorf("unhandled UseItem ActionType for PlayerAuthInput packet %v", data.ActionType)
	}
	return nil
}

// handleBlockActions handles a slice of protocol.PlayerBlockAction present in a PlayerAuthInput packet.
func (h PlayerAuthInputHandler) handleBlockActions(a []protocol.PlayerBlockAction, s *Session, tx *world.Tx, c Controllable) error {
	for _, action := range a {
		if err := handlePlayerAction(action.Action, action.Face, action.BlockPos, selfEntityRuntimeID, s, c); err != nil {
			return err
		}
	}
	return nil
}
