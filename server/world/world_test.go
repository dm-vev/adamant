package world

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSynchronousWorldDo verifies that Do on a synchronous World runs the task
// on the calling goroutine and returns a completed task.
func TestSynchronousWorldDo(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	var ran bool
	task := w.Do(func(tx *Tx) { ran = true })
	if !ran {
		t.Fatal("expected task to have run when Do returned")
	}
	select {
	case <-task.Done():
	default:
		t.Fatal("expected task returned by Do to be done when Do returned")
	}
}

// TestSynchronousWorldAdvanceTick verifies that a synchronous World does not
// tick on its own and that AdvanceTick advances the current tick exactly once
// per call, even without any viewers.
func TestSynchronousWorldAdvanceTick(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	current := func() int64 {
		w.set.Lock()
		defer w.set.Unlock()
		return w.set.CurrentTick
	}
	start := current()
	time.Sleep(time.Second / 10)
	if got := current(); got != start {
		t.Fatalf("expected no automatic ticking, tick advanced from %v to %v", start, got)
	}
	for range 5 {
		w.AdvanceTick()
	}
	if got := current(); got != start+5 {
		t.Fatalf("expected current tick %v after 5 AdvanceTick calls, got %v", start+5, got)
	}
}

func TestSynchronousWorldTicksAdvancePerDimension(t *testing.T) {
	settings := defaultSettings()
	provider := NopProvider{Set: settings}
	worlds := []*World{
		Config{Provider: provider, Dim: Overworld, Synchronous: true}.New(),
		Config{Provider: provider, Dim: Nether, Synchronous: true}.New(),
		Config{Provider: provider, Dim: End, Synchronous: true}.New(),
		Config{Provider: provider, Dim: testDimension{i: 1029}, Synchronous: true}.New(),
	}
	for _, w := range worlds {
		defer w.Close()
	}

	starts := make([]int64, len(worlds))
	for i, w := range worlds {
		starts[i] = w.CurrentTick()
	}
	startTime := settings.Time
	for i, w := range worlds {
		for range i + 1 {
			w.AdvanceTick()
		}
	}

	for i, w := range worlds {
		want := starts[i] + int64(i+1)
		if got := w.CurrentTick(); got != want {
			t.Fatalf("world %v current tick = %d, want %d", w.Dimension(), got, want)
		}
		var txTick int64
		<-w.Exec(func(tx *Tx) { txTick = tx.CurrentTick() })
		if txTick != want {
			t.Fatalf("world %v transaction tick = %d, want %d", w.Dimension(), txTick, want)
		}
	}
	if got, want := settings.CurrentTick, starts[0]+1; got != want {
		t.Fatalf("shared current tick = %d, want %d", got, want)
	}
	if got, want := settings.Time, startTime+1; got != want {
		t.Fatalf("shared time = %d, want %d", got, want)
	}
}

func TestSharedProviderClosesAfterLastWorld(t *testing.T) {
	for _, ownerFirst := range []bool{true, false} {
		name := "owner_last"
		if ownerFirst {
			name = "owner_first"
		}
		t.Run(name, func(t *testing.T) {
			settings := defaultSettings()
			provider := &lifecycleProvider{NopProvider: NopProvider{Set: settings}}
			owner := Config{Provider: provider, Dim: Overworld, Synchronous: true}.New()
			other := Config{Provider: provider, Dim: testDimension{i: 1029}, Synchronous: true}.New()
			t.Cleanup(func() {
				_ = owner.Close()
				_ = other.Close()
			})

			first, remaining := other, owner
			if ownerFirst {
				first, remaining = owner, other
			}
			if err := first.Close(); err != nil {
				t.Fatalf("close first world: %v", err)
			}
			if got := provider.closes.Load(); got != 0 {
				t.Fatalf("provider closed with a world remaining: %d", got)
			}

			startTick, startTime := settings.CurrentTick, settings.Time
			remaining.AdvanceTick()
			if got := settings.CurrentTick; got != startTick+1 {
				t.Fatalf("shared tick after handoff = %d, want %d", got, startTick+1)
			}
			if got := settings.Time; got != startTime+1 {
				t.Fatalf("shared time after handoff = %d, want %d", got, startTime+1)
			}
			saves := provider.saves.Load()
			remaining.Save()
			if got := provider.saves.Load(); got != saves+1 {
				t.Fatalf("remaining world saves = %d, want %d", got, saves+1)
			}

			if err := remaining.Close(); err != nil {
				t.Fatalf("close remaining world: %v", err)
			}
			if got := provider.closes.Load(); got != 1 {
				t.Fatalf("provider close count = %d, want 1", got)
			}
		})
	}
}

func TestIndependentProvidersSharingSettingsCloseIndependently(t *testing.T) {
	settings := defaultSettings()
	firstProvider := &lifecycleProvider{NopProvider: NopProvider{Set: settings}}
	secondProvider := &lifecycleProvider{NopProvider: NopProvider{Set: settings}}
	first := Config{Provider: firstProvider, Synchronous: true}.New()
	second := Config{Provider: secondProvider, Dim: Nether, Synchronous: true}.New()
	t.Cleanup(func() {
		_ = first.Close()
		_ = second.Close()
	})

	if err := first.Close(); err != nil {
		t.Fatalf("close first world: %v", err)
	}
	if got := firstProvider.closes.Load(); got != 1 {
		t.Fatalf("first provider close count = %d, want 1", got)
	}
	if got := secondProvider.closes.Load(); got != 0 {
		t.Fatalf("second provider closed early: %d", got)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second world: %v", err)
	}
	if got := secondProvider.closes.Load(); got != 1 {
		t.Fatalf("second provider close count = %d, want 1", got)
	}
}

func TestSharedProviderWithDistinctSettingsClosesOnce(t *testing.T) {
	provider := &lifecycleProvider{freshSettings: true}
	first := Config{Provider: provider, Synchronous: true}.New()
	second := Config{Provider: provider, Dim: Nether, Synchronous: true}.New()
	t.Cleanup(func() {
		_ = first.Close()
		_ = second.Close()
	})

	if err := first.Close(); err != nil {
		t.Fatalf("close first world: %v", err)
	}
	if got := provider.closes.Load(); got != 0 {
		t.Fatalf("shared provider closed early: %d", got)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second world: %v", err)
	}
	if got := provider.closes.Load(); got != 1 {
		t.Fatalf("shared provider close count = %d, want 1", got)
	}
}

func TestSharedProviderConstructionRetainedBeforeProviderUse(t *testing.T) {
	provider := &lifecycleProvider{
		freshSettings:        true,
		blockRegistryStarted: make(chan struct{}),
		releaseBlockRegistry: make(chan struct{}),
	}
	first := Config{Provider: provider, Synchronous: true}.New()
	secondWorld := make(chan *World)
	go func() {
		secondWorld <- Config{Provider: provider, Dim: Nether, Synchronous: true}.New()
	}()
	<-provider.blockRegistryStarted

	if err := first.Close(); err != nil {
		t.Fatalf("close first world: %v", err)
	}
	closedDuringConstruction := provider.closes.Load()
	close(provider.releaseBlockRegistry)
	second := <-secondWorld
	t.Cleanup(func() { _ = second.Close() })

	if closedDuringConstruction != 0 {
		t.Fatalf("provider closed during world construction: %d", closedDuringConstruction)
	}
	if first.set == second.set {
		t.Fatal("provider did not return distinct Settings values")
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second world: %v", err)
	}
	if got := provider.closes.Load(); got != 1 {
		t.Fatalf("provider close count = %d, want 1", got)
	}
}

func TestProviderRetentionReleasedWhenConstructionPanics(t *testing.T) {
	provider := &lifecycleProvider{panicBlockRegistry: true}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		Config{Provider: provider, Synchronous: true}.New()
	}()
	if recovered == nil {
		t.Fatal("world construction did not panic")
	}
	if got := provider.closes.Load(); got != 1 {
		t.Fatalf("provider close count = %d, want 1", got)
	}
}

func TestConstructionPanicUnregistersSharedSettingsWorld(t *testing.T) {
	settings := defaultSettings()
	settings.CurrentTick = 19
	provider := &lifecycleProvider{NopProvider: NopProvider{Set: settings}}
	entered, release := make(chan struct{}), make(chan struct{})
	panicResult := make(chan any)
	go func() {
		defer func() { panicResult <- recover() }()
		Config{Provider: provider, Dim: panickingTimeCycleDimension{entered: entered, release: release}, Synchronous: true}.New()
	}()
	<-entered

	w := Config{Provider: provider, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	close(release)
	if recovered := <-panicResult; recovered == nil {
		t.Fatal("world construction did not panic")
	}

	settings.Lock()
	if settings.owner != w || len(settings.worlds) != 1 {
		t.Fatalf("failed world remains registered: owner=%p, worlds=%d", settings.owner, len(settings.worlds))
	}
	settings.Unlock()
	if got := provider.closes.Load(); got != 0 {
		t.Fatalf("shared provider closed during rollback: %d", got)
	}

	start := settings.CurrentTick
	w.AdvanceTick()
	if got := settings.CurrentTick; got != start+1 {
		t.Fatalf("shared current tick = %d, want %d", got, start+1)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close successful world: %v", err)
	}
	if got := provider.closes.Load(); got != 1 {
		t.Fatalf("shared provider close count = %d, want 1", got)
	}
	settings.Lock()
	defer settings.Unlock()
	if settings.owner != nil || len(settings.worlds) != 0 {
		t.Fatalf("closed world remains registered: owner=%p, worlds=%d", settings.owner, len(settings.worlds))
	}
}

type panickingTimeCycleDimension struct {
	testDimension
	entered chan<- struct{}
	release <-chan struct{}
}

func (d panickingTimeCycleDimension) TimeCycle() bool {
	close(d.entered)
	<-d.release
	panic("time cycle")
}

type lifecycleProvider struct {
	NopProvider
	freshSettings        bool
	panicBlockRegistry   bool
	blockRegistryCalls   atomic.Int32
	blockRegistryStarted chan struct{}
	releaseBlockRegistry chan struct{}
	saves                atomic.Int32
	closes               atomic.Int32
}

func (p *lifecycleProvider) Settings() *Settings {
	if p.freshSettings {
		return defaultSettings()
	}
	return p.NopProvider.Settings()
}
func (p *lifecycleProvider) SetBlockRegistry(BlockRegistry) {
	if p.panicBlockRegistry {
		panic("set block registry")
	}
	if p.blockRegistryCalls.Add(1) == 2 && p.blockRegistryStarted != nil {
		close(p.blockRegistryStarted)
		<-p.releaseBlockRegistry
	}
}
func (p *lifecycleProvider) SaveSettings(*Settings) { p.saves.Add(1) }
func (p *lifecycleProvider) Close() error           { p.closes.Add(1); return nil }

func TestCloseDoesNotLeaveOwnerWaitingForGeneration(t *testing.T) {
	w := Config{GeneratorWorkers: 1, GeneratorQueueSize: 1}.New()

	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	w.exec(func(*Tx) {
		close(ownerStarted)
		<-releaseOwner
	})
	<-ownerStarted

	closeDone := make(chan struct{})
	go func() {
		_ = w.Close()
		close(closeDone)
	}()
	<-w.closeStarted

	requestDone := w.exec(func(tx *Tx) {
		for x := int32(0); x < 16; x++ {
			tx.chunk(ChunkPos{x, 0})
		}
	})
	w.generatorRunning.Wait()
	close(releaseOwner)

	select {
	case <-requestDone:
	case <-time.After(3 * time.Second):
		t.Fatal("close-time chunk request waited for stopped generation workers")
	}
	select {
	case <-closeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("world close did not complete after close-time chunk request")
	}
}

func TestSynchronousEntityDoCanRemoveEntity(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	h := EntitySpawnOpts{Position: mgl64.Vec3{0, 4, 0}}.New(testEntityType{}, testEntityConfig{})
	<-w.exec(func(tx *Tx) {
		tx.AddEntity(h)
	})

	task := h.Do(func(tx *Tx, e Entity) {
		tx.RemoveEntity(e)
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	if err := task.Wait(ctx); err != nil {
		t.Fatalf("entity Do self-removal did not complete: %v", err)
	}
}

func TestSynchronousEntityDoWaitsForAddEntityToFinish(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	state := &blockingOpenState{
		firstOpen:  make(chan struct{}),
		secondOpen: make(chan struct{}),
		release:    make(chan struct{}),
	}
	h := EntitySpawnOpts{}.New(blockingOpenType{}, blockingOpenConfig{state: state})
	task := h.Do(func(*Tx, Entity) {})
	added := make(chan struct{})
	go func() {
		w.Do(func(tx *Tx) { tx.AddEntity(h) })
		close(added)
	}()
	<-state.firstOpen

	premature := false
	select {
	case <-state.secondOpen:
		premature = true
	case <-time.After(time.Millisecond * 50):
	}
	close(state.release)
	<-added
	if err := task.Wait(context.Background()); err != nil {
		t.Fatalf("entity Do failed: %v", err)
	}
	if premature {
		t.Fatal("entity callback opened before AddEntity completed")
	}
}

func TestSynchronousAdvanceTickTicksViewerlessEntities(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	h := EntitySpawnOpts{Position: mgl64.Vec3{0, 4, 0}}.New(testEntityType{}, testEntityConfig{})
	<-w.exec(func(tx *Tx) {
		tx.AddEntity(h)
	})

	start := h.data.Pos
	for range 3 {
		w.AdvanceTick()
	}
	if got := h.data.Pos; got == start {
		t.Fatalf("expected entity position to change after ticking, got %v", got)
	}
}

func TestLoadedEntityCanBeRemoved(t *testing.T) {
	pos := ChunkPos{0, 0}
	provider := &entityLoadProvider{columns: make(map[ChunkPos]*chunk.Column)}
	w := Config{
		Synchronous: true,
		Provider:    provider,
		Entities:    (EntityRegistryConfig{}).New([]EntityType{testEntityType{}}),
	}.New()
	defer w.Close()
	provider.columns[pos] = loadedTestEntityColumn(w, 1, mgl64.Vec3{1, 4, 1})

	<-w.Exec(func(tx *Tx) {
		col := tx.chunk(pos)
		if len(col.Entities) != 1 {
			t.Fatalf("loaded entity count = %d, want 1", len(col.Entities))
		}
		handle := col.Entities[0]
		if index, ok := col.entityIndices[handle]; !ok || index != 0 {
			t.Fatalf("loaded entity index = %d, %v, want 0, true", index, ok)
		}
		entity, ok := handle.Entity(tx)
		if !ok {
			t.Fatal("loaded entity was not bound to world")
		}
		if tx.RemoveEntity(entity) != handle {
			t.Fatal("loaded entity was not removed")
		}
		if len(col.Entities) != 0 || len(col.entityIndices) != 0 {
			t.Fatalf("loaded column retained entity after removal: entities=%d indices=%d", len(col.Entities), len(col.entityIndices))
		}
	})
}

func TestLoadedEntityMigratesWithoutLeavingDuplicate(t *testing.T) {
	sourcePos, destinationPos := ChunkPos{0, 0}, ChunkPos{1, 0}
	provider := &entityLoadProvider{columns: make(map[ChunkPos]*chunk.Column)}
	w := Config{
		Synchronous: true,
		Provider:    provider,
		Entities:    (EntityRegistryConfig{}).New([]EntityType{testEntityType{}}),
	}.New()
	defer w.Close()
	provider.columns[sourcePos] = loadedTestEntityColumn(w, 1, mgl64.Vec3{15.5, 4, 1})
	provider.columns[destinationPos] = &chunk.Column{Chunk: chunk.New(w.conf.Blocks, w.Range())}

	var source, destination *Column
	<-w.Exec(func(tx *Tx) {
		source = tx.chunk(sourcePos)
		destination = tx.chunk(destinationPos)
		source.Entities[0].data.Pos[0] = 16.5
	})
	w.AdvanceTick()

	<-w.Exec(func(tx *Tx) {
		if len(source.Entities) != 0 || len(source.entityIndices) != 0 {
			t.Fatalf("source retained migrated entity: entities=%d indices=%d", len(source.Entities), len(source.entityIndices))
		}
		if len(destination.Entities) != 1 || destination.entityIndices[destination.Entities[0]] != 0 {
			t.Fatalf("destination entity bookkeeping invalid: entities=%d indices=%d", len(destination.Entities), len(destination.entityIndices))
		}
		if got := len(w.columnTo(source, sourcePos).Entities); got != 0 {
			t.Fatalf("saved source entity count = %d, want 0", got)
		}
		if got := len(w.columnTo(destination, destinationPos).Entities); got != 1 {
			t.Fatalf("saved destination entity count = %d, want 1", got)
		}
	})
}

func TestEntityCrossingIntoLaterColumnTicksOnce(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	moverTicks, residentTicks := 0, 0
	mover := EntitySpawnOpts{Position: mgl64.Vec3{15.5, 4, 1}}.New(testEntityType{}, boundaryTickerConfig{ticks: &moverTicks, crossBoundary: true})
	resident := EntitySpawnOpts{Position: mgl64.Vec3{16.5, 4, 1}}.New(testEntityType{}, boundaryTickerConfig{ticks: &residentTicks})
	<-w.Exec(func(tx *Tx) {
		tx.AddEntity(mover)
		tx.AddEntity(resident)
	})

	w.AdvanceTick()
	w.AdvanceTick()
	if moverTicks != 2 {
		t.Fatalf("boundary-crossing entity ticked %d times in 2 world ticks, want 2", moverTicks)
	}
	if residentTicks != 2 {
		t.Fatalf("destination resident ticked %d times in 2 world ticks, want 2", residentTicks)
	}
}

func TestSynchronousAdvanceTickTicksViewerlessBlockEntities(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	pos := cube.Pos{0, 4, 0}
	tb := &testTickerBlock{}
	<-w.exec(func(tx *Tx) {
		col := tx.chunk(chunkPosFromBlockPos(pos))
		chest, ok := tx.World().conf.Blocks.BlockByName("minecraft:chest", map[string]any{"minecraft:cardinal_direction": "north"})
		if !ok {
			t.Fatal("expected chest block to be registered")
		}
		col.SetBlock(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0, tx.World().conf.Blocks.BlockRuntimeID(chest))
		col.BlockEntities[pos] = tb
	})

	w.AdvanceTick()
	if tb.ticks == 0 {
		t.Fatal("expected block entity to tick")
	}
}

func TestEntitiesWithinCanRemoveYieldedEntities(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	handles := []*EntityHandle{
		EntitySpawnOpts{Position: mgl64.Vec3{0, 4, 0}}.New(testEntityType{}, testEntityConfig{}),
		EntitySpawnOpts{Position: mgl64.Vec3{1, 4, 1}}.New(testEntityType{}, testEntityConfig{}),
	}
	removed := 0
	<-w.Exec(func(tx *Tx) {
		for _, handle := range handles {
			tx.AddEntity(handle)
		}
		for entity := range tx.EntitiesWithin(cube.Box(-1, 0, -1, 2, 8, 2)) {
			tx.RemoveEntity(entity)
			removed++
		}
	})
	if removed != len(handles) {
		t.Fatalf("expected to remove %d entities, removed %d", len(handles), removed)
	}
}

type testEntityConfig struct{}

func (testEntityConfig) Apply(*EntityData) {}

type testEntityType struct{}

func (testEntityType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	return &testEntity{handle: handle, data: data}
}

func (testEntityType) EncodeEntity() string {
	return "dragonfly:test_entity"
}

func (testEntityType) BBox(Entity) cube.BBox {
	return cube.Box(0, 0, 0, 1, 1, 1)
}

func (testEntityType) DecodeNBT(map[string]any, *EntityData) {}

func (testEntityType) EncodeNBT(*EntityData) map[string]any {
	return nil
}

type testEntity struct {
	handle *EntityHandle
	data   *EntityData
}

func (e *testEntity) Close() error {
	return nil
}

func (e *testEntity) H() *EntityHandle {
	return e.handle
}

func (e *testEntity) Position() mgl64.Vec3 {
	return e.data.Pos
}

func (e *testEntity) Rotation() cube.Rotation {
	return e.data.Rot
}

func (e *testEntity) Tick(*Tx, int64) {
	if config, ok := e.data.Data.(boundaryTickerConfig); ok {
		*config.ticks++
		if config.crossBoundary && *config.ticks == 1 {
			e.data.Pos[0] = 16.5
		}
		return
	}
	e.data.Pos = e.data.Pos.Add(mgl64.Vec3{0, -0.1, 0})
}

type entityLoadProvider struct {
	NopProvider
	columns map[ChunkPos]*chunk.Column
}

func (p *entityLoadProvider) LoadColumn(pos ChunkPos, dim Dimension) (*chunk.Column, error) {
	if col, ok := p.columns[pos]; ok {
		return col, nil
	}
	return p.NopProvider.LoadColumn(pos, dim)
}

func loadedTestEntityColumn(w *World, id int64, pos mgl64.Vec3) *chunk.Column {
	return &chunk.Column{
		Chunk: chunk.New(w.conf.Blocks, w.Range()),
		Entities: []chunk.Entity{{ID: id, Data: map[string]any{
			"identifier": "dragonfly:test_entity",
			"Pos":        []float32{float32(pos[0]), float32(pos[1]), float32(pos[2])},
		}}},
	}
}

type boundaryTickerConfig struct {
	ticks         *int
	crossBoundary bool
}

func (c boundaryTickerConfig) Apply(data *EntityData) { data.Data = c }

type testTickerBlock struct {
	ticks int
}

type blockingOpenState struct {
	opens      atomic.Int32
	firstOpen  chan struct{}
	secondOpen chan struct{}
	release    chan struct{}
}

type blockingOpenConfig struct {
	state *blockingOpenState
}

func (c blockingOpenConfig) Apply(data *EntityData) { data.Data = c.state }

type blockingOpenType struct{}

func (blockingOpenType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	state := data.Data.(*blockingOpenState)
	switch state.opens.Add(1) {
	case 1:
		close(state.firstOpen)
		<-state.release
	case 2:
		close(state.secondOpen)
	}
	return &testEntity{handle: handle, data: data}
}

func (blockingOpenType) EncodeEntity() string { return "dragonfly:blocking_open" }

func (blockingOpenType) BBox(Entity) cube.BBox { return cube.BBox{} }

func (blockingOpenType) DecodeNBT(map[string]any, *EntityData) {}

func (blockingOpenType) EncodeNBT(*EntityData) map[string]any { return nil }

func (*testTickerBlock) EncodeBlock() (string, map[string]any) {
	return "dragonfly:test_ticker", nil
}

func (*testTickerBlock) Hash() (uint64, uint64) {
	return 1<<32 - 1, 0
}

func (*testTickerBlock) Model() BlockModel {
	return unknownModel{}
}

func (*testTickerBlock) DecodeNBT(map[string]any) any {
	return &testTickerBlock{}
}

func (*testTickerBlock) EncodeNBT() map[string]any {
	return nil
}

func (b *testTickerBlock) Tick(int64, cube.Pos, *Tx) {
	b.ticks++
}
