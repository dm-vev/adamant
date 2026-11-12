package world

import "testing"

// TestGenerationQueuePrioritizesAwaitedChunk checks that the chunk the world
// goroutine waits for is generated next, with the rest keeping their order.
func TestGenerationQueuePrioritizesAwaitedChunk(t *testing.T) {
	t.Parallel()
	q := newGenerationQueue()
	for _, pos := range []ChunkPos{{0, 0}, {1, 0}, {2, 0}, {3, 0}} {
		q.push(generationTask{pos: pos})
	}
	if task, _ := q.pop(); task.pos != (ChunkPos{0, 0}) {
		t.Fatalf("first task = %v, want [0 0]", task.pos)
	}
	q.prioritize(ChunkPos{3, 0})
	q.prioritize(ChunkPos{9, 9})
	for _, want := range []ChunkPos{{3, 0}, {1, 0}, {2, 0}} {
		if task, _ := q.pop(); task.pos != want {
			t.Fatalf("task = %v, want %v", task.pos, want)
		}
	}
}

// TestGenerationQueueTakesChunkNearestLoaderFirst checks that workers take
// the chunk nearest to a loader first, and the older of equally near ones.
func TestGenerationQueueTakesChunkNearestLoaderFirst(t *testing.T) {
	t.Parallel()
	q := newGenerationQueue()
	for _, pos := range []ChunkPos{{0, 0}, {20, 0}, {9, 0}, {5, 0}} {
		q.push(generationTask{pos: pos})
	}
	q.setFocus([]ChunkPos{{10, 0}, {-50, -50}})
	for _, want := range []ChunkPos{{9, 0}, {5, 0}, {0, 0}, {20, 0}} {
		if task, _ := q.pop(); task.pos != want {
			t.Fatalf("task = %v, want %v", task.pos, want)
		}
	}
}

// TestGenerationQueueKeepsWorkerOnItsFrontier checks that a worker takes the
// task next to the chunk it generated last among those about as near to a
// loader as the nearest, but not one far behind the nearest, and that it
// takes the nearest to a loader again after frontierRun chunks in a row.
func TestGenerationQueueKeepsWorkerOnItsFrontier(t *testing.T) {
	t.Parallel()
	q := newGenerationQueue()
	// Two loaders, at 0,0 and 100,0: the tasks at 10,0 and 112,0 wait
	// about as long, the one at 130,0 much longer.
	for _, pos := range []ChunkPos{{10, 0}, {112, 0}, {130, 0}} {
		q.push(generationTask{pos: pos})
	}
	q.setFocus([]ChunkPos{{0, 0}, {100, 0}})
	cursor := generationCursor{last: ChunkPos{131, 0}, run: 1}
	if task, _ := q.popNear(&cursor); task.pos != (ChunkPos{112, 0}) {
		t.Fatalf("task = %v, want the one on the worker's frontier, [112 0]", task.pos)
	}
	if task, _ := q.popNear(&cursor); task.pos != (ChunkPos{10, 0}) {
		t.Fatalf("task = %v, want the one nearest to a loader, [10 0]", task.pos)
	}
	q.prioritize(ChunkPos{130, 0})
	q.push(generationTask{pos: ChunkPos{1, 0}})
	if task, _ := q.popNear(&cursor); task.pos != (ChunkPos{130, 0}) {
		t.Fatalf("task = %v, want the awaited one, [130 0]", task.pos)
	}

	// At the end of its run, the worker leaves its frontier.
	q.push(generationTask{pos: ChunkPos{110, 0}})
	cursor = generationCursor{last: ChunkPos{111, 0}, run: frontierRun}
	if task, _ := q.popNear(&cursor); task.pos != (ChunkPos{1, 0}) {
		t.Fatalf("task = %v after a run of %d, want the one nearest to a loader, [1 0]", task.pos, frontierRun)
	}
	if cursor.run != 1 || cursor.last != (ChunkPos{1, 0}) {
		t.Fatalf("cursor = %+v, want a run of 1 from [1 0]", cursor)
	}
}
