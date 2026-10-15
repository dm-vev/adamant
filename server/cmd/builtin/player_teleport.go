package builtin

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// registerPlayerTeleportCommands registers Java 26.3 teleport, spawnpoint and ride commands.
func registerPlayerTeleportCommands(srv serverAdapter) {
	cmd.Register(cmd.New("tp", "Teleports entities.", []string{"teleport"}, teleportCommand{srv: srv}, teleportSelfCommand{srv: srv}, teleportPositionCommand{srv: srv}, teleportSelfPositionCommand{srv: srv}, teleportRotationCommand{srv: srv}, teleportFacingPositionCommand{srv: srv}, teleportFacingEntityCommand{srv: srv}))
	cmd.Register(cmd.New("spawnpoint", "Sets a player's respawn point.", nil, spawnpointCommand{srv: srv}))
	cmd.Register(cmd.New("ride", "Makes an entity ride another entity.", nil, rideCommand{srv: srv}, dismountCommand{srv: srv}))
	cmd.Register(cmd.New("spreadplayers", "Spreads players around a center position.", nil, spreadPlayersCommand{srv: srv}, spreadPlayersUnderCommand{srv: srv}))
}

// teleportCommand moves targets to a single destination entity.
type teleportCommand struct {
	srv         serverAdapter
	Targets     []cmd.Target `cmd:"targets"`
	Destination []cmd.Target `cmd:"destination"`
}

// teleportSelfCommand moves the executing entity to a destination entity.
type teleportSelfCommand struct {
	srv         serverAdapter
	Destination []cmd.Target `cmd:"destination"`
}

func (c teleportCommand) Allow(src cmd.Source) bool     { return worldOpCommand{c.srv}.Allow(src) }
func (c teleportSelfCommand) Allow(src cmd.Source) bool { return worldOpCommand{c.srv}.Allow(src) }

func (c teleportCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	teleportToEntity(c.Targets, c.Destination, o)
}

func (c teleportSelfCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	self, ok := commandExecutor(src)
	if !ok {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	teleportToEntity([]cmd.Target{self}, c.Destination, o)
}

// teleportToEntity applies Java's single-destination rule and copies the
// destination rotation, as TeleportCommand.teleportToEntity does.
func teleportToEntity(targets, destination []cmd.Target, o *cmd.Output) {
	if len(destination) != 1 {
		o.Errorf("Only one entity is allowed, but the provided selector allows more than one")
		return
	}
	if len(targets) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	dest := destination[0]
	yaw, pitch := 0.0, 0.0
	if rotated, ok := dest.(interface{ Rotation() cube.Rotation }); ok {
		yaw, pitch = rotated.Rotation().Yaw(), rotated.Rotation().Pitch()
	}
	for _, t := range targets {
		moveTeleportTarget(t, dest.Position(), yaw, pitch)
	}
	o.Printf("Teleported %d entities.", len(targets))
}

// teleportPositionCommand is `tp <targets> <location>`.
type teleportPositionCommand struct {
	srv      serverAdapter
	Targets  []cmd.Target `cmd:"targets"`
	Position mgl64.Vec3   `cmd:"position"`
}

// teleportSelfPositionCommand is `tp <location>` for the executing entity.
type teleportSelfPositionCommand struct {
	srv      serverAdapter
	Position mgl64.Vec3 `cmd:"position"`
}

func (c teleportPositionCommand) Allow(src cmd.Source) bool { return worldOpCommand{c.srv}.Allow(src) }
func (c teleportSelfPositionCommand) Allow(src cmd.Source) bool {
	return worldOpCommand{c.srv}.Allow(src)
}

func (c teleportPositionCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	teleportToPosition(c.Targets, c.Position, o, src, tx)
}

func (c teleportSelfPositionCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	self, ok := commandExecutor(src)
	if !ok {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	teleportToPosition([]cmd.Target{self}, c.Position, o, src, tx)
}

// teleportToPosition keeps each target's rotation, as Java does when no
// rotation argument is given.
func teleportToPosition(targets []cmd.Target, pos mgl64.Vec3, o *cmd.Output, src cmd.Source, tx *world.Tx) {
	if len(targets) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	var destination = tx.World()
	if context, ok := cmd.ExecutionContextOf(src); ok && context.World != nil {
		destination = context.World
	}
	for _, t := range targets {
		if p, ok := t.(*player.Player); ok && p.H().World() != destination {
			p.TeleportToWorld(tx, destination, pos)
			continue
		}
		if p, ok := t.(interface{ Teleport(mgl64.Vec3) }); ok {
			p.Teleport(pos)
		}
	}
	o.Printf("Teleported %d entities.", len(targets))
}

// commandExecutor returns the entity a command runs as, following execute
// contexts back to their executor.
func commandExecutor(src cmd.Source) (cmd.Target, bool) {
	if context, ok := cmd.ExecutionContextOf(src); ok && context.Executor != nil {
		return context.Executor, true
	}
	if e, ok := src.(world.Entity); ok {
		return e, true
	}
	return nil, false
}

type teleportRotationCommand struct {
	srv      serverAdapter
	Targets  []cmd.Target `cmd:"targets"`
	Position mgl64.Vec3   `cmd:"position"`
	Yaw      float64      `cmd:"yaw"`
	Pitch    float64      `cmd:"pitch"`
}

func (c teleportRotationCommand) Allow(src cmd.Source) bool { return worldOpCommand{c.srv}.Allow(src) }
func (c teleportRotationCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if len(c.Targets) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	for _, target := range c.Targets {
		moveTeleportTarget(target, c.Position, c.Yaw, c.Pitch)
	}
	o.Printf("Teleported %d entities.", len(c.Targets))
}

type teleportFacingPositionCommand struct {
	srv      serverAdapter
	Targets  []cmd.Target   `cmd:"targets"`
	Position mgl64.Vec3     `cmd:"position"`
	Facing   cmd.SubCommand `cmd:"facing"`
	LookAt   mgl64.Vec3     `cmd:"location"`
}

func (c teleportFacingPositionCommand) Allow(src cmd.Source) bool {
	return worldOpCommand{c.srv}.Allow(src)
}
func (c teleportFacingPositionCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if len(c.Targets) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	for _, target := range c.Targets {
		yaw, pitch := teleportLookAt(c.Position, c.LookAt)
		moveTeleportTarget(target, c.Position, yaw, pitch)
	}
	o.Printf("Teleported %d entities.", len(c.Targets))
}

type teleportFacingEntityCommand struct {
	srv      serverAdapter
	Targets  []cmd.Target   `cmd:"targets"`
	Position mgl64.Vec3     `cmd:"position"`
	Facing   cmd.SubCommand `cmd:"facing"`
	Entity   cmd.SubCommand `cmd:"entity"`
	LookAt   []cmd.Target   `cmd:"target"`
}

func (c teleportFacingEntityCommand) Allow(src cmd.Source) bool {
	return worldOpCommand{c.srv}.Allow(src)
}
func (c teleportFacingEntityCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if len(c.Targets) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	if len(c.LookAt) != 1 {
		o.Errorf("Only one entity is allowed, but the provided selector allows more than one")
		return
	}
	look := c.LookAt[0].Position()
	if eyed, ok := c.LookAt[0].(interface{ EyeHeight() float64 }); ok {
		look[1] += eyed.EyeHeight()
	}
	for _, target := range c.Targets {
		yaw, pitch := teleportLookAt(c.Position, look)
		moveTeleportTarget(target, c.Position, yaw, pitch)
	}
	o.Printf("Teleported %d entities.", len(c.Targets))
}

// moveTeleportTarget ports Entity.teleportTo: the target turns before it
// teleports, so the teleport a player's client receives carries the new
// rotation as Java's single position packet does.
func moveTeleportTarget(target cmd.Target, position mgl64.Vec3, yaw, pitch float64) {
	if mover, ok := target.(interface {
		Move(mgl64.Vec3, float64, float64)
	}); ok {
		if rotatable, ok := target.(interface{ Rotation() cube.Rotation }); ok {
			rotation := rotatable.Rotation()
			mover.Move(mgl64.Vec3{}, yaw-rotation.Yaw(), pitch-rotation.Pitch())
		}
	}
	if teleporter, ok := target.(interface{ Teleport(mgl64.Vec3) }); ok {
		teleporter.Teleport(position)
	}
}
func teleportLookAt(from, to mgl64.Vec3) (yaw, pitch float64) {
	delta := to.Sub(from)
	yaw = math.Atan2(-delta[0], delta[2]) * 180 / math.Pi
	pitch = -math.Atan2(delta[1], math.Hypot(delta[0], delta[2])) * 180 / math.Pi
	return
}

type spawnpointCommand struct {
	srv      serverAdapter
	Targets  cmd.Optional[[]cmd.Target] `cmd:"targets"`
	Position cmd.Optional[mgl64.Vec3]   `cmd:"position"`
}

func (c spawnpointCommand) Allow(src cmd.Source) bool { return worldOpCommand{c.srv}.Allow(src) }
func (c spawnpointCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	targets, ok := c.Targets.Load()
	if !ok {
		if p, yes := src.(*player.Player); yes {
			targets = []cmd.Target{p}
		}
	}
	pos, hasPos := c.Position.Load()
	for _, target := range targets {
		p, yes := target.(*player.Player)
		if !yes {
			continue
		}
		if !hasPos {
			pos = p.Position()
		}
		p.SetRespawnPosition(cube.PosFromVec3(pos), tx.World().Dimension())
	}
	o.Printf("Set the respawn point for %d players.", len(targets))
}

type rideCommand struct {
	srv     serverAdapter
	Target  []cmd.Target   `cmd:"target"`
	Mount   cmd.SubCommand `cmd:"mount"`
	Vehicle []cmd.Target   `cmd:"vehicle"`
}

func (c rideCommand) Allow(src cmd.Source) bool { return worldOpCommand{c.srv}.Allow(src) }
func (c rideCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	vehicleTarget, ok := singleTarget(c.Vehicle, o)
	if !ok {
		return
	}
	rider, ok := target.(world.Entity)
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, "target")
		return
	}
	vehicle, ok := vehicleTarget.(world.Entity)
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, "vehicle")
		return
	}
	if !entity.MountVehicle(tx, vehicle, rider) {
		o.Errort(cmd.MessageParameterInvalid, "vehicle")
		return
	}
	o.Printf("%s is now riding %s.", entityName(rider), entityName(vehicle))
}

type dismountCommand struct {
	srv      serverAdapter
	Target   []cmd.Target   `cmd:"target"`
	Dismount cmd.SubCommand `cmd:"dismount"`
}

func (c dismountCommand) Allow(src cmd.Source) bool { return worldOpCommand{c.srv}.Allow(src) }
func (c dismountCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	rider, ok := target.(world.Entity)
	if !ok || !entity.DismountRider(rider, tx) {
		o.Errort(cmd.MessageParameterInvalid, "target")
		return
	}
	o.Printf("%s dismounted.", entityName(rider))
}
func entityName(e world.Entity) string {
	if n, ok := e.(interface{ Name() string }); ok {
		return n.Name()
	}
	return "Entity"
}

type spreadPlayersCommand struct {
	srv            serverAdapter
	Center         mgl64.Vec3   `cmd:"center"`
	SpreadDistance float64      `cmd:"spreadDistance"`
	MaxRange       float64      `cmd:"maxRange"`
	RespectTeams   bool         `cmd:"respectTeams"`
	Targets        []cmd.Target `cmd:"targets"`
}
type spreadPlayersUnderCommand struct {
	srv            serverAdapter
	Center         mgl64.Vec3     `cmd:"center"`
	SpreadDistance float64        `cmd:"spreadDistance"`
	MaxRange       float64        `cmd:"maxRange"`
	Under          cmd.SubCommand `cmd:"under"`
	MaxHeight      int            `cmd:"maxHeight"`
	RespectTeams   bool           `cmd:"respectTeams"`
	Targets        []cmd.Target   `cmd:"targets"`
}

func (c spreadPlayersCommand) Allow(src cmd.Source) bool { return worldOpCommand{c.srv}.Allow(src) }
func (c spreadPlayersCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	spreadPlayers(tx, o, c.Center, c.SpreadDistance, c.MaxRange, tx.Range().Max(), c.Targets)
}
func (c spreadPlayersUnderCommand) Allow(src cmd.Source) bool {
	return worldOpCommand{c.srv}.Allow(src)
}
func (c spreadPlayersUnderCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	spreadPlayers(tx, o, c.Center, c.SpreadDistance, c.MaxRange, c.MaxHeight, c.Targets)
}

type spreadPosition struct{ x, z float64 }

// spreadPlayers ports SpreadPlayersCommand: positions start uniformly in the
// square range, are pushed one block away from the average of too-close
// neighbours, and unsafe positions are re-rolled, for at most 10000 rounds.
func spreadPlayers(tx *world.Tx, o *cmd.Output, center mgl64.Vec3, distance, maxRange float64, maxHeight int, targets []cmd.Target) {
	players := playersFromTargets(targets)
	if len(players) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	if distance < 0 || maxRange < distance+1 {
		o.Errorf("Maximum range must be at least spread distance + 1")
		return
	}
	if maxHeight < tx.Range().Min() {
		o.Errorf("maxHeight is below the world's minimum height")
		return
	}
	minX, minZ := center[0]-maxRange, center[2]-maxRange
	maxX, maxZ := center[0]+maxRange, center[2]+maxRange
	randomize := func(p *spreadPosition) {
		p.x = minX + rand.Float64()*(maxX-minX)
		p.z = minZ + rand.Float64()*(maxZ-minZ)
	}
	positions := make([]spreadPosition, len(players))
	for i := range positions {
		randomize(&positions[i])
	}
	iteration, collisions := 0, true
	for ; iteration < 10000 && collisions; iteration++ {
		collisions = false
		for i := range positions {
			p := &positions[i]
			neighbours, avgX, avgZ := 0, 0.0, 0.0
			for j := range positions {
				if i == j {
					continue
				}
				n := positions[j]
				if math.Hypot(p.x-n.x, p.z-n.z) < distance {
					neighbours++
					avgX += n.x - p.x
					avgZ += n.z - p.z
				}
			}
			if neighbours > 0 {
				avgX, avgZ = avgX/float64(neighbours), avgZ/float64(neighbours)
				if length := math.Hypot(avgX, avgZ); length > 0 {
					p.x -= avgX / length
					p.z -= avgZ / length
				} else {
					randomize(p)
				}
				collisions = true
			}
			clamped := spreadPosition{min(max(p.x, minX), maxX), min(max(p.z, minZ), maxZ)}
			if clamped != *p {
				*p = clamped
				collisions = true
			}
		}
		if !collisions {
			for i := range positions {
				if _, safe := spreadSpawnY(tx, positions[i], maxHeight); !safe {
					randomize(&positions[i])
					collisions = true
				}
			}
		}
	}
	if iteration >= 10000 {
		o.Errorf("Could not spread %d players around %g, %g (too many players for space - try using spread of at most %g)", len(players), center[0], center[2], distance)
		return
	}
	for i, p := range players {
		y, _ := spreadSpawnY(tx, positions[i], maxHeight)
		p.Teleport(mgl64.Vec3{math.Floor(positions[i].x) + .5, float64(y), math.Floor(positions[i].z) + .5})
	}
	o.Printf("Spread %d players around %g, %g", len(players), center[0], center[2])
}

// spreadSpawnY ports Position.getSpawnY and isSafe: the first non-air block
// below two air blocks, scanning down from maxHeight, that is neither a
// liquid nor fire.
func spreadSpawnY(tx *world.Tx, p spreadPosition, maxHeight int) (int, bool) {
	x, z := int(math.Floor(p.x)), int(math.Floor(p.z))
	isAir := func(y int) bool {
		name, _ := tx.Block(cube.Pos{x, y, z}).EncodeBlock()
		return name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air"
	}
	y := maxHeight + 1
	airAbove2, airAbove1 := isAir(y), isAir(y-1)
	spawnY := maxHeight + 1
	for y--; y > tx.Range().Min(); {
		y--
		current := isAir(y)
		if !current && airAbove1 && airAbove2 {
			spawnY = y + 1
			break
		}
		airAbove2, airAbove1 = airAbove1, current
	}
	ground := cube.Pos{x, spawnY - 1, z}
	if ground[1] >= maxHeight {
		return spawnY, false
	}
	if _, liquid := tx.Liquid(ground); liquid {
		return spawnY, false
	}
	name, _ := tx.Block(ground).EncodeBlock()
	return spawnY, name != "minecraft:fire" && name != "minecraft:soul_fire"
}
