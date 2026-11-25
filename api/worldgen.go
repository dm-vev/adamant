package api

import (
	"image/color"
	"reflect"
	"slices"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/end"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/nether"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
)

// WorldGenRandom provides deterministic per-chunk random numbers inside
// WorldGenHooks.OnDecorate without exposing internal generator packages.
type WorldGenRandom interface {
	NextInt(bound int32) int32
	NextInt32() int32
	NextLong() int64
	NextFloat() float32
	NextDouble() float64
	NextBoolean() bool
	NextGaussian() float64
	Consume(count int)
}

// WorldGenHooks customizes Overworld, Nether, or End generation stages cleanly
// from external code. Non-nil hooks must be safe for concurrent invocation
// across generation workers and deterministic for a given world seed and chunk
// position.
type WorldGenHooks struct {
	// OnBiome overrides the Bedrock biome ID at world coordinates (x, y, z).
	OnBiome func(x, y, z int, defaultBiome uint8) uint8
	// OnBiomes runs once per generated chunk after base biomes are populated.
	OnBiomes func(pos world.ChunkPos, c *chunk.Chunk)
	// OnTerrain runs after base terrain, surface, and carving complete, before
	// features and structures decorate c.
	OnTerrain func(pos world.ChunkPos, c *chunk.Chunk)
	// OnDecorate runs after structures and features finish decorating c with a
	// deterministic per-chunk random stream.
	OnDecorate func(pos world.ChunkPos, c *chunk.Chunk, rng WorldGenRandom)
}

func bindOnDecorate(nativePtr any, onDecorate func(pos world.ChunkPos, c *chunk.Chunk, rng WorldGenRandom)) {
	if onDecorate == nil {
		return
	}
	field := reflect.ValueOf(nativePtr).Elem().FieldByName("OnDecorate")
	fn := reflect.MakeFunc(field.Type(), func(args []reflect.Value) []reflect.Value {
		pos := args[0].Interface().(world.ChunkPos)
		c := args[1].Interface().(*chunk.Chunk)
		var rng WorldGenRandom
		if !args[2].IsNil() {
			rng, _ = args[2].Interface().(WorldGenRandom)
		}
		onDecorate(pos, c, rng)
		return nil
	})
	field.Set(fn)
}

// AttachOverworldHooks installs hooks on g. Passing zero WorldGenHooks clears
// previously installed hooks and restores vanilla generation fast paths.
func AttachOverworldHooks(g *overworld.Overworld, hooks WorldGenHooks) {
	if g == nil {
		return
	}
	native := overworld.Hooks{
		OnBiome:   hooks.OnBiome,
		OnBiomes:  hooks.OnBiomes,
		OnTerrain: hooks.OnTerrain,
	}
	bindOnDecorate(&native, hooks.OnDecorate)
	g.SetHooks(native)
}

// ClearOverworldHooks removes any custom generation hooks attached to g.
func ClearOverworldHooks(g *overworld.Overworld) {
	if g == nil {
		return
	}
	g.SetHooks(overworld.Hooks{})
}

// AttachNetherHooks installs hooks on a Nether generator g. Passing zero
// WorldGenHooks clears previously installed hooks and restores vanilla fast paths.
func AttachNetherHooks(g *nether.Nether, hooks WorldGenHooks) {
	if g == nil {
		return
	}
	native := nether.Hooks{
		OnBiome:   hooks.OnBiome,
		OnBiomes:  hooks.OnBiomes,
		OnTerrain: hooks.OnTerrain,
	}
	bindOnDecorate(&native, hooks.OnDecorate)
	g.SetHooks(native)
}

// ClearNetherHooks removes any custom generation hooks attached to g.
func ClearNetherHooks(g *nether.Nether) {
	if g == nil {
		return
	}
	g.SetHooks(nether.Hooks{})
}

// AttachEndHooks installs hooks on an End generator g. Passing zero
// WorldGenHooks clears previously installed hooks and restores vanilla fast paths.
func AttachEndHooks(g *end.End, hooks WorldGenHooks) {
	if g == nil {
		return
	}
	native := end.Hooks{
		OnBiome:   hooks.OnBiome,
		OnBiomes:  hooks.OnBiomes,
		OnTerrain: hooks.OnTerrain,
	}
	bindOnDecorate(&native, hooks.OnDecorate)
	g.SetHooks(native)
}

// ClearEndHooks removes any custom generation hooks attached to g.
func ClearEndHooks(g *end.End) {
	if g == nil {
		return
	}
	g.SetHooks(end.Hooks{})
}

// CustomBiomeSpec declaratively configures a custom world.Biome registered via
// NewCustomBiome and RegisterBiome.
type CustomBiomeSpec struct {
	ID               int
	Name             string
	Temperature      float64
	Rainfall         float64
	Depth            float64
	Scale            float64
	HasPrecipitation bool
	WaterColour      color.RGBA
	Tags             []string
}

type customBiome struct {
	id               int
	name             string
	temperature      float64
	rainfall         float64
	depth            float64
	scale            float64
	hasPrecipitation bool
	waterColour      color.RGBA
	tags             []string
}

func (b customBiome) Temperature() float64    { return b.temperature }
func (b customBiome) Rainfall() float64       { return b.rainfall }
func (b customBiome) Depth() float64          { return b.depth }
func (b customBiome) Scale() float64          { return b.scale }
func (b customBiome) HasPrecipitation() bool  { return b.hasPrecipitation }
func (b customBiome) WaterColour() color.RGBA { return b.waterColour }
func (b customBiome) Tags() []string          { return slices.Clone(b.tags) }
func (b customBiome) String() string          { return b.name }
func (b customBiome) EncodeBiome() int        { return b.id }

// NewCustomBiome constructs a world.Biome from spec.
func NewCustomBiome(spec CustomBiomeSpec) world.Biome {
	if spec.Name == "" {
		panic("api: CustomBiomeSpec.Name must not be empty")
	}
	water := spec.WaterColour
	if water == (color.RGBA{}) {
		water = color.RGBA{R: 0x44, G: 0xaf, B: 0xf5, A: 0xff}
	}
	return customBiome{
		id:               spec.ID,
		name:             spec.Name,
		temperature:      spec.Temperature,
		rainfall:         spec.Rainfall,
		depth:            spec.Depth,
		scale:            spec.Scale,
		hasPrecipitation: spec.HasPrecipitation,
		waterColour:      water,
		tags:             slices.Clone(spec.Tags),
	}
}

// RegisterBiome registers b in the global world biome registry.
func RegisterBiome(b world.Biome) {
	world.RegisterBiome(b)
}

// BiomeByID looks up a registered biome by numeric Bedrock biome ID.
func BiomeByID(id int) (world.Biome, bool) {
	return world.BiomeByID(id)
}

// BiomeByName looks up a registered biome by name.
func BiomeByName(name string) (world.Biome, bool) {
	return world.BiomeByName(name)
}

// StructureRotation specifies clockwise 90-degree increments around the Y axis.
type StructureRotation uint8

const (
	Rotate0   StructureRotation = 0
	Rotate90  StructureRotation = 1
	Rotate180 StructureRotation = 2
	Rotate270 StructureRotation = 3
)

// StructureBlock describes one block placement relative to a StructureTemplate origin.
type StructureBlock struct {
	Offset cube.Pos
	Block  world.Block
	RID    uint32
	UseRID bool
}

// StructureTemplate is a reusable, immutable multi-block template that can be
// stamped into a live *world.Tx or clipped safely into a chunk during worldgen
// decoration hooks.
type StructureTemplate struct {
	name   string
	size   cube.Pos
	blocks []StructureBlock
}

// StructurePlaceOpts configures how a StructureTemplate is transformed and placed.
type StructurePlaceOpts struct {
	Rotation  StructureRotation
	MirrorX   bool
	MirrorZ   bool
	IgnoreAir bool
	SetOpts   *world.SetOpts
}

// NewStructure builds a reusable StructureTemplate from name and block entries.
func NewStructure(name string, blocks ...StructureBlock) *StructureTemplate {
	cloned := slices.Clone(blocks)
	var maxPos cube.Pos
	for _, b := range cloned {
		if b.Offset[0] >= maxPos[0] {
			maxPos[0] = b.Offset[0] + 1
		}
		if b.Offset[1] >= maxPos[1] {
			maxPos[1] = b.Offset[1] + 1
		}
		if b.Offset[2] >= maxPos[2] {
			maxPos[2] = b.Offset[2] + 1
		}
	}
	return &StructureTemplate{name: name, size: maxPos, blocks: cloned}
}

// Name returns the template name.
func (s *StructureTemplate) Name() string {
	if s == nil {
		return ""
	}
	return s.name
}

// Size returns the bounding dimensions (width, height, depth) of the template.
func (s *StructureTemplate) Size() cube.Pos {
	if s == nil {
		return cube.Pos{}
	}
	return s.size
}

// Blocks returns a copy of the template's block entries.
func (s *StructureTemplate) Blocks() []StructureBlock {
	if s == nil {
		return nil
	}
	return slices.Clone(s.blocks)
}

func (s *StructureTemplate) transformOffset(off cube.Pos, opts StructurePlaceOpts) cube.Pos {
	x, y, z := off[0], off[1], off[2]
	maxX := max(s.size[0]-1, 0)
	maxZ := max(s.size[2]-1, 0)
	if opts.MirrorX {
		x = maxX - x
	}
	if opts.MirrorZ {
		z = maxZ - z
	}
	switch opts.Rotation % 4 {
	case Rotate90:
		return cube.Pos{maxZ - z, y, x}
	case Rotate180:
		return cube.Pos{maxX - x, y, maxZ - z}
	case Rotate270:
		return cube.Pos{z, y, maxX - x}
	default:
		return cube.Pos{x, y, z}
	}
}

// PlaceStructure stamps tmpl into a live world transaction tx anchored at origin,
// returning the number of blocks placed.
func PlaceStructure(tx *world.Tx, origin cube.Pos, tmpl *StructureTemplate, opts StructurePlaceOpts) int {
	if tx == nil || tx.Closed() || tmpl == nil {
		return 0
	}
	airRID := tx.World().BlockRegistry().BlockRuntimeID(block.Air{})
	placed := 0
	for _, entry := range tmpl.blocks {
		pos := origin.Add(tmpl.transformOffset(entry.Offset, opts))
		if pos[1] < tx.Range().Min() || pos[1] > tx.Range().Max() {
			continue
		}
		if entry.UseRID {
			if opts.IgnoreAir && entry.RID == airRID {
				continue
			}
			tx.SetBlockRID(pos, entry.RID, opts.SetOpts)
			placed++
			continue
		}
		b := entry.Block
		if b == nil {
			b = block.Air{}
		}
		if opts.IgnoreAir {
			if _, isAir := b.(block.Air); isAir {
				continue
			}
		}
		tx.SetBlock(pos, b, opts.SetOpts)
		placed++
	}
	return placed
}

// PlaceStructureInChunk stamps the portion of tmpl intersecting chunk (pos, c)
// anchored at world coordinates origin. Safe for concurrent invocation inside
// WorldGenHooks.OnTerrain and WorldGenHooks.OnDecorate.
func PlaceStructureInChunk(pos world.ChunkPos, c *chunk.Chunk, origin cube.Pos, tmpl *StructureTemplate, opts StructurePlaceOpts) int {
	if c == nil || tmpl == nil {
		return 0
	}
	var reg world.BlockRegistry = world.DefaultBlockRegistry
	if br, ok := c.BlockRegistry().(world.BlockRegistry); ok && br != nil {
		reg = br
	}
	airRID := reg.BlockRuntimeID(block.Air{})
	minX, minZ := int(pos[0])<<4, int(pos[1])<<4
	maxX, maxZ := minX+15, minZ+15
	r := c.Range()
	placed := 0
	for _, entry := range tmpl.blocks {
		wp := origin.Add(tmpl.transformOffset(entry.Offset, opts))
		if wp[0] < minX || wp[0] > maxX || wp[2] < minZ || wp[2] > maxZ || wp[1] < r.Min() || wp[1] > r.Max() {
			continue
		}
		var rid uint32
		switch {
		case entry.UseRID:
			rid = entry.RID
		case entry.Block != nil:
			rid = reg.BlockRuntimeID(entry.Block)
		default:
			rid = airRID
		}
		if opts.IgnoreAir && rid == airRID {
			continue
		}
		c.SetBlock(uint8(wp[0]-minX), int16(wp[1]), uint8(wp[2]-minZ), 0, rid)
		placed++
	}
	return placed
}
