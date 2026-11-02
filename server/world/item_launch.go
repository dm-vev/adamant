package world

import (
	"math"
	"math/rand/v2"
	"sync"

	"github.com/go-gl/mathgl/mgl64"
)

type nativeItemLaunch struct {
	phase, yaw float32
	velocity   mgl64.Vec3
}

// ItemActor uses four consecutive nextFloat calls from a separate native MT
// stream. Phase and yaw precede the two horizontal velocity components.
func nativeItemLaunchState(next func() float32) nativeItemLaunch {
	phase := float32(next() * float32(math.Pi))
	phase = float32(phase + phase)
	yaw := float32(next() * 360)
	x := float32(next() * .2)
	x = float32(x - .1)
	z := float32(next() * .2)
	z = float32(z - .1)
	return nativeItemLaunch{phase: phase, yaw: yaw,
		velocity: mgl64.Vec3{float64(x), float64(float32(.2)), float64(z)}}
}

// The native simulation thread shares its independent stream across item
// constructors. Go world owners may run concurrently, so each four-word group
// is protected. Entropy seeding does not draw from any world's LevelRandom.
var itemConstructorRandom struct {
	sync.Mutex
	stream *levelRandomSource
}

func nextNativeItemLaunch() nativeItemLaunch {
	itemConstructorRandom.Lock()
	defer itemConstructorRandom.Unlock()
	if itemConstructorRandom.stream == nil {
		itemConstructorRandom.stream = newLevelRandomSource(rand.Uint32())
	}
	return nativeItemLaunchState(func() float32 {
		return nativeRandomFloat32(itemConstructorRandom.stream.Uint32())
	})
}

func applyNativeItemLaunch(data *EntityData, opts EntitySpawnOpts, launch nativeItemLaunch) {
	if opts.ItemToss {
		data.Vel = launch.velocity
	}
	// ActorFactory applies its requested rotation after construction. The
	// constructor's random yaw is consumed even when the spawner replaces it.
	// The native phase is consumed even though the server's item behaviour
	// currently has no equivalent bob-phase property or synchronisation field.
}
