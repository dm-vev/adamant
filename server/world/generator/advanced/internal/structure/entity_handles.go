package structure

import (
	"fmt"
	"strconv"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// EntityFactory decodes a placed Java entity NBT record into its registered Dragonfly entity type.
type EntityFactory func(map[string]any, world.EntitySpawnOpts) (*world.EntityHandle, error)

// NewEntityHandles converts placed records, installing vehicles before riders.
// Templates use stable placement IDs; newly created mobs may supply fresh IDs.
func (p Placement) NewEntityHandles(factory EntityFactory) ([]*world.EntityHandle, error) {
	if factory == nil && len(p.Entities) != 0 {
		return nil, fmt.Errorf("structure: nil entity factory")
	}
	count := 0
	var countRecords func([]Entity)
	countRecords = func(records []Entity) {
		count += len(records)
		for _, record := range records {
			countRecords(record.Passengers)
		}
	}
	countRecords(p.Entities)
	out := make([]*world.EntityHandle, 0, count)
	var create func(Entity, *world.EntityHandle) error
	create = func(e Entity, vehicle *world.EntityHandle) error {
		var rotation cube.Rotation
		if count, ok := templateListLen(e.NBT["Rotation"]); ok && count >= 2 {
			rawYaw, _ := templateListValue(e.NBT["Rotation"], 0)
			if yaw, ok := number(rawYaw); ok {
				rotation[0] = yaw
			}
			rawPitch, _ := templateListValue(e.NBT["Rotation"], 1)
			if pitch, ok := number(rawPitch); ok {
				rotation[1] = pitch
			}
		}
		nbtID, _ := e.NBT["id"].(string)
		id := e.ID
		if id == uuid.Nil {
			key := strconv.FormatInt(e.Seed, 10) + "/" + strconv.FormatInt(int64(e.BlockPos[0]), 10) + "/" + strconv.FormatInt(int64(e.BlockPos[1]), 10) + "/" + strconv.FormatInt(int64(e.BlockPos[2]), 10) + "/" + nbtID + "/" + strconv.FormatInt(int64(e.Record), 10)
			id = uuid.NewSHA1(uuid.NameSpaceOID, []byte(key))
		}
		opts := world.EntitySpawnOpts{
			Position: mgl64.Vec3{e.Pos[0], e.Pos[1], e.Pos[2]},
			Rotation: rotation, ID: id, SpawnReason: e.SpawnReason, SpawnSeed: e.Seed,
			SpawnFinalized: e.SpawnFinalized, EntityRandom: e.EntityRandom, Riding: vehicle,
		}
		handle, err := factory(e.NBT, opts)
		if err != nil {
			return fmt.Errorf("structure entity %q at %v: %w", nbtID, e.BlockPos, err)
		}
		if handle == nil {
			return fmt.Errorf("structure entity %q factory returned nil", nbtID)
		}
		out = append(out, handle)
		for _, passenger := range e.Passengers {
			if err := create(passenger, handle); err != nil {
				return err
			}
		}
		return nil
	}
	for _, record := range p.Entities {
		if err := create(record, nil); err != nil {
			for _, handle := range out {
				_ = handle.Close()
			}
			return nil, err
		}
	}
	return out, nil
}
