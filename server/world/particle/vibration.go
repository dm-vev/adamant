package particle

import "github.com/go-gl/mathgl/mgl64"

// VibrationSignal renders a native Bedrock vibration path from the particle
// origin passed to Tx.AddParticle to Destination over TravelTicks.
type VibrationSignal struct {
	Destination mgl64.Vec3
	TravelTicks int
	particle
}
