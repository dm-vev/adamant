package world

import "github.com/df-mc/dragonfly/server/ecs"

// ActorMetaPatch holds explicit network metadata overrides for an entity.
// Nil pointer fields, zero flag masks, and empty property maps leave the
// underlying actor metadata unchanged.
type ActorMetaPatch struct {
	NameTag           *string
	AlwaysShowNameTag *bool
	ScoreTag          *string
	Scale             *float32
	Width             *float32
	Height            *float32
	Variant           *int32
	MarkVariant       *int32
	Color             *uint8
	Invisible         *bool
	OnFire            *bool
	NoAI              *bool
	FlagSet           uint64
	FlagClear         uint64
	Flag2Set          uint64
	Flag2Clear        uint64
	CustomStrings     map[string]string
	CustomInts        map[string]int32
	CustomFloats      map[string]float32
}

// MetadataContributor is implemented by entities or behaviours that contribute
// explicit ActorMetaPatch overrides to their network metadata.
type MetadataContributor interface {
	ContributeMetadata(patch *ActorMetaPatch)
}

// MetaPatchKey is the ECS component key used to attach an ActorMetaPatch to an
// EntityHandle's component bag.
var MetaPatchKey = ecs.NewKey[ActorMetaPatch]("world:meta_patch")

// SetNameTag sets the NameTag override on p.
func (p *ActorMetaPatch) SetNameTag(s string) { p.NameTag = &s }

// SetAlwaysShowNameTag sets the AlwaysShowNameTag override on p.
func (p *ActorMetaPatch) SetAlwaysShowNameTag(v bool) { p.AlwaysShowNameTag = &v }

// SetScoreTag sets the ScoreTag override on p.
func (p *ActorMetaPatch) SetScoreTag(s string) { p.ScoreTag = &s }

// SetScale sets the Scale override on p.
func (p *ActorMetaPatch) SetScale(v float32) { p.Scale = &v }

// SetWidth sets the Width override on p.
func (p *ActorMetaPatch) SetWidth(v float32) { p.Width = &v }

// SetHeight sets the Height override on p.
func (p *ActorMetaPatch) SetHeight(v float32) { p.Height = &v }

// SetVariant sets the Variant override on p.
func (p *ActorMetaPatch) SetVariant(v int32) { p.Variant = &v }

// SetMarkVariant sets the MarkVariant override on p.
func (p *ActorMetaPatch) SetMarkVariant(v int32) { p.MarkVariant = &v }

// SetColor sets the Color override on p.
func (p *ActorMetaPatch) SetColor(v uint8) { p.Color = &v }

// SetInvisible sets the Invisible override on p.
func (p *ActorMetaPatch) SetInvisible(v bool) { p.Invisible = &v }

// SetOnFire sets the OnFire override on p.
func (p *ActorMetaPatch) SetOnFire(v bool) { p.OnFire = &v }

// SetNoAI sets the NoAI override on p.
func (p *ActorMetaPatch) SetNoAI(v bool) { p.NoAI = &v }

// SetFlag sets an actor metadata flag bit (0..63 in FlagSet, 64..127 in Flag2Set).
func (p *ActorMetaPatch) SetFlag(flag uint8) {
	if flag < 64 {
		bit := uint64(1) << flag
		p.FlagSet |= bit
		p.FlagClear &^= bit
		return
	}
	if flag < 128 {
		bit := uint64(1) << (flag - 64)
		p.Flag2Set |= bit
		p.Flag2Clear &^= bit
	}
}

// ClearFlag clears an actor metadata flag bit (0..63 in FlagClear, 64..127 in Flag2Clear).
func (p *ActorMetaPatch) ClearFlag(flag uint8) {
	if flag < 64 {
		bit := uint64(1) << flag
		p.FlagClear |= bit
		p.FlagSet &^= bit
		return
	}
	if flag < 128 {
		bit := uint64(1) << (flag - 64)
		p.Flag2Clear |= bit
		p.Flag2Set &^= bit
	}
}

// SetString sets a custom string actor property on p.
func (p *ActorMetaPatch) SetString(key, val string) {
	if p.CustomStrings == nil {
		p.CustomStrings = make(map[string]string)
	}
	p.CustomStrings[key] = val
}

// SetInt sets a custom int32 actor property on p.
func (p *ActorMetaPatch) SetInt(key string, val int32) {
	if p.CustomInts == nil {
		p.CustomInts = make(map[string]int32)
	}
	p.CustomInts[key] = val
}

// SetFloat sets a custom float32 actor property on p.
func (p *ActorMetaPatch) SetFloat(key string, val float32) {
	if p.CustomFloats == nil {
		p.CustomFloats = make(map[string]float32)
	}
	p.CustomFloats[key] = val
}

// IsZero reports whether p carries no metadata overrides.
func (p *ActorMetaPatch) IsZero() bool {
	if p == nil {
		return true
	}
	return p.NameTag == nil && p.AlwaysShowNameTag == nil && p.ScoreTag == nil &&
		p.Scale == nil && p.Width == nil && p.Height == nil &&
		p.Variant == nil && p.MarkVariant == nil && p.Color == nil &&
		p.Invisible == nil && p.OnFire == nil && p.NoAI == nil &&
		p.FlagSet == 0 && p.FlagClear == 0 && p.Flag2Set == 0 && p.Flag2Clear == 0 &&
		len(p.CustomStrings) == 0 && len(p.CustomInts) == 0 && len(p.CustomFloats) == 0
}
