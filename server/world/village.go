package world

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/google/uuid"
)

// POIKind identifies a village point-of-interest category.
type POIKind uint8

const (
	Workstation POIKind = iota + 1
	Home
	Meeting
	Other
)

// POI is a registered point of interest. Type is its namespaced block name.
type POI struct {
	Kind     POIKind
	Type     string
	Owner    uuid.UUID
	HasOwner bool
	Capacity int
	Owners   []uuid.UUID
}

// Vacant reports whether the POI has an available occupancy ticket.
func (p POI) Vacant() bool {
	capacity := p.Capacity
	if capacity < 0 {
		capacity = 0
	}
	return len(p.Owners) < capacity
}

type villagePOI struct {
	Pos cube.Pos `json:"pos"`
	POI POI      `json:"poi"`
}

// poiType returns Java POI classification for supported block states.
func poiType(b Block) (POI, bool) {
	name, state := b.EncodeBlock()
	name = strings.TrimPrefix(name, "minecraft:")
	switch name {
	case "lit_blast_furnace":
		name = "blast_furnace"
	case "lit_smoker":
		name = "smoker"
	case "stonecutter_block":
		name = "stonecutter"
	case "portal":
		name = "nether_portal"
	}
	p := POI{Type: "minecraft:" + name, Capacity: 1}
	switch {
	case name == "bell":
		p.Kind, p.Capacity = Meeting, 32
	case name == "bed" && nativeBoolValue(state["head_piece_bit"]):
		p.Kind = Home
	case name == "blast_furnace", name == "brewing_stand", name == "cartography_table", name == "cauldron", name == "lava_cauldron", name == "water_cauldron", name == "powder_snow_cauldron", name == "barrel", name == "composter", name == "fletching_table", name == "grindstone", name == "lectern", name == "loom", name == "smithing_table", name == "smoker", name == "stonecutter":
		p.Kind = Workstation
	case name == "beehive", name == "bee_nest", name == "nether_portal", name == "lodestone", name == "test_instance_block", name == "lightning_rod", strings.HasSuffix(name, "_lightning_rod"):
		p.Kind, p.Capacity = Other, 0
	default:
		return POI{}, false
	}
	return p, true
}

// POI returns the registered POI at pos, if any.
func (tx *Tx) POI(pos cube.Pos) (POI, bool) {
	p, ok := tx.World().pois[pos]
	return p, ok
}

// NearestPOI finds the closest matching registered POI within a spherical radius.
// Ties are resolved lexicographically by block position.
func (tx *Tx) NearestPOI(pos cube.Pos, kind POIKind, radius int, predicate func(POI) bool) (cube.Pos, POI, bool) {
	if radius < 0 {
		return cube.Pos{}, POI{}, false
	}
	limit := int64(radius) * int64(radius)
	bestDistance := int64(1<<63 - 1)
	var best cube.Pos
	var value POI
	found := false
	for candidate, poi := range tx.World().pois {
		if poi.Kind != kind || predicate != nil && !predicate(poi) {
			continue
		}
		dx, dy, dz := int64(candidate[0]-pos[0]), int64(candidate[1]-pos[1]), int64(candidate[2]-pos[2])
		distance := dx*dx + dy*dy + dz*dz
		if distance > limit {
			continue
		}
		if !found || distance < bestDistance || distance == bestDistance && posLess(candidate, best) {
			best, value, bestDistance, found = candidate, poi, distance, true
		}
	}
	return best, value, found
}

func posLess(a, b cube.Pos) bool {
	if a[0] != b[0] { return a[0] < b[0] }
	if a[1] != b[1] { return a[1] < b[1] }
	return a[2] < b[2]
}

// ClaimPOI atomically acquires one available POI ticket for owner.
func (tx *Tx) ClaimPOI(pos cube.Pos, owner uuid.UUID) bool {
	w := tx.World()
	p, ok := w.pois[pos]
	if !ok {
		return false
	}
	for _, existing := range p.Owners {
		if existing == owner {
			return true
		}
	}
	if !p.Vacant() {
		return false
	}
	p.Owners = append(p.Owners, owner)
	normalizePOIOwners(&p)
	w.pois[pos] = p
	return true
}

// ReleasePOI releases one POI ticket owned by owner.
func (tx *Tx) ReleasePOI(pos cube.Pos, owner uuid.UUID) bool {
	w := tx.World()
	p, ok := w.pois[pos]
	if !ok {
		return false
	}
	index := -1
	for i, existing := range p.Owners {
		if existing == owner {
			index = i
			break
		}
	}
	if index < 0 {
		return false
	}
	p.Owners = append(p.Owners[:index], p.Owners[index+1:]...)
	normalizePOIOwners(&p)
	w.pois[pos] = p
	return true
}

// IsVillage reports whether the position is within six sections of a source village section.
func (tx *Tx) IsVillage(pos cube.Pos) bool {
	return tx.VillageDistance(pos) <= 6
}

// VillageDistance reports the propagated village distance at the position's section.
// Sources are sections containing occupied POIs tagged as village.
func (tx *Tx) VillageDistance(pos cube.Pos) int {
	best := 7
	sx, sy, sz := pos[0]>>4, pos[1]>>4, pos[2]>>4
	for poiPos, poi := range tx.World().pois {
		if !isVillagePOI(poi) {
			continue
		}
		dx := absInt((poiPos[0] >> 4) - sx)
		dy := absInt((poiPos[1] >> 4) - sy)
		dz := absInt((poiPos[2] >> 4) - sz)
		distance := max(dx, dy, dz)
		if distance == 0 {
			return 0
		}
		if distance < best {
			best = distance
		}
	}
	return best
}

func isVillagePOI(p POI) bool {
	return p.HasOwner && (p.Kind == Home || p.Kind == Meeting || p.Kind == Workstation)
}


// VillageSections returns section origins containing occupied village POIs
// within the requested section radius, ordered by block position.
func (tx *Tx) VillageSections(pos cube.Pos, radius int) []cube.Pos {
	if radius < 0 {
		return nil
	}
	sections := make(map[cube.Pos]struct{})
	centerX, centerY, centerZ := pos[0]>>4, pos[1]>>4, pos[2]>>4
	for poiPos, poi := range tx.World().pois {
		if !isVillagePOI(poi) {
			continue
		}
		section := cube.Pos{(poiPos[0] >> 4) << 4, (poiPos[1] >> 4) << 4, (poiPos[2] >> 4) << 4}
		if absInt((section[0]>>4)-centerX) <= radius && absInt((section[1]>>4)-centerY) <= radius && absInt((section[2]>>4)-centerZ) <= radius {
			sections[section] = struct{}{}
		}
	}
	out := make([]cube.Pos, 0, len(sections))
	for section := range sections {
		out = append(out, section)
	}
	sort.Slice(out, func(i, j int) bool { return posLess(out[i], out[j]) })
	return out
}

func absInt(v int) int { if v < 0 { return -v }; return v }

// poiClass is the POI a block state is, if any.
type poiClass struct {
	poi POI
	ok  bool
}

// poiTypeOf returns the POI the block state with runtime ID rid is, using a
// table of every state built on first use.
func (w *World) poiTypeOf(rid uint32) (POI, bool) {
	w.poiClassesOnce.Do(func() {
		blocks := w.conf.Blocks.Blocks()
		w.poiClasses = make([]poiClass, len(blocks))
		for i, b := range blocks {
			w.poiClasses[i].poi, w.poiClasses[i].ok = poiType(b)
		}
	})
	if int(rid) >= len(w.poiClasses) {
		return POI{}, false
	}
	class := w.poiClasses[rid]
	return class.poi, class.ok
}

func (w *World) updatePOI(pos cube.Pos, b Block) {
	next, ok := w.poiTypeOf(w.conf.Blocks.BlockRuntimeID(b))
	if !ok {
		delete(w.pois, pos)
		return
	}
	if current, exists := w.pois[pos]; exists && current.Kind == next.Kind && current.Type == next.Type {
		next.Owners = append([]uuid.UUID(nil), current.Owners...)
	}
	normalizePOIOwners(&next)
	w.pois[pos] = next
}
func normalizePOIOwners(p *POI) {
	p.Owner = uuid.Nil
	p.HasOwner = len(p.Owners) != 0
	if p.HasOwner {
		p.Owner = p.Owners[0]
	}
}


// indexPOIChunk indexes the POIs of a column that became ready, keeping the
// owners of POIs that remain.
func (w *World) indexPOIChunk(pos ChunkPos, col *Column) {
	col.poisIndexed = true
	previous := make(map[cube.Pos]POI)
	for poiPos, poi := range w.pois {
		if int32(poiPos[0]>>4) == pos[0] && int32(poiPos[2]>>4) == pos[1] {
			previous[poiPos] = poi
			delete(w.pois, poiPos)
		}
	}
	for subIndex, sub := range col.Chunk.Sub() {
		baseY := int(col.Chunk.SubY(int16(subIndex)))
		minY, maxY := baseY, baseY+15
		if minY < w.ra.Min() {
			minY = w.ra.Min()
		}
		if maxY > w.ra.Max() {
			maxY = w.ra.Max()
		}
		if minY > maxY {
			continue
		}
		hasPOI := false
		for _, storage := range sub.Layers() {
			for paletteIndex := range storage.Palette().Len() {
				if _, ok := w.poiTypeOf(storage.Palette().Value(uint16(paletteIndex))); ok {
					hasPOI = true
					break
				}
			}
			if hasPOI {
				break
			}
		}
		if !hasPOI {
			continue
		}
		for z := range 16 {
			for x := range 16 {
				for y := minY; y <= maxY; y++ {
					poi, ok := w.poiTypeOf(col.Chunk.Block(uint8(x), int16(y), uint8(z), 0))
					if !ok {
						continue
					}
					blockPos := cube.Pos{int(pos[0])*16 + x, y, int(pos[1])*16 + z}
					if old, exists := previous[blockPos]; exists && old.Kind == poi.Kind && old.Type == poi.Type {
						poi.Owners = append([]uuid.UUID(nil), old.Owners...)
					}
					normalizePOIOwners(&poi)
					w.pois[blockPos] = poi
				}
			}
		}
	}
}

func (w *World) villageDataNamespace() string {
	return fmt.Sprintf("village-poi:%s", dimensionStorageKey(w.Dimension()))
}

func (w *World) loadVillagePOIs() {
	provider, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		return
	}
	data, err := provider.LoadWorldData(w.villageDataNamespace())
	if errors.Is(err, leveldb.ErrNotFound) {
		return
	}
	if err != nil {
		w.conf.Log.Error("load village POI data: " + err.Error())
		return
	}
	var records []villagePOI
	if err := json.Unmarshal(data, &records); err != nil {
		w.conf.Log.Error("decode village POI data: " + err.Error())
		return
	}
	for _, record := range records {
		poi := record.POI
		normalizePOIOwners(&poi)
		w.pois[record.Pos] = poi
	}
}

func (w *World) saveVillagePOIs() {
	provider, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		return
	}
	records := make([]villagePOI, 0, len(w.pois))
	for pos, poi := range w.pois {
		records = append(records, villagePOI{Pos: pos, POI: poi})
	}
	sort.Slice(records, func(i, j int) bool { return posLess(records[i].Pos, records[j].Pos) })
	data, err := json.Marshal(records)
	if err != nil {
		w.conf.Log.Error("encode village POI data: " + err.Error())
		return
	}
	if err := provider.SaveWorldData(w.villageDataNamespace(), data); err != nil {
		w.conf.Log.Error("save village POI data: " + err.Error())
	}
}
