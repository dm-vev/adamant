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

func (c teleportPositionCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	teleportToPosition(c.Targets, c.Position, o)
}

func (c teleportSelfPositionCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	self, ok := commandExecutor(src)
	if !ok {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	teleportToPosition([]cmd.Target{self}, c.Position, o)
}

// teleportToPosition keeps each target's rotation, as Java does when no
// rotation argument is given.
func teleportToPosition(targets []cmd.Target, pos mgl64.Vec3, o *cmd.Output) {
	if len(targets) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	for _, t := range targets {
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

func moveTeleportTarget(target cmd.Target, position mgl64.Vec3, yaw, pitch float64) {
	if teleporter, ok := target.(interface{ Teleport(mgl64.Vec3) }); ok {
		teleporter.Teleport(position)
	}
	if mover, ok := target.(interface {
		Move(mgl64.Vec3, float64, float64)
	}); ok {
		if rotatable, ok := target.(interface{ Rotation() cube.Rotation }); ok {
			rotation := rotatable.Rotation()
			mover.Move(mgl64.Vec3{}, yaw-rotation.Yaw(), pitch-rotation.Pitch())
		}
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
	mounter, ok := vehicle.(interface {
		Mount(*world.Tx, world.Entity) bool
	})
	if !ok || !mounter.Mount(tx, rider) {
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

func spreadPlayers(tx *world.Tx, o *cmd.Output, center mgl64.Vec3, distance, maxRange float64, maxHeight int, targets []cmd.Target) {
	players := playersFromTargets(targets)
	if len(players) == 0 || distance < 0 || maxRange <= 0 {
		o.Errort(cmd.MessageParameterInvalid, "spreadplayers")
		return
	}
	positions := make([]spreadPosition, len(players))
	for i := range positions {
		positions[i] = spreadPosition{center[0] + (rand.Float64()*2-1)*maxRange, center[2] + (rand.Float64()*2-1)*maxRange}
	}
	for iteration := 0; iteration < 10000; iteration++ {
		moved := false
		for i := range positions {
			dx, dz := 0.0, 0.0
			for j := range positions {
				if i == j {
					continue
				}
				x, z := positions[i].x-positions[j].x, positions[i].z-positions[j].z
				d := math.Hypot(x, z)
				if d < distance {
					if d < 1e-6 {
						x, z = rand.Float64()*2-1, rand.Float64()*2-1
						d = math.Hypot(x, z)
					}
					dx += x / d * (distance - d)
					dz += z / d * (distance - d)
				}
			}
			if dx != 0 || dz != 0 {
				positions[i].x += dx
				positions[i].z += dz
				moved = true
			}
			x, z := positions[i].x-center[0], positions[i].z-center[2]
			d := math.Hypot(x, z)
			if d > maxRange {
				positions[i].x = center[0] + x/d*maxRange
				positions[i].z = center[2] + z/d*maxRange
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	for i, p := range players {
		x, z := int(math.Floor(positions[i].x)), int(math.Floor(positions[i].z))
		y, ok := spreadSafeGround(tx, x, z, maxHeight)
		if !ok {
			o.Errort(cmd.MessageParameterInvalid, "safe position")
			return
		}
		p.Teleport(mgl64.Vec3{positions[i].x, float64(y), positions[i].z})
	}
	o.Printf("Spread %d players.", len(players))
}
func spreadSafeGround(tx *world.Tx, x, z, maxHeight int) (int, bool) {
	r := tx.Range()
	if maxHeight < r.Min() {
		return 0, false
	}
	if maxHeight > r.Max() {
		maxHeight = r.Max()
	}
	for y := maxHeight; y >= r.Min(); y-- {
		b := tx.Block(cube.Pos{x, y, z})
		name, _ := b.EncodeBlock()
		if name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air" {
			continue
		}
		if name == "minecraft:lava" || name == "minecraft:fire" || name == "minecraft:soul_fire" {
			return 0, false
		}
		above, _ := tx.Block(cube.Pos{x, y + 1, z}).EncodeBlock()
		if above == "minecraft:air" || above == "minecraft:cave_air" || above == "minecraft:void_air" {
			return y + 1, true
		}
	}
	return 0, false
}
