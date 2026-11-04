package entity

import "github.com/df-mc/dragonfly/server/world"

// spawnMobFromNBT applies species-specific source data before first insertion.
func spawnMobFromNBT(id string, opts world.EntitySpawnOpts, nbt map[string]any) *world.EntityHandle {
	source := world.CloneNativeEntityNBT(nbt)
	if source == nil {
		source = make(map[string]any)
	}
	source["id"] = id
	handle, err := NewFromStructureNBT(source, opts)
	if err != nil {
		return nil
	}
	return handle
}
