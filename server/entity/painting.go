package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/painting"
	"github.com/df-mc/dragonfly/server/world"
	"math/rand/v2"
	"slices"
)

// NewPainting creates a new painting entity with the motive and direction provided.
func NewPainting(opts world.EntitySpawnOpts, motive painting.Motive, direction cube.Direction) *world.EntityHandle {
	conf := paintingConf
	conf.Motive = motive
	conf.Direction = direction
	return opts.New(PaintingType, conf)
}

var paintingConf = PaintingBehaviourConfig{
	Stationary: StationaryBehaviourConfig{},
	Motive:     painting.Alban(),
	Direction:  cube.North,
}

// PaintingType is a world.EntityType implementation for Painting entities.
var PaintingType paintingType

type paintingType struct{}

func (paintingType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Painting{Ent: &Ent{tx: tx, handle: handle, data: data}}
}

func (paintingType) EncodeEntity() string { return "minecraft:painting" }

func (paintingType) BBox(e world.Entity) cube.BBox {
	p, ok := e.(*Painting)
	if !ok {
		return cube.BBox{}
	}
	w, h := p.Motive().Size()
	return cube.Box(-(w / 2), 0, -(w / 2), w/2, h, w/2)
}

func (paintingType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := paintingConf
	if motive := nbtconv.String(m, "Motive"); motive != "" {
		conf.Motive = painting.FromString(motive)
	} else {
		conf.Motive = painting.Alban()
	}
	if dir := nbtconv.Uint8(m, "Direction"); int(dir) < len(cube.Directions()) {
		conf.Direction = cube.Directions()[dir]
	} else {
		conf.Direction = cube.North
	}
	data.Data = conf.New()
}

func (paintingType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*PaintingBehaviour)
	dir := slices.Index(cube.Directions(), b.direction)
	if dir == -1 {
		dir = 0
	}
	return map[string]any{
		"Direction": byte(dir),
		"Motive":    b.motive.String(),
		"UniqueID":  -rand.Int64(),
	}
}

// Painting represents a decorative entity that hangs on walls.
type Painting struct {
	*Ent
}

// Motive returns the motive of the painting.
func (p *Painting) Motive() painting.Motive {
	return p.behaviour().Motive()
}

// Direction returns the direction the painting is facing.
func (p *Painting) Direction() cube.Direction {
	return p.behaviour().Direction()
}

func (p *Painting) behaviour() *PaintingBehaviour {
	return p.Behaviour().(*PaintingBehaviour)
}

// PaintingBehaviourConfig configures painting behaviour.
type PaintingBehaviourConfig struct {
	Stationary StationaryBehaviourConfig
	Motive     painting.Motive
	Direction  cube.Direction
}

// Apply applies the behaviour config to the entity data.
func (conf PaintingBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a new PaintingBehaviour from the config.
func (conf PaintingBehaviourConfig) New() *PaintingBehaviour {
	behaviour := &PaintingBehaviour{
		stationary: conf.Stationary.New(),
		motive:     conf.Motive,
		direction:  conf.Direction,
	}
	return behaviour
}

// PaintingBehaviour implements the stationary behaviour for a painting entity.
type PaintingBehaviour struct {
	stationary *StationaryBehaviour
	motive     painting.Motive
	direction  cube.Direction
}

// Tick ticks the stationary behaviour.
func (b *PaintingBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	return b.stationary.Tick(e, tx)
}

// Motive returns the motive stored in the behaviour.
func (b *PaintingBehaviour) Motive() painting.Motive {
	return b.motive
}

// Direction returns the direction stored in the behaviour.
func (b *PaintingBehaviour) Direction() cube.Direction {
	return b.direction
}
