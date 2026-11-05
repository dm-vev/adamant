package player

import (
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// The false-positive suites of the anti-cheat: traces of legitimate play,
// from the game's own physics and from javaMover, Java's travel, fed to a
// checked player as its client would send them. None may violate a check.

// floorY is the height of the feet of a player on the floor of traceWorld.
const floorY = 65

// The lanes of traceWorld: each runs along X from x = -40 to 40, three
// blocks wide, at the Z passed.
const (
	laneStone          = -40
	laneIce            = -36
	laneBlueIceCeiling = -32
	lanePackedIce      = -28
	laneSlime          = -24
	laneHoney          = -20
	laneSoulSand       = -16
	laneIceCeiling     = -12
	// The pool spans Z 0 to 8, four blocks of water deep.
	poolZ = 4
)

// The features of traceWorld beside the lanes.
var (
	bubbleUp    = cube.Pos{30, 61, 16}
	bubbleDown  = cube.Pos{34, 61, 16}
	ladder      = cube.Pos{-43, floorY, 30}
	scaffolding = cube.Pos{-40, floorY, 34}
	cobweb      = cube.Pos{-32, floorY, 34}
	powderSnow  = cube.Pos{-28, floorY, 34}
	hay         = cube.Pos{20, floorY - 1, 20}
	slimePad    = cube.Pos{24, floorY - 1, 20}
	// wall is a wall one block thick across X = 10, from Z 38 to 42 and
	// three high.
	wall = cube.Pos{10, floorY, 40}
	// tunnel is a tunnel two blocks high along X, at Z 44, and the gap one
	// block high along X at Z 46.
	tunnelZ = 44
	gapZ    = 46
)

// traceWorld returns a synchronous world with the courses of the traces:
// a stone floor, lanes of ice, blue ice and packed ice, slime, honey and
// soul sand, two of them under trapdoors two blocks up, a pool, bubble
// columns up and down, a ladder, scaffolding, cobweb, powder snow, hay, a
// wall and a tunnel.
func traceWorld(t testing.TB) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry}.New()
	viewer := clienttest.New()
	loader := world.NewLoader(5, w, viewer)
	t.Cleanup(func() {
		<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	})
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, mgl64.Vec3{0.5, floorY, 0.5})
		loader.Load(tx, 121)
		for x := -48; x <= 48; x++ {
			for z := -48; z <= 48; z++ {
				tx.SetBlock(cube.Pos{x, floorY - 1, z}, block.Stone{}, nil)
			}
		}
		lane := func(z int, b world.Block, ceiling bool) {
			for x := -40; x <= 40; x++ {
				for dz := -1; dz <= 1; dz++ {
					tx.SetBlock(cube.Pos{x, floorY - 1, z + dz}, b, nil)
					if ceiling {
						tx.SetBlock(cube.Pos{x, floorY + 2, z + dz}, block.WoodTrapdoor{Wood: block.OakWood()}, nil)
					}
				}
			}
		}
		lane(laneIce, block.Ice{}, false)
		lane(laneBlueIceCeiling, block.BlueIce{}, true)
		lane(lanePackedIce, block.PackedIce{}, false)
		lane(laneSlime, block.Slime{}, false)
		lane(laneHoney, block.HoneyBlock{}, false)
		lane(laneSoulSand, block.SoulSand{}, false)
		lane(laneIceCeiling, block.Ice{}, true)
		for x := -40; x <= 40; x++ {
			for z := 0; z <= 8; z++ {
				tx.SetBlock(cube.Pos{x, floorY - 5, z}, block.Stone{}, nil)
				for y := floorY - 4; y < floorY; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, nil, nil)
					tx.SetLiquid(cube.Pos{x, y, z}, block.Water{Depth: 8, Still: true})
				}
			}
		}
		column := func(base cube.Pos, down bool) {
			tx.SetBlock(base.Side(cube.FaceDown), block.SoulSand{}, nil)
			for y := base[1]; y < floorY; y++ {
				tx.SetBlock(cube.Pos{base[0], y, base[2]}, block.BubbleColumn{DragDown: down}, nil)
				tx.SetLiquid(cube.Pos{base[0], y, base[2]}, block.Water{Depth: 8, Still: true})
			}
		}
		column(bubbleUp, false)
		column(bubbleDown, true)
		for y := 0; y < 10; y++ {
			tx.SetBlock(ladder.Add(cube.Pos{-1, y, 0}), block.Stone{}, nil)
			tx.SetBlock(ladder.Add(cube.Pos{0, y, 0}), block.Ladder{Facing: cube.East}, nil)
			tx.SetBlock(scaffolding.Add(cube.Pos{0, y, 0}), block.Scaffolding{}, nil)
		}
		tx.SetBlock(cobweb, block.Cobweb{}, nil)
		tx.SetBlock(powderSnow, block.PowderSnow{}, nil)
		tx.SetBlock(hay, block.HayBale{Axis: cube.Y}, nil)
		tx.SetBlock(slimePad, block.Slime{}, nil)
		for z := -2; z <= 2; z++ {
			for y := 0; y < 3; y++ {
				tx.SetBlock(wall.Add(cube.Pos{0, y, z}), block.Stone{}, nil)
			}
		}
		for x := -10; x <= 10; x++ {
			tx.SetBlock(cube.Pos{x, floorY + 2, tunnelZ}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{x, floorY + 1, gapZ}, block.Stone{}, nil)
		}
	})
	return w
}

// acTrace is a trace of legitimate play.
type acTrace struct {
	name  string
	start mgl64.Vec3
	ticks int
	// latency is the latency of the client, half its round trip.
	latency time.Duration
	// perTick is how many moves the server handles a tick, as a lagging
	// server does: 1 if 0. The server ticks every perTick × 50 ms.
	perTick int
	// arrival, if set, is when move i of the trace reaches the server,
	// after the trace began; by default (i + 1) × 50 ms, a move a tick.
	arrival func(i int) time.Duration
	// setup prepares the player and the mover before the trace.
	setup func(tx *world.Tx, p *Player, j *javaMover)
	// input is what the client holds down in tick i of the trace.
	input func(i int, j *javaMover) javaInput
	// event runs on the server before the move of tick i of the client;
	// rtt is the round trip in ticks, after which the client applies what
	// the server gave it.
	event func(i int, tx *world.Tx, p *Player, j *javaMover, rtt int)
	// positions, if set, are the positions of the trace in place of the
	// mover's.
	positions func(tx *world.Tx) []mgl64.Vec3
	// conf, if set, edits the anti-cheat of the trace.
	conf func(c *anticheat.Config)
	// bedrock sends the positions as a Bedrock client does: the eye as
	// float32, which the session turns into the feet, and tells the server
	// its player collided below (the VerticalCollision flag) when the
	// mover's player is on the ground.
	bedrock bool
	// claims, if set, says in place of the mover whether move i of a
	// bedrock trace claims to stand on the ground.
	claims func(i int) bool
	// lift is how much higher than the server's blocks a bedrock client
	// stands: a shape the client has and the server lacks.
	lift float64
}

// traceResult is what the anti-cheat did of a trace.
type traceResult struct {
	violations  []anticheat.Violation
	corrections int
	refused     int
	deferred    int
}

// runTrace feeds the trace tr to a new player of w, checked by the default
// anti-cheat as edited by tr.conf, after it stood still through the grace
// of its join, as a client sends a move every tick. The moves reach the
// server when tr.arrival says, and the server ticks every perTick × 50 ms
// of its wall clock, handling the moves that arrived in order: a move the
// game defers waits, with those after it, for the next tick, as the
// session holds them.
func runTrace(t testing.TB, w *world.World, tr acTrace) traceResult {
	t.Helper()
	r := clienttest.New()
	r.Answers.Latency = tr.latency
	conf := anticheat.Default()
	if tr.conf != nil {
		tr.conf(conf)
	}
	h := &violationRecorder{}
	pconf := Config{Position: tr.start, GameMode: world.GameModeSurvival, Client: r, AntiCheat: conf, Name: "Trace", Log: slog.New(slog.DiscardHandler)}
	handle := world.EntitySpawnOpts{Position: tr.start, ID: uuid.New()}.New(Type, pconf)
	var res traceResult
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(handle).(*Player)
		defer func() { _ = p.Close() }()
		p.Handle(h)
		wall := time.Unix(1_000_000, 0)
		p.anticheat().now = func() time.Time { return wall }
		perTick := max(tr.perTick, 1)
		current := int64(0)
		tick := func(d time.Duration) {
			current++
			wall = wall.Add(d)
			p.Tick(tx, current)
		}
		rtt := int(math.Ceil(float64(2*tr.latency) / float64(anticheatTick)))
		j := &javaMover{pos: tr.start, onGround: math.Abs(tr.start[1]-floorY) < 1e-9}
		// The client stands still through the grace of its join.
		for range conf.Grace.Join + 1 {
			at := p.Position()
			if tr.bedrock && tr.lift != 0 {
				// The client stands where its blocks hold it up.
				at = bedrockWire(tr.start.Add(mgl64.Vec3{0, tr.lift, 0}))
			}
			p.ClientMove(client.Move{Position: at, Rotation: p.Rotation(), Arrived: wall, ClaimsGround: tr.bedrock})
			tick(anticheatTick)
		}
		if tr.setup != nil {
			tr.setup(tx, p, j)
		}
		var positions []mgl64.Vec3
		if tr.positions != nil {
			positions = tr.positions(tx)
		}
		corrections := len(r.CallsTo("CorrectMove"))
		ticks := tr.ticks
		if positions != nil {
			ticks = len(positions)
		}
		arrival := tr.arrival
		if arrival == nil {
			arrival = func(i int) time.Duration { return time.Duration(i+1) * anticheatTick }
		}
		begin := wall
		type sent struct {
			m  client.Move
			in javaInput
		}
		var queue []sent
		next := 0
		for extra := 0; (next < ticks || len(queue) > 0) && extra < 400; {
			// The moves the client sent that reached the server by now.
			for next < ticks && !begin.Add(arrival(next)).After(wall) {
				i := next
				next++
				if tr.event != nil {
					tr.event(i, tx, p, j, rtt)
				}
				var in javaInput
				if tr.input != nil {
					in = tr.input(i, j)
				}
				var pos mgl64.Vec3
				if positions != nil {
					pos = positions[i]
				} else {
					pos = j.step(tx, p, in)
				}
				claim := false
				if tr.bedrock {
					claim = positions == nil && j.onGround
					if tr.claims != nil {
						claim = tr.claims(i)
					}
					pos = bedrockWire(pos.Add(mgl64.Vec3{0, tr.lift, 0}))
				}
				m := client.Move{Position: pos, Rotation: cube.Rotation{in.yaw, 0}, Tick: uint64(i + 1), Jumping: in.jump, Sprinting: in.sprint, Sneaking: in.sneak,
					Crawling: in.crawl, Riding: p.Riding() != nil, Arrived: begin.Add(arrival(i)), ClaimsGround: claim}
				if j.acknowledge {
					m.Acknowledged, j.acknowledge = true, false
				}
				queue = append(queue, sent{m: m, in: in})
			}
			for len(queue) > 0 && !p.Dead() && !p.closing.Load() {
				m, in := queue[0].m, queue[0].in
				// As the session does: the move, then the flags of the input.
				r.SetHandlingInput(true)
				result := p.ClientMove(m)
				if result == client.MoveDeferred {
					r.SetHandlingInput(false)
					res.deferred++
					break
				}
				queue = queue[1:]
				if result == client.MoveRefused {
					res.refused++
				}
				if in.sprint && !p.Sprinting() {
					p.StartSprinting()
				} else if !in.sprint && p.Sprinting() {
					p.StopSprinting()
				}
				if in.sneak && !p.Sneaking() {
					p.StartSneaking()
				} else if !in.sneak && p.Sneaking() {
					p.StopSneaking()
				}
				r.SetHandlingInput(false)
			}
			if p.Dead() || p.closing.Load() {
				break
			}
			tick(time.Duration(perTick) * anticheatTick)
			if next >= ticks {
				extra++
			}
		}
		res.corrections = len(r.CallsTo("CorrectMove")) - corrections
	})
	res.violations = h.violations
	return res
}

// forward returns an input that holds forward along yaw, with the options
// passed.
func forward(yaw float64, sprint, jump bool) func(int, *javaMover) javaInput {
	return func(int, *javaMover) javaInput {
		return javaInput{forward: 1, yaw: yaw, sprint: sprint, jump: jump}
	}
}

// east is the yaw of a player that looks along +X, north along -Z, south
// along +Z.
const (
	east  = -90.0
	south = 0.0
	north = 180.0
)

// laneStart returns where a trace along the lane at z starts.
func laneStart(z int) mgl64.Vec3 { return mgl64.Vec3{-35.5, floorY, float64(z) + 0.5} }

// legitimateTraces returns the traces of the false-positive suites that
// the game's physics and Java's travel make: walking, sprinting and
// sprint-jumping on every lane, swimming, bubble columns, climbing,
// Levitation and Slow Falling, falls, pushes and the conditions of play.
func legitimateTraces() []acTrace {
	var traces []acTrace
	for _, lane := range []struct {
		name string
		z    int
	}{
		{"stone", laneStone}, {"ice", laneIce}, {"blue ice under trapdoors", laneBlueIceCeiling}, {"packed ice", lanePackedIce},
		{"slime", laneSlime}, {"honey", laneHoney}, {"soul sand", laneSoulSand}, {"ice under trapdoors", laneIceCeiling},
	} {
		traces = append(traces,
			acTrace{name: "walking on " + lane.name, start: laneStart(lane.z), ticks: 200, input: forward(east, false, false)},
			acTrace{name: "sprinting on " + lane.name, start: laneStart(lane.z), ticks: 200, input: forward(east, true, false)},
			acTrace{name: "sprint-jumping on " + lane.name, start: laneStart(lane.z), ticks: 200, input: forward(east, true, true)},
		)
	}
	traces = append(traces,
		acTrace{name: "walking diagonally", start: laneStart(laneStone), ticks: 100, input: func(int, *javaMover) javaInput {
			return javaInput{forward: 1, strafe: 1, yaw: east, sprint: true}
		}},
		acTrace{name: "sneaking off the edge of the pool", start: mgl64.Vec3{0.5, floorY, -3.5}, ticks: 80, input: func(int, *javaMover) javaInput {
			return javaInput{forward: 1, yaw: south, sneak: true}
		}},
		acTrace{name: "walking into the pool", start: mgl64.Vec3{0.5, floorY, -3.5}, ticks: 120, input: forward(south, false, false)},
		acTrace{name: "swimming at the surface", start: mgl64.Vec3{-30.5, floorY - 0.6, float64(poolZ) + 0.5}, ticks: 200, input: func(int, *javaMover) javaInput {
			return javaInput{forward: 1, yaw: east, jump: true}
		}},
		acTrace{name: "swimming under water", start: mgl64.Vec3{-30.5, floorY - 4, float64(poolZ) + 0.5}, ticks: 200, input: func(int, *javaMover) javaInput {
			return javaInput{forward: 1, yaw: east, sprint: true}
		}},
		acTrace{name: "swimming with Dolphin's Grace", start: mgl64.Vec3{-30.5, floorY - 4, float64(poolZ) + 0.5}, ticks: 200, input: func(int, *javaMover) javaInput {
			return javaInput{forward: 1, yaw: east, sprint: true, dolphin: true}
		}},
		acTrace{name: "rising in a bubble column", start: bubbleUp.Vec3Middle().Add(mgl64.Vec3{0, -0.5, 0}), ticks: 60},
		acTrace{name: "sinking in a bubble column", start: mgl64.Vec3{bubbleDown.Vec3Middle()[0], floorY, bubbleDown.Vec3Middle()[2]}, ticks: 60},
		acTrace{name: "climbing a ladder", start: mgl64.Vec3{float64(ladder[0]) + 0.3, floorY, float64(ladder[2]) + 0.5}, ticks: 80, input: forward(90, false, false)},
		acTrace{name: "climbing scaffolding", start: scaffolding.Vec3Middle().Add(mgl64.Vec3{0, -0.5, 0}), ticks: 80, input: func(int, *javaMover) javaInput {
			return javaInput{jump: true}
		}},
		acTrace{name: "walking through cobweb", start: mgl64.Vec3{float64(cobweb[0]) - 2.5, floorY, float64(cobweb[2]) + 0.5}, ticks: 80, input: forward(east, false, false)},
		acTrace{name: "walking into powder snow", start: mgl64.Vec3{float64(powderSnow[0]) - 2.5, floorY, float64(powderSnow[2]) + 0.5}, ticks: 80, input: forward(east, false, false)},
		acTrace{name: "jumping in the tunnel", start: mgl64.Vec3{-9.5, floorY, float64(tunnelZ) + 0.5}, ticks: 80, input: forward(east, true, true)},
		acTrace{name: "crawling through a gap", start: mgl64.Vec3{-9.5, floorY, float64(gapZ) + 0.5}, ticks: 120,
			setup: func(_ *world.Tx, p *Player, _ *javaMover) { p.StartCrawling() },
			input: func(int, *javaMover) javaInput { return javaInput{forward: 1, yaw: east, crawl: true} }},
		acTrace{name: "Levitation V", start: laneStart(laneStone), ticks: 100, input: func(int, *javaMover) javaInput {
			return javaInput{levitation: 5, forward: 1, yaw: east}
		}, setup: withEffect(effectLevitation(5))},
		acTrace{name: "falling with Slow Falling", start: mgl64.Vec3{-20.5, floorY + 30, -40.5}, ticks: 300, input: func(int, *javaMover) javaInput {
			return javaInput{slowFalling: true, forward: 1, yaw: east}
		}, setup: withEffect(effectSlowFalling())},
		acTrace{name: "falling from 30 blocks onto stone", start: mgl64.Vec3{-20.5, floorY + 30, -40.5}, ticks: 60},
		acTrace{name: "falling from 30 blocks into the pool", start: mgl64.Vec3{-20.5, floorY + 30, float64(poolZ) + 0.5}, ticks: 80},
		acTrace{name: "falling from 30 blocks onto slime", start: mgl64.Vec3{float64(slimePad[0]) + 0.5, floorY + 30, float64(slimePad[2]) + 0.5}, ticks: 120},
	)
	traces = append(traces, gamePhysicsTraces()...)
	traces = append(traces, pushTraces()...)
	traces = append(traces, conditionTraces()...)
	return traces
}

// gamePhysicsTraces returns falls a player without a client makes, ticked
// by the game's own MovementComputer: from y = 120, straight and thrown,
// onto stone, into water, onto slime and onto hay.
func gamePhysicsTraces() []acTrace {
	var traces []acTrace
	for _, tc := range []struct {
		name string
		at   mgl64.Vec3
		vel  mgl64.Vec3
	}{
		{"onto stone", mgl64.Vec3{-20.5, 120, -40.5}, mgl64.Vec3{}},
		{"onto stone, thrown", mgl64.Vec3{-20.5, 120, -40.5}, mgl64.Vec3{0.6, 0.4, 0}},
		{"into water", mgl64.Vec3{-20.5, 120, float64(poolZ) + 0.5}, mgl64.Vec3{}},
		{"onto slime", mgl64.Vec3{float64(slimePad[0]) + 0.5, 120, float64(slimePad[2]) + 0.5}, mgl64.Vec3{}},
		{"onto hay", mgl64.Vec3{float64(hay[0]) + 0.5, 120, float64(hay[2]) + 0.5}, mgl64.Vec3{}},
	} {
		tc := tc
		traces = append(traces, acTrace{name: "the game's fall " + tc.name, start: tc.at,
			// The server threw the player, as it throws the body.
			setup: func(_ *world.Tx, p *Player, _ *javaMover) { p.SetVelocity(tc.vel) },
			positions: func(tx *world.Tx) []mgl64.Vec3 {
				return simulatedFall(tx, tc.at, tc.vel, 160)
			}})
	}
	return traces
}

// simulatedFall ticks a player without a client from at, moving at vel, for
// the ticks passed, and returns its positions.
func simulatedFall(tx *world.Tx, at, vel mgl64.Vec3, ticks int) []mgl64.Vec3 {
	h := world.EntitySpawnOpts{Position: at, Velocity: vel, ID: uuid.New()}.New(Type, Config{Position: at, Velocity: vel, GameMode: world.GameModeSurvival})
	body := tx.AddEntity(h).(*Player)
	positions := make([]mgl64.Vec3, 0, ticks)
	for i := range ticks {
		body.Tick(tx, int64(i))
		positions = append(positions, body.Position())
	}
	tx.RemoveEntity(body)
	_ = h.Close()
	return positions
}

// pushTraces returns traces of what pushes a player: knockback, a TNT
// explosion, a wind charge, riptide and a piston, at a latency of 0 and of
// 150, 500 and 1000 ms, the client applying the push a round trip later.
func pushTraces() []acTrace {
	var traces []acTrace
	for _, latency := range []time.Duration{0, 150 * time.Millisecond, 500 * time.Millisecond, time.Second} {
		for _, push := range []struct {
			name string
			vel  mgl64.Vec3
		}{
			{"knockback", mgl64.Vec3{0.4, 0.4, 0}},
			{"a TNT explosion", mgl64.Vec3{12, 1.5, 0}},
			{"a wind charge", mgl64.Vec3{0.2, 1.1, 0}},
			{"riptide", mgl64.Vec3{2.8, 1.5, 0}},
		} {
			push := push
			traces = append(traces, acTrace{
				name: push.name + " at " + latency.String(), start: laneStart(laneStone), ticks: 100, latency: latency,
				input: forward(east, false, false),
				event: func(i int, _ *world.Tx, p *Player, j *javaMover, rtt int) {
					if i == 20 {
						p.SetVelocity(push.vel)
						j.apply(j.tick+rtt+1, push.vel, false)
					}
				},
			})
		}
		traces = append(traces, acTrace{
			name: "knockback of a zombie's attack at " + latency.String(), start: laneStart(laneStone), ticks: 60, latency: latency,
			input: forward(east, true, false),
			event: func(i int, _ *world.Tx, p *Player, j *javaMover, rtt int) {
				if i == 20 {
					p.KnockBack(p.Position().Add(mgl64.Vec3{1, 0, 0}), 0.4, 0.4)
					// Java's knockback halves the velocity and adds the push.
					j.apply(j.tick+rtt+1, mgl64.Vec3{-0.4 - j.vel[0]/2, 0.4, -j.vel[2] / 2}, true)
				}
			},
		})
	}
	traces = append(traces, acTrace{
		name: "pushed by a piston", start: laneStart(laneStone), ticks: 60, input: forward(east, false, false),
		event: func(i int, _ *world.Tx, p *Player, j *javaMover, _ int) {
			if i == 20 {
				p.Displace(mgl64.Vec3{1, 0, 0})
				j.pos, j.vel = p.Position(), mgl64.Vec3{}
			}
		},
	})
	return traces
}

// conditionTraces returns the traces of the conditions of play: teleports,
// a respawn's teleport, a change of game mode while flying, a vehicle and
// its dismount, gliding, a lagging server and a stalled connection.
func conditionTraces() []acTrace {
	return []acTrace{
		{name: "teleported while sprinting", start: laneStart(laneStone), ticks: 100, input: forward(east, true, true),
			event: func(i int, _ *world.Tx, p *Player, j *javaMover, _ int) {
				if i == 40 {
					// A command, an ender pearl or chorus fruit: the client
					// acknowledges at the target and moves on from rest.
					to := laneStart(laneIce)
					p.Teleport(to)
					j.pos, j.vel, j.acknowledge = to, mgl64.Vec3{}, true
				}
			}},
		{name: "creative flight, then survival in mid-air", start: mgl64.Vec3{-30.5, floorY + 10, -40.5}, ticks: 100,
			setup: func(_ *world.Tx, p *Player, j *javaMover) {
				p.SetGameMode(world.GameModeCreative)
				p.StartFlying()
			},
			positions: func(*world.Tx) []mgl64.Vec3 {
				// Flying fast, the game mode changes at tick 30 and the client
				// falls on with what it carried.
				var ps []mgl64.Vec3
				pos, vel := mgl64.Vec3{-30.5, floorY + 10, -40.5}, mgl64.Vec3{1.1, 0, 0}
				for i := range 100 {
					if i >= 30 {
						vel = mgl64.Vec3{vel[0] * 0.91, (vel[1] - 0.08) * 0.98, 0}
					}
					pos = pos.Add(vel)
					if pos[1] < floorY {
						pos[1], vel[1] = floorY, 0
					}
					ps = append(ps, pos)
				}
				return ps
			},
			event: func(i int, _ *world.Tx, p *Player, _ *javaMover, _ int) {
				if i == 30 {
					p.SetGameMode(world.GameModeSurvival)
				}
			}},
		{name: "a lagging server: three moves a tick", start: laneStart(laneStone), ticks: 150, perTick: 3, input: forward(east, true, true)},
		{name: "jitter: bursts of five after 250 ms", start: laneStart(laneStone), ticks: 200, input: forward(east, true, false),
			arrival: func(i int) time.Duration { return time.Duration(i/5+1) * 250 * time.Millisecond }},
		{name: "jitter of 200 ms", start: laneStart(laneStone), ticks: 200, input: forward(east, true, false),
			arrival: func(i int) time.Duration {
				d := time.Duration(i+1) * anticheatTick
				if i%4 == 1 {
					d += 200 * time.Millisecond
				}
				return d
			}},
		{name: "a stall of 2.5 s and its burst", start: laneStart(laneStone), ticks: 200, input: forward(east, false, false),
			arrival: func(i int) time.Duration {
				if i >= 60 && i < 110 {
					return 110 * anticheatTick
				}
				return time.Duration(i+1) * anticheatTick
			}},
		{name: "riding, then dismounting", start: laneStart(laneStone), ticks: 80,
			setup: func(tx *world.Tx, p *Player, _ *javaMover) {
				pig := tx.AddEntity(entity.NewPig(world.EntitySpawnOpts{Position: p.Position()}))
				p.SetRiding(pig.H())
			},
			positions: func(*world.Tx) []mgl64.Vec3 {
				var ps []mgl64.Vec3
				for i := range 80 {
					ps = append(ps, laneStart(laneStone).Add(mgl64.Vec3{float64(min(i, 40)) * 0.3, 0, 0}))
				}
				return ps
			},
			event: func(i int, _ *world.Tx, p *Player, _ *javaMover, _ int) {
				if i == 40 {
					// The vehicle carried the player 12 blocks: it dismounts
					// there.
					p.setPosition(laneStart(laneStone).Add(mgl64.Vec3{12, 0, 0}))
					p.SetRiding(nil)
				}
			}},
	}
}

// withEffect returns a setup that gives the player an effect.
func withEffect(e func() effect.Effect) func(*world.Tx, *Player, *javaMover) {
	return func(_ *world.Tx, p *Player, _ *javaMover) { p.AddEffect(e()) }
}

// effectLevitation returns Levitation of the level passed, for a minute.
func effectLevitation(level int) func() effect.Effect {
	return func() effect.Effect { return effect.New(effect.Levitation, level, time.Minute) }
}

// effectSlowFalling returns Slow Falling for a minute.
func effectSlowFalling() func() effect.Effect {
	return func() effect.Effect { return effect.New(effect.SlowFalling, 1, time.Minute) }
}

// TestAntiCheatFalsePositiveSuites runs every legitimate trace through the
// default anti-cheat: none may violate any check, and none is corrected.
func TestAntiCheatFalsePositiveSuites(t *testing.T) {
	w := traceWorld(t)
	for _, tr := range legitimateTraces() {
		res := runTrace(t, w, tr)
		if len(res.violations) != 0 || res.corrections != 0 {
			first := res.violations
			if len(first) > 3 {
				first = first[:3]
			}
			t.Errorf("%s: %d violations, %d corrections: %+v", tr.name, len(res.violations), res.corrections, first)
		}
	}
}
