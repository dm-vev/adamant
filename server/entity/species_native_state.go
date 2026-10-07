package entity

import "github.com/df-mc/dragonfly/server/world"

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
		nbt["Size"] = int32(s.size - 1)
	}
	return nbt
}
