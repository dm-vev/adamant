package session

import (
	"math"
	"testing"

	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// TestClientMoveCarriesTheAim checks that the move the game is handed holds
// what the client interacted along and where its camera looked: the ray of a
// tap on a touch screen, and the camera of third person, which AttackAim
// holds an attack to. A camera that is not a direction is left out.
func TestClientMoveCarriesTheAim(t *testing.T) {
	t.Parallel()
	pk := &mc.PlayerAuthInputPacket{InteractPitch: 20, InteractYaw: 90, CameraOrientation: [3]float32{0, 0, 2}}
	m := clientMove(pk, mgl64.Vec3{}, &Session{})
	if m.Aim.Len() < 0.99 || m.Aim[0] > -0.9 {
		t.Errorf("the aim of a tap at yaw 90: %v, want a unit vector along -X", m.Aim)
	}
	if m.Camera != (mgl64.Vec3{}) {
		t.Errorf("a camera of length 2: %v, want none", m.Camera)
	}
	pk.CameraOrientation = [3]float32{0, 0.6, 0.8}
	m = clientMove(pk, mgl64.Vec3{}, &Session{})
	if math.Abs(m.Camera.Len()-1) > 1e-6 || math.Abs(m.Camera[2]-0.8) > 1e-6 {
		t.Errorf("a camera of 0, 0.6, 0.8: %v", m.Camera)
	}
	m = clientMove(&mc.PlayerAuthInputPacket{}, mgl64.Vec3{}, &Session{})
	if m.Aim != (mgl64.Vec3{}) || m.Camera != (mgl64.Vec3{}) {
		t.Errorf("a packet with no aim: aim %v, camera %v", m.Aim, m.Camera)
	}
}
