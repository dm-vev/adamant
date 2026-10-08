package block

import (
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EndGateway is a block entity that teleports entities within the End dimension.
type EndGateway struct {
	replaceable
	transparent
	empty

	age              *atomic.Int64
	teleportCooldown *atomic.Int32

	exitPortal    cube.Pos
	hasExitPortal bool
	exactTeleport bool
}

// NewEndGateway creates a new initialised EndGateway.
func NewEndGateway() EndGateway {
	return EndGateway{
		age:              &atomic.Int64{},
		teleportCooldown: &atomic.Int32{},
	}
}

// Tick advances the gateway beam and its periodic cooldown.
func (e EndGateway) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if e.age == nil || e.teleportCooldown == nil {
		return
	}
	age := e.age.Add(1)
	cooling := e.teleportCooldown.Load() > 0
	if cooling {
		e.teleportCooldown.Add(-1)
	} else if age%2400 == 0 {
		e.teleportCooldown.Store(40)
	}
	tx.SetBlockEntity(pos, e)
	if age == 200 || cooling != (e.teleportCooldown.Load() > 0) {
		tx.SetBlock(pos, e, nil)
	}
}

// EntityInside uses the collision visitor rather than testing entity centres,
// so an entity touching the gateway with the edge of its box can enter.
func (e EndGateway) EntityInside(pos cube.Pos, tx *world.Tx, ent world.Entity) {
	if traveller, ok := ent.(interface{ CanUsePortal() bool }); ok && !traveller.CanUsePortal() {
		return
	}
	if e.teleportCooldown == nil || e.teleportCooldown.Load() > 0 {
		return
	}
	if dead, ok := ent.(interface{ Dead() bool }); ok && dead.Dead() {
		return
	}
	if rider, ok := ent.(interface{ Riding() *world.EntityHandle }); ok && rider.Riding() != nil {
		return
	}
	teleportable, ok := ent.(interface{ Teleport(mgl64.Vec3) })
	if !ok {
		return
	}
	e.teleportCooldown.Store(40)
	destination, found := e.destination(tx, pos)
	tx.SetBlock(pos, e, nil)
	if !found {
		return
	}
	if traveller, ok := ent.(interface{ TeleportThroughGateway(mgl64.Vec3) }); ok {
		traveller.TeleportThroughGateway(destination.Vec3Middle())
	} else {
		teleportable.Teleport(destination.Vec3Middle())
	}
}

func (e *EndGateway) destination(tx *world.Tx, entry cube.Pos) (cube.Pos, bool) {
	if !e.hasExitPortal && tx.World().Dimension() == world.End {
		exit := gatewayFindOrCreateExit(tx, entry).Add(cube.Pos{0, 10, 0})
		paired := NewEndGateway()
		paired.exitPortal, paired.hasExitPortal = entry, true
		GenerateEndGateway(tx, exit, paired)
		e.exitPortal, e.hasExitPortal = exit, true
	}
	if !e.hasExitPortal {
		return cube.Pos{}, false
	}
	if e.exactTeleport {
		return e.exitPortal, true
	}
	pos := gatewayTallestBlock(tx, e.exitPortal.Add(cube.Pos{0, 2, 0}), 5, false).Side(cube.FaceUp)
	return pos, true
}

// EncodeNBT ...
func (e EndGateway) EncodeNBT() map[string]any {
	m := map[string]any{
		"id": "EndGateway",
	}
	if e.age != nil {
		m["Age"] = e.age.Load()
	}
	if e.teleportCooldown != nil {
		m["TeleportCooldown"] = int32(e.teleportCooldown.Load())
	}
	if e.hasExitPortal {
		m["ExitPortal"] = map[string]any{
			"X": int32(e.exitPortal.X()),
			"Y": int32(e.exitPortal.Y()),
			"Z": int32(e.exitPortal.Z()),
		}
	}
	if e.exactTeleport {
		m["ExactTeleport"] = uint8(1)
	}
	return m
}

// DecodeNBT ...
func (e EndGateway) DecodeNBT(data map[string]any) any {
	eg := NewEndGateway()
	eg.age.Store(nbtconv.Int64(data, "Age"))
	eg.teleportCooldown.Store(nbtconv.Int32(data, "TeleportCooldown"))
	eg.exactTeleport = nbtconv.Bool(data, "ExactTeleport")

	if raw, ok := data["ExitPortal"]; ok {
		switch v := raw.(type) {
		case map[string]any:
			eg.exitPortal = cube.Pos{int(nbtconv.Int32(v, "X")), int(nbtconv.Int32(v, "Y")), int(nbtconv.Int32(v, "Z"))}
			eg.hasExitPortal = true
		case []any:
			if len(v) == 3 {
				x, _ := v[0].(int32)
				y, _ := v[1].(int32)
				z, _ := v[2].(int32)
				eg.exitPortal = cube.Pos{int(x), int(y), int(z)}
				eg.hasExitPortal = true
			}
		case []int32:
			if len(v) == 3 {
				eg.exitPortal = cube.Pos{int(v[0]), int(v[1]), int(v[2])}
				eg.hasExitPortal = true
			}
		}
	}
	return eg
}

// LightEmissionLevel returns the light level emitted by the gateway.
func (EndGateway) LightEmissionLevel() uint8 {
	return 15
}

// EncodeItem ...
func (EndGateway) EncodeItem() (name string, meta int16) {
	return "minecraft:end_gateway", 0
}

// EncodeBlock ...
func (EndGateway) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:end_gateway", nil
}
