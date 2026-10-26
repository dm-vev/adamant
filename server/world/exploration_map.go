package world

import (
	"errors"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// MapDecorationTypeFromJava returns the Bedrock map decoration type of a Java
// map decoration type used by exploration maps.
func MapDecorationTypeFromJava(name string) (byte, bool) {
	switch strings.TrimPrefix(name, "minecraft:") {
	case "red_x":
		return 4, true
	case "woodland_mansion", "mansion":
		return 14, true
	case "monument":
		return 15, true
	case "village_desert":
		return 17, true
	case "village_plains":
		return 18, true
	case "village_savanna":
		return 19, true
	case "village_snowy":
		return 20, true
	case "village_taiga":
		return 21, true
	case "jungle_temple":
		return 22, true
	case "swamp_hut":
		return 23, true
	case "trial_chambers":
		return 24, true
	}
	return 0, false
}

// CreateExplorationMap ports the exploration map functions of loot tables and
// cartographer trades: the nearest structure of the destination tag within
// searchRadius chunks of origin, skipping structures already on a map when
// skipKnown is set, is drawn as a biome preview map of the zoom passed,
// marked with the decoration. ok is false if no structure was found, which
// is always the case in worlds whose generator places no structures.
func (tx *Tx) CreateExplorationMap(origin cube.Pos, destination string, decoration byte, zoom uint8, searchRadius int, skipKnown bool) (data MapData, ok bool, err error) {
	tx.lockShared()
	defer tx.unlockShared()
	w := tx.World()
	destination = strings.TrimPrefix(destination, "#")
	if skipKnown {
		if _, err := w.RecordStructureAt(destination, origin); errors.Is(err, ErrNoStructureLookup) {
			return MapData{}, false, nil
		} else if err != nil {
			return MapData{}, false, err
		}
	}
	location, found, err := w.FindNearestMapFeature(destination, origin, searchRadius, skipKnown)
	if errors.Is(err, ErrNoStructureLookup) {
		return MapData{}, false, nil
	}
	if err != nil || !found {
		return MapData{}, false, err
	}
	if data, err = tx.CreateMap(location.Position, zoom); err != nil {
		return MapData{}, false, err
	}
	data.UnlimitedTracking = true
	if err = w.StoreMapData(data); err != nil {
		return MapData{}, false, err
	}
	if data, err = tx.RenderMapBiomePreview(data.ID); err != nil {
		return MapData{}, false, err
	}
	if err = tx.AddMapDecorationAt(data.ID, decoration, 8, 1, location.Position, "+"); err != nil {
		return MapData{}, false, err
	}
	if err = w.RecordStructureReference(location); err != nil {
		return MapData{}, false, err
	}
	data, err = w.MapData(data.ID)
	return data, err == nil, err
}
