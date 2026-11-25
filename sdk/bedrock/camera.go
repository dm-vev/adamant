package bedrock

import (
	"errors"
	"image/color"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// CameraShakeType selects positional vs rotational screen shake.
type CameraShakeType uint8

const (
	ShakePositional CameraShakeType = 0
	ShakeRotational CameraShakeType = 1
)

// CameraEaseType matches Bedrock camera easing curve indices.
type CameraEaseType uint8

const (
	EaseLinear CameraEaseType = iota
	EaseSpring
	EaseInQuad
	EaseOutQuad
	EaseInOutQuad
	EaseInCubic
	EaseOutCubic
	EaseInOutCubic
)

// CameraEase configures smooth interpolation for CameraSet.
type CameraEase struct {
	Type     CameraEaseType
	Duration float32
}

// CameraSetOpts configures a CameraInstructionPacket Set instruction.
type CameraSetOpts struct {
	// Preset is the index into the active camera preset list (e.g. minecraft:free).
	Preset uint32
	Ease   *CameraEase
	Pos    *mgl64.Vec3
	// Rot is [pitch, yaw] in degrees.
	Rot    *[2]float32
	Facing *mgl64.Vec3
}

func opt[T any](v T) proto.Optional[T] {
	return proto.Optional[T]{Set: true, Val: v}
}

func vec3f(v mgl64.Vec3) [3]float32 {
	return [3]float32{float32(v[0]), float32(v[1]), float32(v[2])}
}

// CameraFadeSession fades s's screen to col over fadeIn, holds for hold, and
// fades back out over fadeOut (durations in seconds).
func CameraFadeSession(s *session.Session, fadeIn, hold, fadeOut float32, col color.RGBA) {
	if s == nil {
		return
	}
	s.SendRawPacket(&mc.CameraInstructionPacket{
		Fade: opt(mc.CameraInstructionFade{
			TimeData: opt(mc.CameraFadeTimeData{
				FadeInDuration:  fadeIn,
				WaitDuration:    hold,
				FadeOutDuration: fadeOut,
			}),
			Colour: opt([3]float32{
				float32(col.R) / 255,
				float32(col.G) / 255,
				float32(col.B) / 255,
			}),
		}),
	})
}

// CameraFade fades p's screen to col (durations in seconds).
func CameraFade(p *player.Player, fadeIn, hold, fadeOut float32, col color.RGBA) bool {
	s, ok := SessionOf(p)
	if !ok {
		return false
	}
	CameraFadeSession(s, fadeIn, hold, fadeOut, col)
	return true
}

// CameraSetSession moves or interpolates s's camera according to opts.
func CameraSetSession(s *session.Session, opts CameraSetOpts) {
	if s == nil {
		return
	}
	set := mc.CameraInstructionSet{Preset: opts.Preset}
	if opts.Ease != nil {
		set.Ease = opt(mc.CameraEase{Type: uint8(opts.Ease.Type), Duration: opts.Ease.Duration})
	}
	if opts.Pos != nil {
		set.Position = opt(vec3f(*opts.Pos))
	}
	if opts.Rot != nil {
		set.Rotation = opt(*opts.Rot)
	}
	if opts.Facing != nil {
		set.Facing = opt(vec3f(*opts.Facing))
	}
	s.SendRawPacket(&mc.CameraInstructionPacket{Set: opt(set)})
}

// CameraSet moves or interpolates p's camera according to opts.
func CameraSet(p *player.Player, opts CameraSetOpts) bool {
	s, ok := SessionOf(p)
	if !ok {
		return false
	}
	CameraSetSession(s, opts)
	return true
}

// CameraClearSession resets s's camera back to normal player control.
func CameraClearSession(s *session.Session) {
	if s == nil {
		return
	}
	s.SendRawPacket(&mc.CameraInstructionPacket{Clear: opt(true)})
}

// CameraClear resets p's camera back to normal player control.
func CameraClear(p *player.Player) bool {
	s, ok := SessionOf(p)
	if !ok {
		return false
	}
	CameraClearSession(s)
	return true
}

// CameraFOVSession changes or clears s's field of view over easeTime seconds.
func CameraFOVSession(s *session.Session, fov, easeTime float32, easeType CameraEaseType, clear bool) {
	if s == nil {
		return
	}
	s.SendRawPacket(&mc.CameraInstructionPacket{
		FieldOfView: opt(mc.CameraInstructionFieldOfView{
			FieldOfView: fov,
			EaseTime:    easeTime,
			EaseType:    int32(easeType),
			Clear:       clear,
		}),
	})
}

// CameraFOV changes or clears p's field of view over easeTime seconds.
func CameraFOV(p *player.Player, fov, easeTime float32, easeType CameraEaseType, clear bool) bool {
	s, ok := SessionOf(p)
	if !ok {
		return false
	}
	CameraFOVSession(s, fov, easeTime, easeType, clear)
	return true
}

// CameraShakeSession starts a camera shake effect on s.
func CameraShakeSession(s *session.Session, intensity, duration float32, kind CameraShakeType) {
	if s == nil {
		return
	}
	s.SendRawPacket(&mc.CameraShakePacket{
		Intensity: intensity,
		Duration:  duration,
		Type:      uint8(kind),
		Action:    0, // add
	})
}

// CameraShake starts a camera shake effect on p.
func CameraShake(p *player.Player, intensity, duration float32, kind CameraShakeType) bool {
	s, ok := SessionOf(p)
	if !ok {
		return false
	}
	CameraShakeSession(s, intensity, duration, kind)
	return true
}

// StopCameraShakeSession stops any active camera shake on s.
func StopCameraShakeSession(s *session.Session, kind CameraShakeType) {
	if s == nil {
		return
	}
	s.SendRawPacket(&mc.CameraShakePacket{
		Type:   uint8(kind),
		Action: 1, // stop
	})
}

// StopCameraShake stops any active camera shake on p.
func StopCameraShake(p *player.Player, kind CameraShakeType) bool {
	s, ok := SessionOf(p)
	if !ok {
		return false
	}
	StopCameraShakeSession(s, kind)
	return true
}

// SetSessionFog sets the active Bedrock fog identifier stack on s (e.g.
// "minecraft:fog_crimson_forest", "myplugin:dungeon_fog").
func SetSessionFog(s *session.Session, fogIDs ...string) {
	if s == nil {
		return
	}
	stack := append([]string(nil), fogIDs...)
	s.SendRawPacket(&mc.PlayerFogPacket{Stack: stack})
}

// SetFog sets the active Bedrock fog identifier stack on p.
func SetFog(p *player.Player, fogIDs ...string) bool {
	s, ok := SessionOf(p)
	if !ok {
		return false
	}
	SetSessionFog(s, fogIDs...)
	return true
}

// ClearFog removes all custom fog layers from p.
func ClearFog(p *player.Player) bool {
	return SetFog(p)
}

// Transfer transfers p to another Bedrock server at address ("host:port").
func Transfer(p *player.Player, address string) error {
	if p == nil {
		return errors.New("sdk/bedrock: nil Player")
	}
	return p.Transfer(address)
}

// TransferSession queues a raw TransferPacket directly on s.
func TransferSession(s *session.Session, host string, port uint16, reloadWorld bool) {
	if s == nil || host == "" {
		return
	}
	s.SendRawPacket(&mc.TransferPacket{
		Address:     host,
		Port:        port,
		ReloadWorld: reloadWorld,
	})
}

// AnimateOpts configures an AnimateEntityPacket sent via AnimateEntity.
type AnimateOpts struct {
	Animation     string
	NextState     string
	StopCondition string
	Controller    string
	BlendOutTime  float32
}

// AnimateEntity sends an AnimateEntityPacket to s triggering a Bedrock
// resource-pack animation on the specified entity runtime IDs.
func AnimateEntity(s *session.Session, opts AnimateOpts, runtimeIDs ...uint64) {
	if s == nil || opts.Animation == "" || len(runtimeIDs) == 0 {
		return
	}
	s.SendRawPacket(&mc.AnimateEntityPacket{
		Animation:        opts.Animation,
		NextState:        opts.NextState,
		StopCondition:    opts.StopCondition,
		Controller:       opts.Controller,
		BlendOutTime:     opts.BlendOutTime,
		EntityRuntimeIDs: append([]uint64(nil), runtimeIDs...),
	})
}
