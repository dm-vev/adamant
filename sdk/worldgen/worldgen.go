// Package worldgen provides unified generation hooks for Overworld, Nether,
// and End generators, custom biome definitions, and reusable block structure
// templates.
package worldgen

import (
	"fmt"
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

// Random provides deterministic per-chunk random numbers inside
// Hooks.OnDecorate without exposing internal generator packages.
type Random interface {
	NextInt(bound int32) int32
	NextInt32() int32
	NextLong() int64
	NextFloat() float32
	NextDouble() float64
	NextBoolean() bool
	NextGaussian() float64
	Consume(count int)
}

// Hooks customizes Overworld, Nether, or End generation stages cleanly.
// Non-nil hooks must be safe for concurrent invocation across generation
// workers and deterministic for a given world seed and chunk position.
type Hooks struct {
	// OnBiome overrides the Bedrock biome ID at world coordinates (x, y, z).
	OnBiome func(x, y, z int, defaultBiome uint8) uint8
	// OnBiomes runs once per generated chunk after base biomes are populated.
	OnBiomes func(pos world.ChunkPos, c *chunk.Chunk)
	// OnTerrain runs after base terrain, surface, and carving complete, before
	// features and structures decorate c.
	OnTerrain func(pos world.ChunkPos, c *chunk.Chunk)
	// OnDecorate runs after structures and features finish decorating c with a
	// deterministic per-chunk random stream.
	OnDecorate func(pos world.ChunkPos, c *chunk.Chunk, rng Random)
}

func bindOnDecorate(nativePtr any, onDecorate func(pos world.ChunkPos, c *chunk.Chunk, rng Random)) {
	if onDecorate == nil {
		return
	}
	field := reflect.ValueOf(nativePtr).Elem().FieldByName("OnDecorate")
	fn := reflect.MakeFunc(field.Type(), func(args []reflect.Value) []reflect.Value {
		pos := args[0].Interface().(world.ChunkPos)
		c := args[1].Interface().(*chunk.Chunk)
		var rng Random
		if !args[2].IsNil() {
			rng, _ = args[2].Interface().(Random)
		}
		onDecorate(pos, c, rng)
		return nil
	})
	field.Set(fn)
}

// Attach installs hooks on an Overworld, Nether, or End generator. Passing a
// zero Hooks value clears previously installed hooks and restores vanilla fast
// paths. Returns an error if gen is nil or an unsupported generator type.
func Attach(gen any, hooks Hooks) error {
	switch g := gen.(type) {
	case *overworld.Overworld:
		if g == nil {
			return fmt.Errorf("sdk/worldgen: nil overworld generator")
		}
		native := overworld.Hooks{
			OnBiome:   hooks.OnBiome,
			OnBiomes:  hooks.OnBiomes,
			OnTerrain: hooks.OnTerrain,
		}
		bindOnDecorate(&native, hooks.OnDecorate)
		g.SetHooks(native)
		return nil
	case *nether.Nether:
		if g == nil {
			return fmt.Errorf("sdk/worldgen: nil nether generator")
		}
		native := nether.Hooks{
			OnBiome:   hooks.OnBiome,
			OnBiomes:  hooks.OnBiomes,
			OnTerrain: hooks.OnTerrain,
		}
		bindOnDecorate(&native, hooks.OnDecorate)
		g.SetHooks(native)
		return nil
	case *end.End:
		if g == nil {
			return fmt.Errorf("sdk/worldgen: nil end generator")
		}
		native := end.Hooks{
			OnBiome:   hooks.OnBiome,
			OnBiomes:  hooks.OnBiomes,
			OnTerrain: hooks.OnTerrain,
		}
		bindOnDecorate(&native, hooks.OnDecorate)
		g.SetHooks(native)
		return nil
	default:
		return fmt.Errorf("sdk/worldgen: unsupported generator type %T", gen)
	}
}

// Clear removes any custom generation hooks attached to an Overworld, Nether,
// or End generator and restores vanilla fast paths.
func Clear(gen any) error {
	return Attach(gen, Hooks{})
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

// BiomeOption configures a custom biome created by NewBiome.
type BiomeOption func(*customBiome)

// Temperature sets the biome temperature.
func Temperature(v float64) BiomeOption { return func(b *customBiome) { b.temperature = v } }

// Rainfall sets the biome rainfall.
func Rainfall(v float64) BiomeOption { return func(b *customBiome) { b.rainfall = v } }

// Depth sets the biome terrain depth.
func Depth(v float64) BiomeOption { return func(b *customBiome) { b.depth = v } }

// Scale sets the biome terrain scale.
func Scale(v float64) BiomeOption { return func(b *customBiome) { b.scale = v } }

// Precipitation enables or disables rain/snow precipitation.
func Precipitation(v bool) BiomeOption { return func(b *customBiome) { b.hasPrecipitation = v } }

// WaterColour sets the biome water colour.
func WaterColour(c color.RGBA) BiomeOption { return func(b *customBiome) { b.waterColour = c } }

// Tags sets the biome string tags.
func Tags(tags ...string) BiomeOption { return func(b *customBiome) { b.tags = slices.Clone(tags) } }

// NewBiome constructs a custom world.Biome with numeric id and name.
func NewBiome(id int, name string, opts ...BiomeOption) world.Biome {
	if name == "" {
		panic("sdk/worldgen: biome name must not be empty")
	}
	b := customBiome{
		id:          id,
		name:        name,
		waterColour: color.RGBA{R: 0x44, G: 0xaf, B: 0xf5, A: 0xff},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&b)
		}
	}
	return b
}

// RegisterBiome registers b in the global world biome registry.
func RegisterBiome(b world.Biome) { world.RegisterBiome(b) }

// BiomeByID looks up a registered biome by numeric Bedrock biome ID.
func BiomeByID(id int) (world.Biome, bool) { return world.BiomeByID(id) }

// BiomeByName looks up a registered biome by name.
func BiomeByName(name string) (world.Biome, bool) { return world.BiomeByName(name) }

// Rotation specifies clockwise 90-degree increments around the Y axis.
type Rotation uint8

const (
	Rotate0   Rotation = 0
	Rotate90  Rotation = 1
	Rotate180 Rotation = 2
	Rotate270 Rotation = 3
)

// Block describes one block placement relative to a Structure origin.
type Block struct {
	Offset cube.Pos
	Block  world.Block
	RID    uint32
	UseRID bool
}

// At constructs a Structure Block entry placing b at relative offset pos.
func At(pos cube.Pos, b world.Block) Block { return Block{Offset: pos, Block: b} }

// AtRID constructs a Structure Block entry placing runtime ID rid at relative offset pos.
func AtRID(pos cube.Pos, rid uint32) Block { return Block{Offset: pos, RID: rid, UseRID: true} }

// PlaceOpts configures how a Structure is transformed and stamped.
type PlaceOpts struct {
	Rotation  Rotation
	MirrorX   bool
	MirrorZ   bool
	IgnoreAir bool
	SetOpts   *world.SetOpts
}

// Structure is a reusable, immutable multi-block template that can be stamped
// into a live *world.Tx or clipped safely into a chunk during worldgen hooks.
type Structure struct {
	name   string
	size   cube.Pos
	blocks []Block
}

// NewStructure builds a reusable Structure from name and block entries.
func NewStructure(name string, blocks ...Block) *Structure {
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
	return &Structure{name: name, size: maxPos, blocks: cloned}
}

// Name returns the structure name.
func (s *Structure) Name() string {
	if s == nil {
		return ""
	}
	return s.name
}

// Size returns the bounding dimensions (width, height, depth) of the structure.
func (s *Structure) Size() cube.Pos {
	if s == nil {
		return cube.Pos{}
	}
	return s.size
}

// Blocks returns a copy of the structure's block entries.
func (s *Structure) Blocks() []Block {
	if s == nil {
		return nil
	}
	return slices.Clone(s.blocks)
}

func (s *Structure) transformOffset(off cube.Pos, opts PlaceOpts) cube.Pos {
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

// Place stamps s into a live world transaction tx anchored at origin,
// returning the number of blocks placed.
func (s *Structure) Place(tx *world.Tx, origin cube.Pos, opts PlaceOpts) int {
	if s == nil || tx == nil || tx.Closed() {
		return 0
	}
	airRID := tx.World().BlockRegistry().BlockRuntimeID(block.Air{})
	placed := 0
	for _, entry := range s.blocks {
		pos := origin.Add(s.transformOffset(entry.Offset, opts))
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

// PlaceInChunk stamps the portion of s intersecting chunk (pos, c) anchored at
// world coordinates origin. Safe for concurrent invocation inside
// Hooks.OnTerrain and Hooks.OnDecorate.
func (s *Structure) PlaceInChunk(pos world.ChunkPos, c *chunk.Chunk, origin cube.Pos, opts PlaceOpts) int {
	if s == nil || c == nil {
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
	for _, entry := range s.blocks {
		wp := origin.Add(s.transformOffset(entry.Offset, opts))
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
