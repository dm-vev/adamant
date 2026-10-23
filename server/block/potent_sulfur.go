package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// PotentSulfurState is the activity of a potent sulfur block.
type PotentSulfurState uint8

const (
	// PotentSulfurDry has no water source above it.
	PotentSulfurDry PotentSulfurState = iota
	// PotentSulfurWet releases noxious gas into the water above it.
	PotentSulfurWet
	// PotentSulfurDormant is a geyser over magma waiting to erupt.
	PotentSulfurDormant
	// PotentSulfurErupting is a geyser over magma launching entities upward.
	PotentSulfurErupting
	// PotentSulfurContinuous is a geyser over lava that never stops erupting.
	PotentSulfurContinuous
)

// Uint8 returns the state as a uint8.
func (s PotentSulfurState) Uint8() uint8 { return uint8(s) }

// String returns the Bedrock potent_sulfur_state value.
func (s PotentSulfurState) String() string {
	return [...]string{"dry", "wet", "dormant", "erupting", "continuous"}[s]
}

// geyser reports whether the state is a periodic geyser phase.
func (s PotentSulfurState) geyser() bool {
	return s == PotentSulfurDormant || s == PotentSulfurErupting
}

// PotentSulfur is a sulfur block that fills water above it with nauseating
// gas, and turns into a geyser over magma or lava.
type PotentSulfur struct {
	solid
	bassDrum

	// State is the activity of the block, following the water above and the
	// block below.
	State PotentSulfurState
	// Countdown is the number of seconds left in the current phase of a
	// periodic geyser, or -1 when the next phase has not been rolled yet.
	Countdown int32 //blockhash:ignore
}

// NewPotentSulfur returns a dry potent sulfur block without a rolled geyser
// phase.
func NewPotentSulfur() PotentSulfur {
	return PotentSulfur{Countdown: -1}
}

// BreakInfo ...
func (s PotentSulfur) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, pickaxeHarvestable, pickaxeEffective, oneOf(NewPotentSulfur())).withBlastResistance(6)
}

// UseOnBlock places the block in the state its surroundings give it.
func (s PotentSulfur) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}
	place(tx, pos, NewPotentSulfur().withSurroundings(pos, tx), user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick updates the state to the surroundings.
func (s PotentSulfur) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if next := s.withSurroundings(pos, tx); next != s {
		tx.SetBlock(pos, next, nil)
	}
}

// withSurroundings ports PotentSulfurBlock.validBlockState: dry without a
// water source above, a continuous geyser over a lava source, a periodic
// geyser over magma, and wet otherwise. Becoming a periodic geyser resets the
// countdown.
func (s PotentSulfur) withSurroundings(pos cube.Pos, tx *world.Tx) PotentSulfur {
	if !waterSourceAt(tx, pos.Side(cube.FaceUp)) {
		s.State = PotentSulfurDry
		return s
	}
	below := pos.Side(cube.FaceDown)
	sourceIfFluid := true
	if l, ok := tx.Liquid(below); ok {
		sourceIfFluid = l.LiquidDepth() == 8 && !l.LiquidFalling()
	}
	switch {
	case sourceIfFluid && javaBlockTag(tx, below, "causes_continuous_geyser_eruptions"):
		s.State = PotentSulfurContinuous
	case sourceIfFluid && javaBlockTag(tx, below, "causes_periodic_geyser_eruptions"):
		if !s.State.geyser() {
			s.Countdown = -1
		}
		if s.State != PotentSulfurErupting {
			s.State = PotentSulfurDormant
		}
	default:
		s.State = PotentSulfurWet
	}
	return s
}

// waterSourceAt reports whether pos holds a water source, as a block or
// waterlogged.
func waterSourceAt(tx *world.Tx, pos cube.Pos) bool {
	l, ok := tx.Liquid(pos)
	if !ok {
		return false
	}
	_, water := l.(Water)
	return water && l.LiquidDepth() == 8 && !l.LiquidFalling()
}

// Placed ports PotentSulfurBlock.onPlace: a geyser starting to erupt plays its
// start sound and emits a block activation.
func (s PotentSulfur) Placed(pos cube.Pos, tx *world.Tx, _ world.Block) {
	if s.State == PotentSulfurErupting || s.State == PotentSulfurContinuous {
		tx.PlaySound(pos.Vec3Centre(), sound.GeyserEruptionStart{Continuous: s.State == PotentSulfurContinuous})
		tx.EmitGameEvent(world.GameEventBlockActivate, pos.Vec3Centre(), world.GameEventContext{AffectedState: s})
	}
}

// Tick runs the server tickers PotentSulfurBlock.getTicker selects for the
// state.
func (s PotentSulfur) Tick(now int64, pos cube.Pos, tx *world.Tx) {
	switch s.State {
	case PotentSulfurWet:
		s.spreadNoxiousGas(now, pos, tx)
	case PotentSulfurDormant:
		s.countDown(now, pos, tx)
		s.spreadNoxiousGas(now, pos, tx)
	case PotentSulfurErupting:
		launchFromGeyser(pos, tx)
		s.countDown(now, pos, tx)
	case PotentSulfurContinuous:
		launchFromGeyser(pos, tx)
	}
}

// countDown ports SERVER_WAITING_COUNTDOWN_TICKER: once a second, a periodic
// geyser with gas above it counts down its phase, rolling the phase length
// when none is left, and switches between dormant and erupting at zero.
func (s PotentSulfur) countDown(now int64, pos cube.Pos, tx *world.Tx) {
	if now%20 != 0 {
		return
	}
	source, ok := noxiousGasSource(tx, pos)
	if !ok {
		return
	}
	if s.Countdown <= 0 {
		water := int32(source.Y() - pos.Y() - 1)
		r := geyserRandom(tx, pos)
		if s.State == PotentSulfurDormant {
			s.Countdown = 10*(water-1) + 15 + r.NextInt(16)
		} else {
			r.NextInt32()
			s.Countdown = water - 1 + 1 + r.NextInt(2)
		}
	}
	if s.Countdown > 0 {
		s.Countdown--
	}
	if s.Countdown != 0 {
		tx.SetBlockEntity(pos, s)
		return
	}
	next := s
	next.State = PotentSulfurErupting
	if s.State == PotentSulfurErupting {
		next.State = PotentSulfurDormant
	}
	tx.SetBlock(pos, next, nil)
	if next.State == PotentSulfurDormant {
		tx.EmitGameEvent(world.GameEventBlockDeactivate, pos.Vec3Centre(), world.GameEventContext{AffectedState: s})
	}
}

// geyserRandom ports PotentSulfurBlockEntity.geyserPositional, which fixes
// the phase lengths of a geyser by its position.
func geyserRandom(tx *world.Tx, pos cube.Pos) vanilla.Random {
	r := vanilla.NewXoroshiroRandom(tx.World().Seed() ^ -904011478)
	return r.ForkPositional().At(int32(pos.X()), int32(pos.Y()), int32(pos.Z()))
}

// geyserPassable ports isGeyserPassableBlock: air, water, or a block without
// collision.
func geyserPassable(tx *world.Tx, pos cube.Pos) bool {
	switch b := tx.Block(pos).(type) {
	case Air, Water:
		return true
	default:
		return len(b.Model().BBox(pos, tx)) == 0
	}
}

// noxiousGasSource ports findNoxiousGasSourceBlock: the first passable block
// above at most four water sources over the potent sulfur.
func noxiousGasSource(tx *world.Tx, pos cube.Pos) (cube.Pos, bool) {
	for p := pos.Side(cube.FaceUp); p.Y() <= pos.Y()+5; p = p.Side(cube.FaceUp) {
		if !waterSourceAt(tx, p) || !geyserPassable(tx, p) {
			return p, geyserPassable(tx, p)
		}
	}
	return cube.Pos{}, false
}

// entitiesTouching returns the entities whose bounding box intersects box.
func entitiesTouching(tx *world.Tx, box cube.BBox) []world.Entity {
	var touching []world.Entity
	for e := range tx.EntitiesWithin(box.Grow(4)) {
		if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			touching = append(touching, e)
		}
	}
	return touching
}

// geyserAffected ports EFFECT_PREDICATE: entities that are alive and not
// spectators.
func geyserAffected(e world.Entity) bool {
	if l, ok := e.(interface{ Dead() bool }); ok && l.Dead() {
		return false
	}
	m, ok := e.(interface{ GameMode() world.GameMode })
	return !ok || m.GameMode() != world.GameModeSpectator
}

// spreadNoxiousGas ports SERVER_NAUSEA_EFFECT_TICKER: every 10 ticks, living
// entities touching the gas above the water, with their eyes within 3 blocks
// of its source and above water in sight of it, get four seconds of ambient
// nausea.
func (s PotentSulfur) spreadNoxiousGas(now int64, pos cube.Pos, tx *world.Tx) {
	if now%10 != 0 {
		return
	}
	source, ok := noxiousGasSource(tx, pos)
	if !ok {
		return
	}
	box := cube.Box(-2.5, 0, -2.5, 3.5, 1, 3.5).Translate(source.Vec3())
	for _, e := range entitiesTouching(tx, box) {
		living, ok := e.(interface{ AddEffect(effect.Effect) })
		if !ok || !geyserAffected(e) {
			continue
		}
		if _, ok := e.(livingEntity); !ok {
			continue
		}
		eye := e.Position().Add(mgl64.Vec3{0, entityEyeHeight(e)})
		if noxiousGasReaches(tx, source, eye) {
			living.AddEffect(effect.NewAmbient(effect.Nausea, 1, 4*time.Second))
		}
	}
}

// noxiousGasReaches ports canBeReachedByNoxiousGas.
func noxiousGasReaches(tx *world.Tx, source cube.Pos, eye mgl64.Vec3) bool {
	if !geyserPassable(tx, cube.PosFromVec3(eye)) || eye.Sub(source.Vec3Centre()).LenSqr() > 9 {
		return false
	}
	start := source.Side(cube.FaceDown).Vec3Centre()
	end := eye.Sub(mgl64.Vec3{0, 1})
	if !waterSourceAt(tx, cube.PosFromVec3(end)) {
		return false
	}
	clear := true
	trace.TraverseBlocks(start, end, func(p cube.Pos) bool {
		if _, hit := trace.BlockIntercept(p, tx, tx.Block(p), start, end, trace.CollisionShape); hit {
			clear = false
		}
		return clear
	})
	return clear
}

// entityEyeHeight is the eye height of an entity: its own if it reports one,
// otherwise 85% of its height as Java's default.
func entityEyeHeight(e world.Entity) float64 {
	if h, ok := e.(interface{ EyeHeight() float64 }); ok {
		return h.EyeHeight()
	}
	return e.H().Type().BBox(e).Height() * 0.85
}

// launchFromGeyser ports LAUNCH_ENTITY_TICKER: entities touching the column
// above a geyser, six blocks per water block or up to an obstruction, have
// their fall distance capped while not falling fast and rise by 0.2 blocks
// per tick while slower than 0.3 plus 0.1 per water block. Players and the
// vehicles they control move themselves, and passengers move with their
// vehicle.
func launchFromGeyser(pos cube.Pos, tx *world.Tx) {
	source, ok := noxiousGasSource(tx, pos)
	if !ok {
		return
	}
	water := source.Y() - pos.Y() - 1
	height := 0
	for height < 6*water && geyserPassable(tx, pos.Add(cube.Pos{0, 1 + height, 0})) {
		height++
	}
	box := cube.Box(0, 1, 0, 1, 2, 1).Translate(pos.Vec3()).Extend(mgl64.Vec3{0, float64(height - 1)})
	for _, e := range entitiesTouching(tx, box) {
		if !geyserAffected(e) {
			continue
		}
		v, ok := e.(velocityEntity)
		if !ok {
			continue
		}
		vel := v.Velocity()
		if f, ok := e.(interface {
			FallDistance() float64
			SetFallDistance(float64)
		}); ok && vel[1] > -0.5 && f.FallDistance() > 1 {
			f.SetFallDistance(1)
		}
		if !geyserLaunches(tx, e) || vel[1] >= float64(float32(0.3))+float64(water)*0.1 {
			continue
		}
		vel[1] += float64(float32(0.2))
		v.SetVelocity(vel)
	}
}

// geyserLaunches reports whether the server moves e through a geyser: not
// players or vehicles they ride, which their client moves, not passengers,
// and not entities in #not_affected_by_geysers.
func geyserLaunches(tx *world.Tx, e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:player", "minecraft:ender_dragon":
		return false
	}
	if r, ok := e.(interface{ Riding() *world.EntityHandle }); ok && r.Riding() != nil {
		return false
	}
	for p := range tx.Players() {
		if r, ok := p.(interface{ Riding() *world.EntityHandle }); ok && r.Riding() == e.H() {
			return false
		}
	}
	return true
}

// EncodeItem ...
func (PotentSulfur) EncodeItem() (name string, meta int16) {
	return "minecraft:potent_sulfur", 0
}

// EncodeBlock ...
func (s PotentSulfur) EncodeBlock() (string, map[string]any) {
	return "minecraft:potent_sulfur", map[string]any{"potent_sulfur_state": s.State.String()}
}

// EncodeNBT ...
func (s PotentSulfur) EncodeNBT() map[string]any {
	return map[string]any{"id": "PotentSulfurBlock", "countdown": s.Countdown}
}

// DecodeNBT ...
func (s PotentSulfur) DecodeNBT(data map[string]any) any {
	s.Countdown = nbtconv.Int32(data, "countdown")
	if _, ok := data["countdown"]; !ok {
		s.Countdown = -1
	}
	return s
}

// allPotentSulfur returns every potent sulfur state.
func allPotentSulfur() (b []world.Block) {
	for s := PotentSulfurDry; s <= PotentSulfurContinuous; s++ {
		b = append(b, PotentSulfur{State: s, Countdown: -1})
	}
	return
}
