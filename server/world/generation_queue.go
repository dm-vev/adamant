package world

import "sync"

// generationQueue holds the chunk generation tasks shared by the generator
// workers. Workers take the task of the chunk nearest to a loader first, as
// Java generates the chunks with the lowest ticket level first, so the chunks
// next to a player never wait behind those at the edge of a view. Pushing
// never blocks, so the world goroutine requesting chunks never waits for the
// workers.
type generationQueue struct {
	mu    sync.Mutex
	ready sync.Cond
	tasks []queuedGeneration
	next  uint64
	// focus holds the chunk positions of the loaders.
	focus  []ChunkPos
	closed bool
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
	q.ready.Signal()
	return len(q.tasks), true
}

// setFocus sets the chunk positions of the loaders that tasks are ordered by.
func (q *generationQueue) setFocus(focus []ChunkPos) {
	q.mu.Lock()
	q.focus = append(q.focus[:0], focus...)
	q.mu.Unlock()
}

// pop waits for the next task: the awaited one, otherwise the one nearest
// to a loader, otherwise the oldest. It returns false once the queue is
// closed.
func (q *generationQueue) pop() (generationTask, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.tasks) == 0 && !q.closed {
		q.ready.Wait()
	}
	if q.closed {
		return generationTask{}, false
	}
	best := 0
	bestDist := q.distance(q.tasks[0].task.pos)
	for i := 1; i < len(q.tasks); i++ {
		candidate, current := &q.tasks[i], &q.tasks[best]
		if candidate.awaited != current.awaited {
			if candidate.awaited {
				best, bestDist = i, q.distance(candidate.task.pos)
			}
			continue
		}
		dist := q.distance(candidate.task.pos)
		if dist < bestDist || dist == bestDist && candidate.seq < current.seq {
			best, bestDist = i, dist
		}
	}
	task := q.tasks[best].task
	last := len(q.tasks) - 1
	q.tasks[best] = q.tasks[last]
	q.tasks[last] = queuedGeneration{}
	q.tasks = q.tasks[:last]
	return task, true
}

// distance returns the squared distance from pos to the nearest loader, or
// zero if there are none.
func (q *generationQueue) distance(pos ChunkPos) int64 {
	if len(q.focus) == 0 {
		return 0
	}
	nearest := int64(-1)
	for _, f := range q.focus {
		dx, dz := int64(pos[0]-f[0]), int64(pos[1]-f[1])
		if d := dx*dx + dz*dz; nearest < 0 || d < nearest {
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
	return remaining
}
