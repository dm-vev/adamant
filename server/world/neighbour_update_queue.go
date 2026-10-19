package world

// neighbourUpdateQueue is an owner-thread FIFO. Consuming a bounded batch must
// not copy or rotate the unprocessed backlog on every world tick.
type neighbourUpdateQueue struct {
	updates []neighbourUpdate
	head    int
	count   int
}

func (q *neighbourUpdateQueue) push(update neighbourUpdate) {
	if q.count == len(q.updates) {
		capacity := max(64, len(q.updates)*2)
		updates := make([]neighbourUpdate, capacity)
		n := copy(updates, q.updates[q.head:])
		copy(updates[n:], q.updates[:q.head])
		q.updates = updates
		q.head = 0
	}
	tail := q.head + q.count
	if tail >= len(q.updates) {
		tail -= len(q.updates)
	}
	q.updates[tail] = update
	q.count++
}

func (q *neighbourUpdateQueue) pop() neighbourUpdate {
	update := q.updates[q.head]
	q.head++
	q.count--
	if q.head == len(q.updates) || q.count == 0 {
		q.head = 0
	}
	return update
}
