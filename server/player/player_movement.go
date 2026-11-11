package player

import (
	"errors"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// movementState is how the player moves: its flags, the movement computer, the
// portal travel, the collisions and the move events.
type movementState struct {
	// frostWalkPos is the block Frost Walker last ran in.
	frostWalkPos                                               cube.Pos
	frostWalkPosSet                                            bool
	scale                                                      float64
	sneaking, sprinting, swimming, gliding, crawling, onGround bool
	// moved marks a player whose viewers are yet to be shown where it moved,
	// which they are with the next tick, and movedByServer one that the
	// server moved, whose own client is shown the move too.
	moved, movedByServer                     bool
	glideTicks                               int64
	moveEventDistance, nextMoveEventStep     float32
	movementSupport                          cube.Pos
	hasMovementSupport, onGroundNoBlocks     bool
	mc                                       *entity.MovementComputer
	portalTravel                             *entity.PortalTravelComputer
	collidedVertically, collidedHorizontally bool
	prevWorld                                *world.World
}

// StartSprinting makes a player start sprinting, increasing the speed of the player by 30% and making
// particles show up under the feet. The player will only start sprinting if its food level is high enough.
// If the player is sneaking when calling StartSprinting, it is stopped from sneaking.
func (p *Player) StartSprinting() {
	if !p.hunger.canSprint() && p.GameMode().AllowsTakingDamage() || p.crawling || p.sprinting {
		return
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleToggleSprint(ctx, true); ctx.Cancelled() {
		return
	}
	p.StopSneaking()
	p.sprinting = true
	p.SetSpeed(p.speed * 1.3)
	p.updateState()
}

// Sprinting checks if the player is currently sprinting.
func (p *Player) Sprinting() bool {
	return p.sprinting
}

// StopSprinting makes a player stop sprinting, setting back the speed of the player to its original value.
func (p *Player) StopSprinting() {
	if !p.sprinting {
		return
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleToggleSprint(ctx, false); ctx.Cancelled() {
		return
	}
	p.sprinting = false
	p.SetSpeed(p.speed / 1.3)
	p.updateState()
}

// StartSneaking makes a player start sneaking. If the player is already sneaking, StartSneaking will not do
// anything.
// If the player is sprinting while StartSneaking is called, the sprinting is stopped.
func (p *Player) StartSneaking() {
	if p.sneaking {
		return
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleToggleSneak(ctx, true); ctx.Cancelled() {
		return
	}
	if p.sneaking {
		return
	}
	if !p.Flying() {
		p.StopSprinting()
	}
	p.sneaking = true
	p.updateState()
}

// Sneaking checks if the player is currently sneaking.
func (p *Player) Sneaking() bool {
	return p.sneaking
}

// StopSneaking makes a player stop sneaking if it currently is. If the player is not sneaking, StopSneaking
// will not do anything.
func (p *Player) StopSneaking() {
	if !p.sneaking {
		return
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleToggleSneak(ctx, false); ctx.Cancelled() {
		return
	}
	p.sneaking = false
	p.updateState()
}

// StartSwimming makes the player start swimming if it is not currently doing so. If the player is sneaking
// while StartSwimming is called, the sneaking is stopped.
func (p *Player) StartSwimming() {
	if p.swimming {
		return
	}
	p.StopSneaking()
	p.swimming = true
	p.updateState()
}

// Swimming checks if the player is currently swimming.
func (p *Player) Swimming() bool {
	return p.swimming
}

// StopSwimming makes the player stop swimming if it is currently doing so.
func (p *Player) StopSwimming() {
	if !p.swimming {
		return
	}
	p.swimming = false
	p.updateState()
}

// StartCrawling makes the player start crawling if it is not currently doing so. If the player is sneaking
// while StartCrawling is called, the sneaking is stopped.
func (p *Player) StartCrawling() {
	if p.crawling {
		return
	}
	for _, corner := range p.H().Type().BBox(p).Translate(p.Position()).Corners() {
		if _, isAir := p.tx.Block(cube.PosFromVec3(corner).Add(cube.Pos{0, 1, 0})).(block.Air); !isAir {
			p.crawling = true
			break
		}
	}
	p.StopSneaking()
	p.updateState()
}

// StopCrawling makes the player stop crawling if it is currently doing so.
func (p *Player) StopCrawling() {
	if !p.crawling {
		return
	}
	p.crawling = false
	p.updateState()
}

// Crawling checks if the player is currently crawling.
func (p *Player) Crawling() bool {
	return p.crawling
}

// StartGliding makes the player start gliding if it is not currently doing so.
func (p *Player) StartGliding() {
	if p.gliding {
		return
	}
	chest := p.Armour().Chestplate()
	if _, ok := chest.Item().(item.Elytra); !ok || chest.Durability() < 2 {
		return
	}
	p.gliding = true
	p.updateState()
}

// Gliding checks if the player is currently gliding.
func (p *Player) Gliding() bool {
	return p.gliding
}

// StopGliding makes the player stop gliding if it is currently doing so.
func (p *Player) StopGliding() {
	if !p.gliding {
		return
	}
	p.gliding = false
	p.glideTicks = 0
	p.updateState()
}

// Jump makes the player jump if they are on ground. It exhausts the player by 0.05 food points, an additional 0.15
// is exhausted if the player is sprint jumping.
func (p *Player) Jump() {
	if p.Dead() {
		return
	}

	p.Handler().HandleJump(p)
	if p.OnGround() {
		jumpVel := 0.42
		if e, ok := p.Effect(effect.JumpBoost); ok {
			jumpVel = float64(e.Level()) / 10
		}
		p.data.Vel = mgl64.Vec3{0, jumpVel}
	}
	if p.Sprinting() {
		p.Exhaust(0.2)
	} else {
		p.Exhaust(0.05)
	}
}

// Teleport teleports the player to a target position in the world. Unlike Move, it immediately changes the
// position of the player, rather than showing an animation.
func (p *Player) Teleport(pos mgl64.Vec3) {
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleTeleport(ctx, pos); ctx.Cancelled() {
		return
	}
	p.StopFishing(p.tx, false)
	p.forceTeleport(pos)
}

// TeleportToWorld transfers the player to destination and places them at pos,
// as a teleport command run in another dimension does. The caller must own
// the player's current world transaction. If the destination is closed, the
// player stays where they were. While a checkpoint holds transfers out of
// the player's world, the player is moved a tick later.
func (p *Player) TeleportToWorld(tx *world.Tx, destination *world.World, pos mgl64.Vec3) {
	if destination == nil || destination == tx.World() {
		p.Teleport(pos)
		return
	}
	transfer, ok := world.BeginTransfer(tx)
	if !ok {
		p.H().DoAfter(transferRetryDelay, func(tx *world.Tx, e world.Entity) {
			if p, ok := e.(*Player); ok {
				p.TeleportToWorld(tx, destination, pos)
			}
		})
		return
	}
	// A menu stays in the world it was opened in.
	p.closeMenu(tx, false)
	handle := transfer.Remove(tx, p)
	task := destination.Do(func(destinationTx *world.Tx) {
		np := destinationTx.AddEntity(handle).(*Player)
		np.Teleport(pos)
		np.client().SendRespawn(pos, p)
		np.SetVisible()
	})
	if errors.Is(task.Err(), world.ErrWorldClosed) {
		tx.AddEntity(handle)
	}
}

// forceTeleport teleports the player without calling the Handler.
func (p *Player) forceTeleport(pos mgl64.Vec3) {
	p.Wake()
	p.teleport(pos)
}

// teleport teleports the player to a target position in the world. It does not call the Handler of the
// player.
func (p *Player) teleport(pos mgl64.Vec3) {
	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityTeleport(p, pos)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
	p.setPosition(pos)
	p.data.Vel = mgl64.Vec3{}
	p.ResetFallDistance()
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.Teleport }))
}

// setPosition sets the position of the player and tells its world, so that
// mobs finding the players near them find it there.
func (p *Player) setPosition(pos mgl64.Vec3) {
	p.data.Pos = pos
	p.tx.PlayerMoved()
}

// showMove shows the viewers of the player where it moved since the last tick, if that changed what they see:
// one absolute move, encoded once for all of them. Its own session is shown it only if the server moved it.
func (p *Player) showMove() {
	if !p.moved {
		return
	}
	byServer := p.movedByServer
	p.moved, p.movedByServer = false, false
	pos, rot := p.Position(), p.Rotation()
	move, teleport := p.data.Moves.Move(pos, rot, rot.Yaw(), mgl64.Vec3{}, p.OnGround(), true, 0)
	visible := move != nil || teleport
	if !visible && !byServer {
		return
	}
	shown := &world.EntityMove{Pos: pos, Rot: rot, OnGround: p.OnGround(), Absolute: true}
	viewers, release := p.viewers()
	for _, v := range viewers {
		// Its own client is shown only where the server put it, the other
		// viewers only a move they can see.
		if self := p.c != nil && v == world.Viewer(p.c); self && !byServer || !self && !visible {
			continue
		}
		v.ViewEntityMove(p, shown)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// Move moves the player from one position to another in the world, by adding the delta passed to the current
// position of the player.
// Move also rotates the player, adding deltaYaw and deltaPitch to the respective values.
func (p *Player) Move(deltaPos mgl64.Vec3, deltaYaw, deltaPitch float64) {
	if p.Dead() || (deltaPos.ApproxEqual(mgl64.Vec3{}) && mgl64.FloatEqual(deltaYaw, 0) && mgl64.FloatEqual(deltaPitch, 0)) {
		p.onGround = true
		p.updateFallState(deltaPos.Y())
		return
	}
	if p.immobile {
		if mgl64.FloatEqual(deltaYaw, 0) && mgl64.FloatEqual(deltaPitch, 0) {
			// If only the position was changed, don't continue with the movement when immobile.
			return
		}
		// Still update rotation if it was changed.
		deltaPos = mgl64.Vec3{}
	}
	var (
		pos         = p.Position()
		res, resRot = pos.Add(deltaPos), p.Rotation().Add(cube.Rotation{deltaYaw, deltaPitch})
	)
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleMove(ctx, res, resRot); ctx.Cancelled() {
		if p.hasClient() && pos.ApproxEqual(p.Position()) {
			// The position of the player was changed and the event cancelled. This means we still need to notify the
			// player of this movement change.
			p.teleport(pos)
		}
		return
	}
	// The viewers are shown the move with the next tick, as Java's
	// ServerEntity shows that of a player, so that the moves of a crowd go
	// out in one batch a tick rather than in one for every move.
	p.moved = true
	if !p.client().ClientMoving() {
		p.movedByServer = true
	}
	if ac := p.anticheat(); ac != nil && !ac.clientMove {
		// The server moved a player with a client: its client moves on from
		// where it had it until it sees the move.
		p.startGrace(ac.conf.Grace.Teleport)
	}

	p.setPosition(res)
	p.data.Rot = resRot
	if deltaPos.Len() <= 3 {
		// Only update velocity if the player is not moving too fast to prevent potential OOMs.
		p.data.Vel = deltaPos
		p.checkBlockCollisions(deltaPos)
	}

	horizontalVel := deltaPos
	horizontalVel[1] = 0
	if p.Gliding() {
		if deltaPos.Y() >= -0.5 {
			p.fallDistance = 1.0
		}
		if p.collidedHorizontally {
			if force := horizontalVel.Len()*10.0 - 3.0; force > 0.0 {
				p.tx.PlaySound(p.Position(), sound.Fall{Distance: force})
				p.Hurt(force, entity.GlideDamageSource{})
			}
		}
	}

	beforePos := cube.PosFromVec3(pos.Add(mgl64.Vec3{0, p.EyeHeight()}))
	afterPos := cube.PosFromVec3(res.Add(mgl64.Vec3{0, p.EyeHeight()}))
	if p.movementChunkReady(beforePos) && p.movementChunkReady(afterPos) {
		_, submergedBefore := p.tx.Liquid(beforePos)
		_, submergedAfter := p.tx.Liquid(afterPos)
		if submergedBefore != submergedAfter {
			// Player wasn't either breathing before and no longer isn't, or wasn't breathing before and now is,
			// so send the updated metadata.
			p.client().ViewEntityState(p)
		}
	}

	p.onGround = p.checkOnGround(deltaPos)
	p.emitMovementGameEvents(deltaPos)
	p.updateFallState(deltaPos.Y())

	p.checkMovementStatistics(deltaPos)
}

// checkMovementStatistics ports the exhaustion of
// ServerPlayer.checkMovementStatistics: 0.01 per metre swimming, under water
// or wading, 0.1 per metre sprinting on the ground and none otherwise.
func (p *Player) checkMovementStatistics(delta mgl64.Vec3) {
	if p.Riding() != nil || delta == (mgl64.Vec3{}) {
		return
	}
	full := float32(math.Round(float64(float32(delta.Len()) * 100)))
	horizontal := float32(math.Round(float64(float32(math.Hypot(delta[0], delta[2])) * 100)))
	switch {
	case p.Swimming() || p.insideOfWater():
		p.Exhaust(float64(0.01 * full * 0.01))
	case p.touchingWater():
		p.Exhaust(float64(0.01 * horizontal * 0.01))
	case p.onClimbable():
	case p.onGround && p.Sprinting():
		p.Exhaust(float64(0.1 * horizontal * 0.01))
	}
}

// onClimbable ports LivingEntity.onClimbable: the player is in a climbable
// block, or an open trapdoor above a ladder facing the same way.
func (p *Player) onClimbable() bool {
	if p.GameMode() == world.GameModeSpectator {
		return false
	}
	pos := cube.PosFromVec3(p.Position())
	b, loaded := p.tx.BlockLoaded(pos)
	if !loaded {
		return false
	}
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return false
	}
	name := strings.TrimPrefix(state.Name, "minecraft:")
	if p.Gliding() && bedStateTag(name, "minecraft:can_glide_through") {
		return false
	}
	if bedStateTag(name, "minecraft:climbable") {
		return true
	}
	if !strings.HasSuffix(name, "_trapdoor") || !javaStateFlag(state.Properties["open"]) {
		return false
	}
	below, loaded := p.tx.BlockLoaded(pos.Side(cube.FaceDown))
	if !loaded {
		return false
	}
	ladder, err := world.CanonicalStateOf(below)
	return err == nil && strings.TrimPrefix(ladder.Name, "minecraft:") == "ladder" && ladder.Properties["facing"] == state.Properties["facing"]
}

// javaStateFlag reports if a boolean property of a canonical Java block state
// is set. The codec gives booleans as bool, but a state read from NBT may
// still carry them as the strings Java writes.
func javaStateFlag(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

// Displace moves the player by a server-authoritative relative delta, clipped against block collision boxes.
func (p *Player) Displace(deltaPos mgl64.Vec3) {
	if p.Dead() || deltaPos.ApproxEqual(mgl64.Vec3{}) {
		return
	}
	pos := p.Position()
	deltaPos, velocity := p.mc.CheckCollision(p.tx, p, pos, deltaPos)
	if deltaPos.ApproxEqual(mgl64.Vec3{}) {
		return
	}
	res := pos.Add(deltaPos)
	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityTeleport(p, res)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
	p.data.Pos, p.data.Vel = res, velocity
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.Teleport }))
	p.checkBlockCollisions(deltaPos)
	p.onGround = p.checkOnGround(deltaPos)
	p.emitMovementGameEvents(deltaPos)
	p.updateFallState(deltaPos[1])
}

// Position returns the current position of the player. It may be changed as the player moves or is moved
// around the world.
func (p *Player) Position() mgl64.Vec3 {
	return p.data.Pos
}

// Velocity returns the players current velocity. If there is an attached session, this will be empty.
func (p *Player) Velocity() mgl64.Vec3 {
	return p.data.Vel
}

// SetVelocity updates the player's velocity. If there is an attached session, this will just send
// the velocity to the player session for the player to update.
func (p *Player) SetVelocity(velocity mgl64.Vec3) {
	if p.simulated() {
		p.data.Vel = velocity
		return
	}
	// The client applies the velocity within a round trip: the anti-cheat
	// lets its moves carry it until then.
	p.recordImpulse(velocity)
	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityVelocity(p, velocity)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// Rotation returns the yaw and pitch of the player in degrees. Yaw is horizontal rotation (rotation around the
// vertical axis, 0 when facing forward), pitch is vertical rotation (rotation around the horizontal axis, also 0
// when facing forward).
func (p *Player) Rotation() cube.Rotation {
	return p.data.Rot
}

// Riding returns the entity handle this player is riding, if any.
func (p *Player) Riding() *world.EntityHandle {
	return p.data.Riding
}

// SetRiding updates the entity handle this player is riding.
func (p *Player) SetRiding(r *world.EntityHandle) {
	if r == nil && p.data.Riding != nil {
		p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.Dismount }))
	}
	p.data.Riding = r
}

// RiderSeat returns the seat of the vehicle the player rides, or last rode.
func (p *Player) RiderSeat() world.RiderSeat {
	return p.data.Seat
}

// SetRiderSeat changes the seat the player rides in and shows it to viewers.
func (p *Player) SetRiderSeat(s world.RiderSeat) {
	if p.data.Seat == s {
		return
	}
	p.data.Seat = s
	p.updateState()
}

// TravelThroughPortal handles the player touching a portal block.
func (p *Player) TravelThroughPortal(tx *world.Tx, target world.Dimension) {
	if !p.GameMode().HasCollision() {
		return
	}
	p.portalTravel.EnterPortal(p, tx, target)
}

// checkCollisions checks the player's block collisions.
func (p *Player) checkBlockCollisions(vel mgl64.Vec3) {
	if p.GameMode() == world.GameModeSpectator {
		return
	}
	entityBBox := Type.BBox(p).Translate(p.Position())
	deltaX, deltaY, deltaZ := vel[0], vel[1], vel[2]

	entity.CheckEntityInsiders(p.tx, entityBBox.Grow(-0.0001), p)

	grown := entityBBox.Extend(vel).Grow(0.25)
	low, high := grown.Min(), grown.Max()
	minX, minY, minZ := int(math.Floor(low[0])), int(math.Floor(low[1])), int(math.Floor(low[2]))
	maxX, maxY, maxZ := int(math.Ceil(high[0])), int(math.Ceil(high[1])), int(math.Ceil(high[2]))

	// A prediction of one BBox per block, plus an additional 2, in case
	blocks := make([]cube.BBox, 0, (maxX-minX)*(maxY-minY)*(maxZ-minZ)+2)
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				pos := cube.Pos{x, y, z}
				b, loaded := p.tx.BlockLoaded(pos)
				// BlockLoaded never waits; unready positions have no collision shape to apply yet.
				if !loaded {
					continue
				}
				for _, box := range b.Model().BBox(pos, p.tx) {
					blocks = append(blocks, box.Translate(pos.Vec3()))
				}
			}
		}
	}

	// epsilon is the epsilon used for thresholds for change used for change in position and velocity.
	const epsilon = 0.001

	if !mgl64.FloatEqualThreshold(deltaY, 0, epsilon) {
		// First we move the entity BBox on the Y axis.
		for _, blockBBox := range blocks {
			deltaY = entityBBox.YOffset(blockBBox, deltaY)
		}
		entityBBox = entityBBox.Translate(mgl64.Vec3{0, deltaY})
	}
	if !mgl64.FloatEqualThreshold(deltaX, 0, epsilon) {
		// Then on the X axis.
		for _, blockBBox := range blocks {
			deltaX = entityBBox.XOffset(blockBBox, deltaX)
		}
		entityBBox = entityBBox.Translate(mgl64.Vec3{deltaX})
	}
	if !mgl64.FloatEqualThreshold(deltaZ, 0, epsilon) {
		// And finally on the Z axis.
		for _, blockBBox := range blocks {
			deltaZ = entityBBox.ZOffset(blockBBox, deltaZ)
		}
	}

	p.collidedHorizontally = !mgl64.FloatEqual(deltaX, vel[0]) || !mgl64.FloatEqual(deltaZ, vel[2])
	p.collidedVertically = !mgl64.FloatEqual(deltaY, vel[1])
}

// movementChunkReady reports whether a block-position read is safe without loading or waiting.
func (p *Player) movementChunkReady(pos cube.Pos) bool {
	_, ready := p.tx.ChunkState(world.ChunkPosition(pos))
	return ready
}

// checkEntitySteppers checks if the player is standing on any EntityStepper blocks.
func (p *Player) checkEntitySteppers() {
	if !p.OnGround() {
		return
	}
	low, high := p.blocksUnder()
	for x := low[0]; x <= high[0]; x++ {
		for z := low[2]; z <= high[2]; z++ {
			pos := cube.Pos{x, low[1], z}
			b, loaded := p.tx.BlockLoaded(pos)
			if !loaded {
				continue
			}
			stepper, ok := b.(block.EntityStepper)
			if !ok {
				continue
			}
			stepper.EntityStepOn(pos, p.tx, p)
			return
		}
	}
}

// checkOnGround checks if the player is currently considered to be on the ground.
// maxGroundProbe bounds the descent checkOnGround probes below a player:
// more than the 3.92 blocks a player falls at most in a tick.
const maxGroundProbe = 8.0

func (p *Player) checkOnGround(deltaPos mgl64.Vec3) bool {
	if p.GameMode() == world.GameModeSpectator {
		return false
	}
	// Probe straight down (0.05 plus this tick's descent) instead of along the
	// whole movement, so a block the player falls past at its side is not ground.
	// The descent is bound: a move of any length must not make the probe, a
	// loop over blocks, run long.
	box := Type.BBox(p).Translate(p.Position()).Extend(mgl64.Vec3{0, -0.05}).Extend(mgl64.Vec3{0, max(math.Min(deltaPos[1], 0), -maxGroundProbe)})
	b := box.Grow(1)
	epsilon := mgl64.Vec3{mgl64.Epsilon, mgl64.Epsilon, mgl64.Epsilon}
	low, high := cube.PosFromVec3(b.Min().Add(epsilon)), cube.PosFromVec3(b.Max().Sub(epsilon))

	unavailable := false
	for x := low[0]; x <= high[0]; x++ {
		for z := low[2]; z <= high[2]; z++ {
			for y := low[1]; y < high[1]; y++ {
				pos := cube.Pos{x, y, z}
				b, loaded := p.tx.BlockLoaded(pos)
				if !loaded {
					unavailable = true
					continue
				}
				for _, bb := range b.Model().BBox(pos, p.tx) {
					if bb.Translate(pos.Vec3()).IntersectsWith(box) {
						return true
					}
				}
			}
		}
	}
	if unavailable {
		return p.onGround
	}
	return false
}

// Scale returns the scale modifier of the Player. The default value for a normal scale is 1. A scale of 0
// will make the Player completely invisible.
func (p *Player) Scale() float64 {
	return p.scale
}

// SetScale changes the scale modifier of the Player. The default value for a normal scale is 1. A scale of 0
// will make the Player completely invisible.
func (p *Player) SetScale(s float64) {
	p.scale = s
	p.updateState()
	// The box of the player changed, which its client applies a round trip
	// later.
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.Teleport }))
}

// OnGround checks if the player is considered to be on the ground.
func (p *Player) OnGround() bool {
	if p.GameMode() == world.GameModeSpectator {
		return false
	}
	if p.simulated() {
		return p.mc.OnGround()
	}
	return p.onGround
}

// EyeHeight returns the eye height of the player: 1.62, 1.26 if player is sneaking or 0.52 if the player is
// swimming, gliding or crawling.
func (p *Player) EyeHeight() float64 {
	switch {
	case p.swimming || p.crawling || p.gliding:
		return 0.4
	case p.sneaking:
		return 1.27
	default:
		return 1.62
	}
}

// TorsoHeight returns the player's torso offset above its feet. This is 0.1 blocks below the eye height.
func (p *Player) TorsoHeight() float64 {
	return p.EyeHeight() - 0.1
}

// Emote makes the player perform the emote with the UUID passed. The viewers
// of the position of the player are shown the emote, the client of the
// player too if it views that position: the player itself is not left out.
func (p *Player) Emote(emote uuid.UUID) {
	viewers := p.tx.Viewers(p.Position())
	// Emotes are cosmetic but frequent; reusing the viewer slice prevents busy
	// lobbies from generating a stream of temporary allocations as players
	// spam emotes.
	for _, viewer := range viewers {
		viewer.ViewEmote(p, emote)
	}
	p.tx.ReleaseViewers(viewers)
}
