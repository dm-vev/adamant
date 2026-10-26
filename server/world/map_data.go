package world

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// MapData is semantic map state shared by the renderer and a world provider.
// Colors contains 128x128 Java packed map-color indices; NativeColors retains
// Bedrock's exact RGBA byte array for lossless storage and packet delivery.
type MapData struct {
	ID int64
	JavaID int32
	HasJavaID bool
	ParentID int64
	Dimension int32
	CenterX, CenterZ int32
	Scale uint8
	Locked, Tracking, UnlimitedTracking, FullyExplored bool
	Colors []byte
	ColorsChanged, ForceFullTexture bool
	PixelPatch *MapPixelPatch
	NativeColors []byte
	NativeNBT map[string]any
	Decorations []MapDecoration
	TrackedObjects []MapTrackedObject
}

// MapDecoration is a map icon. X/Z are signed map-pixel offsets from the map
// center. The block location and key type preserve Bedrock's tracked key data.
type MapDecoration struct {
	Type, Rotation byte
	X, Z int8
	Label string
	BlockX, BlockY, BlockZ int32
	KeyType int32
	NativeNBT map[string]any
}
// MapTrackedObject is transient Bedrock packet state for a carried entity or frame.
type MapTrackedObject struct {
	Entity *EntityHandle
	Block  *cube.Pos
}
// MapPixelPatch contains a rectangular Java packed-color update.
type MapPixelPatch struct {
	X, Y, Width, Height int
	Colors []byte
}

// MapFrameInfo describes the map stack displayed by a loaded item frame.
type MapFrameInfo struct {
	NativeID int64
	HasNativeID bool
	JavaID int32
	HasJavaID bool
}

type mapFrameProvider interface {
	MapFrameData() (MapFrameInfo, bool)
}

// ForEachMapFrame visits map-bearing block entities in currently loaded chunks.
func (tx *Tx) ForEachMapFrame(fn func(cube.Pos, MapFrameInfo)) {
	if fn == nil { return }
	for chunkPos, column := range tx.w.chunks {
		if !column.Ready() { continue }
		if r := tx.region; r != nil && (!column.settled || !r.owns(tx.w, chunkPos)) {
			// A region only visits the frames of its own columns.
			continue
		}
		for pos, block := range column.BlockEntities {
			frame, ok := block.(mapFrameProvider)
			if !ok { continue }
			data, ok := frame.MapFrameData()
			if ok { fn(pos, data) }
		}
	}
}

// MapDataProvider is the optional world-provider capability for persistent maps.
// Implementations preserve signed IDs and return storage errors unchanged.
type MapDataProvider interface {
	LoadMap(id int64) (MapData, error)
	StoreMap(MapData) error
	AllocateMapID() (int64, error)
	JavaMapID(nativeID int64) (int32, error)
	NativeMapID(javaID int32) (int64, error)
}

var ErrMapDataUnavailable = errors.New("world provider does not support map data")
var ErrInvalidMapData = errors.New("invalid map data")

const mapPixelCount = 128 * 128

// StoreMapData persists a complete map state, expanding Java packed colors to
// Bedrock RGBA only when no exact native buffer is available.
func (w *World) StoreMapData(data MapData) error {
	if w == nil || w.conf.Provider == nil { return ErrMapDataUnavailable }
	p, ok := w.conf.Provider.(MapDataProvider)
	if !ok { return ErrMapDataUnavailable }
	if data.Scale > 4 { return fmt.Errorf("%w: map scale %d", ErrInvalidMapData, data.Scale) }
	if len(data.Colors) != mapPixelCount && len(data.NativeColors) == mapPixelCount*4 {
		data.Colors = javaColoursFromNative(data.NativeColors)
	}
	if len(data.Colors) != mapPixelCount { return fmt.Errorf("%w: map %d has %d Java pixels", ErrInvalidMapData, data.ID, len(data.Colors)) }
	if len(data.NativeColors) != mapPixelCount*4 {
		data.NativeColors = make([]byte, mapPixelCount*4)
		for i, packed := range data.Colors {
			c := MapColourRGBA(packed)
			data.NativeColors[i*4], data.NativeColors[i*4+1], data.NativeColors[i*4+2], data.NativeColors[i*4+3] = c.R, c.G, c.B, c.A
		}
	}
	return p.StoreMap(data)
}

func javaColoursFromNative(native []byte) []byte {
	colors := make([]byte, mapPixelCount)
	converted := make(map[uint32]byte, 256)
	for i := range colors {
		if native[i*4+3] == 0 {
			continue
		}
		r, g, b := native[i*4], native[i*4+1], native[i*4+2]
		rgb := uint32(r)<<16 | uint32(g)<<8 | uint32(b)
		if packed, ok := converted[rgb]; ok { colors[i] = packed; continue }
		bestDistance := uint64(^uint64(0))
		var best byte
		for id := 1; id < len(javaMapColours); id++ {
			for brightness := byte(0); brightness < 4; brightness++ {
				c := MapColourRGBA(byte(id<<2) | brightness)
				dr, dg, db := int64(r)-int64(c.R), int64(g)-int64(c.G), int64(b)-int64(c.B)
				distance := uint64(dr*dr + dg*dg + db*db)
				if distance < bestDistance { bestDistance, best = distance, byte(id<<2)|brightness }
			}
		}
		converted[rgb], colors[i] = best, best
	}
	return colors
}

// MapColourRGBA expands a Java packed map color to the RGBA color used by map packets.
func MapColourRGBA(packed byte) color.RGBA {
	id, brightness := packed>>2, packed&3
	if id == 0 || int(id) >= len(javaMapColours) { return color.RGBA{} }
	base := javaMapColours[id]
	modifier := [...]uint32{180, 220, 255, 135}[brightness]
	return color.RGBA{R:uint8((base>>16&0xff)*modifier/255), G:uint8((base>>8&0xff)*modifier/255), B:uint8((base&0xff)*modifier/255), A:0xff}
}
// Java MapColor IDs/RGB values from the mapped Java 26.3 MapColor registry.
var javaMapColours = [...]uint32{
	0, 8368696, 16247203, 13092807, 16711680, 10526975, 10987431, 31744,
	16777215, 10791096, 9923917, 7368816, 4210943, 9402184, 16776437,
	14188339, 11685080, 6724056, 15066419, 8375321, 15892389, 5000268,
	10066329, 5013401, 8339378, 3361970, 6704179, 6717235, 10040115,
	1644825, 16445005, 6085589, 4882687, 55610, 8476209, 7340544,
	13742497, 10441252, 9787244, 7367818, 12223780, 6780213, 10505550,
	3746083, 8874850, 5725276, 8014168, 4996700, 4993571, 5001770,
	9321518, 2430480, 12398641, 9715553, 6035741, 1474182, 3837580,
	5647422, 1356933, 6579300, 14200723, 8365974,
}

// MapData loads a map by native signed ID and returns independently owned buffers.
func (w *World) MapData(id int64) (MapData, error) {
	if w == nil || w.conf.Provider == nil { return MapData{}, ErrMapDataUnavailable }
	p, ok := w.conf.Provider.(MapDataProvider)
	if !ok { return MapData{}, ErrMapDataUnavailable }
	data, err := p.LoadMap(id)
	if err != nil { return MapData{}, err }
	javaID, err := p.JavaMapID(id)
	if err != nil { return MapData{}, err }
	data.JavaID, data.HasJavaID = javaID, true
	if data.ID != id || (len(data.Colors) != mapPixelCount && len(data.NativeColors) != mapPixelCount*4) {
		return MapData{}, fmt.Errorf("%w: map %d has invalid ID or pixel buffers", ErrInvalidMapData, id)
	}
	if len(data.Colors) != mapPixelCount { data.Colors = javaColoursFromNative(data.NativeColors) }
	if len(data.NativeColors) != mapPixelCount*4 {
		data.NativeColors = make([]byte, mapPixelCount*4)
		for i, packed := range data.Colors {
			c := MapColourRGBA(packed)
			data.NativeColors[i*4], data.NativeColors[i*4+1], data.NativeColors[i*4+2], data.NativeColors[i*4+3] = c.R, c.G, c.B, c.A
		}
	}
	data.Colors = append([]byte(nil), data.Colors...)
	data.NativeColors = append([]byte(nil), data.NativeColors...)
	data.Decorations = append([]MapDecoration(nil), data.Decorations...)
	return data, nil
}

// MapIDByJavaID resolves Java's item map_id to its persistent Bedrock map ID.
func (w *World) MapIDByJavaID(javaID int32) (int64, error) {
	if w == nil || w.conf.Provider == nil { return 0, ErrMapDataUnavailable }
	p, ok := w.conf.Provider.(MapDataProvider)
	if !ok { return 0, ErrMapDataUnavailable }
	return p.NativeMapID(javaID)
}

// MapDataByJavaID resolves and loads a map referenced by Java's int32 map_id.
func (w *World) MapDataByJavaID(javaID int32) (MapData, error) {
	id, err := w.MapIDByJavaID(javaID)
	if err != nil { return MapData{}, err }
	return w.MapData(id)
}

// CreateMap allocates and persists a Java-grid-aligned map outside a transaction.
func (w *World) CreateMap(pos cube.Pos, scale uint8) (MapData, error) {
	var data MapData
	var createErr error
	task := w.Do(func(tx *Tx) { data, createErr = tx.CreateMap(pos, scale) })
	if err := task.Wait(nil); err != nil { return MapData{}, err }
	return data, createErr
}

// CreateMap allocates and persists a map from within an existing world transaction.
func (tx *Tx) CreateMap(pos cube.Pos, scale uint8) (MapData, error) {
	tx.lockShared()
	defer tx.unlockShared()
	w := tx.World()
	if w == nil || w.conf.Provider == nil { return MapData{}, ErrMapDataUnavailable }
	if scale > 4 { return MapData{}, fmt.Errorf("%w: scale %d exceeds Java maximum 4", ErrInvalidMapData, scale) }
	dimensionID, ok := DimensionID(w.Dimension())
	if !ok { return MapData{}, fmt.Errorf("map dimension %v has no registered id", w.Dimension()) }
	factor := 1 << scale
	size := 128 * factor
	centerX := floorDiv(pos[0]+64, size)*size + size/2 - 64
	centerZ := floorDiv(pos[2]+64, size)*size + size/2 - 64
	data := MapData{Dimension:int32(dimensionID), CenterX:int32(centerX), CenterZ:int32(centerZ), Scale:scale, Tracking:true, Colors:make([]byte, mapPixelCount)}
	// Java creates a blank saved map; exploration and biome-preview rendering fill pixels later.
	return w.storeNewMap(data)
}

// storeNewMap assigns both identities before publishing an independent map.
func (w *World) storeNewMap(data MapData) (MapData, error) {
	if w == nil || w.conf.Provider == nil { return MapData{}, ErrMapDataUnavailable }
	p, ok := w.conf.Provider.(MapDataProvider)
	if !ok { return MapData{}, ErrMapDataUnavailable }
	id, err := p.AllocateMapID()
	if err != nil { return MapData{}, err }
	data.ID = id
	data.JavaID, err = p.JavaMapID(id)
	if err != nil { return MapData{}, err }
	data.HasJavaID = true
	if err := w.StoreMapData(data); err != nil { return MapData{}, err }
	return data, nil
}

// ScaleMap creates a newly rendered map at the next coarser scale.
func (w *World) ScaleMap(id int64) (MapData, error) {
	if w == nil {
		return MapData{}, ErrMapDataUnavailable
	}
	var data MapData
	var scaleErr error
	task := w.Do(func(tx *Tx) { data, scaleErr = tx.ScaleMap(id) })
	if err := task.Wait(nil); err != nil {
		return MapData{}, err
	}
	return data, scaleErr
}

// LockMap stores a locked, independent copy.
func (w *World) LockMap(id int64) (MapData, error) {
	if w == nil {
		return MapData{}, ErrMapDataUnavailable
	}
	var data MapData
	var lockErr error
	task := w.Do(func(tx *Tx) { data, lockErr = tx.LockMap(id) })
	if err := task.Wait(nil); err != nil {
		return MapData{}, err
	}
	return data, lockErr
}

// CloneMap creates a persisted independent copy with the explored pixels/decorations.
func (w *World) CloneMap(id int64) (MapData, error) {
	data, err := w.MapData(id)
	if err != nil { return MapData{}, err }
	data.ParentID = data.ID
	return w.storeNewMap(data)
}
// ScaleMap creates a new map at the next coarser scale within a world transaction.
func (tx *Tx) ScaleMap(id int64) (MapData, error) {
	tx.lockShared()
	defer tx.unlockShared()
	source, err := tx.World().MapData(id)
	if err != nil {
		return MapData{}, err
	}
	if source.Locked || source.Scale >= 4 {
		return MapData{}, fmt.Errorf("%w: map %d cannot be scaled", ErrInvalidMapData, id)
	}
	scale := source.Scale + 1
	size := 128 << scale
	child := MapData{
		ParentID: source.ID, Dimension: source.Dimension, Scale: scale,
		CenterX: int32(floorDiv(int(source.CenterX)+64, size)*size + size/2 - 64),
		CenterZ: int32(floorDiv(int(source.CenterZ)+64, size)*size + size/2 - 64),
		Tracking: source.Tracking, UnlimitedTracking: source.UnlimitedTracking,
		Colors: make([]byte, mapPixelCount),
	}
	return tx.World().storeNewMap(child)
}

// LockMap stores a locked, independent copy within a world transaction.
func (tx *Tx) LockMap(id int64) (MapData, error) {
	tx.lockShared()
	defer tx.unlockShared()
	w := tx.World()
	data, err := w.MapData(id)
	if err != nil {
		return MapData{}, err
	}
	if data.Locked {
		return MapData{}, fmt.Errorf("%w: map %d is already locked", ErrInvalidMapData, id)
	}
	data.ParentID = data.ID
	data.Locked = true
	return w.storeNewMap(data)
}

// AddMapDecoration adds or replaces a fixed icon at the same type and pixel location.
func (w *World) AddMapDecoration(id int64, decoration MapDecoration) error {
	data, err := w.MapData(id)
	if err != nil { return err }
	return addMapDecoration(w, data, decoration)
}

// AddMapDecoration adds a fixed icon to an existing map inside a world transaction.
func (tx *Tx) AddMapDecoration(id int64, decoration MapDecoration) error {
	tx.lockShared()
	defer tx.unlockShared()
	data, err := tx.World().MapData(id)
	if err != nil { return err }
	return addMapDecoration(tx.World(), data, decoration)
}

func addMapDecoration(w *World, data MapData, decoration MapDecoration) error {
	for i := range data.Decorations {
		old := data.Decorations[i]
		if old.Type == decoration.Type && old.X == decoration.X && old.Z == decoration.Z {
			data.Decorations[i] = decoration
			return w.StoreMapData(data)
		}
	}
	data.Decorations = append(data.Decorations, decoration)
	return w.StoreMapData(data)
}

// AddMapDecorationAt persists a Java map icon and its native key at an absolute target position.
func (tx *Tx) AddMapDecorationAt(id int64, typ, rotation byte, keyType int32, target cube.Pos, label string) error {
	tx.lockShared()
	defer tx.unlockShared()
	data, err := tx.World().MapData(id)
	if err != nil { return err }
	factor := 1 << data.Scale
	pixelX := mapDecorationCoordinate((float64(target[0])+0.5-float64(data.CenterX))/float64(factor))
	pixelZ := mapDecorationCoordinate((float64(target[2])+0.5-float64(data.CenterZ))/float64(factor))
	return addMapDecoration(tx.World(), data, MapDecoration{
		Type:typ, Rotation:rotation, X:pixelX, Z:pixelZ, Label:label,
		BlockX:int32(target[0]), BlockY:int32(target[1]), BlockZ:int32(target[2]), KeyType:keyType,
	})
}

func mapDecorationCoordinate(delta float64) int8 {
	if delta <= -63 { return -128 }
	if delta >= 63 { return 127 }
	return int8(delta*2 + 0.5)
}

// UpdateMap advances Java's player-centred map raster by the current scan stripe.
func (w *World) UpdateMap(id int64, player cube.Pos) (MapData, error) {
	var data MapData
	var updateErr error
	task := w.Do(func(tx *Tx) { data, updateErr = tx.UpdateMap(id, player) })
	if err := task.Wait(nil); err != nil { return MapData{}, err }
	return data, updateErr
}

// UpdateMap explores the Java map renderer's current player-centered scan stripe.
func (tx *Tx) UpdateMap(id int64, player cube.Pos) (MapData, error) {
	w := tx.World()
	tx.lockShared()
	defer tx.unlockShared()
	data, err := w.MapData(id)
	if err != nil { return MapData{}, err }
	if data.Locked || !data.Tracking { return data, nil }
	dimensionID, ok := DimensionID(w.Dimension())
	if !ok || data.Dimension != int32(dimensionID) { return data, nil }
	factor := 1 << data.Scale
	playerX := (player[0]-int(data.CenterX))/factor + 64
	playerZ := (player[2]-int(data.CenterZ))/factor + 64
	radius := 128 / factor
	if w.Dimension() == Nether { radius /= 2 }
	step := int(tx.CurrentTick()+1) & 15
	startX, endX := playerX-radius+1, playerX+radius
	startZ, endZ := playerZ-radius-1, playerZ+radius
	changed := false
	patchMinX, patchMinY, patchMaxX, patchMaxY := 128, 128, -1, -1
	for imgX := startX; imgX < endX; imgX++ {
		if imgX&15 != step { continue }
		previousHeight := 0.0
		for imgZ := startZ; imgZ < endZ; imgZ++ {
			if imgX < 0 || imgZ < -1 || imgX >= 128 || imgZ >= 128 { continue }
			dx, dz := imgX-playerX, imgZ-playerZ
			distanceSquared := dx*dx + dz*dz
			ditherBlack := distanceSquared > (radius-2)*(radius-2)
			sampleX := (int(data.CenterX)/factor + imgX - 64) * factor
			sampleZ := (int(data.CenterZ)/factor + imgZ - 64) * factor
			if !tx.mapSampleReadable(sampleX, sampleZ, factor) {
				// A region leaves pixels of columns it does not own as they
				// are.
				continue
			}
			pixel, averageHeight, err := sampleMapPixelShaded(tx, sampleX, sampleZ, factor, imgX, imgZ, previousHeight)
			if err != nil { return MapData{}, err }
			previousHeight = averageHeight
			if imgZ < 0 || distanceSquared >= radius*radius || ditherBlack && (imgX+imgZ)&1 == 0 { continue }
			index := imgZ*128 + imgX
			if data.Colors[index] != pixel {
				data.Colors[index], changed = pixel, true
				if len(data.NativeColors) == mapPixelCount*4 {
					c := MapColourRGBA(pixel)
					data.NativeColors[index*4], data.NativeColors[index*4+1] = c.R, c.G
					data.NativeColors[index*4+2], data.NativeColors[index*4+3] = c.B, c.A
				}
				patchMinX, patchMinY = min(patchMinX, imgX), min(patchMinY, imgZ)
				patchMaxX, patchMaxY = max(patchMaxX, imgX), max(patchMaxY, imgZ)
			}
		}
	}
	if changed {
		if err := w.StoreMapData(data); err != nil { return MapData{}, err }
		width, height := patchMaxX-patchMinX+1, patchMaxY-patchMinY+1
		patch := MapPixelPatch{X:patchMinX, Y:patchMinY, Width:width, Height:height, Colors:make([]byte,width*height)}
		for row := range height {
			copy(patch.Colors[row*width:(row+1)*width], data.Colors[(patchMinY+row)*128+patchMinX:(patchMinY+row)*128+patchMaxX+1])
		}
		data.PixelPatch = &patch
	}
	data.ColorsChanged = changed
	return data, nil
}
// RenderMapBiomePreview applies Java's WATER_ON_MAP_OUTLINES preview to a new explorer map.
func (tx *Tx) RenderMapBiomePreview(id int64) (MapData, error) {
	tx.lockShared()
	defer tx.unlockShared()
	w := tx.World()
	data, err := w.MapData(id)
	if err != nil { return MapData{}, err }
	dimensionID, ok := DimensionID(w.Dimension())
	if !ok || data.Dimension != int32(dimensionID) { return data, nil }
	factor := 1 << data.Scale
	var watery [mapPixelCount]bool
	for z := range 128 {
		for x := range 128 {
			worldX := (int(data.CenterX)/factor + x - 64) * factor
			worldZ := (int(data.CenterZ)/factor + z - 64) * factor
			watery[z*128+x] = biomeWateryMapOutline(tx.previewBiome(cube.Pos{worldX, 63, worldZ}))
		}
	}
	changed := false
	for x := 1; x < 127; x++ {
		for z := 1; z < 127; z++ {
			waterCount := 0
			for dx := -1; dx <= 1; dx++ {
				for dz := -1; dz <= 1; dz++ {
					if (dx != 0 || dz != 0) && watery[(z+dz)*128+x+dx] { waterCount++ }
				}
			}
			brightness, colorID := byte(3), byte(0)
			if watery[z*128+x] {
				colorID = 15 // MapColor.COLOR_ORANGE
				if waterCount > 7 && z%2 == 0 {
					switch (x + int(float32(math.Sin(float64(float32(z)))*7.0))) / 8 % 5 {
					case 0, 4: brightness = 0
					case 1, 3: brightness = 1
					case 2: brightness = 2
					}
				} else if waterCount > 7 {
					colorID = 0
				} else if waterCount > 5 {
					brightness = 1
				} else if waterCount > 3 {
					brightness = 0
				} else if waterCount > 1 {
					brightness = 0
				}
			} else if waterCount > 0 {
				colorID = 26 // MapColor.COLOR_BROWN
				if waterCount > 3 { brightness = 1 }
			}
			if colorID != 0 {
				index, pixel := z*128+x, colorID<<2|brightness
				if data.Colors[index] != pixel {
					data.Colors[index], changed = pixel, true
					if len(data.NativeColors) == mapPixelCount*4 {
						c := MapColourRGBA(pixel)
						data.NativeColors[index*4], data.NativeColors[index*4+1] = c.R, c.G
						data.NativeColors[index*4+2], data.NativeColors[index*4+3] = c.B, c.A
					}
				}
			}
		}
	}
	if changed {
		if err := w.StoreMapData(data); err != nil { return MapData{}, err }
	}
	data.ColorsChanged = changed
	return data, nil
}

func (w *World) RenderMapBiomePreview(id int64) (MapData, error) {
	var data MapData
	var updateErr error
	task := w.Do(func(tx *Tx) { data, updateErr = tx.RenderMapBiomePreview(id) })
	if err := task.Wait(nil); err != nil { return MapData{}, err }
	return data, updateErr
}

func biomeWateryMapOutline(name string) bool {
	switch name {
	case "ocean", "frozen_ocean", "cold_ocean", "lukewarm_ocean", "warm_ocean",
		"deep_ocean", "deep_cold_ocean", "deep_frozen_ocean", "deep_lukewarm_ocean",
		"river", "frozen_river", "swamp", "mangrove_swamp":
		return true
	default:
		return false
	}
}

func floorDiv(a, b int) int {
	q, r := a/b, a%b
	if r != 0 && (r < 0) != (b < 0) { q-- }
	return q
}

type mapColourBlock interface { MapColourIndex() (byte, bool) }

func sampleMapPixelShaded(tx *Tx, x, z, scale, imgX, imgZ int, previousHeight float64) (byte, float64, error) {
	if tx.World().Dimension() == Nether {
		noise := int32(x + z*231871)
		noise = noise*noise*31287121 + noise*11
		colorID := byte(11)
		if uint32(noise)>>20&1 == 0 { colorID = 10 }
		return colorID<<2 | 1, 100, nil
	}
	var colorCounts [256]int
	var colorOrder [256]byte
	orderLength, waterDepth := 0, 0
	averageHeight := 0.0
	area := scale * scale
	for dx := range scale {
		for dz := range scale {
			sampleX, sampleZ := x+dx, z+dz
			columnY := tx.HighestBlock(sampleX, sampleZ) + 1
			if columnY <= tx.Range().Min() {
				colorID := byte(11) // Java substitutes bedrock below the world surface.
				if colorCounts[colorID] == 0 { colorOrder[orderLength], orderLength = colorID, orderLength+1 }
				colorCounts[colorID]++
				averageHeight += float64(tx.Range().Min()) / float64(area)
				continue
			}
			columnY = min(columnY-1, tx.Range().Max())
			for ; columnY >= tx.Range().Min(); columnY-- {
				block := tx.Block(cube.Pos{sampleX, columnY, sampleZ})
				colorID, ok := javaMapColorID(block)
				if !ok { return 0, 0, fmt.Errorf("map renderer has no mapped Java color for %T (%s)", block, blockName(block)) }
				if colorID == 0 { continue }
				if colorCounts[colorID] == 0 { colorOrder[orderLength], orderLength = colorID, orderLength+1 }
				colorCounts[colorID]++
				if _, fluid := tx.Liquid(cube.Pos{sampleX, columnY, sampleZ}); fluid {
					for belowY := columnY - 1; belowY > tx.Range().Min(); belowY-- {
						if _, fluid := tx.Liquid(cube.Pos{sampleX, belowY, sampleZ}); !fluid { break }
						waterDepth++
					}
				}
				break
			}
			if columnY < tx.Range().Min() { columnY = tx.Range().Min() }
			averageHeight += float64(columnY) / float64(area)
		}
	}
	if orderLength == 0 { return 0, averageHeight, nil }
	best, count := colorOrder[0], colorCounts[colorOrder[0]]
	for _, color := range colorOrder[1:orderLength] {
		if colorCounts[color] > count { best, count = color, colorCounts[color] }
	}
	waterDepth /= area
	brightness := 1
	if best == 12 {
		diff := float64(waterDepth)*0.1 + float64((imgX+imgZ)&1)*0.2
		if diff < 0.5 { brightness = 2 } else if diff > 0.9 { brightness = 0 }
	} else {
		diff := (averageHeight-previousHeight)*4.0/float64(scale+4) + (float64((imgX+imgZ)&1)-0.5)*0.4
		if diff > 0.6 { brightness = 2 } else if diff < -0.6 { brightness = 0 }
	}
	return best<<2 | byte(brightness), averageHeight, nil
}


func javaMapColorID(block Block) (byte, bool) {
	if mapped, ok := block.(mapColourBlock); ok {
		if packed, found := mapped.MapColourIndex(); found { return packed >> 2, true }
	}
	name, properties := block.EncodeBlock()
	definition, ok := javaMapColorsByBlock[name]
	if !ok { return 0, false }
	color := definition.base
	if definition.axis {
		if properties["pillar_axis"] == "y" { color = definition.top } else { color = definition.side }
	}
	if definition.wheat {
		if age, ok := mapBlockIntProperty(properties, "age"); ok && age >= 6 { color = 18 } else { color = 7 }
	}
	if definition.bed && properties["part"] != "foot" { color = 8 }
	if definition.whiteSpecial { color = 8 }
	return color, true
}

func mapBlockIntProperty(properties map[string]any, key string) (int, bool) {
	switch value := properties[key].(type) {
	case int: return value, true
	case int8: return int(value), true
	case int16: return int(value), true
	case int32: return int(value), true
	case int64: return int(value), true
	case uint8: return int(value), true
	case uint16: return int(value), true
	case uint32: return int(value), true
	case uint64: return int(value), true
	case string:
		n, err := strconv.Atoi(value)
		return n, err == nil
	}
	return 0, false
}

func blockName(b Block) string { name, _ := b.EncodeBlock(); return name }

// mapSampleReadable reports whether the blocks a map pixel is drawn from,
// factor by factor blocks from x and z, may be read: in a region phase, only
// if the region owns their settled columns.
func (tx *Tx) mapSampleReadable(x, z, factor int) bool {
	if tx.region == nil {
		return true
	}
	lo, hi := chunkPosFromBlockPos(cube.Pos{x, 0, z}), chunkPosFromBlockPos(cube.Pos{x + factor - 1, 0, z + factor - 1})
	for cx := lo[0]; cx <= hi[0]; cx++ {
		for cz := lo[1]; cz <= hi[1]; cz++ {
			if tx.region.column(tx.w, ChunkPos{cx, cz}) == nil {
				return false
			}
		}
	}
	return true
}

// previewBiome returns the name of the biome at pos for a biome preview map.
// A region reads the biomes of columns it does not own from the generator
// instead, as Java always does, or none if the generator cannot tell.
func (tx *Tx) previewBiome(pos cube.Pos) string {
	if tx.region == nil || tx.region.column(tx.w, chunkPosFromBlockPos(pos)) != nil {
		return tx.Biome(pos).String()
	}
	provider, ok := tx.w.conf.Generator.(JavaBiomeAtProvider)
	if !ok {
		return ""
	}
	return strings.TrimPrefix(provider.JavaBiomeAt(int32(pos[0]), int32(pos[1]), int32(pos[2])), "minecraft:")
}
