package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func encodeSpeciesNative(k speciesKind, data *world.EntityData) map[string]any {
	nbt := encodeSpecies(k, data)
	s := data.Data.(*speciesState)
	switch k {
	case creeperSpecies:
		nbt["Fuse"] = int32(s.fuse)
		nbt["Swell"] = int32(s.swell)
		if s.charged {
			nbt["powered"] = byte(1)
		} else {
			nbt["powered"] = byte(0)
		}
		if s.ignited {
			nbt["ignited"] = byte(1)
		} else {
			nbt["ignited"] = byte(0)
		}
	case slimeSpecies:
		nbt["Size"] = uint8(s.size - 1)
	case endermanSpecies:
		delete(nbt, "carriedBlock")
		if s.carried != nil {
			name, properties := s.carried.EncodeBlock()
			nbt["carriedBlock"] = map[string]any{"name": name, "states": properties, "version": chunk.CurrentBlockVersion}
		}
		nbt["IsAngry"] = byte(boolByte(s.creepy))
	}
	return nbt
}
