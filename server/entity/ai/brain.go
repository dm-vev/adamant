package ai

import (
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Snapshot is a read-only view of the world state relevant to a single AI tick. It is safe to pass between goroutines.
type Snapshot struct {
	Tick int64

	SelfPos mgl64.Vec3
	SelfVel mgl64.Vec3

	HasNearestPlayer bool
	NearestPlayer    EntitySnapshot
}

// EntitySnapshot represents a minimal snapshot of an entity needed for targeting decisions.
type EntitySnapshot struct {
	Handle *world.EntityHandle
	Pos    mgl64.Vec3
}

// Intent describes the action a mob wants to take on the main thread.
type Intent struct {
	ComputedAt int64

	MoveDir mgl64.Vec3

	HasLookAt bool
	LookAt    mgl64.Vec3

	Target     *world.EntityHandle
	WantAttack bool
}

// ComputeFunc computes an Intent from a Snapshot. It must be concurrency-safe with respect to the Snapshot, but may
// mutate its own receiver state if the caller ensures it is not run concurrently.
type ComputeFunc func(s Snapshot) Intent

// Brain computes AI intents asynchronously and exposes them through Poll. Only one job is allowed in flight at a time.
type Brain struct {
	scheduler *Scheduler
	compute   ComputeFunc

	inFlight atomic.Bool
	results  chan Intent
}

// NewBrain creates a new Brain using the Scheduler provided.
func NewBrain(scheduler *Scheduler, compute ComputeFunc) *Brain {
	if scheduler == nil {
		scheduler = Default()
	}
	return &Brain{
		scheduler: scheduler,
		compute:   compute,
		results:   make(chan Intent, 1),
	}
}

// Request schedules computation for snapshot if no job is currently running. Request never blocks.
func (b *Brain) Request(snapshot Snapshot) bool {
	if b == nil || b.compute == nil {
		return false
	}
	if !b.inFlight.CompareAndSwap(false, true) {
		return false
	}

	ok := b.scheduler.Submit(func() {
		intent := b.compute(snapshot)
		b.publish(intent)
		b.inFlight.Store(false)
	})
	if !ok {
		b.inFlight.Store(false)
		return false
	}
	return true
}

func (b *Brain) publish(intent Intent) {
	select {
	case b.results <- intent:
		return
	default:
	}
	select {
	case <-b.results:
	default:
	}
	select {
	case b.results <- intent:
	default:
	}
}

// Poll returns the latest computed Intent if one is available.
func (b *Brain) Poll() (Intent, bool) {
	if b == nil {
		return Intent{}, false
	}
	select {
	case intent := <-b.results:
		return intent, true
	default:
		return Intent{}, false
	}
}
