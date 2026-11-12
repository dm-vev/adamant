package world

import (
	"math"
	"slices"
	"sync"
	"sync/atomic"
)

// generationQueue holds the chunk generation tasks shared by the generator
// workers. Workers take the task of the chunk nearest to a loader first, as
// Java generates the chunks with the lowest ticket level first, so the chunks
// next to a player never wait behind those at the edge of a view. Of the
// tasks about as near, a worker takes the one next to its last, so that it
// keeps to one player's frontier, whose terrain the generator still holds,
// see popNear. Pushing never blocks, so the world goroutine requesting chunks never waits for the
// workers.
type generationQueue struct {
	mu    sync.Mutex
	ready sync.Cond
	tasks []queuedGeneration
	// dists holds the distances of tasks to the loaders while pop looks for
	// the next.
	dists []int64
	next  uint64
	// focus holds the chunk positions of the loaders.
	focus  []ChunkPos
	closed bool
	// length is the number of tasks waiting, stored under mu and read
	// without it, for metrics.
	length atomic.Int64
}

// queuedGeneration is a waiting task, numbered in the order it was pushed.
type queuedGeneration struct {
	task generationTask
	seq  uint64
	// awaited is set for the chunk the world goroutine waits for.
	awaited bool
}

func newGenerationQueue() *generationQueue {
	q := &generationQueue{}
	q.ready.L = &q.mu
	return q
}

// push appends a task and returns the number of tasks waiting. It returns
// false if the queue was closed.
func (q *generationQueue) push(task generationTask) (int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return 0, false
	}
	q.tasks = append(q.tasks, queuedGeneration{task: task, seq: q.next})
	q.next++
	q.length.Store(int64(len(q.tasks)))
	q.ready.Signal()
	return len(q.tasks), true
}

// setFocus sets the chunk positions of the loaders that tasks are ordered by.
func (q *generationQueue) setFocus(focus []ChunkPos) {
	q.mu.Lock()
	q.focus = append(q.focus[:0], focus...)
	q.mu.Unlock()
}

// frontierBand is how much farther from its loader, in chunks, a task may be
// than the task nearest to any loader for a worker to take it because it is
// next to the chunk the worker generated last.
const frontierBand = 8

// frontierRun is how many chunks in a row a worker generates on one frontier
// before it takes the task nearest to a loader again. A chunk generated
// next to others shares the terrain and feature records of its neighbours,
// and costs a fraction of one generated alone; but a loader that outruns the
// workers always has tasks next to the last, and would keep them all.
const frontierRun = 64

// generationCursor is where a generation worker is: the chunk it generated
// last and how many it generated in a row on that frontier.
type generationCursor struct {
	last ChunkPos
	run  int
}

// pop waits for the next task: the awaited one, otherwise the one nearest
// to a loader, otherwise the oldest. It returns false once the queue is
// closed.
func (q *generationQueue) pop() (generationTask, bool) {
	return q.popNear(nil)
}

// popNear is pop for the worker at cursor, if not nil, which it updates. For
// frontierRun chunks in a row, it takes the one nearest to the last of the
// tasks within frontierBand of the distance of the task nearest to a loader:
// a worker keeps to the frontier of the loader it is on, whose terrain the
// generator still holds, rather than alternating between the frontiers of
// loaders that wait about as long. Then it takes the task nearest to a loader,
// and keeps to the frontier of that one.
func (q *generationQueue) popNear(cursor *generationCursor) (generationTask, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		for len(q.tasks) == 0 && !q.closed {
			q.ready.Wait()
		}
		if q.closed {
			return generationTask{}, false
		}
		if q.dropAbandoned(); len(q.tasks) != 0 {
			break
		}
	}
	dists := q.dists[:0]
	best := 0
	for i := range q.tasks {
		dist := q.distance(q.tasks[i].task.pos)
		dists = append(dists, dist)
		if i == 0 {
			continue
		}
		candidate, current := &q.tasks[i], &q.tasks[best]
		if candidate.awaited != current.awaited {
			if candidate.awaited {
				best = i
			}
			continue
		}
		if dist < dists[best] || dist == dists[best] && candidate.seq < current.seq {
			best = i
		}
	}
	q.dists = dists
	if cursor != nil {
		switch {
		case q.tasks[best].awaited || len(q.focus) == 0:
		case cursor.run > 0 && cursor.run < frontierRun:
			best = q.nearestWithinBand(cursor.last, best)
			cursor.run++
		default:
			cursor.run = 1
		}
		cursor.last = q.tasks[best].task.pos
	}
	task := q.tasks[best].task
	end := len(q.tasks) - 1
	q.tasks[best] = q.tasks[end]
	q.tasks[end] = queuedGeneration{}
	q.tasks = q.tasks[:end]
	q.length.Store(int64(end))
	return task, true
}

// nearestWithinBand returns the index of the task nearest to last among those
// at most frontierBand chunks farther from a loader than the task at nearest,
// the nearest to a loader, preferring the nearer to a loader, then the older.
// q.dists holds the squared distances of the tasks to the loaders.
func (q *generationQueue) nearestWithinBand(last ChunkPos, nearest int) int {
	bound := math.Sqrt(float64(q.dists[nearest])) + frontierBand
	limit := int64(bound * bound)
	best, bestLast := nearest, squaredChunkDistance(q.tasks[nearest].task.pos, last)
	for i := range q.tasks {
		if q.dists[i] > limit {
			continue
		}
		d := squaredChunkDistance(q.tasks[i].task.pos, last)
		switch {
		case d < bestLast:
		case d == bestLast && q.dists[i] < q.dists[best]:
		case d == bestLast && q.dists[i] == q.dists[best] && q.tasks[i].seq < q.tasks[best].seq:
		default:
			continue
		}
		best, bestLast = i, d
	}
	return best
}

// squaredChunkDistance returns the squared distance between two chunks.
func squaredChunkDistance(a, b ChunkPos) int64 {
	dx, dz := int64(a[0]-b[0]), int64(a[1]-b[1])
	return dx*dx + dz*dz
}

// dropAbandoned removes the tasks of the columns nothing wants any more.
func (q *generationQueue) dropAbandoned() {
	q.tasks = slices.DeleteFunc(q.tasks, func(t queuedGeneration) bool {
		return t.task.col != nil && t.task.col.abandoned()
	})
	q.length.Store(int64(len(q.tasks)))
}

// distance returns the squared distance from pos to the nearest loader, or
// zero if there are none.
func (q *generationQueue) distance(pos ChunkPos) int64 {
	if len(q.focus) == 0 {
		return 0
	}
	nearest := int64(-1)
	for _, f := range q.focus {
		if d := squaredChunkDistance(pos, f); nearest < 0 || d < nearest {
			nearest = d
		}
	}
	return nearest
}

// prioritize makes the waiting task that generates pos, if any, the next one
// a free worker takes. The world goroutine calls it before it waits for a
// chunk it needs now.
func (q *generationQueue) prioritize(pos ChunkPos) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.tasks {
		if q.tasks[i].task.pos == pos {
			q.tasks[i].awaited = true
			return
		}
	}
}

// close stops the queue and returns the tasks no worker took.
func (q *generationQueue) close() []generationTask {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.ready.Broadcast()
	remaining := make([]generationTask, 0, len(q.tasks))
	for _, t := range q.tasks {
		remaining = append(remaining, t.task)
	}
	q.tasks = nil
	q.length.Store(0)
	return remaining
}
