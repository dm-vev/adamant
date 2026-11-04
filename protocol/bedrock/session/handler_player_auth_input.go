package session

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
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
	m := clientMove(pk, newPos, s)

	if expected := s.teleportPos.Load(); expected != nil {
		if teleportAcknowledged(newPos, *expected) {
			s.teleportPos.Store(nil)
			m.Acknowledged = true
		} else if !mgl64.FloatEqual(deltaPos.Len(), 0) || !mgl64.FloatEqual(deltaYaw, 0) || !mgl64.FloatEqual(deltaPitch, 0) {
			// The player has moved before it received the teleport packet. The game ignores this movement
			// and waits for the client to sync itself back to the server. Once we get a movement that is
			// close enough to the teleport position, the player may move around again.
			m.AwaitingTeleport = true
		}
	}
	if !m.AwaitingTeleport {
		// The player steers what it rides rather than move itself.
		m.Riding = c.SteerVehicle(float64(pk.MoveVector[0]), float64(pk.MoveVector[1]), m.Jumping)
	}

	// Mark client-originated movement so we don't echo it back to the same client.
	s.moving.Store(true)
	c.ClientMove(m)
	s.moving.Store(false)
	return nil
}

// clientMove returns the client.Move of a PlayerAuthInput packet, at the
// position passed: the feet of the player, clamped to the world.
func clientMove(pk *v2193.PlayerAuthInputPacket, pos mgl64.Vec3, s *Session) client.Move {
	flags := pk.InputData
	m := client.Move{
		Position:       pos,
		Rotation:       cube.Rotation{float64(pk.Yaw), float64(pk.Pitch)},
		HeadYaw:        float64(pk.HeadYaw),
		Tick:           pk.Tick,
		Arrived:        s.handlingArrived,
		Input:          mgl64.Vec2{float64(pk.MoveVector[0]), float64(pk.MoveVector[1])},
		ClientVelocity: vec32To64(pk.Delta),
		Sprinting:      flags.Has(v2193.InputFlagSprinting),
		Sneaking:       flags.Has(v2193.InputFlagSneaking),
		Jumping:        flags.Has(v2193.InputFlagJumping) || flags.Has(v2193.InputFlagJumpDown),
		Swimming:       flags.Has(v2193.InputFlagStartSwimming),
		Crawling:       flags.Has(v2193.InputFlagStartCrawling),
		Gliding:        flags.Has(v2193.InputFlagStartGliding),
		SpinAttack:     flags.Has(v2193.InputFlagStartSpinAttack),
		ClaimsGround:   flags.Has(v2193.InputFlagVerticalCollision),
		Loading:        s.changingDimension.Load(),
	}
	if pk.InteractYaw != 0 || pk.InteractPitch != 0 {
		m.Aim = cube.Rotation{float64(pk.InteractYaw), float64(pk.InteractPitch)}.Vec3()
	}
	return m
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
	jumped := flags.Has(v2193.InputFlagStartJumping)
	dashedByJump := jumped && c.DashVehicle()
	jumpedByVehicle := !dashedByJump && jumped && c.RideJump()
	dismountedByJump := !dashedByJump && !jumpedByVehicle && h.tryDismount(flags, c)
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
	// The game refuses a flight the game mode does not allow, and a stop in a mode that flies for good.
	if flags.Has(v2193.InputFlagStartFlying) {
		c.StartFlying()
	}
	if flags.Has(v2193.InputFlagStopFlying) {
		c.StopFlying()
	}
}

// tryDismount dismounts the player from what it rides when its client
// sneaks or jumps, and reports whether it dismounted by jumping.
func (h PlayerAuthInputHandler) tryDismount(flags v2193.InputFlags, c Controllable) bool {
	if !flags.Has(v2193.InputFlagStartSneaking) && !flags.Has(v2193.InputFlagStartJumping) {
		return false
	}
	return c.DismountVehicle() && flags.Has(v2193.InputFlagStartJumping)
}

// handleUseItemData handles the v2193.PlayerInventoryAction found in a PlayerAuthInput packet.
func (h PlayerAuthInputHandler) handleUseItemData(data v2193.PlayerInventoryAction, s *Session, c Controllable) error {
	s.swingingArm.Store(true)
	defer s.swingingArm.Store(false)

	held, _ := c.HeldItems()
	if !held.Equal(stackToItem(s.tables(), data.HeldItem.Stack)) {
		s.conf.Log.Debug("process packet: PlayerAuthInput: UseItemTransaction: mismatch between actual held item and client held item")
		return nil
	}
	pos := cube.Pos{int(data.BlockPosition[0]), int(data.BlockPosition[1]), int(data.BlockPosition[2])}

	// Seems like this is only used for breaking blocks at the moment.
	switch data.ActionType {
	case v2193.UseItemActionBreakBlock:
		c.DestroyBlock(pos)
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
