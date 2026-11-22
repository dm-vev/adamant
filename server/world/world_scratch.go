package world

import "github.com/df-mc/dragonfly/server/block/cube"

// scratch is the buffers the tick of the world reuses.
type scratch struct {
	scratchPrefetch               []prefetchItem
	scratchRegionRefs             []columnRef
	scratchViews                  []ReadView
	scratchRandom                 []cube.Pos
	scratchRandomScan             []randomTickScan
	scratchRandomFound            [][]cube.Pos
	scratchBlockEntities          []blockEntityTickRef
	scratchLoaderAreas            []loaderActiveArea
	scratchFocus                  []ChunkPos
	scratchInhabitedPlayers       []inhabitedTimePlayer
	scratchPostProcessingColumns  []columnRef
	scratchBlockAreaSet           blockAreaSet
	scratchEligibleColumns        map[ChunkPos]*Column
	scratchSeenChunks             map[ChunkPos]struct{}
	scratchVisitedAreas           map[blockAreaKey]struct{}
	scratchForcedColumns          []columnRef
	scratchRedstoneTransient      map[ChunkPos]uint64
	scratchRedstoneTransientClock uint64
	scratchLateChunkCache         map[ChunkPos]lateChunkCache
	scratchLateCircuitSkip        map[ChunkPos]struct{}
	scratchLateCircuitActive      map[ChunkPos]bool
	scratchLateCircuitEvaluated   []ChunkPos
}
