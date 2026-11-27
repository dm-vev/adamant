package builtin

import (
	"errors"
	"math"
	"math/rand/v2"
	"time"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// registerPlayerTeleportCommands registers Java 26.3 teleport, spawnpoint and ride commands.
func registerPlayerTeleportCommands(srv serverAdapter) {
	cmd.Register(newCommand("tp", []string{"teleport"}, teleportCommand{srv: srv}, teleportSelfCommand{srv: srv}, teleportPositionCommand{srv: srv}, teleportSelfPositionCommand{srv: srv}, teleportRotationCommand{srv: srv}, teleportFacingPositionCommand{srv: srv}, teleportFacingEntityCommand{srv: srv}))
	cmd.Register(newCommand("spawnpoint", nil, spawnpointCommand{srv: srv}))
	cmd.Register(newCommand("ride", nil, rideCommand{srv: srv}, dismountCommand{srv: srv}))
	cmd.Register(newCommand("spreadplayers", nil, spreadPlayersCommand{srv: srv}, spreadPlayersUnderCommand{srv: srv}))
}

// teleportCommand moves targets to a single destination entity.
type teleportCommand struct {
	srv         serverAdapter
	Targets     cmd.Entities  `cmd:"targets"`
	Destination cmd.OneEntity `cmd:"destination"`
}

// teleportSelfCommand moves the executing entity to a destination entity.
type teleportSelfCommand struct {
	srv         serverAdapter
	Destination cmd.OneEntity `cmd:"destination"`
}

func (teleportCommand) RequiredLevel() cmd.Level     { return cmd.LevelGameMaster }
func (teleportSelfCommand) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }

func (c teleportCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	teleportToEntity(src, o, tx, c.Targets, c.Destination)
}

func (c teleportSelfCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	teleportToEntity(src, o, tx, optionalEntities(cmd.Optional[cmd.Entities]{}, src), c.Destination)
}

type teleportDest struct {
	world *world.World
	pos   mgl64.Vec3
	yaw   float64
	pitch float64
}

// teleportToEntity applies Java's single-destination rule and copies the
// destination rotation, as TeleportCommand.teleportToEntity does.
func teleportToEntity(src cmd.Source, o *cmd.Output, tx *world.Tx, targets cmd.Selector, destination cmd.OneEntity) {
	cmd.ForEach(src, o, tx, destination,
		func(rtx *world.Tx, t cmd.Target) teleportDest {
			d := teleportDest{pos: t.Position()}
			if rtx != nil {
				d.world = rtx.World()
			}
			if rotated, ok := t.(interface{ Rotation() cube.Rotation }); ok {
				d.yaw, d.pitch = rotated.Rotation().Yaw(), rotated.Rotation().Pitch()
			}
			return d
		},
		func(src cmd.Source, o *cmd.Output, tx *world.Tx, rs []cmd.Result[teleportDest]) {
			if len(rs) != 1 {
				o.Errort(cmd.MessageNoTargets)
				return
			}
			dest := rs[0].Value
			cmd.ForEach(src, o, tx, targets,
				func(rtx *world.Tx, t cmd.Target) int {
					if _, ok := t.(world.Entity); !ok {
						return 0
					}
					transferTargetToWorld(rtx, t, dest.world, dest.pos, true, dest.yaw, dest.pitch)
					return 1
				},
				func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
					tally := cmd.TallyOf(rs)
					if tally.NonZero == 0 {
						o.Errort(cmd.MessageNoTargets)
						return
					}
					o.SetResult(tally.NonZero)
					o.Printf("Teleported %d entities.", tally.NonZero)
				},
			)
		},
	)
}

// teleportPositionCommand is `tp <targets> <location>`.
type teleportPositionCommand struct {
	srv      serverAdapter
	Targets  cmd.Entities `cmd:"targets"`
	Position mgl64.Vec3   `cmd:"position"`
}

// teleportSelfPositionCommand is `tp <location>` for the executing entity.
type teleportSelfPositionCommand struct {
	srv      serverAdapter
	Position mgl64.Vec3 `cmd:"position"`
}

func (teleportPositionCommand) RequiredLevel() cmd.Level     { return cmd.LevelGameMaster }
func (teleportSelfPositionCommand) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }

func (c teleportPositionCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	teleportToPosition(src, o, tx, c.Targets, c.Position, false, 0, 0)
}

func (c teleportSelfPositionCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	teleportToPosition(src, o, tx, optionalEntities(cmd.Optional[cmd.Entities]{}, src), c.Position, false, 0, 0)
}

func commandContextWorld(src cmd.Source, tx *world.Tx) *world.World {
	if context, ok := cmd.ExecutionContextOf(src); ok && context.World != nil {
		return context.World
	}
	if tx != nil {
		return tx.World()
	}
	return nil
}

// teleportToPosition moves each target to destination world and pos, optionally
// updating rotation.
func teleportToPosition(src cmd.Source, o *cmd.Output, tx *world.Tx, targets cmd.Selector, pos mgl64.Vec3, setRot bool, yaw, pitch float64) {
	destination := commandContextWorld(src, tx)
	cmd.ForEach(src, o, tx, targets,
		func(rtx *world.Tx, t cmd.Target) int {
			if _, ok := t.(world.Entity); !ok {
				return 0
			}
			transferTargetToWorld(rtx, t, destination, pos, setRot, yaw, pitch)
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Errort(cmd.MessageNoTargets)
				return
			}
			o.SetResult(tally.NonZero)
			o.Printf("Teleported %d entities.", tally.NonZero)
		},
	)
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
	Targets  cmd.Entities `cmd:"targets"`
	Position mgl64.Vec3   `cmd:"position"`
	Yaw      float64      `cmd:"yaw"`
	Pitch    float64      `cmd:"pitch"`
}

func (teleportRotationCommand) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }
func (c teleportRotationCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	teleportToPosition(src, o, tx, c.Targets, c.Position, true, c.Yaw, c.Pitch)
}

type teleportFacingPositionCommand struct {
	srv      serverAdapter
	Targets  cmd.Entities   `cmd:"targets"`
	Position mgl64.Vec3     `cmd:"position"`
	Facing   cmd.SubCommand `cmd:"facing"`
	LookAt   mgl64.Vec3     `cmd:"location"`
}

func (teleportFacingPositionCommand) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }
func (c teleportFacingPositionCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	yaw, pitch := teleportLookAt(c.Position, c.LookAt)
	teleportToPosition(src, o, tx, c.Targets, c.Position, true, yaw, pitch)
}

type teleportFacingEntityCommand struct {
	srv      serverAdapter
	Targets  cmd.Entities   `cmd:"targets"`
	Position mgl64.Vec3     `cmd:"position"`
	Facing   cmd.SubCommand `cmd:"facing"`
	Entity   cmd.SubCommand `cmd:"entity"`
	LookAt   cmd.OneEntity  `cmd:"target"`
}

func (teleportFacingEntityCommand) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }
func (c teleportFacingEntityCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	pos := c.Position
	targets := c.Targets
	cmd.ForEach(src, o, tx, c.LookAt,
		func(_ *world.Tx, t cmd.Target) mgl64.Vec3 {
			look := t.Position()
			if eyed, ok := t.(interface{ EyeHeight() float64 }); ok {
				look[1] += eyed.EyeHeight()
			}
			return look
		},
		func(src cmd.Source, o *cmd.Output, tx *world.Tx, rs []cmd.Result[mgl64.Vec3]) {
			if len(rs) != 1 {
				o.Errort(cmd.MessageNoTargets)
				return
			}
			yaw, pitch := teleportLookAt(pos, rs[0].Value)
			teleportToPosition(src, o, tx, targets, pos, true, yaw, pitch)
		},
	)
}

func transferTargetToWorld(tx *world.Tx, target cmd.Target, destination *world.World, pos mgl64.Vec3, setRot bool, yaw, pitch float64) {
	if destination == nil || tx == nil || destination == tx.World() {
		if setRot {
			moveTeleportTarget(target, pos, yaw, pitch)
		} else if teleporter, ok := target.(interface{ Teleport(mgl64.Vec3) }); ok {
			teleporter.Teleport(pos)
		}
		return
	}
	if p, ok := target.(*player.Player); ok {
		if setRot {
			rot := p.Rotation()
			p.Move(mgl64.Vec3{}, yaw-rot.Yaw(), pitch-rot.Pitch())
		}
		p.TeleportToWorld(tx, destination, pos)
		return
	}
	if e, ok := target.(world.Entity); ok {
		transferEntityToWorld(tx, e, destination, pos, setRot, yaw, pitch)
	}
}

func transferEntityToWorld(tx *world.Tx, e world.Entity, destination *world.World, pos mgl64.Vec3, setRot bool, yaw, pitch float64) {
	if destination == nil || tx == nil || destination == tx.World() {
		if setRot {
			moveTeleportTarget(e, pos, yaw, pitch)
		} else if teleporter, ok := e.(interface{ Teleport(mgl64.Vec3) }); ok {
			teleporter.Teleport(pos)
		}
		return
	}
	transfer, ok := world.BeginTransfer(tx)
	if !ok {
		e.H().DoAfter(50*time.Millisecond, func(rtx *world.Tx, ent world.Entity) {
			transferEntityToWorld(rtx, ent, destination, pos, setRot, yaw, pitch)
		})
		return
	}
	handle := transfer.Remove(tx, e)
	if handle == nil {
		return
	}
	task := destination.Do(func(destTx *world.Tx) {
		ne := destTx.AddEntity(handle)
		if setRot {
			moveTeleportTarget(ne, pos, yaw, pitch)
		} else if teleporter, ok := ne.(interface{ Teleport(mgl64.Vec3) }); ok {
			teleporter.Teleport(pos)
		}
	})
	if errors.Is(task.Err(), world.ErrWorldClosed) {
		tx.AddEntity(handle)
	}
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
	Targets  cmd.Optional[cmd.Players] `cmd:"targets"`
	Position cmd.Optional[mgl64.Vec3]  `cmd:"position"`
}

func (spawnpointCommand) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }
func (c spawnpointCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	pos, hasPos := c.Position.Load()
	var dim world.Dimension
	if ec, ok := cmd.ExecutionContextOf(src); ok && ec.World != nil {
		dim = ec.World.Dimension()
	} else if tx != nil {
		dim = tx.World().Dimension()
	}
	cmd.ForEach(src, o, tx, optionalPlayers(c.Targets, src),
		func(rtx *world.Tx, t cmd.Target) int {
			p, ok := t.(*player.Player)
			if !ok {
				return 0
			}
			pPos, pDim := pos, dim
			if !hasPos {
				pPos = p.Position()
				pDim = rtx.World().Dimension()
			} else if pDim == nil {
				pDim = rtx.World().Dimension()
			}
			p.SetRespawnPosition(cube.PosFromVec3(pPos), pDim)
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Set the respawn point for %d players.", tally.NonZero)
		},
	)
}

type rideCommand struct {
	srv     serverAdapter
	Target  cmd.OneEntity  `cmd:"target"`
	Mount   cmd.SubCommand `cmd:"mount"`
	Vehicle cmd.OneEntity  `cmd:"vehicle"`
}

type rideTargetInfo struct {
	ok    bool
	world *world.World
	name  string
}

type rideMountResult struct {
	mounted        bool
	diffDim        bool
	invalidTarget  bool
	invalidVehicle bool
	vehicleName    string
}

func (rideCommand) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }
func (c rideCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	vehicleSel := c.Vehicle
	cmd.ForEach(src, o, tx, c.Target,
		func(rtx *world.Tx, t cmd.Target) rideTargetInfo {
			e, ok := t.(world.Entity)
			if !ok {
				return rideTargetInfo{}
			}
			var w *world.World
			if rtx != nil {
				w = rtx.World()
			}
			return rideTargetInfo{ok: true, world: w, name: entityName(e)}
		},
		func(src cmd.Source, o *cmd.Output, tx *world.Tx, rs []cmd.Result[rideTargetInfo]) {
			if len(rs) != 1 {
				o.Errort(cmd.MessageNoTargets)
				return
			}
			riderInfo := rs[0].Value
			riderHandle := rs[0].Handle
			if !riderInfo.ok || riderHandle == nil {
				o.Errort(cmd.MessageParameterInvalid, "target")
				return
			}
			cmd.ForEach(src, o, tx, vehicleSel,
				func(vtx *world.Tx, t cmd.Target) rideMountResult {
					vehicle, ok := t.(world.Entity)
					if !ok {
						return rideMountResult{invalidVehicle: true}
					}
					if vtx == nil || vtx.World() != riderInfo.world {
						return rideMountResult{diffDim: true}
					}
					rider, ok := riderHandle.Entity(vtx)
					if !ok {
						return rideMountResult{invalidTarget: true}
					}
					if !entity.MountVehicle(vtx, vehicle, rider) {
						return rideMountResult{invalidVehicle: true}
					}
					return rideMountResult{mounted: true, vehicleName: entityName(vehicle)}
				},
				func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[rideMountResult]) {
					if len(rs) != 1 {
						o.Errort(cmd.MessageNoTargets)
						return
					}
					res := rs[0].Value
					switch {
					case res.diffDim:
						o.Errorf("Can't mount entity from a different dimension")
					case res.invalidTarget:
						o.Errort(cmd.MessageParameterInvalid, "target")
					case !res.mounted || res.invalidVehicle:
						o.Errort(cmd.MessageParameterInvalid, "vehicle")
					default:
						o.Printf("%s is now riding %s.", riderInfo.name, res.vehicleName)
					}
				},
			)
		},
	)
}

type dismountCommand struct {
	srv      serverAdapter
	Target   cmd.OneEntity  `cmd:"target"`
	Dismount cmd.SubCommand `cmd:"dismount"`
}

type dismountResult struct {
	ok   bool
	name string
}

func (dismountCommand) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }
func (c dismountCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	cmd.ForEach(src, o, tx, c.Target,
		func(rtx *world.Tx, t cmd.Target) dismountResult {
			rider, ok := t.(world.Entity)
			if !ok || !entity.DismountRider(rider, rtx) {
				return dismountResult{}
			}
			return dismountResult{ok: true, name: entityName(rider)}
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[dismountResult]) {
			if len(rs) != 1 {
				o.Errort(cmd.MessageNoTargets)
				return
			}
			if !rs[0].Value.ok {
				o.Errort(cmd.MessageParameterInvalid, "target")
				return
			}
			o.Printf("%s dismounted.", rs[0].Value.name)
		},
	)
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
	Targets        cmd.Entities `cmd:"targets"`
}
type spreadPlayersUnderCommand struct {
	srv            serverAdapter
	Center         mgl64.Vec3     `cmd:"center"`
	SpreadDistance float64        `cmd:"spreadDistance"`
	MaxRange       float64        `cmd:"maxRange"`
	Under          cmd.SubCommand `cmd:"under"`
	MaxHeight      int            `cmd:"maxHeight"`
	RespectTeams   bool           `cmd:"respectTeams"`
	Targets        cmd.Entities   `cmd:"targets"`
}

func (spreadPlayersCommand) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }
func (c spreadPlayersCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	destWorld := commandContextWorld(src, tx)
	maxHeight := tx.Range().Max()
	if destWorld != nil {
		maxHeight = destWorld.Range().Max()
	}
	spreadPlayersForEach(src, o, tx, destWorld, c.Center, c.SpreadDistance, c.MaxRange, maxHeight, c.Targets)
}
func (spreadPlayersUnderCommand) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }
func (c spreadPlayersUnderCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	destWorld := commandContextWorld(src, tx)
	spreadPlayersForEach(src, o, tx, destWorld, c.Center, c.SpreadDistance, c.MaxRange, c.MaxHeight, c.Targets)
}

type spreadPosition struct{ x, z float64 }

func spreadPlayersForEach(src cmd.Source, o *cmd.Output, tx *world.Tx, destWorld *world.World, center mgl64.Vec3, distance, maxRange float64, maxHeight int, targets cmd.Entities) {
	if distance < 0 || maxRange < distance+1 {
		o.Errorf("Maximum range must be at least spread distance + 1")
		return
	}
	minY := tx.Range().Min()
	if destWorld != nil {
		minY = destWorld.Range().Min()
	}
	if maxHeight < minY {
		o.Errorf("maxHeight is below the world's minimum height")
		return
	}
	cmd.ForEach(src, o, tx, targets,
		func(_ *world.Tx, t cmd.Target) bool {
			_, ok := t.(*player.Player)
			return ok
		},
		func(src cmd.Source, o *cmd.Output, rtx *world.Tx, rs []cmd.Result[bool]) {
			var handles []*world.EntityHandle
			for _, r := range rs {
				if r.Value && r.Handle != nil {
					handles = append(handles, r.Handle)
				}
			}
			if len(handles) == 0 {
				o.Errort(cmd.MessageNoTargets)
				return
			}
			cmd.InWorld(src, o, rtx, destWorld, func(_ cmd.Source, o *cmd.Output, dtx *world.Tx) {
				dests, ok := computeSpreadPositions(dtx, center, distance, maxRange, maxHeight, len(handles))
				if !ok {
					o.Errorf("Could not spread %d players around %g, %g (too many players for space - try using spread of at most %g)", len(handles), center[0], center[2], distance)
					return
				}
				for i, h := range handles {
					pos := dests[i]
					if e, ok := h.Entity(dtx); ok {
						if p, ok := e.(*player.Player); ok {
							p.Teleport(pos)
						}
						continue
					}
					h.Do(func(ptx *world.Tx, e world.Entity) {
						if p, ok := e.(*player.Player); ok {
							p.TeleportToWorld(ptx, destWorld, pos)
						}
					})
				}
				o.SetResult(len(handles))
				o.Printf("Spread %d players around %g, %g", len(handles), center[0], center[2])
			})
		},
	)
}

// spreadPlayers ports SpreadPlayersCommand for direct single-world callers.
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
	dests, ok := computeSpreadPositions(tx, center, distance, maxRange, maxHeight, len(players))
	if !ok {
		o.Errorf("Could not spread %d players around %g, %g (too many players for space - try using spread of at most %g)", len(players), center[0], center[2], distance)
		return
	}
	for i, p := range players {
		p.Teleport(dests[i])
	}
	o.Printf("Spread %d players around %g, %g", len(players), center[0], center[2])
}

// computeSpreadPositions ports SpreadPlayersCommand's relaxation loop:
// positions start uniformly in the square range, are pushed one block away
// from the average of too-close neighbours, and unsafe positions are re-rolled,
// for at most 10000 rounds.
func computeSpreadPositions(tx *world.Tx, center mgl64.Vec3, distance, maxRange float64, maxHeight, count int) ([]mgl64.Vec3, bool) {
	minX, minZ := center[0]-maxRange, center[2]-maxRange
	maxX, maxZ := center[0]+maxRange, center[2]+maxRange
	randomize := func(p *spreadPosition) {
		p.x = minX + rand.Float64()*(maxX-minX)
		p.z = minZ + rand.Float64()*(maxZ-minZ)
	}
	positions := make([]spreadPosition, count)
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
		return nil, false
	}
	dests := make([]mgl64.Vec3, count)
	for i := range positions {
		y, _ := spreadSpawnY(tx, positions[i], maxHeight)
		dests[i] = mgl64.Vec3{math.Floor(positions[i].x) + .5, float64(y), math.Floor(positions[i].z) + .5}
	}
	return dests, true
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
