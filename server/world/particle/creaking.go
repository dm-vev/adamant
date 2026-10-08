package particle

import "github.com/go-gl/mathgl/mgl64"

// CreakingTrail is the native creaking-heart trail particle event.
type CreakingTrail struct {
	Heart          mgl64.Vec3
	Creaking       mgl64.Vec3
	HeartAmount    int32
	CreakingAmount int32

	particle
}
