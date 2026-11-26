package cmd

import (
	"cmp"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const errUnexpectedCommand = "An unexpected error occurred trying to execute that command"

// ForEach resolves sel as src sees it, runs each once per target, in a
// transaction of the world the target is in, and then runs then once, with
// what each returned in the order of the selection.
//
// If every target is in the world of tx, all of it happens before ForEach
// returns, and the output of the command is sent as that of any command.
// Otherwise ForEach returns at once: each runs on the owners of the other
// worlds, then runs later in a new transaction of the world of tx, and the
// output is sent after it. Nothing waits.
//
// each must use only tx and t and the values it captured. It must not
// capture src, o, the transaction of the command, or an entity.
// After calling ForEach, Run must return without using o: what follows
// belongs in then.
func ForEach[R any](
	src Source, o *Output, tx *world.Tx, sel Selector,
	each func(tx *world.Tx, t Target) R,
	then func(src Source, o *Output, tx *world.Tx, results []Result[R]),
) {
	if sel == nil || sel.selector() == nil {
		if then != nil {
			then(src, o, tx, nil)
		}
		return
	}
	s := sel.selector()
	ctxWorld := contextWorld(src, tx)
	if tx == nil && ctxWorld == nil {
		if then != nil {
			then(src, o, nil, nil)
		}
		return
	}
	switch s.scope() {
	case scopeSelf:
		forEachSelf(src, o, tx, ctxWorld, s, each, then)
	case scopeNamed:
		forEachNamed(src, o, tx, ctxWorld, s, each, then)
	case scopeWorld:
		forEachWorld(src, o, tx, ctxWorld, s, each, then)
	case scopePlayers:
		forEachPlayers(src, o, tx, ctxWorld, s, each, then)
	default:
		forEachEntities(src, o, tx, ctxWorld, s, each, then)
	}
}

// contextWorld returns the execution world of src, falling back to tx's world.
func contextWorld(src Source, tx *world.Tx) *world.World {
	if ec, ok := ExecutionContextOf(src); ok && ec.World != nil {
		return ec.World
	}
	if tx != nil {
		return tx.World()
	}
	return nil
}

// evalTarget reads the target's display name and handle before invoking each,
// so commands that kill or despawn the target still report its name.
func evalTarget[R any](tx *world.Tx, t Target, each func(*world.Tx, Target) R) Result[R] {
	name := targetDisplayName(t)
	h := targetHandle(t)
	return Result[R]{Name: name, Handle: h, Value: each(tx, t)}
}

// evalTargets filters, sorts, limits and evaluates local targets synchronously.
func evalLocalTargets[R any](tx *world.Tx, s *entitySelector, candidates []Target, each func(*world.Tx, Target) R) []Result[R] {
	matched := make([]Target, 0, len(candidates))
	for _, t := range candidates {
		if s.matchesTarget(tx, t) {
			matched = append(matched, t)
		}
	}
	chosen := s.sortAndLimitTargets(matched)
	results := make([]Result[R], len(chosen))
	for i, t := range chosen {
		results[i] = evalTarget(tx, t, each)
	}
	return results
}

func forEachSelf[R any](
	src Source, o *Output, tx *world.Tx, ctxWorld *world.World, s *entitySelector,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	self, ok := executor(src)
	if !ok || self == nil {
		if then != nil {
			then(src, o, tx, nil)
		}
		return
	}
	if e, ok := self.(world.Entity); ok {
		h := e.H()
		if live, ok := handleEntityInTx(h, tx); ok {
			var results []Result[R]
			if s.matchesTarget(tx, live) {
				results = []Result[R]{evalTarget(tx, live, each)}
			}
			if then != nil {
				then(src, o, tx, results)
			}
			return
		}
		forEachSingleHandle(src, o, ctxWorld, h, s, each, then)
		return
	}
	var results []Result[R]
	if s.matchesTarget(tx, self) {
		results = []Result[R]{evalTarget(tx, self, each)}
	}
	if then != nil {
		then(src, o, tx, results)
	}
}

func forEachNamed[R any](
	src Source, o *Output, tx *world.Tx, ctxWorld *world.World, s *entitySelector,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	if s.hasUUID {
		forEachUUID(src, o, tx, ctxWorld, s, each, then)
		return
	}
	if w, ok := installedWorlds(); ok {
		h, found := w.PlayerByName(s.name)
		if !found || h == nil {
			if then != nil {
				then(src, o, tx, nil)
			}
			return
		}
		if e, ok := handleEntityInTx(h, tx); ok {
			var results []Result[R]
			if s.matchesTarget(tx, e) {
				results = []Result[R]{evalTarget(tx, e, each)}
			}
			if then != nil {
				then(src, o, tx, results)
			}
			return
		}
		forEachSingleHandle(src, o, ctxWorld, h, s, each, then)
		return
	}
	var results []Result[R]
	if tx != nil {
		for p := range tx.Players() {
			if named, ok := p.(NamedTarget); ok && strings.EqualFold(named.Name(), s.name) {
				if s.matchesTarget(tx, p) {
					results = []Result[R]{evalTarget(tx, p, each)}
				}
				break
			}
		}
	}
	if then != nil {
		then(src, o, tx, results)
	}
}

func forEachUUID[R any](
	src Source, o *Output, tx *world.Tx, ctxWorld *world.World, s *entitySelector,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	if tx != nil {
		for e := range tx.Entities() {
			if e.H().UUID() == s.entityUUID {
				var results []Result[R]
				if s.matchesTarget(tx, e) {
					results = []Result[R]{evalTarget(tx, e, each)}
				}
				if then != nil {
					then(src, o, tx, results)
				}
				return
			}
		}
	}
	w, ok := installedWorlds()
	if !ok {
		if then != nil {
			then(src, o, tx, nil)
		}
		return
	}
	var remote []*world.World
	for _, rw := range w.Worlds() {
		if tx == nil || rw != tx.World() {
			remote = append(remote, rw)
		}
	}
	if len(remote) == 0 {
		if then != nil {
			then(src, o, tx, nil)
		}
		return
	}
	ref := captureSource(src)
	o.acquire(src)
	j := newJoiner(func(items []orderedResult[R], timedOut int, panicked bool) {
		finishAsync(ctxWorld, ref, o, unwrapOrdered(items), timedOut, panicked, then)
	})
	for idx, rw := range remote {
		j.spawn(rw.Name(), 1, func() *world.Task {
			return rw.Do(func(rtx *world.Tx) {
				for e := range rtx.Entities() {
					if e.H().UUID() == s.entityUUID {
						if s.matchesTarget(rtx, e) {
							j.record(orderedResult[R]{worldIdx: idx, res: evalTarget(rtx, e, each)})
						}
						return
					}
				}
			})
		})
	}
	j.ready(getCrossWorldTimeout())
}

func forEachSingleHandle[R any](
	src Source, o *Output, ctxWorld *world.World, h *world.EntityHandle, s *entitySelector,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	ref := captureSource(src)
	o.acquire(src)
	j := newJoiner(func(items []orderedResult[R], timedOut int, panicked bool) {
		finishAsync(ctxWorld, ref, o, unwrapOrdered(items), timedOut, panicked, then)
	})
	j.spawn(handleWorldName(h), 1, func() *world.Task {
		return h.Do(func(rtx *world.Tx, e world.Entity) {
			if s.matchesTarget(rtx, e) {
				j.record(orderedResult[R]{res: evalTarget(rtx, e, each)})
			}
		})
	})
	j.ready(getCrossWorldTimeout())
}

func forEachWorld[R any](
	src Source, o *Output, tx *world.Tx, ctxWorld *world.World, s *entitySelector,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	if tx != nil && ctxWorld == tx.World() {
		if !s.includesEntities {
			if w, ok := installedWorlds(); ok {
				part := partitionPlayers(tx, w.Online(), ctxWorld)
				if len(part.remoteGroups) == 0 && len(part.remoteHandles) == 0 {
					results := evalLocalTargets(tx, s, part.localTargets(), each)
					if then != nil {
						then(src, o, tx, results)
					}
					return
				}
				forEachPlayerPartition(src, o, tx, ctxWorld, s, part, each, then)
				return
			}
		}
		results := evalLocalTargets(tx, s, txCandidates(tx, s.includesEntities), each)
		if then != nil {
			then(src, o, tx, results)
		}
		return
	}
	// Context world differs from tx.World(): single hop to ctxWorld.
	ref := captureSource(src)
	o.acquire(src)
	j := newJoiner(func(items []orderedResult[R], timedOut int, panicked bool) {
		finishAsync(ctxWorld, ref, o, unwrapOrdered(items), timedOut, panicked, then)
	})
	j.spawn(ctxWorld.Name(), 1, func() *world.Task {
		return ctxWorld.Do(func(cTx *world.Tx) {
			rs := evalLocalTargets(cTx, s, worldCandidatesInTx(cTx, s.includesEntities), each)
			batch := make([]orderedResult[R], len(rs))
			for i, r := range rs {
				batch[i] = orderedResult[R]{localIdx: i, res: r}
			}
			j.record(batch...)
		})
	})
	j.ready(getCrossWorldTimeout())
}

func forEachPlayers[R any](
	src Source, o *Output, tx *world.Tx, ctxWorld *world.World, s *entitySelector,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	w, ok := installedWorlds()
	if !ok {
		var results []Result[R]
		if tx != nil {
			results = evalLocalTargets(tx, s, txCandidates(tx, false), each)
		}
		if then != nil {
			then(src, o, tx, results)
		}
		return
	}
	part := partitionPlayers(tx, w.Online(), nil)
	if len(part.remoteGroups) == 0 && len(part.remoteHandles) == 0 {
		var results []Result[R]
		if tx != nil {
			results = evalLocalTargets(tx, s, part.localTargets(), each)
		}
		if then != nil {
			then(src, o, tx, results)
		}
		return
	}
	forEachPlayerPartition(src, o, tx, ctxWorld, s, part, each, then)
}

func forEachPlayerPartition[R any](
	src Source, o *Output, tx *world.Tx, ctxWorld *world.World, s *entitySelector,
	part playerPartition,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	if !s.bounded() {
		forEachPlayersUnbounded(src, o, tx, ctxWorld, s, part, each, then)
		return
	}
	forEachPlayersBounded(src, o, tx, ctxWorld, s, part, each, then)
}

func forEachPlayersUnbounded[R any](
	src Source, o *Output, tx *world.Tx, ctxWorld *world.World, s *entitySelector,
	part playerPartition,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	// Evaluate local players inline before acquiring o or spawning remote tasks.
	localResults := make([]orderedResult[R], 0, len(part.local))
	for _, lp := range part.local {
		if s.matchesTarget(tx, lp.entity) {
			localResults = append(localResults, orderedResult[R]{
				localIdx: lp.order,
				res:      evalTarget(tx, lp.entity, each),
			})
		}
	}
	ref := captureSource(src)
	o.acquire(src)
	j := newJoiner(func(items []orderedResult[R], timedOut int, panicked bool) {
		finishAsync(ctxWorld, ref, o, unwrapOrdered(items), timedOut, panicked, then)
	})
	j.record(localResults...)
	for rw, grp := range part.remoteGroups {
		j.spawn(rw.Name(), len(grp), func() *world.Task {
			return rw.Do(func(rtx *world.Tx) {
				var batch []orderedResult[R]
				for _, p := range grp {
					if e, ok := handleEntityInTx(p.handle, rtx); ok {
						if s.matchesTarget(rtx, e) {
							batch = append(batch, orderedResult[R]{localIdx: p.order, res: evalTarget(rtx, e, each)})
						}
						continue
					}
					resendHandleEffect(j, p, s, each)
				}
				j.record(batch...)
			})
		})
	}
	for _, p := range part.remoteHandles {
		resendHandleEffect(j, p, s, each)
	}
	j.ready(getCrossWorldTimeout())
}

func forEachPlayersBounded[R any](
	src Source, o *Output, tx *world.Tx, ctxWorld *world.World, s *entitySelector,
	part playerPartition,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	localCands := make([]selectedCandidate, 0, len(part.local))
	for _, lp := range part.local {
		if s.matchesTarget(tx, lp.entity) {
			localCands = append(localCands, selectedCandidate{
				localIdx: lp.order,
				handle:   lp.entity.H(),
				pos:      lp.entity.Position(),
			})
		}
	}
	deadline := time.Now().Add(getCrossWorldTimeout())
	ref := captureSource(src)
	o.acquire(src)
	j := newJoiner(func(cands []selectedCandidate, timedOut int, panicked bool) {
		dispatchRound2(ctxWorld, ref, o, s, cands, timedOut, panicked, deadline, each, then)
	})
	j.record(localCands...)
	for rw, grp := range part.remoteGroups {
		j.spawn(rw.Name(), len(grp), func() *world.Task {
			return rw.Do(func(rtx *world.Tx) {
				var batch []selectedCandidate
				for _, p := range grp {
					if e, ok := handleEntityInTx(p.handle, rtx); ok {
						if s.matchesTarget(rtx, e) {
							batch = append(batch, selectedCandidate{
								localIdx: p.order,
								handle:   p.handle,
								pos:      e.Position(),
							})
						}
						continue
					}
					resendHandleSelect(j, p, s)
				}
				j.record(batch...)
			})
		})
	}
	for _, p := range part.remoteHandles {
		resendHandleSelect(j, p, s)
	}
	j.ready(time.Until(deadline))
}

func forEachEntities[R any](
	src Source, o *Output, tx *world.Tx, ctxWorld *world.World, s *entitySelector,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	w, ok := installedWorlds()
	if !ok {
		var results []Result[R]
		if tx != nil {
			results = evalLocalTargets(tx, s, txCandidates(tx, true), each)
		}
		if then != nil {
			then(src, o, tx, results)
		}
		return
	}
	ws := w.Worlds()
	if len(ws) == 0 || (tx != nil && len(ws) == 1 && ws[0] == tx.World()) {
		var results []Result[R]
		if tx != nil {
			results = evalLocalTargets(tx, s, txCandidates(tx, true), each)
		}
		if then != nil {
			then(src, o, tx, results)
		}
		return
	}
	if !s.bounded() {
		forEachEntitiesUnbounded(src, o, tx, ctxWorld, s, ws, each, then)
		return
	}
	forEachEntitiesBounded(src, o, tx, ctxWorld, s, ws, each, then)
}

func forEachEntitiesUnbounded[R any](
	src Source, o *Output, tx *world.Tx, ctxWorld *world.World, s *entitySelector,
	ws []*world.World,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	var localResults []orderedResult[R]
	if tx != nil {
		for wIdx, rw := range ws {
			if rw != tx.World() {
				continue
			}
			lIdx := 0
			for e := range tx.Entities() {
				if s.matchesTarget(tx, e) {
					localResults = append(localResults, orderedResult[R]{
						worldIdx: wIdx,
						localIdx: lIdx,
						res:      evalTarget(tx, e, each),
					})
				}
				lIdx++
			}
		}
	}
	ref := captureSource(src)
	o.acquire(src)
	j := newJoiner(func(items []orderedResult[R], timedOut int, panicked bool) {
		finishAsync(ctxWorld, ref, o, unwrapOrdered(items), timedOut, panicked, then)
	})
	j.record(localResults...)
	for wIdx, rw := range ws {
		if tx != nil && rw == tx.World() {
			continue
		}
		j.spawn(rw.Name(), 1, func() *world.Task {
			return rw.Do(func(rtx *world.Tx) {
				var batch []orderedResult[R]
				lIdx := 0
				for e := range rtx.Entities() {
					if s.matchesTarget(rtx, e) {
						batch = append(batch, orderedResult[R]{
							worldIdx: wIdx,
							localIdx: lIdx,
							res:      evalTarget(rtx, e, each),
						})
					}
					lIdx++
				}
				j.record(batch...)
			})
		})
	}
	j.ready(getCrossWorldTimeout())
}

func forEachEntitiesBounded[R any](
	src Source, o *Output, tx *world.Tx, ctxWorld *world.World, s *entitySelector,
	ws []*world.World,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	var localCands []selectedCandidate
	if tx != nil {
		for wIdx, rw := range ws {
			if rw != tx.World() {
				continue
			}
			lIdx := 0
			for e := range tx.Entities() {
				if s.matchesTarget(tx, e) {
					localCands = append(localCands, selectedCandidate{
						worldIdx: wIdx,
						localIdx: lIdx,
						handle:   e.H(),
						pos:      e.Position(),
					})
				}
				lIdx++
			}
		}
	}
	deadline := time.Now().Add(getCrossWorldTimeout())
	ref := captureSource(src)
	o.acquire(src)
	j := newJoiner(func(cands []selectedCandidate, timedOut int, panicked bool) {
		dispatchRound2(ctxWorld, ref, o, s, cands, timedOut, panicked, deadline, each, then)
	})
	j.record(localCands...)
	for wIdx, rw := range ws {
		if tx != nil && rw == tx.World() {
			continue
		}
		j.spawn(rw.Name(), 1, func() *world.Task {
			return rw.Do(func(rtx *world.Tx) {
				var batch []selectedCandidate
				lIdx := 0
				for e := range rtx.Entities() {
					if s.matchesTarget(rtx, e) {
						batch = append(batch, selectedCandidate{
							worldIdx: wIdx,
							localIdx: lIdx,
							handle:   e.H(),
							pos:      e.Position(),
						})
					}
					lIdx++
				}
				j.record(batch...)
			})
		})
	}
	j.ready(time.Until(deadline))
}

// dispatchRound2 sorts and limits Round-1 candidates on ctxWorld, runs each
// inline for targets already in ctxWorld and via EntityHandle.Do for remote
// targets, then delivers results to then.
func dispatchRound2[R any](
	ctxWorld *world.World, ref sourceRef, o *Output, s *entitySelector,
	cands []selectedCandidate, prevTimedOut int, prevPanicked bool, deadline time.Time,
	each func(*world.Tx, Target) R,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	t := ctxWorld.Do(func(cTx *world.Tx) {
		chosen := s.sortAndLimitCandidates(cands)
		if len(chosen) == 0 {
			src := ref.resolve(cTx)
			runThen(src, o, cTx, nil, prevTimedOut, prevPanicked, then)
			o.release(cTx, src)
			return
		}
		var (
			localResults []orderedResult[R]
			remote       []orderedHandle
		)
		for idx, c := range chosen {
			if e, ok := handleEntityInTx(c.handle, cTx); ok {
				localResults = append(localResults, orderedResult[R]{
					localIdx: idx,
					res:      evalTarget(cTx, e, each),
				})
				continue
			}
			remote = append(remote, orderedHandle{order: idx, handle: c.handle})
		}
		if len(remote) == 0 {
			src := ref.resolve(cTx)
			runThen(src, o, cTx, unwrapOrdered(localResults), prevTimedOut, prevPanicked, then)
			o.release(cTx, src)
			return
		}
		j2 := newJoiner(func(items []orderedResult[R], timedOut int, panicked bool) {
			finishAsync(ctxWorld, ref, o, unwrapOrdered(items), prevTimedOut+timedOut, prevPanicked || panicked, then)
		})
		j2.record(localResults...)
		for _, rh := range remote {
			j2.spawn(handleWorldName(rh.handle), 1, func() *world.Task {
				return rh.handle.Do(func(rtx *world.Tx, e world.Entity) {
					j2.record(orderedResult[R]{localIdx: rh.order, res: evalTarget(rtx, e, each)})
				})
			})
		}
		rem := time.Until(deadline)
		if rem <= 0 {
			rem = time.Nanosecond
		}
		j2.ready(rem)
	})
	t.OnDone(func(err error) {
		if err == nil {
			return
		}
		if errors.Is(err, world.ErrTaskPanicked) {
			o.Error(errUnexpectedCommand)
			o.release(nil, ref.resolve(nil))
			return
		}
		o.abort()
	})
}

func finishAsync[R any](
	ctxWorld *world.World, ref sourceRef, o *Output,
	results []Result[R], timedOut int, panicked bool,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	t := ctxWorld.Do(func(cTx *world.Tx) {
		src := ref.resolve(cTx)
		runThen(src, o, cTx, results, timedOut, panicked, then)
		o.release(cTx, src)
	})
	t.OnDone(func(err error) {
		if err == nil {
			return
		}
		if errors.Is(err, world.ErrTaskPanicked) {
			o.Error(errUnexpectedCommand)
			o.release(nil, ref.resolve(nil))
			return
		}
		o.abort()
	})
}

func runThen[R any](
	src Source, o *Output, tx *world.Tx,
	results []Result[R], timedOut int, panicked bool,
	then func(Source, *Output, *world.Tx, []Result[R]),
) {
	if then != nil {
		then(src, o, tx, results)
	}
	if panicked {
		o.Error(errUnexpectedCommand)
	}
	if timedOut > 0 {
		o.Errorf("Could not reach %d targets in time", timedOut)
	}
}

func resendHandleEffect[R any](j *joiner[orderedResult[R]], p orderedHandle, s *entitySelector, each func(*world.Tx, Target) R) {
	j.spawn(handleWorldName(p.handle), 1, func() *world.Task {
		return p.handle.Do(func(rtx *world.Tx, e world.Entity) {
			if s.matchesTarget(rtx, e) {
				j.record(orderedResult[R]{localIdx: p.order, res: evalTarget(rtx, e, each)})
			}
		})
	})
}

func resendHandleSelect(j *joiner[selectedCandidate], p orderedHandle, s *entitySelector) {
	j.spawn(handleWorldName(p.handle), 1, func() *world.Task {
		return p.handle.Do(func(rtx *world.Tx, e world.Entity) {
			if s.matchesTarget(rtx, e) {
				j.record(selectedCandidate{localIdx: p.order, handle: p.handle, pos: e.Position()})
			}
		})
	})
}

type orderedEntity struct {
	order  int
	entity world.Entity
}

type orderedHandle struct {
	order  int
	handle *world.EntityHandle
}

type playerPartition struct {
	local         []orderedEntity
	remoteGroups  map[*world.World][]orderedHandle
	remoteHandles []orderedHandle
}

func (p playerPartition) localTargets() []Target {
	out := make([]Target, len(p.local))
	for i, lp := range p.local {
		out[i] = lp.entity
	}
	return out
}

// handleEntityInTx opens h in tx only if h is currently owned by tx.World().
// Checking h.World() first acquires the handle's lock, avoiding a data race
// with concurrent cross-world transfers when h is in another world.
func handleEntityInTx(h *world.EntityHandle, tx *world.Tx) (world.Entity, bool) {
	if h == nil || tx == nil || h.World() != tx.World() {
		return nil, false
	}
	return h.Entity(tx)
}

// partitionPlayers splits online player handles into those directly accessible
// in tx, those grouped by another world, and those that must be reached via
// EntityHandle.Do (in transfer or in another region during a region phase).
// If onlyWorld is non-nil, players in worlds other than onlyWorld are ignored.
func partitionPlayers(tx *world.Tx, handles []*world.EntityHandle, onlyWorld *world.World) playerPartition {
	var p playerPartition
	var txWorld *world.World
	if tx != nil {
		txWorld = tx.World()
	}
	for idx, h := range handles {
		rw := h.World()
		if tx != nil && rw == txWorld {
			if e, ok := h.Entity(tx); ok {
				if onlyWorld == nil || txWorld == onlyWorld {
					p.local = append(p.local, orderedEntity{order: idx, entity: e})
				}
				continue
			}
		}
		if onlyWorld != nil && rw != onlyWorld {
			continue
		}
		if rw != nil && rw != txWorld {
			if p.remoteGroups == nil {
				p.remoteGroups = make(map[*world.World][]orderedHandle)
			}
			p.remoteGroups[rw] = append(p.remoteGroups[rw], orderedHandle{order: idx, handle: h})
			continue
		}
		p.remoteHandles = append(p.remoteHandles, orderedHandle{order: idx, handle: h})
	}
	return p
}

func txCandidates(tx *world.Tx, includeEntities bool) []Target {
	if includeEntities {
		var out []Target
		for e := range tx.Entities() {
			out = append(out, e)
		}
		return out
	}
	var out []Target
	for p := range tx.Players() {
		out = append(out, p)
	}
	return out
}

func worldCandidatesInTx(tx *world.Tx, includeEntities bool) []Target {
	if !includeEntities {
		if w, ok := installedWorlds(); ok {
			var out []Target
			for _, h := range w.Online() {
				if e, ok := handleEntityInTx(h, tx); ok {
					out = append(out, e)
				}
			}
			return out
		}
	}
	return txCandidates(tx, includeEntities)
}

type orderedResult[R any] struct {
	worldIdx int
	localIdx int
	res      Result[R]
}

func unwrapOrdered[R any](items []orderedResult[R]) []Result[R] {
	if len(items) == 0 {
		return nil
	}
	slices.SortFunc(items, func(a, b orderedResult[R]) int {
		if c := cmp.Compare(a.worldIdx, b.worldIdx); c != 0 {
			return c
		}
		return cmp.Compare(a.localIdx, b.localIdx)
	})
	out := make([]Result[R], len(items))
	for i, it := range items {
		out[i] = it.res
	}
	return out
}

type taskEntry struct {
	task      *world.Task
	worldName string
	weight    int
	done      bool
}

type joiner[T any] struct {
	mu       sync.Mutex
	pending  int
	finished bool
	items    []T
	tasks    []*taskEntry
	timedOut int
	panicked bool
	timer    *time.Timer
	onFinish func(items []T, timedOut int, panicked bool)
}

func newJoiner[T any](onFinish func(items []T, timedOut int, panicked bool)) *joiner[T] {
	return &joiner[T]{pending: 1, onFinish: onFinish}
}

func (j *joiner[T]) record(items ...T) {
	if len(items) == 0 {
		return
	}
	j.mu.Lock()
	if !j.finished {
		j.items = append(j.items, items...)
	}
	j.mu.Unlock()
}

func (j *joiner[T]) spawn(worldName string, weight int, schedule func() *world.Task) {
	entry := &taskEntry{worldName: worldName, weight: weight}
	j.mu.Lock()
	if j.finished {
		j.mu.Unlock()
		return
	}
	j.pending++
	j.tasks = append(j.tasks, entry)
	j.mu.Unlock()

	t := schedule()

	j.mu.Lock()
	if j.finished {
		j.mu.Unlock()
		t.Cancel()
		return
	}
	entry.task = t
	j.mu.Unlock()

	t.OnDone(func(err error) { j.complete(entry, err) })
}

func (j *joiner[T]) complete(entry *taskEntry, err error) {
	j.mu.Lock()
	if entry.done {
		j.mu.Unlock()
		return
	}
	entry.done = true
	if !j.finished && err != nil {
		switch {
		case errors.Is(err, world.ErrTaskPanicked):
			j.panicked = true
		case errors.Is(err, world.ErrEntityClosed), errors.Is(err, world.ErrWorldClosed):
			slog.Debug("cross-world selector target unavailable", "world", entry.worldName, "err", err)
		}
	}
	j.pending--
	finish := !j.finished && j.pending == 0
	if finish {
		j.finished = true
	}
	timer := j.timer
	items, timedOut, panicked := j.items, j.timedOut, j.panicked
	j.mu.Unlock()
	if finish {
		if timer != nil {
			timer.Stop()
		}
		j.onFinish(items, timedOut, panicked)
	}
}

func (j *joiner[T]) ready(timeout time.Duration) {
	j.mu.Lock()
	j.pending--
	finish := !j.finished && j.pending == 0
	if finish {
		j.finished = true
	} else if !j.finished {
		if timeout <= 0 {
			timeout = time.Nanosecond
		}
		j.timer = time.AfterFunc(timeout, j.onTimeout)
	}
	items, timedOut, panicked := j.items, j.timedOut, j.panicked
	j.mu.Unlock()
	if finish {
		j.onFinish(items, timedOut, panicked)
	}
}

func (j *joiner[T]) onTimeout() {
	j.mu.Lock()
	if j.finished {
		j.mu.Unlock()
		return
	}
	j.finished = true
	var (
		toCancel []*world.Task
		names    []string
	)
	for _, e := range j.tasks {
		if !e.done {
			j.timedOut += e.weight
			if e.task != nil {
				toCancel = append(toCancel, e.task)
			}
			names = append(names, e.worldName)
		}
	}
	items, timedOut, panicked := j.items, j.timedOut, j.panicked
	j.mu.Unlock()

	for _, t := range toCancel {
		t.Cancel()
	}
	for _, name := range names {
		slog.Warn("cross-world selector timed out", "world", name)
	}
	j.onFinish(items, timedOut, panicked)
}

func handleWorldName(h *world.EntityHandle) string {
	if h != nil {
		if w := h.World(); w != nil {
			return w.Name()
		}
	}
	return "unknown"
}

// sourceRef snapshots a command Source across transaction boundaries without
// retaining a world.Entity pointer.
type sourceRef struct {
	ctx        *ExecutionContext
	execHandle *world.EntityHandle
	base       Source
	handle     *world.EntityHandle
	name       string
	level      Level
	pos        mgl64.Vec3
}

func captureSource(src Source) sourceRef {
	var ref sourceRef
	if src == nil {
		return ref
	}
	ref.pos = src.Position()
	ref.level = SourceLevel(src)
	if ec, ok := ExecutionContextOf(src); ok {
		if ec.Executor != nil {
			ref.execHandle = ec.Executor.H()
			ec.Executor = nil
		}
		ref.ctx = &ec
	}
	inner := src
	for {
		cs, ok := inner.(ContextSource)
		if !ok {
			break
		}
		inner = cs.Source
	}
	base := inner
	for {
		if _, ok := base.(world.Entity); ok {
			break
		}
		w, ok := base.(Wrapper)
		if !ok {
			break
		}
		base = w.Unwrap()
	}
	if e, ok := base.(world.Entity); ok {
		ref.handle = e.H()
		ref.name = targetDisplayName(base)
		ref.pos = base.Position()
		return ref
	}
	ref.base = inner
	return ref
}

func (r sourceRef) resolve(tx *world.Tx) Source {
	var base Source
	if r.handle != nil {
		if e, ok := handleEntityInTx(r.handle, tx); ok {
			if s, ok := e.(Source); ok {
				base = s
			}
		}
		if base == nil {
			base = standInSource{
				handle: r.handle,
				name:   r.name,
				level:  r.level,
				pos:    r.pos,
			}
		}
	} else {
		base = r.base
	}
	if r.ctx == nil {
		return base
	}
	ec := *r.ctx
	if e, ok := handleEntityInTx(r.execHandle, tx); ok {
		ec.Executor = e
	}
	return WithContext(base, ec)
}

// standInSource stands in for an entity source that moved worlds or left the
// server before an asynchronous command's then callback ran.
type standInSource struct {
	handle *world.EntityHandle
	name   string
	level  Level
	pos    mgl64.Vec3
}

func (s standInSource) Name() string           { return s.name }
func (s standInSource) Position() mgl64.Vec3   { return s.pos }
func (s standInSource) PermissionLevel() Level { return s.level }

func (s standInSource) SendCommandOutput(o *Output) {
	if s.handle == nil {
		return
	}
	s.handle.Do(func(_ *world.Tx, e world.Entity) {
		if src, ok := e.(Source); ok {
			src.SendCommandOutput(o)
		}
	})
}
