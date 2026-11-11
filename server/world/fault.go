package world

import (
	"fmt"
	"log/slog"
	"math"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// faultState is the panics recovered in the world, the tick guard and the hooks
// of tests.
type faultState struct {
	// faults holds the log of the panics recovered in the world and the
	// offenders quarantined.
	faults faults
	// guard is the tick guard of the transactions of the owner.
	guard tickGuard
	// blockFaults holds the blocks whose ticks panicked.
	blockFaults blockFaults
	// stepFaults holds the subsystems of the tick that panicked, by name.
	stepFaults map[string]*stepFault
	// testTickHook, if not nil, is called by each tick that simulates,
	// before anything else of the world runs: tests make the bookkeeping
	// of the tick panic with it.
	testTickHook func(tx *Tx)
	// testClockHook, if not nil, is called by each tick that advances the
	// clock of the world, under the lock of its settings: a test panics in it.
	testClockHook func()
}

// FaultKind is the code of the game a recovered panic came from.
type FaultKind uint8

const (
	// FaultEntity is a panic of TickerEntity.Tick, of the ticks of an entity
	// before or after the blocks, or of the bookkeeping of the column loop for
	// an entity.
	FaultEntity FaultKind = iota + 1
	// FaultPlayer is a panic of the tick or the controller of a player.
	FaultPlayer
	// FaultBlockEntity is a panic of TickerBlock.Tick.
	FaultBlockEntity
	// FaultScheduledTick is a panic of a scheduled block or fluid tick.
	FaultScheduledTick
	// FaultRandomTick is a panic of a random tick or of precipitation.
	FaultRandomTick
	// FaultNeighbourUpdate is a panic of NeighbourUpdateTicker.
	FaultNeighbourUpdate
	// FaultSubsystem is a panic of a part of the tick of the world, as
	// lightning, the dragon fight, collisions, spawning, raids or redstone.
	FaultSubsystem
	// FaultTransaction is a panic of an Exec, Do or Call callback, of a
	// Tx.Defer callback, or of opening an entity for one.
	FaultTransaction
	// FaultSave is a panic capturing or writing a column.
	FaultSave
	// FaultGeneration is a panic of a generator worker or a light fill.
	FaultGeneration
	// FaultViewer is a panic of a Viewer shown a change of the world.
	FaultViewer
	// FaultFatal is a panic the tick could not blame on anything.
	FaultFatal
)

// String returns the name of the kind, as logs show it.
func (k FaultKind) String() string {
	switch k {
	case FaultEntity:
		return "entity"
	case FaultPlayer:
		return "player"
	case FaultBlockEntity:
		return "block entity"
	case FaultScheduledTick:
		return "scheduled tick"
	case FaultRandomTick:
		return "random tick"
	case FaultNeighbourUpdate:
		return "neighbour update"
	case FaultSubsystem:
		return "subsystem"
	case FaultTransaction:
		return "transaction"
	case FaultSave:
		return "save"
	case FaultGeneration:
		return "generation"
	case FaultViewer:
		return "viewer"
	case FaultFatal:
		return "fatal"
	}
	return fmt.Sprintf("FaultKind(%d)", uint8(k))
}

// Fault is a recovered panic and what it is blamed on.
type Fault struct {
	Kind  FaultKind
	World *World
	// Type is the entity type, the block name or the subsystem the fault is
	// blamed on.
	Type string
	// Pos is the position of the block, or of the block the entity was in.
	Pos cube.Pos
	// Entity is the entity the fault is blamed on, if any.
	Entity *EntityHandle
	// Value is the recovered panic value and Stack the stack of the
	// goroutine that panicked.
	Value any
	Stack []byte
	// Count is how many panics the offender had within FaultWindow, this
	// one included. Quarantined is set once Count reached FaultLimit.
	Count       int
	Quarantined bool
}

const (
	// FaultLimit panics of one offender within FaultWindow ticks quarantine
	// it: it no longer ticks, but is still saved.
	FaultLimit = 3
	// FaultWindow is the window, in ticks, that FaultLimit counts panics
	// in: 60 seconds.
	FaultWindow = 1200
)

const (
	// faultLogInterval is how long the log stays quiet about a key after a
	// line about it.
	faultLogInterval = time.Minute
	// faultLogKeys is how many keys the log of faults remembers at most.
	faultLogKeys = 256
)

// newFault returns a fault of kind for the recovered value r, with the
// stack of the calling goroutine, which is that of the panic in a deferred
// function.
func newFault(kind FaultKind, r any) Fault {
	return Fault{Kind: kind, Value: r, Stack: debug.Stack()}
}

// panicSite returns the function that panicked, when called by a function
// deferred while panicking: the first function below the panic that is not
// of the runtime.
func panicSite() string {
	var pcs [48]uintptr
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs[:])])
	panicking := false
	for {
		fr, more := frames.Next()
		if panicking && !strings.HasPrefix(fr.Function, "runtime.") {
			return fr.Function
		}
		if fr.Function == "runtime.gopanic" {
			panicking = true
		}
		if !more {
			return "unknown"
		}
	}
}

// faultCount counts the panics of one offender within FaultWindow ticks of
// the first one it counted.
type faultCount struct {
	n     uint8
	since int64
}

// add counts a panic at tick and returns how many panics it counted in
// the window, this one included. The count starts again once FaultWindow
// ticks passed since the first panic of the window.
func (c *faultCount) add(tick int64) int {
	if c.n == 0 || tick-c.since >= FaultWindow || tick < c.since {
		c.n, c.since = 0, tick
	}
	if c.n < math.MaxUint8 {
		c.n++
	}
	return int(c.n)
}

// count sets the Count and Quarantined of f from c, counting a panic at
// tick, and returns whether the offender is to be quarantined.
func (c *faultCount) count(f *Fault, tick int64) bool {
	f.Count = c.add(tick)
	f.Quarantined = f.Count >= FaultLimit
	return f.Quarantined
}

// faultLogKey is what the log of faults limits the lines of.
type faultLogKey struct {
	kind FaultKind
	typ  string
}

// faultLogEntry is what the log of faults remembers of a key.
type faultLogEntry struct {
	// logged is when the last line about the key was logged.
	logged time.Time
	// suppressed counts the panics of the key since that were not logged.
	suppressed int
}

// quarantineKey is an offender in quarantine: an entity, the block entity
// at a position or a block at a position.
type quarantineKey struct {
	entity      *EntityHandle
	pos         cube.Pos
	name        string
	blockEntity bool
}

// faults is what a world keeps of the panics of its game: the rate-limited
// log and the offenders in quarantine. Region workers report faults at the
// same time, hence the lock.
type faults struct {
	mu          sync.Mutex
	now         func() time.Time
	entries     map[faultLogKey]*faultLogEntry
	quarantined map[quarantineKey]Fault
	// failed is set once the world failed: it reported a fatal fault.
	failed atomic.Bool
}

// faultLine is a line the log of faults is to log.
type faultLine struct {
	msg   string
	f     Fault
	stack bool
}

// record decides what to log about f, which happened at now, and returns
// the lines: none if the key of f logged less than faultLogInterval ago,
// one otherwise, after a summary of the key evicted to make room for it,
// if any.
func (l *faults) record(f Fault, now time.Time) []faultLine {
	key := faultLogKey{kind: f.Kind, typ: f.Type}
	e, ok := l.entries[key]
	if ok && now.Sub(e.logged) < faultLogInterval {
		e.suppressed++
		return nil
	}
	var lines []faultLine
	if !ok {
		if l.entries == nil {
			l.entries = make(map[faultLogKey]*faultLogEntry)
		}
		if len(l.entries) >= faultLogKeys {
			if line, evicted := l.evictOldest(); evicted {
				lines = append(lines, line...)
			}
		}
		e = &faultLogEntry{}
		l.entries[key] = e
	}
	msg := f.Kind.String() + " panicked"
	if e.suppressed > 0 {
		msg = summary(e.suppressed, f.Kind, f.Type, e.logged)
	}
	e.logged, e.suppressed = now, 0
	return append(lines, faultLine{msg: msg, f: f, stack: true})
}

// evictOldest forgets the key that logged longest ago, returning a summary
// of its panics not logged, if any.
func (l *faults) evictOldest() ([]faultLine, bool) {
	var (
		oldest faultLogKey
		entry  *faultLogEntry
	)
	for k, e := range l.entries {
		if entry == nil || e.logged.Before(entry.logged) {
			oldest, entry = k, e
		}
	}
	if entry == nil {
		return nil, false
	}
	delete(l.entries, oldest)
	if entry.suppressed == 0 {
		return nil, true
	}
	return []faultLine{{msg: summary(entry.suppressed, oldest.kind, oldest.typ, entry.logged), f: Fault{Kind: oldest.kind, Type: oldest.typ}}}, true
}

// summary is the line logged for n panics of a key not logged since a
// line about it at since.
func summary(n int, kind FaultKind, typ string, since time.Time) string {
	return fmt.Sprintf("%d more panics of %v %v since %v", n, kind, typ, since.Format(time.TimeOnly))
}

// log logs line through log.
func (line faultLine) log(log *slog.Logger) {
	f := line.f
	attrs := make([]any, 0, 16)
	if f.Type != "" {
		attrs = append(attrs, "type", f.Type)
	}
	if f.Entity != nil {
		attrs = append(attrs, "uuid", f.Entity.UUID().String())
	}
	if line.stack {
		attrs = append(attrs, "pos", f.Pos.String(), "count", f.Count)
		if f.Quarantined {
			attrs = append(attrs, "quarantined", true)
		}
		attrs = append(attrs, "panic", fmt.Sprint(f.Value), "stack", string(f.Stack))
	}
	log.Error(line.msg, attrs...)
}

// faultLog returns the logger of the faults of kind: that of generation for
// the panics of generator workers and light fills, the world's otherwise.
func (w *World) faultLog(kind FaultKind) *slog.Logger {
	if kind == FaultGeneration {
		return w.genLog
	}
	return w.log
}

// reportFault reports the fault f of the world: it is logged, rate-limited
// by kind and type, and passed to Config.Fault. A fault with Quarantined
// set lists its offender, of key, as quarantined.
func (w *World) reportFault(f Fault, key quarantineKey) {
	f.World = w
	l := &w.faults
	l.mu.Lock()
	lines := l.record(f, l.clock()())
	if f.Quarantined {
		if l.quarantined == nil {
			l.quarantined = make(map[quarantineKey]Fault)
		}
		l.quarantined[key] = f
	}
	l.mu.Unlock()
	for _, line := range lines {
		line.log(w.faultLog(line.f.Kind))
	}
	if hook := w.conf.Fault; hook != nil {
		hook(f)
	}
}

// clock returns the clock of the log of faults.
func (l *faults) clock() func() time.Time {
	if l.now != nil {
		return l.now
	}
	return time.Now
}

// release takes the offender of key out of quarantine, as when it leaves
// the world or is replaced.
func (w *World) release(key quarantineKey) {
	l := &w.faults
	l.mu.Lock()
	delete(l.quarantined, key)
	l.mu.Unlock()
}

// releaseChunk takes the blocks and block entities of the chunk at pos out
// of quarantine, as when it unloads.
func (w *World) releaseChunk(pos ChunkPos) {
	l := &w.faults
	l.mu.Lock()
	defer l.mu.Unlock()
	for key := range l.quarantined {
		if key.entity == nil && chunkPosFromBlockPos(key.pos) == pos {
			delete(l.quarantined, key)
		}
	}
}

// Quarantined returns the faults that quarantined the offenders of the
// world still in quarantine: entities, block entities and blocks that
// panicked FaultLimit times within FaultWindow ticks. They no longer tick,
// but are still saved. It is ordered by kind, type and position, and may be
// called from any goroutine.
func (w *World) Quarantined() []Fault {
	l := &w.faults
	l.mu.Lock()
	out := make([]Fault, 0, len(l.quarantined))
	for _, f := range l.quarantined {
		out = append(out, f)
	}
	l.mu.Unlock()
	slices.SortFunc(out, func(a, b Fault) int {
		if a.Kind != b.Kind {
			return int(a.Kind) - int(b.Kind)
		}
		if a.Type != b.Type {
			if a.Type < b.Type {
				return -1
			}
			return 1
		}
		for i := range a.Pos {
			if a.Pos[i] != b.Pos[i] {
				return a.Pos[i] - b.Pos[i]
			}
		}
		return 0
	})
	return out
}

// fail fails the world for the fault f, which nothing could be blamed for:
// it marks the world failed and reports f, logging it and passing it to
// Config.Fatal. A failed world no longer simulates: its ticks only have the
// controllers of its players act, so that their clients can leave it and be
// saved. Its transactions still run, and closing it still saves it. Once
// failed, a world logs the faults it fails for again as other faults,
// rate-limited, and calls Config.Fatal no more.
func (w *World) fail(f Fault) {
	f.Kind, f.World = FaultFatal, w
	l := &w.faults
	if !l.failed.CompareAndSwap(false, true) {
		l.mu.Lock()
		lines := l.record(f, l.clock()())
		l.mu.Unlock()
		for _, line := range lines {
			line.log(w.log)
		}
		return
	}
	faultLine{msg: "world failed", f: f, stack: true}.log(w.log)
	if hook := w.conf.Fatal; hook != nil {
		hook(f)
	}
}

// stepFaultLimit is how many ticks in a row a subsystem of the tick may
// panic in before the world fails.
const stepFaultLimit = 20

// stepFault is what a world keeps of the panics of a subsystem of its
// tick: the last tick it panicked in, and how many ticks in a row up to it.
type stepFault struct {
	tick  int64
	ticks int
}

// step runs f, the subsystem name of the tick at tick, and reports whether
// it ended without a panic. A panic ends the subsystem for this tick and is
// blamed on it. Once it panicked in stepFaultLimit ticks in a row, the world
// fails: what it is part of, the world, is what breaks.
func (w *World) step(tx *Tx, tick int64, name string, f func(tx *Tx)) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			w.stepPanicked(tick, name, r)
		}
	}()
	f(tx)
	return true
}

// stepPanicked blames the panic r of the subsystem name at tick on it,
// failing the world once it panicked in stepFaultLimit ticks in a row.
func (w *World) stepPanicked(tick int64, name string, r any) {
	f := newFault(FaultSubsystem, r)
	f.Type = name
	s := w.stepFaults[name]
	if s == nil {
		if w.stepFaults == nil {
			w.stepFaults = make(map[string]*stepFault)
		}
		s = &stepFault{}
		w.stepFaults[name] = s
	}
	if s.ticks == 0 || tick != s.tick+1 {
		s.ticks = 0
	}
	s.tick, s.ticks = tick, s.ticks+1
	f.Count = s.ticks
	w.reportFault(f, quarantineKey{})
	if s.ticks >= stepFaultLimit {
		w.fail(f)
	}
}

// tickGuard is the guard of a transaction ticking entities: the owner's, or
// a region's, as each region has its own transaction.
type tickGuard struct {
	// ticking is the innermost entity whose behaviour runs, nil between
	// entities.
	ticking *EntityHandle
}

// guard returns the tick guard of tx.
func (tx *Tx) guard() *tickGuard {
	if r := tx.region; r != nil {
		return &r.guard
	}
	return &tx.w.guard
}

// actorFault is what a column keeps of the panics of the block entity at a
// position: the version of the block entity that panicked, which a block
// entity replacing it does not have.
type actorFault struct {
	version     uint64
	faults      faultCount
	quarantined bool
}

// blockEntityPanicked blames the panic r of a tick of the block entity of
// ref at tick on it, counting it and quarantining the block entity once it
// reached FaultLimit. A block entity replaced before it panicked is not
// counted.
func (w *World) blockEntityPanicked(tick int64, ref blockEntityTickRef, r any) {
	f := newFault(FaultBlockEntity, r)
	f.Pos = ref.pos
	c := w.chunks[chunkPosFromBlockPos(ref.pos)]
	if c == nil || c.tickerBlockEntityVersions[ref.pos] != ref.version {
		w.reportFault(f, quarantineKey{})
		return
	}
	if be, ok := c.BlockEntities[ref.pos]; ok {
		f.Type, _ = be.EncodeBlock()
	}
	a := c.faultyActors[ref.pos]
	if a == nil || a.version != ref.version {
		if c.faultyActors == nil {
			c.faultyActors = make(map[cube.Pos]*actorFault)
		}
		a = &actorFault{version: ref.version}
		c.faultyActors[ref.pos] = a
	}
	if a.faults.count(&f, tick) {
		a.quarantined = true
	}
	w.reportFault(f, quarantineKey{pos: ref.pos, blockEntity: true})
}

// actorQuarantined reports whether the block entity of ref, in c, is
// quarantined. What c keeps of a block entity since replaced is dropped.
func (w *World) actorQuarantined(c *Column, ref blockEntityTickRef) bool {
	a, ok := c.faultyActors[ref.pos]
	if !ok {
		return false
	}
	if a.version != ref.version {
		w.forgetActor(c, ref.pos)
		return false
	}
	return a.quarantined
}

// forgetActor drops what c keeps of the panics of the block entity at pos,
// taking it out of quarantine.
func (w *World) forgetActor(c *Column, pos cube.Pos) {
	a, ok := c.faultyActors[pos]
	if !ok {
		return
	}
	delete(c.faultyActors, pos)
	if a.quarantined {
		w.release(quarantineKey{pos: pos, blockEntity: true})
	}
}

// blockFaultEntries is how many blocks whose ticks panicked a world keeps
// at most.
const blockFaultEntries = 4096

// blockFault is what a world keeps of the panics of the ticks of the block
// at a position: its name, the type of its scheduled ticks.
type blockFault struct {
	name        string
	faults      faultCount
	quarantined bool
	// seq orders the blocks by their last panic.
	seq uint64
}

// blockFaults holds the blocks whose ticks panicked, by position, at most
// blockFaultEntries of them: the one whose last panic is the oldest is
// forgotten for a new one.
type blockFaults struct {
	m   map[cube.Pos]*blockFault
	seq uint64
}

// quarantined reports whether the block name at pos is quarantined.
func (b *blockFaults) quarantined(pos cube.Pos, name string) bool {
	e := b.m[pos]
	return e != nil && e.quarantined && e.name == name
}

// blockFaultType returns the type the world keeps the panics of the block b
// under: the type of its scheduled ticks, so that a block changing its own
// state stays the block that panicked, or its name if the registry does not
// know it. It returns "" for nil.
func (w *World) blockFaultType(b Block) (typ string) {
	if b == nil {
		return ""
	}
	defer func() {
		if recover() != nil {
			typ, _ = b.EncodeBlock()
		}
	}()
	return scheduledTickType(w.conf.Blocks, b)
}

// blockQuarantined reports whether the block b at pos is quarantined: its
// random ticks, neighbour updates and precipitation are skipped. The
// common path checks len(w.blockFaults.m) first.
func (w *World) blockQuarantined(pos cube.Pos, b Block) bool {
	e := w.blockFaults.m[pos]
	return e != nil && e.quarantined && e.name == w.blockFaultType(b)
}

// blockPanicked blames the panic r of a tick of kind of the block name at
// pos, at tick, on it, counting it and quarantining the block once it
// reached FaultLimit. The panics of the scheduled ticks, random ticks and
// neighbour updates of a block count together.
func (w *World) blockPanicked(kind FaultKind, tick int64, pos cube.Pos, name string, r any) {
	f := newFault(kind, r)
	f.Type, f.Pos = name, pos
	b := &w.blockFaults
	e := b.m[pos]
	if e == nil || e.name != name {
		if e != nil {
			w.forgetBlock(pos)
		}
		if b.m == nil {
			b.m = make(map[cube.Pos]*blockFault)
		}
		if len(b.m) >= blockFaultEntries {
			w.forgetOldestBlock()
		}
		e = &blockFault{name: name}
		b.m[pos] = e
	}
	b.seq++
	e.seq = b.seq
	if e.faults.count(&f, tick) {
		e.quarantined = true
	}
	w.reportFault(f, quarantineKey{pos: pos, name: name})
}

// forgetBlock forgets the panics of the block at pos, taking it out of
// quarantine.
func (w *World) forgetBlock(pos cube.Pos) {
	e, ok := w.blockFaults.m[pos]
	if !ok {
		return
	}
	delete(w.blockFaults.m, pos)
	if e.quarantined {
		w.release(quarantineKey{pos: pos, name: e.name})
	}
}

// forgetOldestBlock forgets the block whose last panic is the oldest.
func (w *World) forgetOldestBlock() {
	var (
		oldest cube.Pos
		seq    uint64
		found  bool
	)
	for pos, e := range w.blockFaults.m {
		if !found || e.seq < seq {
			oldest, seq, found = pos, e.seq, true
		}
	}
	if found {
		w.forgetBlock(oldest)
	}
}

// blockSet forgets the panics of the block at pos once a block of another
// type, b, replaced it. A block changing its own state stays the block that
// panicked.
func (w *World) blockSet(pos cube.Pos, b Block) {
	e, ok := w.blockFaults.m[pos]
	if !ok {
		return
	}
	if w.blockFaultType(b) == e.name {
		return
	}
	w.forgetBlock(pos)
}

// forgetChunk forgets the panics of the blocks and block entities of the
// chunk at pos, which unloads.
func (w *World) forgetChunk(pos ChunkPos) {
	for p := range w.blockFaults.m {
		if chunkPosFromBlockPos(p) == pos {
			delete(w.blockFaults.m, p)
		}
	}
	w.releaseChunk(pos)
}

// FailingViewer is a Viewer told when it panicked as it was shown a change
// of the world, as the changes of the blocks of a column. A session closes
// its connection. A Viewer that is not one is left as it is once the panic
// was logged.
type FailingViewer interface {
	Viewer
	// ViewFailed is called on the owner of the world, after the panic of
	// the viewer was logged.
	ViewFailed()
}

// view shows v a change of the world through f, blaming a panic of f on v
// (viewerPanicked) instead of on the tick that showed it.
func (w *World) view(v Viewer, f func(v Viewer)) {
	defer func() {
		if r := recover(); r != nil {
			w.viewerPanicked(v, r)
		}
	}()
	f(v)
}

// viewerPanicked blames the panic r of the viewer v on it, and tells it if
// it is a FailingViewer. It is called by a function deferred while v
// panics, so that the fault has the stack of the panic.
func (w *World) viewerPanicked(v Viewer, r any) {
	f := newFault(FaultViewer, r)
	f.Type = fmt.Sprintf("%T", v)
	w.reportFault(f, quarantineKey{})
	fv, ok := v.(FailingViewer)
	if !ok {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			g := newFault(FaultViewer, r)
			g.Type = fmt.Sprintf("%T", v)
			w.reportFault(g, quarantineKey{})
		}
	}()
	fv.ViewFailed()
}

// TickFaulter is an entity that handles a panic of its own tick or of its
// controller, instead of being quarantined. Player implements it by
// disconnecting with an internal error, which saves it.
type TickFaulter interface {
	// TickPanicked is called with the fault of the panic, on the owner of
	// the world, after it was logged.
	TickPanicked(tx *Tx, f Fault)
}

// blameEntity counts the fault f, of a panic at tick, against the entity
// of handle, quarantining it once it reached FaultLimit, and reports it. An
// entity that is a TickFaulter handles the fault itself instead, on the
// owner: at once in a transaction of the owner, after the phase in one of
// a region.
func (w *World) blameEntity(tx *Tx, tick int64, handle *EntityHandle, f Fault) {
	state := handle.state
	if state == nil {
		w.reportFault(f, quarantineKey{})
		return
	}
	if _, ok := state.ent.(TickFaulter); ok {
		if f.Type == "minecraft:player" {
			f.Kind = FaultPlayer
		}
		w.reportFault(f, quarantineKey{})
		tx.deferToOwner(func(tx *Tx) {
			if handle.state != state {
				// It left the world in the meantime.
				return
			}
			w.tickPanicked(tx, handle, state, f)
		})
		return
	}
	if state.faults.count(&f, tick) {
		state.quarantined = true
	}
	w.reportFault(f, quarantineKey{entity: handle})
}

// ActOn runs f, in which a step of the tick of the world acts on the entity
// e alone, as waking a sleeper or pushing a mob does, and reports whether f
// ended without a panic. A panic of f is blamed on e, as one of its own
// tick: a player is disconnected (D2), and another entity counts towards
// its quarantine (D1). The step goes on with its next entity. An entity in
// quarantine is not acted on, and ActOn returns false for it.
func (tx *Tx) ActOn(e Entity, f func()) (ok bool) {
	handle := e.H()
	if s := handle.state; s != nil && s.quarantined {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			tx.actPanicked(handle, r)
		}
	}()
	f()
	return true
}

// actPanicked blames the panic r of ActOn on the entity of handle. In a
// region holding the state regions share, the panic goes on to the guard of
// the region, which releases the state before it blames anyone.
func (tx *Tx) actPanicked(handle *EntityHandle, r any) {
	if rg := tx.region; rg != nil && rg.locked != 0 {
		panic(r)
	}
	f := newFault(FaultEntity, r)
	f.Type, f.Pos, f.Entity = handle.t.EncodeEntity(), cube.PosFromVec3(handle.data.Pos), handle
	tx.w.blameEntity(tx, tx.CurrentTick(), handle, f)
}

// tickPanicked has the entity of handle, a TickFaulter, handle the fault f.
// A panic of it is reported as a fault of the transaction.
func (w *World) tickPanicked(tx *Tx, handle *EntityHandle, state *entityState, f Fault) {
	defer func() {
		if r := recover(); r != nil {
			g := newFault(FaultTransaction, r)
			g.Type = panicSite()
			w.reportFault(g, quarantineKey{})
		}
	}()
	// Bound to tx, as the transaction the entity panicked in, a region's
	// perhaps, may have ended.
	if tf, ok := state.entity(tx, handle).(TickFaulter); ok {
		tf.TickPanicked(tx, f)
	}
}

// forgetEntity takes the entity of handle, whose state in the world is
// state, out of quarantine, as it leaves the world.
func (w *World) forgetEntity(handle *EntityHandle, state *entityState) {
	if state != nil && state.quarantined {
		w.release(quarantineKey{entity: handle})
	}
}
