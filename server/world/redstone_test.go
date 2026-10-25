package world

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/go-gl/mathgl/mgl64"
)

var _ Handler = minimalRedstoneTestHandler{}

type minimalRedstoneTestHandler struct{}

func runWorld(w *World, f func(*Tx)) {
	w.Do(f).Wait(context.Background())
}

func (minimalRedstoneTestHandler) HandleRedstoneUpdate(*Context, RedstoneUpdate)                {}
func (minimalRedstoneTestHandler) HandleLiquidFlow(*Context, cube.Pos, cube.Pos, Liquid, Block) {}
func (minimalRedstoneTestHandler) HandleLiquidDecay(*Context, cube.Pos, Liquid, Liquid)         {}
func (minimalRedstoneTestHandler) HandleLiquidHarden(*Context, cube.Pos, Block, Block, Block)   {}
func (minimalRedstoneTestHandler) HandleSound(*Context, Sound, mgl64.Vec3)                      {}
func (minimalRedstoneTestHandler) HandleFireSpread(*Context, cube.Pos, cube.Pos)                {}
func (minimalRedstoneTestHandler) HandleBlockBurn(*Context, cube.Pos)                           {}
func (minimalRedstoneTestHandler) HandleCropTrample(*Context, cube.Pos)                         {}
func (minimalRedstoneTestHandler) HandleLeavesDecay(*Context, cube.Pos)                         {}
func (minimalRedstoneTestHandler) HandlePortalCreate(*Context, Dimension, []cube.Pos)           {}
func (minimalRedstoneTestHandler) HandlePortalActivate(*Context, Dimension, []cube.Pos)         {}
func (minimalRedstoneTestHandler) HandleEntitySpawn(*Tx, Entity)                                {}
func (minimalRedstoneTestHandler) HandleEntityDespawn(*Tx, Entity)                              {}
func (minimalRedstoneTestHandler) HandleExplosion(*Context, ExplosionSource, *[]Entity, *[]cube.Pos, *float64, *bool) {
}
func (minimalRedstoneTestHandler) HandleClose(*Tx) {}

func TestClampRedstonePower(t *testing.T) {
	tests := []struct {
		name  string
		power int
		want  int
	}{
		{name: "negative", power: -1, want: 0},
		{name: "zero", power: 0, want: 0},
		{name: "middle", power: 8, want: 8},
		{name: "maximum", power: 15, want: 15},
		{name: "over maximum", power: 16, want: 15},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClampRedstonePower(test.power); got != test.want {
				t.Fatalf("ClampRedstonePower(%d) = %d, want %d", test.power, got, test.want)
			}
		})
	}
}

func TestRedstoneStepFace(t *testing.T) {
	tests := []struct {
		name string
		from cube.Pos
		to   cube.Pos
		want cube.Face
	}{
		{name: "diagonal step up", from: cube.Pos{0, 64, 0}, to: cube.Pos{1, 65, 0}, want: cube.FaceUp},
		{name: "diagonal step down", from: cube.Pos{0, 64, 0}, to: cube.Pos{-1, 63, 0}, want: cube.FaceDown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := redstoneStepFace(test.from, test.to); got != test.want {
				t.Fatalf("redstoneStepFace(%v, %v) = %v, want %v", test.from, test.to, got, test.want)
			}
		})
	}
}

func TestCompareBlockPosSortOrder(t *testing.T) {
	positions := []cube.Pos{
		{3, 2, 1},
		{2, 1, 2},
		{1, 1, 1},
		{0, 1, 1},
		{0, 0, 9},
		{9, 0, -1},
	}
	want := []cube.Pos{
		{9, 0, -1},
		{0, 0, 9},
		{0, 1, 1},
		{1, 1, 1},
		{2, 1, 2},
		{3, 2, 1},
	}

	slices.SortFunc(positions, compareBlockPos)
	if !slices.Equal(positions, want) {
		t.Fatalf("sorted positions = %v, want %v", positions, want)
	}
	if got := compareBlockPos(cube.Pos{1, 2, 3}, cube.Pos{1, 2, 3}); got != 0 {
		t.Fatalf("compareBlockPos(equal positions) = %d, want 0", got)
	}
}

func TestRedstoneRelayerNeighbourPositionsAreDeterministic(t *testing.T) {
	engine := newRedstoneEngine(0)
	pos := cube.Pos{0, 64, 0}
	got := engine.redstoneRelayerNeighbourPositions(nil, pos, redstoneNeighbourOrderTestBlock{neighbours: []cube.Pos{
		{1, 64, 0},
		{0, 63, 0},
		{0, 64, -1},
		{-1, 64, 0},
		{0, 65, 0},
		{0, 64, 1},
	}})
	want := []cube.Pos{
		{0, 63, 0},
		{0, 64, -1},
		{-1, 64, 0},
		{1, 64, 0},
		{0, 64, 1},
		{0, 65, 0},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("redstone relayer neighbours = %v, want %v", got, want)
	}
}

func TestRedstoneStrongPowerConductorExcludesMarkedNonConductors(t *testing.T) {
	pos := cube.Pos{0, 64, 0}
	tests := []struct {
		name  string
		block Block
		want  bool
	}{
		{name: "solid block", block: redstoneSolidBlock{}, want: true},
		{name: "marked non-conductor", block: redstoneNonConductiveSolidBlock{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := redstoneStrongPowerConductor(pos, test.block, nil, cube.FaceWest); got != test.want {
				t.Fatalf("redstoneStrongPowerConductor(%T) = %t, want %t", test.block, got, test.want)
			}
		})
	}
}

func TestRedstoneEngineInvalidateAround(t *testing.T) {
	var nilEngine *redstoneEngine
	nilEngine.invalidateAround(cube.Pos{0, 0, 0}, cube.Pos{0, 0, 0}, RedstoneUpdateCauseBlockUpdate, cube.Range{0, 0})

	engine := newRedstoneEngine(42)
	pos, changed := cube.Pos{8, 0, 8}, cube.Pos{9, 0, 8}
	engine.invalidateAround(pos, changed, RedstoneUpdateCauseBlockUpdate, cube.Range{0, 1})

	want := map[cube.Pos]redstoneDirty{
		{8, 0, 8}: redstoneDirty{changed: changed, hasChanged: true, source: changed, hasSource: true, cause: RedstoneUpdateCauseBlockUpdate},
		{9, 0, 8}: redstoneDirty{changed: changed, hasChanged: true, source: changed, hasSource: true, cause: RedstoneUpdateCauseBlockUpdate},
		{7, 0, 8}: redstoneDirty{changed: changed, hasChanged: true, source: changed, hasSource: true, cause: RedstoneUpdateCauseBlockUpdate},
		{8, 1, 8}: redstoneDirty{changed: changed, hasChanged: true, source: changed, hasSource: true, cause: RedstoneUpdateCauseBlockUpdate},
		{8, 0, 9}: redstoneDirty{changed: changed, hasChanged: true, source: changed, hasSource: true, cause: RedstoneUpdateCauseBlockUpdate},
		{8, 0, 7}: redstoneDirty{changed: changed, hasChanged: true, source: changed, hasSource: true, cause: RedstoneUpdateCauseBlockUpdate},
	}
	if len(engine.dirty) != len(want) {
		t.Fatalf("dirty positions = %v, want %v", engine.dirty, want)
	}
	for pos, dirty := range want {
		if got, ok := engine.dirty[pos]; !ok || got != dirty {
			t.Fatalf("dirty[%v] = %v, %t; want %v, true", pos, got, ok, dirty)
		}
	}

	engine.invalidateAround(cube.Pos{0, -1, 0}, changed, RedstoneUpdateCauseScheduledTick, cube.Range{0, 1})
	if len(engine.dirty) != len(want) {
		t.Fatalf("out-of-bounds invalidation changed dirty positions to %v, want %v", engine.dirty, want)
	}
	for pos, dirty := range want {
		if got, ok := engine.dirty[pos]; !ok || got != dirty {
			t.Fatalf("dirty[%v] after out-of-bounds invalidation = %v, %t; want %v, true", pos, got, ok, dirty)
		}
	}
}

func TestBlockLoadedRejectsChunkStillGenerating(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	pos := cube.Pos{32, 64, 0}
	chunkPos := chunkPosFromBlockPos(pos)
	runWorld(w, func(tx *Tx) {
		w.chunks[chunkPos] = newColumn(chunk.New(w.conf.Blocks, w.Range()))
		w.chunkCount.Add(1)
		if _, ok := tx.BlockLoaded(pos); ok {
			t.Fatal("BlockLoaded exposed a chunk before generation completed")
		}
	})
}

func TestRedstoneEngineRemoveChunkKeepsUnchangedDirtyOutsideChunk(t *testing.T) {
	engine := newRedstoneEngine(42)
	unloadedChunk := chunkPosFromBlockPos(cube.Pos{0, 64, 0})
	dirtyPos := cube.Pos{32, 64, 0}
	unchanged := redstoneDirty{cause: RedstoneUpdateCauseCompilerRebuild}
	engine.dirty[dirtyPos] = unchanged

	engine.removeChunk(unloadedChunk)
	if got, ok := engine.dirty[dirtyPos]; !ok || got != unchanged {
		t.Fatalf("dirty[%v] after unrelated chunk removal = %v, %t; want %v, true", dirtyPos, got, ok, unchanged)
	}

	changed := redstoneDirty{changed: cube.Pos{0, 64, 0}, hasChanged: true, cause: RedstoneUpdateCauseBlockUpdate}
	engine.dirty[dirtyPos] = changed
	engine.removeChunk(unloadedChunk)
	if _, ok := engine.dirty[dirtyPos]; ok {
		t.Fatalf("dirty[%v] with changed position in unloaded chunk was not removed", dirtyPos)
	}
}

func TestRedstoneEngineRemoveChunkClearsTransientStateInChunk(t *testing.T) {
	engine := newRedstoneEngine(42)
	unloadedPos := cube.Pos{0, 64, 0}
	keptPos := cube.Pos{32, 64, 0}
	unloadedChunk := chunkPosFromBlockPos(unloadedPos)

	engine.power[unloadedPos] = 15
	engine.output[unloadedPos] = 14
	engine.circuits = map[cube.Pos]RedstoneCircuitState{unloadedPos: nil, keptPos: nil}

	engine.power[keptPos] = 13
	engine.output[keptPos] = 12
	engine.removeChunk(unloadedChunk)

	if _, ok := engine.power[unloadedPos]; ok {
		t.Fatalf("power for unloaded position %v was not cleared", unloadedPos)
	}
	if _, ok := engine.output[unloadedPos]; ok {
		t.Fatalf("output for unloaded position %v was not cleared", unloadedPos)
	}
	if _, ok := engine.circuits[unloadedPos]; ok {
		t.Fatalf("capacitor state for unloaded position %v was not cleared", unloadedPos)
	}
	if got := engine.power[keptPos]; got != 13 {
		t.Fatalf("power for kept position = %d, want 13", got)
	}
	if got := engine.output[keptPos]; got != 12 {
		t.Fatalf("output for kept position = %d, want 12", got)
	}
	if _, ok := engine.circuits[keptPos]; !ok {
		t.Fatal("circuit for kept position was cleared")
	}
}

func TestRedstoneDirtyUnloadedNeighbourReconcilesAfterReload(t *testing.T) {
	sourcePos, sinkPos := cube.Pos{15, 64, 0}, cube.Pos{16, 64, 0}
	provider := &redstoneReloadProvider{NopProvider: NopProvider{WorldData: &WorldDataMemory{}}, columns: make(map[ChunkPos]*chunk.Column)}
	w := Config{Synchronous: true, Blocks: redstoneCancellationTestRegistry(), Provider: provider}.New()
	defer w.Close()

	var powered bool
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneCancellationSource{}, nil)
		tx.SetBlock(sinkPos, redstoneCancellationConsumer{}, nil)
		w.redstone.tick(tx, 1)

		sinkChunk := chunkPosFromBlockPos(sinkPos)
		w.closeChunk(tx, sinkChunk, w.chunks[sinkChunk])
		tx.SetBlock(sourcePos, redstoneCancellationSource{Power: 15}, nil)
		w.redstone.tick(tx, 2)
		if tx.ChunkLoaded(sinkChunk) {
			t.Fatal("redstone traversal loaded the neighbouring chunk")
		}
		if _, ok := w.redstone.dirty[sinkPos]; !ok {
			t.Fatal("dirty position was discarded while its chunk was unloaded")
		}

		_ = tx.Block(sinkPos)
		w.redstone.tick(tx, 3)
		powered = tx.Block(sinkPos).(redstoneCancellationConsumer).Powered
	})
	if !powered {
		t.Fatal("sink was not powered after its chunk reloaded")
	}
}

func TestRedstoneCancelledSourceDoesNotPropagate(t *testing.T) {
	sourcePos, sinkPos := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	w := Config{Synchronous: true, Blocks: redstoneCancellationTestRegistry()}.New()
	defer w.Close()

	w.Handle(&redstoneCancellationHandler{cancel: map[cube.Pos]struct{}{sourcePos: {}}})
	var sinkPowered bool
	var sourceOutput int
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneCancellationSource{Power: 15}, nil)
		tx.SetBlock(sinkPos, redstoneCancellationConsumer{}, nil)
		tx.World().redstone.tick(tx, 1)

		sinkPowered = tx.Block(sinkPos).(redstoneCancellationConsumer).Powered
		sourceOutput = tx.World().redstone.output[sourcePos]
	})
	if sinkPowered {
		t.Fatalf("sink powered after cancelling source update")
	}
	if sourceOutput != 0 {
		t.Fatalf("stored source output = %d, want 0", sourceOutput)
	}
}

func TestRedstoneCancelledSourceKeepsPreviousOutputDuringEvaluation(t *testing.T) {
	sourcePos := cube.Pos{0, 64, 0}
	w := Config{Synchronous: true, Blocks: redstoneCancellationTestRegistry()}.New()
	defer w.Close()

	w.Handle(&redstoneCancellationHandler{cancel: map[cube.Pos]struct{}{sourcePos: {}}})

	var sourceOutput int
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneCancellationSource{}, &SetOpts{DisableRedstoneUpdates: true})
		tx.World().redstone.output[sourcePos] = 15
		tx.World().redstone.invalidate(sourcePos, redstoneDirty{changed: sourcePos, hasChanged: true, source: sourcePos, hasSource: true, cause: RedstoneUpdateCauseBlockUpdate}, tx.Range())
		tx.World().redstone.tick(tx, 2)

		sourceOutput = tx.World().redstone.output[sourcePos]
	})
	if sourceOutput != 15 {
		t.Fatalf("stored source output after cancelled update = %d, want 15", sourceOutput)
	}
}

func TestRedstoneCancelledConsumerDoesNotUpdate(t *testing.T) {
	sourcePos, sinkPos := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	w := Config{Synchronous: true, Blocks: redstoneCancellationTestRegistry()}.New()
	defer w.Close()

	w.Handle(&redstoneCancellationHandler{cancel: map[cube.Pos]struct{}{sinkPos: {}}})
	var sinkPowered bool
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneCancellationSource{Power: 15}, nil)
		tx.SetBlock(sinkPos, redstoneCancellationConsumer{}, nil)
		tx.World().redstone.tick(tx, 1)

		sinkPowered = tx.Block(sinkPos).(redstoneCancellationConsumer).Powered
	})
	if sinkPowered {
		t.Fatalf("sink powered after cancelling consumer update")
	}
}

func TestRedstoneUpdateIncludesContextMetadata(t *testing.T) {
	sourcePos, sinkPos := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	w := Config{Synchronous: true, Blocks: redstoneCancellationTestRegistry()}.New()
	defer w.Close()

	handler := &redstoneRecordingHandler{pos: sinkPos}
	w.Handle(handler)
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sinkPos, redstoneCancellationConsumer{}, &SetOpts{DisableRedstoneUpdates: true})
		tx.SetBlock(sourcePos, redstoneCancellationSource{Power: 15}, nil)
		tx.World().redstone.tick(tx, 7)
	})
	if len(handler.updates) == 0 {
		t.Fatal("no redstone update recorded for consumer")
	}
	update := handler.updates[0]
	if update.Pos != sinkPos {
		t.Fatalf("update Pos = %v, want %v", update.Pos, sinkPos)
	}
	if !update.HasChangedNeighbour {
		t.Fatal("update did not record a changed neighbour")
	}
	if !update.ChangedRedstoneRelevant {
		t.Fatal("update did not mark changed neighbour as redstone relevant")
	}
	if !update.SourceRedstoneRelevant {
		t.Fatal("update did not preserve the original source's redstone relevance")
	}
	if !update.HasSource || update.Source != sourcePos {
		t.Fatalf("update source = %v, %t; want %v, true", update.Source, update.HasSource, sourcePos)
	}
	if update.OldPower != 0 || update.NewPower != 15 {
		t.Fatalf("update power = old:%d new:%d, want old:0 new:15", update.OldPower, update.NewPower)
	}
	if update.CurrentTick != 7 {
		t.Fatalf("update CurrentTick = %d, want 7", update.CurrentTick)
	}
	if update.Cause != RedstoneUpdateCauseBlockUpdate {
		t.Fatalf("update Cause = %d, want RedstoneUpdateCauseBlockUpdate", update.Cause)
	}
	if _, ok := update.Before.(redstoneCancellationConsumer); !ok {
		t.Fatalf("update Before = %T, want redstoneCancellationConsumer", update.Before)
	}
	if after, ok := update.After.(redstoneCancellationConsumer); !ok || !after.Powered {
		t.Fatalf("update After = %T %#v, want powered redstoneCancellationConsumer", update.After, update.After)
	}
}

func TestRedstoneRecursiveSourceEvaluationReturnsZero(t *testing.T) {
	sourcePos, targetPos := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	w := Config{Synchronous: true, Blocks: redstoneRecursiveSourceTestRegistry()}.New()
	defer w.Close()

	var power int
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneRecursiveSource{Target: targetPos}, nil)
		power = tx.RedstonePower(targetPos)
	})
	if power != 0 {
		t.Fatalf("recursive source power = %d, want 0", power)
	}
}

func TestRedstoneCancelledActionDoesNotRun(t *testing.T) {
	sourcePos, actionPos := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	w := Config{Synchronous: true, Blocks: redstoneCancellationTestRegistry()}.New()
	defer w.Close()

	actions := 0
	redstoneCancellationActions = &actions
	t.Cleanup(func() {
		redstoneCancellationActions = nil
	})
	w.Handle(&redstoneCancellationHandler{cancel: map[cube.Pos]struct{}{actionPos: {}}})
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneCancellationSource{Power: 15}, nil)
		tx.SetBlock(actionPos, redstoneCancellationAction{}, nil)
		tx.World().redstone.tick(tx, 1)
	})
	if actions != 0 {
		t.Fatalf("actions = %d, want 0", actions)
	}
}

func TestRedstoneActionOnlyRunsOnPowerChange(t *testing.T) {
	sourcePos, actionPos := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	w := Config{Synchronous: true, Blocks: redstoneCancellationTestRegistry()}.New()
	defer w.Close()

	actions := 0
	redstoneCancellationActions = &actions
	t.Cleanup(func() {
		redstoneCancellationActions = nil
	})
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneCancellationSource{Power: 15}, nil)
		tx.SetBlock(actionPos, redstoneCancellationAction{}, nil)
		tx.World().redstone.tick(tx, 1)
		tx.World().redstone.invalidate(actionPos, redstoneDirty{cause: RedstoneUpdateCauseBlockUpdate}, tx.Range())
		tx.World().redstone.tick(tx, 2)
	})
	if actions != 1 {
		t.Fatalf("actions after same-power dirty evaluation = %d, want 1", actions)
	}
}

func TestRedstoneActionReplacingSelfDoesNotRestorePowerCache(t *testing.T) {
	sourcePos, actionPos := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	w := Config{Synchronous: true, Blocks: redstoneCancellationTestRegistry()}.New()
	defer w.Close()

	redstoneCancellationActionReplaceSelf = true
	t.Cleanup(func() { redstoneCancellationActionReplaceSelf = false })
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneCancellationSource{Power: 15}, nil)
		tx.SetBlock(actionPos, redstoneCancellationAction{}, nil)
		w.redstone.tick(tx, 1)
		if _, ok := w.redstone.power[actionPos]; ok {
			t.Fatal("self-replacing redstone action restored stale power cache")
		}
	})
}

func TestRedstoneRelayerToSinkDoesNotLosePower(t *testing.T) {
	sourcePos, relayerPos, sinkPos := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}, cube.Pos{2, 64, 0}
	w := Config{Synchronous: true, Blocks: redstoneSignalLossTestRegistry()}.New()
	defer w.Close()

	var directPower, sinkPower int
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneLossSource{Power: 15}, nil)
		tx.SetBlock(relayerPos, redstoneLossRelayer{}, nil)
		tx.SetBlock(sinkPos, redstoneLossConsumer{}, nil)

		directPower = tx.RedstonePower(sinkPos)
		tx.World().redstone.tick(tx, 1)
		if sink, ok := tx.Block(sinkPos).(redstoneLossConsumer); ok {
			sinkPower = sink.Power
		}
	})
	if directPower != 15 {
		t.Fatalf("powerFrom through relayer into sink = %d, want 15", directPower)
	}
	if sinkPower != 15 {
		t.Fatalf("graph power through relayer into sink = %d, want 15", sinkPower)
	}
}

func TestRedstoneVerticalRelayerPropagation(t *testing.T) {
	tests := []struct {
		name  string
		from  cube.Pos
		to    cube.Pos
		block Block
		want  int
	}{
		{name: "glowstone upward", from: cube.Pos{1, 64, 0}, to: cube.Pos{0, 65, 0}, block: redstoneLadderGlowstone{}, want: 14},
		{name: "glowstone downward", from: cube.Pos{0, 65, 0}, to: cube.Pos{1, 64, 0}, block: redstoneLadderGlowstone{}, want: 0},
		{name: "glass downward", from: cube.Pos{0, 65, 0}, to: cube.Pos{1, 64, 0}, block: redstoneLadderGlass{}, want: 14},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := Config{Synchronous: true, Blocks: redstoneVerticalRelayerTestRegistry()}.New()
			defer w.Close()

			var got int
			runWorld(w, func(tx *Tx) {
				low, high := cube.Pos{1, 64, 0}, cube.Pos{0, 65, 0}
				source := test.from.Side(cube.FaceNorth)
				tx.SetBlock(low, redstoneVerticalRelayer{Power: 0}, nil)
				tx.SetBlock(high, redstoneVerticalRelayer{Power: 0}, nil)
				tx.SetBlock(high.Side(cube.FaceDown), test.block, nil)
				tx.SetBlock(source, redstoneVerticalSource{}, nil)

				tx.World().redstone.tick(tx, 1)
				got = tx.Block(test.to).(redstoneVerticalRelayer).Power
			})
			if got != test.want {
				t.Fatalf("propagated power = %d, want %d", got, test.want)
			}
		})
	}
}

func TestPlacedRedstoneTorchTurnsOffWhenAttachmentBecomesPowered(t *testing.T) {
	w := Config{Synchronous: true, Blocks: redstoneTorchAttachmentTestRegistry()}.New()
	defer w.Close()

	torchPos := cube.Pos{1, 64, 0}
	attachmentPos := torchPos.Side(cube.FaceWest)
	var lit bool
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(attachmentPos, redstoneSolidBlock{}, nil)
		tx.SetBlock(torchPos, redstoneAttachmentTorch{Facing: cube.FaceWest, Lit: true}, nil)
		tx.SetBlock(attachmentPos.Side(cube.FaceNorth), redstoneWeakBlockSource{}, nil)

		tx.World().scheduledBlockTicks.tick(tx, 2)
		tx.World().scheduledFluidTicks.tick(tx, 2)
		tx.World().redstone.tick(tx, 2)
		lit = tx.Block(torchPos).(redstoneAttachmentTorch).Lit
	})

	if lit {
		t.Fatal("torch stayed lit after its attachment block became powered")
	}
}

func TestRedstoneConsumerUpdatesBehindPoweredConductor(t *testing.T) {
	w := Config{Synchronous: true, Blocks: redstonePoweredConductorTestRegistry()}.New()
	defer w.Close()

	sourcePos, conductorPos, consumerPos := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}, cube.Pos{2, 64, 0}
	var powered bool
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneStrongSource{}, nil)
		tx.SetBlock(conductorPos, redstoneSolidBlock{}, nil)
		tx.SetBlock(consumerPos, redstoneCancellationConsumer{}, nil)

		tx.World().redstone.tick(tx, 1)
		powered = tx.Block(consumerPos).(redstoneCancellationConsumer).Powered
	})

	if !powered {
		t.Fatal("consumer behind strongly powered conductor was not powered")
	}
}

func TestWeaklyPoweredConductorActivatesConsumerButNotDust(t *testing.T) {
	w := Config{Synchronous: true, Blocks: redstoneWeakConductorTestRegistry()}.New()
	defer w.Close()

	sourcePos := cube.Pos{0, 64, 0}
	conductorPos := sourcePos.Side(cube.FaceEast)
	consumerPos := conductorPos.Side(cube.FaceEast)
	dustPos := conductorPos.Side(cube.FaceSouth)
	var consumerPowered bool
	var dustPower int
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneWeakBlockSource{}, nil)
		tx.SetBlock(conductorPos, redstoneSolidBlock{}, nil)
		tx.SetBlock(consumerPos, redstoneCancellationConsumer{}, nil)
		tx.SetBlock(dustPos, redstoneVerticalRelayer{}, nil)

		tx.World().redstone.tick(tx, 1)
		consumerPowered = tx.Block(consumerPos).(redstoneCancellationConsumer).Powered
		dustPower = tx.Block(dustPos).(redstoneVerticalRelayer).Power
	})

	if !consumerPowered {
		t.Fatal("consumer behind weakly powered conductor was not powered")
	}
	if dustPower != 0 {
		t.Fatalf("dust behind weakly powered conductor = %d, want 0", dustPower)
	}
}

func TestDirectSourceDoesNotWeakPowerConductor(t *testing.T) {
	w := Config{Synchronous: true, Blocks: redstonePoweredConductorTestRegistry()}.New()
	defer w.Close()

	sourcePos, conductorPos, consumerPos := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}, cube.Pos{2, 64, 0}
	var powered bool
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(sourcePos, redstoneCancellationSource{Power: 15}, nil)
		tx.SetBlock(conductorPos, redstoneSolidBlock{}, nil)
		tx.SetBlock(consumerPos, redstoneCancellationConsumer{}, nil)

		tx.World().redstone.tick(tx, 1)
		powered = tx.Block(consumerPos).(redstoneCancellationConsumer).Powered
	})

	if powered {
		t.Fatal("direct-only source weak-powered a conductor")
	}
}

func TestScheduledTickQueueDuplicateScheduling(t *testing.T) {
	tests := []struct {
		name     string
		delays   []time.Duration
		wantTick int64
	}{
		{name: "keeps first earlier tick", delays: []time.Duration{time.Second / 20, time.Second / 10}, wantTick: 101},
		{name: "keeps first later tick", delays: []time.Duration{time.Second / 10, time.Second / 20}, wantTick: 102},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := scheduledTickTestRegistry()
			registry.Finalize()
			queue := newScheduledTickQueue(100)
			pos := cube.Pos{8, 64, 8}
			b := scheduledTickTestBlock{}

			for _, delay := range test.delays {
				queue.schedule(registry, pos, b, delay)
			}

			index := scheduledTickIndex{pos: pos, typeName: scheduledTickType(registry, b)}
			if got, want := queue.scheduledTicks[index], test.wantTick; got != want {
				t.Fatalf("scheduled tick = %d, want %d", got, want)
			}
			ticks := queue.fromChunk(chunkPosFromBlockPos(pos))
			if len(ticks) != 1 || ticks[0].Delay != int32(test.wantTick-100) {
				t.Fatalf("saved ticks = %v, want remaining delay %d", ticks, test.wantTick-100)
			}
		})
	}
}
func TestScheduledTickZeroDelayPauseAndDestinationOwnership(t *testing.T) {
	registry := scheduledTickTestRegistry()
	registry.Finalize()
	pos := cube.Pos{64, 64, 8}
	destination := chunkPosFromBlockPos(pos)
	source := ChunkPos{2, 0}
	b := scheduledTickTestBlock{}

	generated := newScheduledTickQueue(100)
	generated.scheduleAt(registry, pos, b, generated.currentTick, 0)
	if ticks := generated.fromChunk(destination); len(ticks) != 1 || ticks[0].Delay != 0 {
		t.Fatalf("generated destination ticks = %v, want one zero-delay tick", ticks)
	}
	if ticks := generated.fromChunk(source); len(ticks) != 0 {
		t.Fatalf("source chunk owns generated ticks = %v, want none", ticks)
	}

	live := newScheduledTickQueue(100)
	live.schedule(registry, pos, b, 3*time.Second/20)
	saved := live.fromChunk(destination)
	if len(saved) != 1 || saved[0].Delay != 3 {
		t.Fatalf("saved live ticks = %v, want one tick with remaining delay 3", saved)
	}
	live.removeChunk(destination)

	reloaded := newScheduledTickQueue(900)
	reloaded.add([]scheduledTick{{
		pos: pos, btype: saved[0].Type, t: reloaded.currentTick + int64(saved[0].Delay),
		priority: saved[0].Priority,
	}})
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()
	tickCount := 0
	scheduledTickTestBlockTicks = &tickCount
	t.Cleanup(func() { scheduledTickTestBlockTicks = nil })
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(pos, b, nil)
		reloaded.tick(tx, 902)
		if tickCount != 0 {
			t.Fatalf("paused live tick ran early at 902: count=%d", tickCount)
		}
		reloaded.tick(tx, 903)
	})
	if tickCount != 1 {
		t.Fatalf("reloaded live ticks ran %d times, want once at remaining delay 3", tickCount)
	}
}

func TestScheduledTickPriorityAndStableOrder(t *testing.T) {
	registry := scheduledTickTestRegistry()
	registry.Finalize()
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()
	queue := newScheduledTickQueue(100)
	positions := []cube.Pos{{1, 64, 1}, {2, 64, 1}, {3, 64, 1}}
	b := scheduledTickTestBlock{}
	var order []cube.Pos
	scheduledTickTestBlockOrder = &order
	t.Cleanup(func() { scheduledTickTestBlockOrder = nil })

	runWorld(w, func(tx *Tx) {
		for _, pos := range positions {
			tx.SetBlock(pos, b, nil)
		}
		queue.scheduleAt(registry, positions[0], b, 101, 0)
		queue.scheduleAt(registry, positions[1], b, 101, -2)
		queue.scheduleAt(registry, positions[2], b, 101, 0)
		queue.tick(tx, 101)
	})
	want := []cube.Pos{positions[1], positions[0], positions[2]}
	if !slices.Equal(order, want) {
		t.Fatalf("scheduled callback order = %v, want priority then stable insertion order %v", order, want)
	}
}

func TestScheduledTicksMergeChunkHeadsByPriority(t *testing.T) {
	registry := scheduledTickTestRegistry()
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()
	queue := newScheduledTickQueue(100)
	first := cube.Pos{1, 64, 1}
	firstChunkHigh := cube.Pos{2, 64, 1}
	otherChunkHigh := cube.Pos{17, 64, 1}
	b := scheduledTickTestBlock{}
	var order []cube.Pos
	scheduledTickTestBlockOrder = &order
	t.Cleanup(func() { scheduledTickTestBlockOrder = nil })

	runWorld(w, func(tx *Tx) {
		tx.SetBlock(first, b, nil)
		tx.SetBlock(firstChunkHigh, b, nil)
		tx.SetBlock(otherChunkHigh, b, nil)
		queue.scheduleAt(registry, first, b, 100, 0)
		queue.scheduleAt(registry, firstChunkHigh, b, 101, -2)
		queue.scheduleAt(registry, otherChunkHigh, b, 101, -2)
		queue.tick(tx, 105)
	})
	want := []cube.Pos{otherChunkHigh, first, firstChunkHigh}
	if !slices.Equal(order, want) {
		t.Fatalf("scheduled callbacks = %v, want per-chunk trigger order merged across chunks by priority/subtick %v", order, want)
	}
}

func TestScheduledTicksWaitForLoadedTickingDestination(t *testing.T) {
	registry := scheduledTickTestRegistry()
	w := Config{Blocks: registry}.New()
	defer w.Close()
	queue := newScheduledTickQueue(0)
	pos := cube.Pos{16, 64, 8}
	source, destination := ChunkPos{0, 0}, ChunkPos{1, 0}
	loader := &Loader{r: 0, pos: source}
	b := scheduledTickTestBlock{}
	ticks := 0
	scheduledTickTestBlockTicks = &ticks
	t.Cleanup(func() { scheduledTickTestBlockTicks = nil })

	runWorld(w, func(tx *Tx) {
		tx.chunk(source)
		queue.scheduleAt(registry, pos, b, 1, 0)
		queue.tick(tx, 1, loader)
		if w.chunkLoaded(destination) || ticks != 0 || queue.listed() != 1 {
			t.Fatalf("unloaded destination after collection: loaded=%t callbacks=%d pending=%d", w.chunkLoaded(destination), ticks, queue.listed())
		}

		tx.SetBlock(pos, b, nil)
		queue.tick(tx, 2, loader)
		if ticks != 0 || queue.listed() != 1 {
			t.Fatalf("non-ticking destination after collection: callbacks=%d pending=%d", ticks, queue.listed())
		}

		loader.pos = destination
		queue.tick(tx, 3, loader)
		if ticks != 0 || queue.listed() != 1 {
			t.Fatalf("destination without FULL neighbours after collection: callbacks=%d pending=%d", ticks, queue.listed())
		}
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				tx.chunk(ChunkPos{destination[0] + int32(dx), destination[1] + int32(dz)})
			}
		}
		queue.tick(tx, 4, loader)
	})
	if ticks != 1 {
		t.Fatalf("scheduled callback count after destination became ticking = %d, want 1", ticks)
	}

	synchronous := Config{Synchronous: true, Blocks: registry}.New()
	defer synchronous.Close()
	queue = newScheduledTickQueue(0)
	ticks = 0
	loader.pos = source
	runWorld(synchronous, func(tx *Tx) {
		tx.SetBlock(pos, b, nil)
		queue.scheduleAt(registry, pos, b, 1, 0)
		queue.tick(tx, 1, loader)
	})
	if ticks != 1 {
		t.Fatalf("synchronous world failed to tick a loaded destination outside loader area: callbacks=%d", ticks)
	}
}

func TestScheduledTickBatchClearsIdentitiesBeforeCallbacks(t *testing.T) {
	registry := scheduledTickTestRegistry()
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()
	queue := newScheduledTickQueue(100)
	first, later := cube.Pos{1, 64, 1}, cube.Pos{2, 64, 1}
	b := scheduledTickTestBlock{}
	ticks := 0
	scheduledTickTestBlockTicks = &ticks
	scheduledTickTestBlockReschedule = func(pos cube.Pos, tx *Tx) {
		if pos == first {
			queue.schedule(registry, later, b, time.Second/20)
		}
	}
	t.Cleanup(func() {
		scheduledTickTestBlockTicks = nil
		scheduledTickTestBlockReschedule = nil
	})

	var afterFirst []chunk.SavedScheduledTick
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(first, b, nil)
		tx.SetBlock(later, b, nil)
		queue.scheduleAt(registry, first, b, 101, 0)
		queue.scheduleAt(registry, later, b, 101, 0)
		queue.tick(tx, 101)
		afterFirst = queue.fromChunk(ChunkPos{})
		if ticks != 2 {
			t.Fatalf("collected callbacks = %d, want 2", ticks)
		}
		queue.tick(tx, 102)
	})
	if len(afterFirst) != 1 || afterFirst[0].Pos != later || afterFirst[0].Delay != 1 {
		t.Fatalf("replacement tick after collection = %v, want one later-position tick delayed by 1", afterFirst)
	}
	if ticks != 3 {
		t.Fatalf("callback count after replacement ran = %d, want 3", ticks)
	}
}

func TestScheduledTickBudgetAndUnpackedOrder(t *testing.T) {
	registry := scheduledTickTestRegistry()
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()
	queue := newScheduledTickQueue(0)
	positions := []cube.Pos{{1, 64, 1}, {2, 64, 1}, {3, 64, 1}}
	b := scheduledTickTestBlock{}
	var order []cube.Pos
	scheduledTickTestBlockOrder = &order
	t.Cleanup(func() { scheduledTickTestBlockOrder = nil })

	runWorld(w, func(tx *Tx) {
		for _, pos := range positions {
			tx.SetBlock(pos, b, nil)
		}
		queue.add([]scheduledTick{
			{pos: positions[0], btype: "test:scheduled_tick", t: 1},
			{pos: positions[1], btype: "test:scheduled_tick", t: 1},
		})
		queue.scheduleAt(registry, positions[2], b, 1, 0)
		queue.tick(tx, 1)
		if !slices.Equal(order, positions) {
			t.Fatalf("saved/unpacked callback order = %v, want saved list order before new tick %v", order, positions)
		}
	})

	large := newScheduledTickQueue(0)
	tickCount := maxScheduledTicksPerTick + 1
	minY := w.Range()[0]
	for i := range tickCount {
		pos := cube.Pos{i & 15, minY + i/(16*16), (i / 16) & 15}
		tick := scheduledTick{pos: pos, btype: "test:scheduled_tick", t: 1, order: int64(i)}
		large.push(tick)
		large.scheduledTicks[scheduledTickIndex{pos: pos, typeName: tick.btype}] = 1
	}
	runWorld(w, func(tx *Tx) {
		tx.chunk(ChunkPos{})
		large.tick(tx, 1)
		if large.listed() != 1 || len(large.scheduledTicks) != 1 {
			t.Fatalf("ticks left after one capped drain = %d, identities=%d, want 1 each", large.listed(), len(large.scheduledTicks))
		}
		large.tick(tx, 2)
		if large.listed() != 0 || len(large.scheduledTicks) != 0 {
			t.Fatalf("ticks after second drain = %d, identities=%d, want empty", large.listed(), len(large.scheduledTicks))
		}
	})
}

func TestScheduledTickChangesMarkDestinationDirty(t *testing.T) {
	registry := scheduledTickTestRegistry()
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()
	pos := cube.Pos{8, 64, 8}
	b := scheduledTickTestBlock{}
	runWorld(w, func(tx *Tx) {
		col := tx.chunk(chunkPosFromBlockPos(pos))
		col.modified = false
		tx.ScheduleBlockUpdate(pos, b, time.Second/20)
		if !col.modified {
			t.Fatal("scheduling a tick did not dirty its loaded destination column")
		}
		col.modified = false
		tx.ScheduleBlockUpdate(pos, b, time.Second/20)
		if col.modified {
			t.Fatal("duplicate scheduling dirtied a column without changing its pending tick set")
		}
		col.modified = false
		w.scheduledBlockTicks.tick(tx, w.scheduledBlockTicks.currentTick+1)
		if !col.modified {
			t.Fatal("collecting a tick did not dirty its loaded destination column")
		}
	})
}

func TestScheduledTickQueueRemoveChunkClearsSchedule(t *testing.T) {
	registry := scheduledTickTestRegistry()
	registry.Finalize()
	queue := newScheduledTickQueue(100)
	pos := cube.Pos{8, 64, 8}
	b := scheduledTickTestBlock{}

	queue.schedule(registry, pos, b, time.Second)
	queue.removeChunk(chunkPosFromBlockPos(pos))

	if queue.listed() != 0 {
		t.Fatalf("ticks after removeChunk = %d, want empty", queue.listed())
	}
	if len(queue.scheduledTicks) != 0 {
		t.Fatalf("scheduled ticks after removeChunk = %v, want empty", queue.scheduledTicks)
	}
}

func TestScheduledTickQueueCanRescheduleFromScheduledCallback(t *testing.T) {
	registry := scheduledTickTestRegistry()
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()

	queue := newScheduledTickQueue(100)
	pos := cube.Pos{8, 64, 8}
	b := scheduledTickTestBlock{}
	ticks := 0
	scheduledTickTestBlockTicks = &ticks
	var savedDuringCallback []chunk.SavedScheduledTick
	scheduledTickTestBlockReschedule = func(pos cube.Pos, tx *Tx) {
		if ticks == 1 {
			queue.schedule(registry, pos, b, time.Second/20)
			savedDuringCallback = queue.fromChunk(chunkPosFromBlockPos(pos))
		}
	}
	t.Cleanup(func() {
		scheduledTickTestBlockTicks = nil
		scheduledTickTestBlockReschedule = nil
	})

	var activeAfterFirst, activeAfterSecond []chunk.SavedScheduledTick
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(pos, b, nil)
		queue.schedule(registry, pos, b, time.Second/20)
		queue.tick(tx, 101)
		activeAfterFirst = queue.fromChunk(chunkPosFromBlockPos(pos))
		queue.tick(tx, 102)
		activeAfterSecond = queue.fromChunk(chunkPosFromBlockPos(pos))
	})
	if ticks != 2 {
		t.Fatalf("scheduled callback executed %d times, want 2", ticks)
	}
	if len(activeAfterFirst) != 1 || activeAfterFirst[0].Delay != 1 {
		t.Fatalf("active ticks after callback rescheduled = %v, want only one tick with remaining delay 1", activeAfterFirst)
	}
	if len(activeAfterSecond) != 0 {
		t.Fatalf("active ticks after second callback = %v, want empty", activeAfterSecond)
	}
	if len(savedDuringCallback) != 1 {
		t.Fatalf("snapshot inside callback lost its continuation: %v", savedDuringCallback)
	}
	restored := newScheduledTickQueue(900)
	saved := savedDuringCallback[0]
	restored.add([]scheduledTick{{pos: saved.Pos, btype: saved.Type, t: 900 + int64(saved.Delay), priority: saved.Priority}})
	runWorld(w, func(tx *Tx) {
		restored.tick(tx, 900)
		if ticks != 2 {
			t.Errorf("restored continuation ran before its remaining delay: callbacks=%d", ticks)
		}
		restored.tick(tx, 901)
	})
	if ticks != 3 {
		t.Fatalf("restored in-callback continuation callbacks=%d, want 3", ticks)
	}
}

func TestScheduledTickCancelledWhenNativePropertyChangesJavaBlockType(t *testing.T) {
	registry := NewBlockRegistry()
	head := scheduledTickTypeVariant{head: true}
	stem := scheduledTickTypeVariant{}
	for _, block := range []scheduledTickTypeVariant{head, stem} {
		name, properties := block.EncodeBlock()
		registry.RegisterBlockState(BlockState{Name: name, Properties: properties})
		registry.RegisterBlock(block)
	}
	registry.Finalize()
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()

	pos := cube.Pos{8, 64, 8}
	queue := newScheduledTickQueue(100)
	ticks := 0
	head.ticks = &ticks
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(pos, head, nil)
		queue.schedule(registry, pos, head, time.Second/20)
		tx.SetBlock(pos, stem, nil)
		queue.tick(tx, 101)
	})
	if ticks != 0 {
		t.Fatalf("scheduled tick ran %d times after native state changed Java block type, want 0", ticks)
	}
}

func TestScheduledTicksAdvancePerDimension(t *testing.T) {
	registry := scheduledTickTestRegistry()
	provider := NopProvider{Set: defaultSettings()}
	worlds := []*World{
		Config{Provider: provider, Blocks: registry, Dim: Overworld, Synchronous: true}.New(),
		Config{Provider: provider, Blocks: registry, Dim: Nether, Synchronous: true}.New(),
		Config{Provider: provider, Blocks: registry, Dim: End, Synchronous: true}.New(),
		Config{Provider: provider, Blocks: registry, Dim: registeredTestDimension(t), Synchronous: true}.New(),
	}
	for _, w := range worlds {
		defer w.Close()
	}

	pos := cube.Pos{8, 64, 8}
	b := scheduledTickTestBlock{}
	ticks := 0
	scheduledTickTestBlockTicks = &ticks
	t.Cleanup(func() { scheduledTickTestBlockTicks = nil })
	starts := make([]int64, len(worlds))
	for i, w := range worlds {
		starts[i] = w.CurrentTick()
		<-w.Exec(func(tx *Tx) {
			tx.SetBlock(pos, b, &SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
			tx.ScheduleBlockUpdate(pos, b, time.Second/10)
		})
	}

	sharedTick := worlds[0].CurrentTick()
	for i, w := range worlds {
		w.AdvanceTick()
		if ticks != i {
			t.Fatalf("scheduled updates after first %v tick = %d, want %d", w.Dimension(), ticks, i)
		}
		w.AdvanceTick()
		if ticks != i+1 {
			t.Fatalf("scheduled updates after second %v tick = %d, want %d", w.Dimension(), ticks, i+1)
		}
		if got, want := w.CurrentTick(), starts[i]+2; got != want {
			t.Fatalf("world %v current tick = %d, want %d", w.Dimension(), got, want)
		}
	}
	if got, want := provider.Set.CurrentTick, sharedTick+2; got != want {
		t.Fatalf("shared current tick = %d, want %d", got, want)
	}
}

type scheduledTickTestBlock struct{}

// Test counters below are package-level because block instances are registry values; tests using them must stay serial.
var (
	scheduledTickTestBlockTicks      *int
	scheduledTickTestBlockOrder      *[]cube.Pos
	scheduledTickTestBlockReschedule func(cube.Pos, *Tx)
)

func (scheduledTickTestBlock) ScheduledTick(pos cube.Pos, tx *Tx, _ *rand.Rand) {
	if scheduledTickTestBlockTicks != nil {
		(*scheduledTickTestBlockTicks)++
	}
	if scheduledTickTestBlockOrder != nil {
		*scheduledTickTestBlockOrder = append(*scheduledTickTestBlockOrder, pos)
	}
	if scheduledTickTestBlockReschedule != nil {
		scheduledTickTestBlockReschedule(pos, tx)
	}
}
func (scheduledTickTestBlock) EncodeBlock() (string, map[string]any) {
	return "test:scheduled_tick", nil
}
func (scheduledTickTestBlock) Hash() (uint64, uint64) { return 1 << 40, 0 }

type scheduledTickTypeVariant struct {
	head  bool
	ticks *int
}

func (b scheduledTickTypeVariant) ScheduledTick(cube.Pos, *Tx, *rand.Rand) {
	if b.ticks != nil {
		(*b.ticks)++
	}
}
func (b scheduledTickTypeVariant) EncodeBlock() (string, map[string]any) {
	return "minecraft:big_dripleaf", map[string]any{"big_dripleaf_head": b.head}
}
func (b scheduledTickTypeVariant) Hash() (uint64, uint64) {
	if b.head {
		return 1 << 58, 1
	}
	return 1 << 58, 0
}
func (scheduledTickTypeVariant) Model() BlockModel { return nil }
func (scheduledTickTestBlock) Model() BlockModel   { return nil }

func scheduledTickTestRegistry() BlockRegistry {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:scheduled_tick", Properties: map[string]any{}})
	registry.RegisterBlock(scheduledTickTestBlock{})
	return registry
}

type redstoneNeighbourOrderTestBlock struct {
	neighbours []cube.Pos
}

func (b redstoneNeighbourOrderTestBlock) RedstoneRelayerNeighbours(cube.Pos, *Tx) []cube.Pos {
	return slices.Clone(b.neighbours)
}
func (redstoneNeighbourOrderTestBlock) EncodeBlock() (string, map[string]any) {
	return "test:redstone_neighbour_order", nil
}
func (redstoneNeighbourOrderTestBlock) Hash() (uint64, uint64) { return 1 << 41, 0 }
func (redstoneNeighbourOrderTestBlock) Model() BlockModel      { return nil }

type redstoneCancellationHandler struct {
	NopHandler
	cancel map[cube.Pos]struct{}
}

func (h *redstoneCancellationHandler) HandleRedstoneUpdate(ctx *Context, update RedstoneUpdate) {
	if _, ok := h.cancel[update.Pos]; ok {
		ctx.Cancel()
	}
}

type redstoneRecordingHandler struct {
	NopHandler
	pos     cube.Pos
	updates []RedstoneUpdate
}

func (h *redstoneRecordingHandler) HandleRedstoneUpdate(_ *Context, update RedstoneUpdate) {
	if update.Pos == h.pos {
		h.updates = append(h.updates, update)
	}
}

var redstoneCancellationActions *int
var redstoneCancellationActionReplaceSelf bool

type redstoneReloadProvider struct {
	NopProvider
	columns map[ChunkPos]*chunk.Column
}

func (p *redstoneReloadProvider) LoadColumn(pos ChunkPos, _ Dimension) (*chunk.Column, error) {
	if col, ok := p.columns[pos]; ok {
		return col, nil
	}
	return nil, leveldb.ErrNotFound
}

func (p *redstoneReloadProvider) StoreColumn(pos ChunkPos, _ Dimension, col *chunk.Column) error {
	p.columns[pos] = col
	return nil
}

func redstoneCancellationTestRegistry() BlockRegistry {
	registry := NewBlockRegistry()
	for _, power := range []int32{0, 15} {
		registry.RegisterBlockState(BlockState{Name: "test:redstone_source", Properties: map[string]any{"power": power}})
		registry.RegisterBlock(redstoneCancellationSource{Power: int(power)})
	}
	for _, powered := range []bool{false, true} {
		registry.RegisterBlockState(BlockState{Name: "test:redstone_consumer", Properties: map[string]any{"powered": powered}})
		registry.RegisterBlock(redstoneCancellationConsumer{Powered: powered})
	}
	registry.RegisterBlockState(BlockState{Name: "test:redstone_action", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneCancellationAction{})
	return registry
}

type redstoneCancellationSource struct {
	Power int
}

func (b redstoneCancellationSource) RedstonePower(cube.Pos, *Tx, cube.Face) int {
	return b.Power
}
func (b redstoneCancellationSource) EncodeBlock() (string, map[string]any) {
	return "test:redstone_source", map[string]any{"power": int32(b.Power)}
}
func (b redstoneCancellationSource) Hash() (uint64, uint64) {
	return 1 << 42, uint64(b.Power)
}
func (redstoneCancellationSource) Model() BlockModel { return redstoneCancellationModel{} }

func redstoneRecursiveSourceTestRegistry() BlockRegistry {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:redstone_recursive_source", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneRecursiveSource{})
	return registry
}

type redstoneRecursiveSource struct {
	Target cube.Pos
}

func (b redstoneRecursiveSource) RedstonePower(_ cube.Pos, tx *Tx, _ cube.Face) int {
	return tx.RedstonePower(b.Target)
}
func (redstoneRecursiveSource) EncodeBlock() (string, map[string]any) {
	return "test:redstone_recursive_source", nil
}
func (redstoneRecursiveSource) Hash() (uint64, uint64) { return 1 << 57, 0 }
func (redstoneRecursiveSource) Model() BlockModel      { return redstoneCancellationModel{} }

type redstoneCancellationConsumer struct {
	Powered bool
}

func (b redstoneCancellationConsumer) RedstonePowerUpdate(_ cube.Pos, _ *Tx, power int) (Block, bool) {
	powered := power > 0
	if b.Powered == powered {
		return b, false
	}
	b.Powered = powered
	return b, true
}
func (b redstoneCancellationConsumer) EncodeBlock() (string, map[string]any) {
	return "test:redstone_consumer", map[string]any{"powered": b.Powered}
}
func (b redstoneCancellationConsumer) Hash() (uint64, uint64) {
	if b.Powered {
		return 1 << 43, 1
	}
	return 1 << 43, 0
}
func (redstoneCancellationConsumer) Model() BlockModel { return redstoneCancellationModel{} }

type redstoneCancellationAction struct{}

func (redstoneCancellationAction) RedstonePowerAction(pos cube.Pos, tx *Tx, _, _ int) {
	if redstoneCancellationActions != nil {
		(*redstoneCancellationActions)++
	}
	if redstoneCancellationActionReplaceSelf {
		tx.SetBlock(pos, redstoneCancellationConsumer{}, nil)
	}
}
func (redstoneCancellationAction) EncodeBlock() (string, map[string]any) {
	return "test:redstone_action", nil
}
func (redstoneCancellationAction) Hash() (uint64, uint64) { return 1 << 44, 0 }
func (redstoneCancellationAction) Model() BlockModel      { return redstoneCancellationModel{} }

type redstoneCancellationModel struct{}

func (redstoneCancellationModel) BBox(cube.Pos, BlockSource) []cube.BBox { return nil }
func (redstoneCancellationModel) FaceSolid(cube.Pos, cube.Face, BlockSource) bool {
	return false
}

func redstoneSignalLossTestRegistry() BlockRegistry {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:redstone_loss_source", Properties: map[string]any{"power": int32(15)}})
	registry.RegisterBlock(redstoneLossSource{Power: 15})
	registry.RegisterBlockState(BlockState{Name: "test:redstone_loss_relayer", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneLossRelayer{})
	for power := int32(0); power <= 15; power++ {
		registry.RegisterBlockState(BlockState{Name: "test:redstone_loss_consumer", Properties: map[string]any{"power": power}})
		registry.RegisterBlock(redstoneLossConsumer{Power: int(power)})
	}
	return registry
}

type redstoneLossSource struct {
	Power int
}

func (b redstoneLossSource) RedstonePower(cube.Pos, *Tx, cube.Face) int {
	return b.Power
}
func (b redstoneLossSource) EncodeBlock() (string, map[string]any) {
	return "test:redstone_loss_source", map[string]any{"power": int32(b.Power)}
}
func (b redstoneLossSource) Hash() (uint64, uint64) {
	return 1 << 45, uint64(b.Power)
}
func (redstoneLossSource) Model() BlockModel { return redstoneCancellationModel{} }

type redstoneLossRelayer struct{}

func (redstoneLossRelayer) RedstoneSignalLoss(cube.Pos, *Tx) int {
	return 1
}
func (redstoneLossRelayer) EncodeBlock() (string, map[string]any) {
	return "test:redstone_loss_relayer", nil
}
func (redstoneLossRelayer) Hash() (uint64, uint64) { return 1 << 46, 0 }
func (redstoneLossRelayer) Model() BlockModel      { return redstoneCancellationModel{} }

type redstoneLossConsumer struct {
	Power int
}

func (b redstoneLossConsumer) RedstonePowerUpdate(_ cube.Pos, _ *Tx, power int) (Block, bool) {
	if b.Power == power {
		return b, false
	}
	b.Power = power
	return b, true
}
func (b redstoneLossConsumer) EncodeBlock() (string, map[string]any) {
	return "test:redstone_loss_consumer", map[string]any{"power": int32(b.Power)}
}
func (b redstoneLossConsumer) Hash() (uint64, uint64) {
	return 1 << 47, uint64(b.Power)
}
func (redstoneLossConsumer) Model() BlockModel { return redstoneCancellationModel{} }

func redstoneVerticalRelayerTestRegistry() BlockRegistry {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:redstone_vertical_source", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneVerticalSource{})
	for power := int32(0); power <= 15; power++ {
		registry.RegisterBlockState(BlockState{Name: "test:redstone_vertical_relayer", Properties: map[string]any{"power": power}})
		registry.RegisterBlock(redstoneVerticalRelayer{Power: int(power)})
	}
	registry.RegisterBlockState(BlockState{Name: "test:redstone_ladder_glowstone", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneLadderGlowstone{})
	registry.RegisterBlockState(BlockState{Name: "test:redstone_ladder_glass", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneLadderGlass{})
	return registry
}

func redstoneTorchAttachmentTestRegistry() BlockRegistry {
	registry := redstoneVerticalRelayerTestRegistry()
	for _, lit := range []bool{false, true} {
		registry.RegisterBlockState(BlockState{Name: "test:redstone_attachment_torch", Properties: map[string]any{"lit": lit}})
		registry.RegisterBlock(redstoneAttachmentTorch{Facing: cube.FaceWest, Lit: lit})
	}
	registry.RegisterBlockState(BlockState{Name: "test:redstone_weak_block_source", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneWeakBlockSource{})
	registry.RegisterBlockState(BlockState{Name: "test:solid_block", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneSolidBlock{})
	return registry
}

func redstonePoweredConductorTestRegistry() BlockRegistry {
	registry := redstoneCancellationTestRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:redstone_strong_source", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneStrongSource{})
	registry.RegisterBlockState(BlockState{Name: "test:solid_block", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneSolidBlock{})
	return registry
}

func redstoneWeakConductorTestRegistry() BlockRegistry {
	registry := redstoneVerticalRelayerTestRegistry()
	for _, powered := range []bool{false, true} {
		registry.RegisterBlockState(BlockState{Name: "test:redstone_consumer", Properties: map[string]any{"powered": powered}})
		registry.RegisterBlock(redstoneCancellationConsumer{Powered: powered})
	}
	registry.RegisterBlockState(BlockState{Name: "test:redstone_weak_block_source", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneWeakBlockSource{})
	registry.RegisterBlockState(BlockState{Name: "test:solid_block", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneSolidBlock{})
	return registry
}

type redstoneVerticalSource struct{}

func (redstoneVerticalSource) RedstonePower(cube.Pos, *Tx, cube.Face) int { return 15 }
func (redstoneVerticalSource) EncodeBlock() (string, map[string]any) {
	return "test:redstone_vertical_source", nil
}
func (redstoneVerticalSource) Hash() (uint64, uint64) { return 1 << 52, 0 }
func (redstoneVerticalSource) Model() BlockModel      { return redstoneCancellationModel{} }

type redstoneStrongSource struct{}

func (redstoneStrongSource) RedstonePower(cube.Pos, *Tx, cube.Face) int { return 15 }
func (redstoneStrongSource) RedstoneStrongPower(cube.Pos, *Tx, cube.Face) int {
	return 15
}
func (redstoneStrongSource) EncodeBlock() (string, map[string]any) {
	return "test:redstone_strong_source", nil
}
func (redstoneStrongSource) Hash() (uint64, uint64) { return 1 << 54, 0 }
func (redstoneStrongSource) Model() BlockModel      { return redstoneCancellationModel{} }

type redstoneWeakBlockSource struct{}

func (redstoneWeakBlockSource) RedstonePower(cube.Pos, *Tx, cube.Face) int { return 15 }
func (redstoneWeakBlockSource) RedstoneWeaklyPowersBlocks() bool           { return true }
func (redstoneWeakBlockSource) EncodeBlock() (string, map[string]any) {
	return "test:redstone_weak_block_source", nil
}
func (redstoneWeakBlockSource) Hash() (uint64, uint64) { return 1 << 55, 0 }
func (redstoneWeakBlockSource) Model() BlockModel      { return redstoneCancellationModel{} }

type redstoneVerticalRelayer struct {
	Power int
}

func (b redstoneVerticalRelayer) RedstonePower(cube.Pos, *Tx, cube.Face) int {
	return b.Power
}
func (redstoneVerticalRelayer) RedstoneSignalLoss(cube.Pos, *Tx) int {
	return 1
}
func (b redstoneVerticalRelayer) RedstonePowerUpdate(_ cube.Pos, _ *Tx, power int) (Block, bool) {
	if b.Power == power {
		return b, false
	}
	b.Power = power
	return b, true
}
func (b redstoneVerticalRelayer) RedstoneRelayerNeighbours(pos cube.Pos, tx *Tx) []cube.Pos {
	neighbours := make([]cube.Pos, 0, 2)
	for _, side := range []cube.Pos{pos.Add(cube.Pos{-1, 1, 0}), pos.Add(cube.Pos{1, -1, 0})} {
		if side.OutOfBounds(tx.Range()) {
			continue
		}
		if side[1] < pos[1] && !redstoneTestCanTransmitDown(tx, pos) {
			continue
		}
		neighbours = append(neighbours, side)
	}
	return neighbours
}
func (b redstoneVerticalRelayer) EncodeBlock() (string, map[string]any) {
	return "test:redstone_vertical_relayer", map[string]any{"power": int32(b.Power)}
}
func (b redstoneVerticalRelayer) Hash() (uint64, uint64) {
	return 1 << 49, uint64(b.Power)
}
func (redstoneVerticalRelayer) Model() BlockModel { return redstoneCancellationModel{} }

type redstoneAttachmentTorch struct {
	Facing cube.Face
	Lit    bool
}

func (t redstoneAttachmentTorch) RedstonePower(cube.Pos, *Tx, cube.Face) int {
	if t.Lit {
		return 15
	}
	return 0
}
func (t redstoneAttachmentTorch) RedstonePowerAction(pos cube.Pos, tx *Tx, _, _ int) {
	if t.Lit == !t.attachmentPowered(pos, tx) {
		return
	}
	t.Lit = !t.Lit
	tx.SetBlock(pos, t, nil)
}
func (t redstoneAttachmentTorch) attachmentPowered(pos cube.Pos, tx *Tx) bool {
	attached := pos.Side(t.Facing)
	attachedBlock := tx.Block(attached)
	if !redstoneStrongPowerConductor(attached, attachedBlock, tx, t.Facing.Opposite()) {
		return false
	}
	return tx.RedstoneConductivePower(attached) > 0
}
func (t redstoneAttachmentTorch) EncodeBlock() (string, map[string]any) {
	return "test:redstone_attachment_torch", map[string]any{"lit": t.Lit}
}
func (t redstoneAttachmentTorch) Hash() (uint64, uint64) {
	if t.Lit {
		return 1 << 53, 1
	}
	return 1 << 53, 0
}
func (redstoneAttachmentTorch) Model() BlockModel { return redstoneCancellationModel{} }

type redstoneLadderGlowstone struct{}

func (redstoneLadderGlowstone) RedstoneNonConductive() {}
func (redstoneLadderGlowstone) EncodeBlock() (string, map[string]any) {
	return "test:redstone_ladder_glowstone", nil
}
func (redstoneLadderGlowstone) Hash() (uint64, uint64) { return 1 << 50, 0 }
func (redstoneLadderGlowstone) Model() BlockModel      { return redstoneSolidModel{} }

type redstoneLadderGlass struct{}

func (redstoneLadderGlass) LightDiffusionLevel() uint8 { return 0 }
func (redstoneLadderGlass) EncodeBlock() (string, map[string]any) {
	return "test:redstone_ladder_glass", nil
}
func (redstoneLadderGlass) Hash() (uint64, uint64) { return 1 << 51, 0 }
func (redstoneLadderGlass) Model() BlockModel      { return redstoneSolidModel{} }

func redstoneTestCanTransmitDown(tx *Tx, pos cube.Pos) bool {
	supportPos := pos.Side(cube.FaceDown)
	support, ok := tx.World().blockLoaded(supportPos)
	if !ok || !support.Model().FaceSolid(supportPos, cube.FaceUp, tx) {
		return false
	}
	if _, ok := support.(RedstoneNonConductive); ok {
		return false
	}
	return true
}

type redstoneSolidBlock struct{}

func (redstoneSolidBlock) EncodeBlock() (string, map[string]any) { return "test:solid_block", nil }
func (redstoneSolidBlock) Hash() (uint64, uint64)                { return 1 << 48, 0 }
func (redstoneSolidBlock) Model() BlockModel                     { return redstoneSolidModel{} }

type redstoneNonConductiveSolidBlock struct {
	redstoneSolidBlock
}

func (redstoneNonConductiveSolidBlock) RedstoneNonConductive() {}
func (redstoneNonConductiveSolidBlock) EncodeBlock() (string, map[string]any) {
	return "test:non_conductive_solid_block", nil
}
func (redstoneNonConductiveSolidBlock) Hash() (uint64, uint64) { return 1 << 56, 0 }

type redstoneSolidModel struct{}

func (redstoneSolidModel) BBox(cube.Pos, BlockSource) []cube.BBox { return nil }
func (redstoneSolidModel) FaceSolid(cube.Pos, cube.Face, BlockSource) bool {
	return true
}

// listed returns the number of ticks the queue holds for its chunks.
func (queue *scheduledTickQueue) listed() int {
	n := 0
	for _, list := range queue.chunks {
		n += len(list.ticks)
	}
	return n
}
