package javastructure

import (
	"fmt"
	"strconv"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/go-gl/mathgl/mgl64"
)

// EntityFactory decodes a placed Java entity NBT record into its registered Dragonfly entity type.
type EntityFactory func(map[string]any, world.EntitySpawnOpts) (*world.EntityHandle, error)

// NewEntityHandles converts all placed entity records using a real entity factory.
// IDs derive from world seed, absolute position, and template record order, so parallel chunk order cannot change them.
func (p Placement) NewEntityHandles(factory EntityFactory) ([]*world.EntityHandle, error) {
	if factory == nil && len(p.Entities) != 0 { return nil, fmt.Errorf("Java structure: nil entity factory") }
	out := make([]*world.EntityHandle, 0, len(p.Entities))
	for _, e := range p.Entities {
		var rotation cube.Rotation
		if values, ok := e.NBT["Rotation"].([]any); ok && len(values) >= 2 {
			if yaw, ok := number(values[0]); ok { rotation[0] = yaw }
			if pitch, ok := number(values[1]); ok { rotation[1] = pitch }
		}
		nbtID, _ := e.NBT["id"].(string)
		key := strconv.FormatInt(e.Seed, 10) + "/" + strconv.FormatInt(int64(e.BlockPos[0]), 10) + "/" + strconv.FormatInt(int64(e.BlockPos[1]), 10) + "/" + strconv.FormatInt(int64(e.BlockPos[2]), 10) + "/" + nbtID + "/" + strconv.FormatInt(int64(e.Record),10)
		opts := world.EntitySpawnOpts{
			Position: mgl64.Vec3{e.Pos[0], e.Pos[1], e.Pos[2]},
			Rotation: rotation,
			ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)),
		}
		handle, err := factory(e.NBT, opts)
		if err != nil { return nil, fmt.Errorf("Java structure entity %q at %v: %w", nbtID, e.BlockPos, err) }
		if handle == nil { return nil, fmt.Errorf("Java structure entity %q factory returned nil", nbtID) }
		out = append(out, handle)
	}
	return out, nil
}
