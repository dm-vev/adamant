package world

import "testing"

// TestGenerationQueuePrioritizesAwaitedChunk checks that the chunk the world
// goroutine waits for is generated next, with the rest keeping their order.
func TestGenerationQueuePrioritizesAwaitedChunk(t *testing.T) {
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
