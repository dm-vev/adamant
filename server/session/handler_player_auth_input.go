package session

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// PlayerAuthInputHandler handles the PlayerAuthInput packet.
type PlayerAuthInputHandler struct{}

// Handle ...
func (h PlayerAuthInputHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*v2193.PlayerAuthInputPacket)
	if err := h.handleMovement(pk, s, tx, c); err != nil {
		return err
	}
	return h.handleActions(pk, s, tx, c)
}

// handleMovement handles the movement part of the PlayerAuthInput packet.
func (h PlayerAuthInputHandler) handleMovement(pk *v2193.PlayerAuthInputPacket, s *Session, tx *world.Tx, c Controllable) error {
	yaw, pitch := c.Rotation().Elem()
	pos := c.Position()
	networkPos := entityNetworkPosition(c, pos)

	reference := []float64{pitch, yaw, yaw, networkPos[0], networkPos[1], networkPos[2]}
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

	pk.Position = entityBasePosition(c, pk.Position)

	newPos := clampToWorld(vec32To64(pk.Position))
	deltaPos, deltaYaw, deltaPitch := newPos.Sub(pos), float64(pk.Yaw)-yaw, float64(pk.Pitch)-pitch

	if expected := s.teleportPos.Load(); expected != nil {
		if teleportAcknowledged(newPos, *expected) {
			s.teleportPos.Store(nil)
		} else {
			if !mgl64.FloatEqual(deltaPos.Len(), 0) || !mgl64.FloatEqual(deltaYaw, 0) || !mgl64.FloatEqual(deltaPitch, 0) {
				// The player has moved before it received the teleport packet. Ignore this movement entirely and
				// wait for the client to sync itself back to the server. Once we get a movement that is close
				// enough to the teleport position, we'll allow the player to move around again.
				return nil
			}
		}
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
					if withRider, ok := ridden.(entity.VehicleInputWithRider); ok {
						withRider.SetVehicleInputFrom(c, float64(pk.MoveVector[0]), float64(pk.MoveVector[1]))
					} else {
						input.SetVehicleInput(float64(pk.MoveVector[0]), float64(pk.MoveVector[1]))
					}
					if jump, ok := ridden.(entity.VehicleJumpInput); ok {
						jump.SetVehicleJumping(pk.InputData.Has(v2193.InputFlagJumping) || pk.InputData.Has(v2193.InputFlagJumpDown))
					}
				}
			}
		}
	}
	if mgl64.FloatEqual(deltaPos.Len(), 0) && mgl64.FloatEqual(deltaYaw, 0) && mgl64.FloatEqual(deltaPitch, 0) {
		// The PlayerAuthInput packet is sent every tick, so don't do anything if the position and rotation
		// were unchanged.
		return nil
	}

	if movedTooQuickly(c, deltaPos) {
		// As Java does, a move no player makes in a tick puts the client back
		// where the server has it, rather than taking it anywhere.
		s.conf.Log.Debug("process packet: PlayerAuthInput: moved too quickly", "delta", fmt.Sprint(deltaPos))
		s.ViewEntityTeleport(c, pos)
		return nil
	}

	// Mark client-originated movement so we don't echo it back to the same client.
	s.moving.Store(true)
	c.Move(deltaPos, deltaYaw, deltaPitch)
	s.moving.Store(false)
	return nil
}

// clampToWorld clamps a position a client sends to the bounds of a world,
// as Java clamps it: 30 million blocks out horizontally, 20 million
// vertically.
func clampToWorld(p mgl64.Vec3) mgl64.Vec3 {
	const horizontal, vertical = 3e7, 2e7
	return mgl64.Vec3{
		max(-horizontal, min(p[0], horizontal)),
		max(-vertical, min(p[1], vertical)),
		max(-horizontal, min(p[2], horizontal)),
	}
}

// movedTooQuickly reports a move farther than a player goes in a tick, as
// Java judges it: by more than 10 blocks, or 17 gliding, beyond what the
// velocity of the player covers.
func movedTooQuickly(c Controllable, delta mgl64.Vec3) bool {
	limit := 100.0
	if c.Gliding() {
		limit = 300
	}
	var vel float64
	if v, ok := c.(interface{ Velocity() mgl64.Vec3 }); ok {
		vel = v.Velocity().LenSqr()
	}
	return delta.LenSqr()-vel > limit
}

func teleportAcknowledged(pos, expected mgl64.Vec3) bool {
	// X/Z cross the wire as float32. Compare with that exact representation rather than widening the tolerance.
	expected[0], expected[2] = float64(float32(expected[0])), float64(float32(expected[2]))
	return pos.Sub(expected).Len() <= 0.01
}

// handleActions handles the actions with the world that are present in the PlayerAuthInput packet.
func (h PlayerAuthInputHandler) handleActions(pk *v2193.PlayerAuthInputPacket, s *Session, tx *world.Tx, c Controllable) error {
	if pk.InputData.Has(v2193.InputFlagPerformItemInteraction) {
		data, ok := pk.ItemInteractionData.Value()
		if !ok {
			return fmt.Errorf("item interaction flag set without item interaction data")
		}
		if err := h.handleUseItemData(data, s, c); err != nil {
			return err
		}
	}
	if pk.InputData.Has(v2193.InputFlagPerformBlockActions) {
		actions, ok := pk.BlockActions.Value()
		if !ok {
			return fmt.Errorf("block actions flag set without block actions")
		}
		if err := h.handleBlockActions(actions, s, tx, c); err != nil {
			return err
		}
	}
	h.handleInputFlags(pk.InputData, s, tx, c)

	if pk.InputData.Has(v2193.InputFlagPerformItemStackRequest) {
		request, ok := pk.ItemStackRequest.Value()
		if !ok {
			return fmt.Errorf("item stack request flag set without item stack request")
		}
		s.inTransaction.Store(true)
		defer s.inTransaction.Store(false)

		// As of 1.18 this is now used for sending item stack requests such as when mining a block.
		sh := s.handlers[v2193.IDItemStackRequest].(*ItemStackRequestHandler)
		if err := sh.handleRequest(request, s, tx, c); err != nil {
			// Item stacks being out of sync isn't uncommon, so don't error. Just debug the error and let the
			// revert do its work.
			s.conf.Log.Debug("process packet: PlayerAuthInput: resolve item stack request: " + err.Error())
		}
	}
	return nil
}

// handleInputFlags handles the toggleable input flags set in a PlayerAuthInput packet.
func (h PlayerAuthInputHandler) handleInputFlags(flags v2193.InputFlags, s *Session, tx *world.Tx, c Controllable) {
	dashedByJump := h.tryDash(flags, tx, c)
	jumpedByVehicle := !dashedByJump && h.tryRideJump(flags, tx, c)
	dismountedByJump := !dashedByJump && !jumpedByVehicle && h.tryDismount(flags, tx, c)
	if flags.Has(v2193.InputFlagStartSprinting) {
		c.StartSprinting()
	}
	if flags.Has(v2193.InputFlagStopSprinting) {
		c.StopSprinting()
	}
	if sneaking := flags.Has(v2193.InputFlagSneaking); sneaking != c.Sneaking() {
		if sneaking {
			c.StartSneaking()
		} else {
			c.StopSneaking()
		}
	}
	if flags.Has(v2193.InputFlagStartSwimming) {
		c.StartSwimming()
	}
	if flags.Has(v2193.InputFlagStopSwimming) {
		c.StopSwimming()
	}
	if flags.Has(v2193.InputFlagStartGliding) {
		c.StartGliding()
	}
	if flags.Has(v2193.InputFlagStopGliding) {
		c.StopGliding()
	}
	if flags.Has(v2193.InputFlagStartJumping) && !dismountedByJump && !dashedByJump {
		c.Jump()
	}
	if flags.Has(v2193.InputFlagStartCrawling) {
		c.StartCrawling()
	}
	if flags.Has(v2193.InputFlagStopCrawling) {
		c.StopCrawling()
	}
	if flags.Has(v2193.InputFlagMissedSwing) {
		s.swingingArm.Store(true)
		defer s.swingingArm.Store(false)
		c.PunchAir()
	}
	if flags.Has(v2193.InputFlagStartFlying) {
		if !c.GameMode().AllowsFlying() {
			s.conf.Log.Debug("process packet: PlayerAuthInput: flying flag enabled while unable to fly")
			s.SendAbilities(c)
		} else {
			c.StartFlying()
		}
	}
	if flags.Has(v2193.InputFlagStopFlying) {
		if !c.GameMode().HasCollision() {
			// Modes without collision fly permanently, so correct the client that tried to stop.
			s.SendAbilities(c)
		} else {
			c.StopFlying()
		}
	}
}

func (h PlayerAuthInputHandler) tryRideJump(flags v2193.InputFlags, tx *world.Tx, c Controllable) bool {
	if !flags.Has(v2193.InputFlagStartJumping) {
		return false
	}
	rider, ok := c.(entity.Rider)
	if !ok || rider.Riding() == nil {
		return false
	}
	ridden, ok := rider.Riding().Entity(tx)
	if !ok {
		return false
	}
	jumper, ok := ridden.(interface{ RideJump() bool })
	return ok && jumper.RideJump()
}

func (h PlayerAuthInputHandler) tryDash(flags v2193.InputFlags, tx *world.Tx, c Controllable) bool {
	if !flags.Has(v2193.InputFlagStartJumping) {
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
	ridden, ok := handle.Entity(tx)
	if !ok {
		return false
	}
	dashable, ok := ridden.(interface {
		Dash(*world.Tx, world.Entity) bool
	})
	return ok && dashable.Dash(tx, c)
}

func (h PlayerAuthInputHandler) tryDismount(flags v2193.InputFlags, tx *world.Tx, c Controllable) bool {
	if !flags.Has(v2193.InputFlagStartSneaking) && !flags.Has(v2193.InputFlagStartJumping) {
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
	return dismounted && flags.Has(v2193.InputFlagStartJumping)
}

// handleUseItemData handles the v2193.PlayerInventoryAction found in a PlayerAuthInput packet.
func (h PlayerAuthInputHandler) handleUseItemData(data v2193.PlayerInventoryAction, s *Session, c Controllable) error {
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
	case v2193.UseItemActionBreakBlock:
		c.BreakBlock(pos)
	default:
		return fmt.Errorf("unhandled UseItem ActionType for PlayerAuthInput packet %v", data.ActionType)
	}
	return nil
}

// handleBlockActions handles a slice of v2193.PlayerBlockAction present in a PlayerAuthInput packet.
func (h PlayerAuthInputHandler) handleBlockActions(a []v2193.PlayerBlockAction, s *Session, tx *world.Tx, c Controllable) error {
	for _, action := range a {
		if err := handlePlayerAction(action.Action, action.Face, action.BlockPos, selfEntityRuntimeID, s, c); err != nil {
			return err
		}
	}
	return nil
}
