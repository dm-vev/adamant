package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"math/rand/v2"
	"time"
)

// Firework is an item (and entity) used for creating decorative explosions, boosting when flying with elytra, and
// loading into a crossbow as ammunition.
type Firework struct {
	// Duration is the flight duration of the firework.
	Duration time.Duration
	// Explosions is the list of explosions the firework should create when launched.
	Explosions []FireworkExplosion
}

// Use ...
func (f Firework) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	if g, ok := user.(interface {
		Gliding() bool
	}); !ok || !g.Gliding() {
		return false
	}

	pos := user.Position()

	tx.PlaySound(pos, sound.FireworkLaunch{})
	create := tx.World().EntityRegistry().Config().Firework
	opts := world.EntitySpawnOpts{Position: pos, Rotation: user.Rotation()}
	tx.AddEntity(create(opts, f, user, 1.15, 0.04, true))

	ctx.SubtractFromCount(1)
	return true
}

// UseOnBlock ...
func (f Firework) UseOnBlock(pos cube.Pos, _ cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	fpos := pos.Vec3().Add(clickPos)
	create := tx.World().EntityRegistry().Config().Firework
	opts := world.EntitySpawnOpts{Position: fpos, Rotation: cube.Rotation{rand.Float64() * 360, 90}}
	tx.AddEntity(create(opts, f, user, 1.15, 0.04, false))
	tx.PlaySound(fpos, sound.FireworkLaunch{})

	ctx.SubtractFromCount(1)
	return true
}

// EncodeNBT ...
func (f Firework) EncodeNBT() map[string]any {
	explosions := make([]any, 0, len(f.Explosions))
	for _, explosion := range f.Explosions {
		explosions = append(explosions, explosion.EncodeNBT())
	}
	flightTicks := encodeFireworkFlightTicks(f.Duration)
	return map[string]any{"Fireworks": map[string]any{
		"Explosions": explosions,
		"Flight":     flightTicks,
	}}
}

// DecodeNBT ...
func (f Firework) DecodeNBT(data map[string]any) any {
	if fireworks, ok := data["Fireworks"].(map[string]any); ok {
		if explosions, ok := fireworks["Explosions"].([]any); ok {
			f.Explosions = make([]FireworkExplosion, 0, len(explosions))
			for _, raw := range explosions {
				explosionData, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				decoded, ok := (FireworkExplosion{}).DecodeNBT(explosionData).(FireworkExplosion)
				if !ok {
					continue
				}
				f.Explosions = append(f.Explosions, decoded)
			}
		}
		if durationTicks, ok := readUint8(fireworks["Flight"]); ok {
			f.Duration = decodeFireworkFlightTicks(durationTicks)
		}
	}
	return f
}

// RandomisedDuration returns the randomised flight duration of the firework.
func (f Firework) RandomisedDuration() time.Duration {
	return f.Duration + time.Duration(rand.IntN(int(time.Millisecond*600)))
}

// OffHand ...
func (Firework) OffHand() bool {
	return true
}

// EncodeItem ...
func (Firework) EncodeItem() (name string, meta int16) {
	return "minecraft:firework_rocket", 0
}

const (
	fireworkFlightStep = 50 * time.Millisecond
	fireworkFlightBase = 50 * time.Millisecond
)

// encodeFireworkFlightTicks maps flight duration to the encoded "Flight" ticks, clamping
// to a safe range to avoid underflows from malformed values.
func encodeFireworkFlightTicks(duration time.Duration) uint8 {
	if duration < 0 {
		duration = 0
	}
	encoded := int64((duration/10 - fireworkFlightBase) / fireworkFlightStep)
	if encoded < 0 {
		encoded = 0
	} else if encoded > 255 {
		encoded = 255
	}
	return uint8(encoded)
}

// decodeFireworkFlightTicks converts encoded ticks back into the approximate flight duration.
func decodeFireworkFlightTicks(ticks uint8) time.Duration {
	return (time.Duration(ticks)*fireworkFlightStep + fireworkFlightBase) * 10
}

func readUint8(value any) (uint8, bool) {
	switch v := value.(type) {
	case uint8:
		return v, true
	case int8:
		if v < 0 {
			return 0, false
		}
		return uint8(v), true
	case int16:
		if v < 0 || v > 255 {
			return 0, false
		}
		return uint8(v), true
	case uint16:
		if v > 255 {
			return 0, false
		}
		return uint8(v), true
	case int32:
		if v < 0 || v > 255 {
			return 0, false
		}
		return uint8(v), true
	case uint32:
		if v > 255 {
			return 0, false
		}
		return uint8(v), true
	case int64:
		if v < 0 || v > 255 {
			return 0, false
		}
		return uint8(v), true
	case uint64:
		if v > 255 {
			return 0, false
		}
		return uint8(v), true
	case int:
		if v < 0 || v > 255 {
			return 0, false
		}
		return uint8(v), true
	case uint:
		if v > 255 {
			return 0, false
		}
		return uint8(v), true
	default:
		return 0, false
	}
}
