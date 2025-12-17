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

// Tick advances the gateway's internal state and teleports entities when active.
func (e EndGateway) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if e.age == nil || e.teleportCooldown == nil {
		// Should never happen for properly initialised block entities.
		return
	}
	e.age.Add(1)

	if cd := e.teleportCooldown.Load(); cd > 0 {
		e.teleportCooldown.Add(-1)
		return
	}

	if e.age.Load()%2400 == 0 {
		// Mimic vanilla behaviour: periodically enter a short cooldown.
		e.teleportCooldown.Store(40)
		return
	}

	box := cube.Box(float64(pos.X()), float64(pos.Y()), float64(pos.Z()), float64(pos.X()+1), float64(pos.Y()+1), float64(pos.Z()+1))
	for ent := range tx.EntitiesWithin(box) {
		teleportable, ok := ent.(interface{ Teleport(mgl64.Vec3) })
		if !ok {
			continue
		}
		if dead, ok := ent.(interface{ Dead() bool }); ok && dead.Dead() {
			continue
		}

		dest := e.destination(tx)
		if dest == nil {
			continue
		}
		teleportable.Teleport(dest.Vec3Middle())
		e.teleportCooldown.Store(40)
		return
	}
}

func (e EndGateway) destination(tx *world.Tx) *cube.Pos {
	if tx.World().Dimension() != world.End {
		return nil
	}
	if !e.hasExitPortal {
		spawn := tx.World().Spawn()
		return &spawn
	}
	if e.exactTeleport {
		dest := e.exitPortal
		return &dest
	}

	x, z := e.exitPortal.X(), e.exitPortal.Z()
	y := tx.HighestBlock(x, z)
	if y < tx.Range().Min() {
		dest := e.exitPortal
		return &dest
	}
	dest := cube.Pos{x, min(tx.Range().Max(), y+1), z}
	return &dest
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
