package world

import "sync"

// generationQueue is a first-in first-out queue of chunk generation tasks
// shared by the generator workers. Pushing never blocks, so the world
// goroutine requesting chunks never waits for the workers.
type generationQueue struct {
	mu     sync.Mutex
	ready  sync.Cond
	tasks  []generationTask
	head   int
	closed bool
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
	if q.head == len(q.tasks) {
		q.tasks, q.head = q.tasks[:0], 0
	}
	q.tasks = append(q.tasks, task)
	q.ready.Signal()
	return len(q.tasks) - q.head, true
}

// pop waits for the next task. It returns false once the queue is closed.
func (q *generationQueue) pop() (generationTask, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.head == len(q.tasks) && !q.closed {
		q.ready.Wait()
	}
	if q.closed {
		return generationTask{}, false
	}
	task := q.tasks[q.head]
	q.tasks[q.head] = generationTask{}
	q.head++
	return task, true
}

// close stops the queue and returns the tasks no worker took.
func (q *generationQueue) close() []generationTask {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.ready.Broadcast()
	remaining := q.tasks[q.head:]
	q.tasks, q.head = nil, 0
	return remaining
}
