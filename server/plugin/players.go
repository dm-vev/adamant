package plugin

import (
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// PlayerSummary captures a snapshot of an online player at the moment the summary
// was produced. It allows plugins to inspect metadata without opening their own
// transactions on the player entity.
type PlayerSummary struct {
	UUID      uuid.UUID
	Name      string
	XUID      string
	Dimension world.Dimension
	Position  mgl64.Vec3
	GameMode  world.GameMode
	Latency   time.Duration
	Connected bool
}
