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
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

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
	// failed is set once the world reported a fatal fault.
	failed bool
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

// reportFault reports the fault f of the world: it is logged, rate-limited
// by kind and type, and passed to Config.Fault. A fault with Quarantined
// set lists its offender, of key, as quarantined.
func (w *World) reportFault(f Fault, key quarantineKey) {
	f.World = w
	l := &w.faults
	l.mu.Lock()
	now := time.Now
	if l.now != nil {
		now = l.now
	}
	lines := l.record(f, now())
	if f.Quarantined {
		if l.quarantined == nil {
			l.quarantined = make(map[quarantineKey]Fault)
		}
		l.quarantined[key] = f
	}
	l.mu.Unlock()
	for _, line := range lines {
		line.log(w.conf.Log)
	}
	if hook := w.conf.Fault; hook != nil {
		hook(f)
	}
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

// fatal reports a fault nothing could be blamed for. The first one of the
// world is logged and passed to Config.Fatal; later ones are logged only.
func (w *World) fatal(f Fault) {
	f.Kind, f.World = FaultFatal, w
	l := &w.faults
	l.mu.Lock()
	first := !l.failed
	l.failed = true
	l.mu.Unlock()
	faultLine{msg: "world failed", f: f, stack: true}.log(w.conf.Log)
	if hook := w.conf.Fatal; first && hook != nil {
		hook(f)
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
