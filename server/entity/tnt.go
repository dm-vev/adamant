package entity

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// NewTNT creates a new primed TNT entity.
func NewTNT(opts world.EntitySpawnOpts, fuse time.Duration, owner world.Entity) *world.EntityHandle {
	if opts.Velocity.Len() == 0 {
		angle := rand.Float64() * math.Pi * 2
		opts.Velocity = mgl64.Vec3{-math.Sin(angle) * 0.02, 0.1, -math.Cos(angle) * 0.02}
	}
	var ownerID uuid.UUID
	nativeOwnerID := int64(-1)
	if projectile, ok := owner.(*Ent); ok {
		if owned, ok := projectile.Behaviour().(interface{ Owner() *world.EntityHandle }); ok {
			owner = nil
			if handle := owned.Owner(); handle != nil {
				owner, _ = handle.Entity(projectile.tx)
			}
		}
	}
	if living, ok := owner.(Living); ok {
		ownerID = living.H().UUID()
		nativeOwnerID = living.H().UniqueID()
	}
	return opts.New(TNTType, tntConfig{fuse: fuse, owner: ownerID, nativeOwnerID: nativeOwnerID})
}

type tntConfig struct {
	fuse          time.Duration
	owner         uuid.UUID
	nativeOwnerID int64
}

func (c tntConfig) Apply(data *world.EntityData) {
	data.Data = &tntBehaviour{PassiveBehaviour: tntConf.New(), remainingFuse: c.fuse, owner: c.owner, nativeOwnerID: c.nativeOwnerID}
}

type tntBehaviour struct {
	*PassiveBehaviour
	remainingFuse time.Duration
	owner         uuid.UUID
	nativeOwnerID int64
}

func (b *tntBehaviour) Fuse() time.Duration { return b.remainingFuse }

// TNT retains the living igniter across chained explosions and world reloads.
type TNT struct{ *Ent }

// Tick decrements the TNT's own fuse independently of its entity age. A zero
// fuse still moves once, then disappears before its explosion is applied.
func (t *TNT) Tick(tx *world.Tx, current int64) {
	t.Ent.Tick(tx, current)
	if _, present := tx.EntityByUUID(t.H().UUID()); !present {
		return
	}
	behaviour := t.data.Data.(*tntBehaviour)
	behaviour.remainingFuse -= time.Second / 20
	if behaviour.remainingFuse <= 0 {
		source := world.NewEntityExplosionSource(t, 4)
		_ = t.CloseIn(tx)
		block.ExplosionConfig{ItemDropChance: 1}.Explode(tx, source)
		return
	}
	if behaviour.remainingFuse%(time.Second/4) == 0 {
		viewers := tx.Viewers(t.Position())
		for _, viewer := range viewers {
			viewer.ViewEntityState(t)
		}
		tx.ReleaseViewers(viewers)
	}
}

func (t *TNT) Owner() *world.EntityHandle {
	behaviour := t.data.Data.(*tntBehaviour)
	if behaviour.owner != uuid.Nil {
		if owner, ok := t.tx.EntityByUUID(behaviour.owner); ok {
			behaviour.nativeOwnerID = owner.H().UniqueID()
			return owner.H()
		}
	} else if behaviour.nativeOwnerID != -1 {
		if owner, ok := t.tx.EntityByUniqueID(behaviour.nativeOwnerID); ok {
			behaviour.owner = owner.H().UUID()
			return owner.H()
		}
	}
	return nil
}

var tntConf = PassiveBehaviourConfig{
	Gravity: 0.04,
	Drag:    0.02,
}

// TNTType is a world.EntityType implementation for TNT.
var TNTType tntType

type tntType struct{}

func (t tntType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &TNT{Ent: &Ent{tx: tx, handle: handle, data: data}}
}

func (tntType) EncodeEntity() string   { return "minecraft:tnt" }
func (tntType) NetworkOffset() float64 { return 0.49 }
func (tntType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.49, 0, -0.49, 0.49, 0.98, 0.49)
}

func (t tntType) DecodeNBT(m map[string]any, data *world.EntityData) {
	remaining := time.Second * 4
	if _, ok := m["Fuse"].(uint8); ok {
		remaining = nbtconv.TickDuration[uint8](m, "Fuse")
	}
	config := tntConfig{nativeOwnerID: -1}
	if owner, ok := m["OwnerNew"].(int64); ok {
		config.nativeOwnerID = owner
	}
	if java, ok := m["adamant:java"].(map[string]any); ok {
		if _, ok := java["fuse"].(int16); ok {
			remaining = nbtconv.TickDuration[int16](java, "fuse")
		}
		if words, ok := java["owner"].([]int32); ok && len(words) == 4 {
			for i, word := range words {
				binary.BigEndian.PutUint32(config.owner[i*4:], uint32(word))
			}
		}
	}
	config.fuse = remaining
	config.Apply(data)
}

func (tntType) EncodeNBT(data *world.EntityData) map[string]any {
	behaviour := data.Data.(*tntBehaviour)
	ticks := behaviour.Fuse() / (time.Second / 20)
	ownerID := behaviour.nativeOwnerID
	words := make([]int32, 4)
	if id := behaviour.owner; id != uuid.Nil {
		for i := range words {
			words[i] = int32(binary.BigEndian.Uint32(id[i*4:]))
		}
	}
	result := map[string]any{
		"Fuse":         uint8(min(max(ticks, 0), 255)),
		"IsFuseLit":    uint8(1),
		"OwnerNew":     ownerID,
		"definitions":  []any{"+minecraft:tnt"},
		"adamant:java": map[string]any{"owner": words, "fuse": int16(ticks)},
	}
	return result
}
