package overworld

import (
	"fmt"
	"strings"
	"sync"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/chunk"
)

var _ world.GenerationShapeAccess = (*featureRegion)(nil)

type regionRandomFactoryKey struct {
	seed   int64
	legacy bool
}

var regionRandomFactoryCache sync.Map // regionRandomFactoryKey -> vanilla.PositionalFactory

func regionRandomFactory(seed int64, legacy bool) vanilla.PositionalFactory {
	key := regionRandomFactoryKey{seed: seed, legacy: legacy}
	if cached, ok := regionRandomFactoryCache.Load(key); ok {
		return cached.(vanilla.PositionalFactory)
	}
	root := vanilla.NewXoroshiroRandom(seed)
	if legacy {
		root = vanilla.NewLegacyRandom(seed)
	}
	named := root.ForkPositional().FromHashOf("minecraft:worldgen_region_random")
	factory := named.ForkPositional()
	actual, _ := regionRandomFactoryCache.LoadOrStore(key, factory)
	return actual.(vanilla.PositionalFactory)
}

func (r *featureRegion) BlockStateID(pos cube.Pos) (uint32, error) {
	if int32(pos[1]) < r.minY || int32(pos[1]) > r.maxY {
		return gamedata.BlockStateID("minecraft:void_air", nil)
	}
	state := r.nativeState(featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])})
	if id := state.CanonicalID(r.registry); id != 0 {
		return id, nil
	}
	return 0, fmt.Errorf("overworld: native-only generation state at %v has no identity", pos)
}

func (r *featureRegion) Range() cube.Range { return r.c.Range() }

func (r *featureRegion) RegionRandom() *vanilla.Random {
	// RandomState creates one named positional factory, then derives one
	// mutable stream per source-origin chunk. Streams survive stage changes
	// and are revisited by source origin rather than destination chunk.
	key := [2]int32{r.writeOriginX, r.writeOriginZ}
	if random := r.regionRandoms[key]; random != nil {
		return random
	}
	factory := regionRandomFactory(r.f.seed, r.f.source.UseLegacyRandomSource())
	random := factory.At(key[0]*16, 0, key[1]*16)
	r.regionRandoms[key] = &random
	return &random
}

func (r *featureRegion) Light(cube.Pos) uint8 {
	if r.f.source.HasSkyLight() {
		return 15
	}
	return 0
}

func (r *featureRegion) FluidTickDelay(name string) int32 {
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	return r.f.source.FluidTickDelay(name)
}

func (r *featureRegion) ScheduleTick(pos cube.Pos, kind chunk.ScheduledTickKind, name string, _ int32) {
	r.reserveTick(chunk.ScheduledTick{Pos: pos, Kind: kind, Type: canonicalResource(name)})
}

func (r *featureRegion) reserveTick(tick chunk.ScheduledTick) bool {
	tick.Type = canonicalResource(tick.Type)
	key := featureTickReservation{pos: tick.Pos, kind: tick.Kind, typeName: tick.Type}
	if r.result.HasScheduledTick(tick.Pos, tick.Kind, tick.Type) {
		r.scheduledTicks[key] = struct{}{}
		return false
	}
	if _, exists := r.scheduledTicks[key]; exists {
		return false
	}
	r.scheduledTicks[key] = struct{}{}
	r.scheduleTick(tick)
	return true
}

func (r *featureRegion) HasBlockTick(pos cube.Pos, name string) bool {
	name = canonicalResource(name)
	if len(r.scheduledTicks) != 0 {
		if _, scheduled := r.scheduledTicks[featureTickReservation{pos: pos, kind: chunk.BlockTick, typeName: name}]; scheduled {
			return true
		}
	}
	return r.result.HasScheduledTick(pos, chunk.BlockTick, name)
}

func canonicalResource(name string) string {
	if strings.Contains(name, ":") {
		return name
	}
	return "minecraft:" + name
}

func (r *featureRegion) Signal(pos cube.Pos, face cube.Face) int {
	name, properties := r.State(featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])})
	facing := signalFacing(properties["facing"])
	if facing == cube.Face(-1) || facing != face.Opposite() {
		return 0
	}
	switch normalizeResource(name) {
	case "repeater":
		if value, _ := properties["powered"].(bool); value {
			return 15
		}
	case "comparator":
		if powered, _ := properties["powered"].(bool); !powered {
			return 0
		}
		actor := r.templateActorData(pos)
		return clampGenerationSignal(actor["OutputSignal"])
	}
	return 0
}

func (r *featureRegion) HiveHasOccupants(pos cube.Pos) bool {
	actor := r.templateActorData(pos)
	if actor == nil {
		return false
	}
	for _, key := range [...]string{"bees", "Occupants"} {
		if hasHiveOccupantList(actor[key]) {
			return true
		}
	}
	return false
}

func (r *featureRegion) SetBlockState(pos cube.Pos, id uint32, flags int) bool {
	p := featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	return r.setWithFlags(p, world.ResolveStateID(r.c.BlockRegistry(), id), flags)
}

func signalFacing(value any) cube.Face {
	direction, ok := value.(string)
	if !ok {
		return cube.Face(-1)
	}
	switch direction {
	case "north":
		return cube.FaceNorth
	case "south":
		return cube.FaceSouth
	case "east":
		return cube.FaceEast
	case "west":
		return cube.FaceWest
	default:
		return cube.Face(-1)
	}
}

func clampGenerationSignal(value any) int {
	switch n := value.(type) {
	case int:
		return max(0, min(15, n))
	case int8:
		return max(0, min(15, int(n)))
	case int16:
		return max(0, min(15, int(n)))
	case int32:
		return max(0, min(15, int(n)))
	case int64:
		return int(max(int64(0), min(int64(15), n)))
	case uint8:
		return min(15, int(n))
	case uint16:
		return min(15, int(n))
	case uint32:
		return int(min(uint32(15), n))
	case uint64:
		return int(min(uint64(15), n))
	default:
		return 0
	}
}
