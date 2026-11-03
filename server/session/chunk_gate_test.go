package session

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// queueConn is a connection that queues for its client as much as a test
// says, behind a congestion window of 16 KiB on a round trip of 100 ms.
type queueConn struct {
	testConn
	queued *atomic.Int64
}

func (c queueConn) SendQueue() (queued, window int, rtt time.Duration) {
	return int(c.queued.Load()), 16 << 10, 100 * time.Millisecond
}

func (queueConn) ClientCacheEnabled() bool { return false }

// subChunkRequest asks for the sub-chunk at the bottom of y 0 of a column.
func subChunkRequest(pos world.ChunkPos) *v2193.SubChunkRequestPacket {
	return &v2193.SubChunkRequestPacket{
		Position: v2193.SubChunkPos{pos[0], 0, pos[1]},
		Offsets:  []v2193.SubChunkOffset{{0, 0, 0}},
	}
}

// gatedSession returns a session over a queueConn whose chunk loader
// loaded the columns passed, each with a block at the bottom of y 0, and
// the bytes the connection queues.
func gatedSession(t *testing.T, w *world.World, columns []world.ChunkPos) (*Session, *atomic.Int64) {
	s := testSession("gated")
	queued := new(atomic.Int64)
	s.conn = queueConn{testConn: s.conn.(testConn), queued: queued}
	do(t, w, func(tx *world.Tx) {
		for _, pos := range columns {
			tx.SetBlock(cube.Pos{int(pos[0]) << 4, 0, int(pos[1]) << 4}, block.Stone{}, nil)
		}
		s.chunkLoader = world.NewLoader(2, w, s)
		s.chunkLoader.ChangeRadius(tx, 2)
		s.chunkLoader.Load(tx, 9)
	})
	for _, pos := range columns {
		if _, ok := s.chunkLoader.Chunk(pos); !ok {
			t.Fatalf("column %v not loaded", pos)
		}
	}
	for s.testPacket() != nil {
	}
	return s, queued
}

// do runs f in a transaction of w and waits for it and what it deferred.
func do(t *testing.T, w *world.World, f func(tx *world.Tx)) {
	t.Helper()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestChunksWaitForRoom fills the queue of a connection: the sub-chunks
// the client asks for and the next chunks wait, while other packets still
// go. Once the queue drains, the sub-chunks go in the order asked, as many
// a tick as the room left takes, then the next chunks.
func TestChunksWaitForRoom(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	columns := []world.ChunkPos{{0, 0}, {1, 0}, {0, 1}}
	s, queued := gatedSession(t, w, columns)

	queued.Store(1 << 20)
	do(t, w, func(tx *world.Tx) {
		for _, pos := range columns {
			if err := (&SubChunkRequestHandler{}).Handle(subChunkRequest(pos), s, tx, nil); err != nil {
				t.Fatal(err)
			}
		}
		s.writePacket(&v2193.TextPacket{TextType: v2193.TextTypeRaw, Message: "still here"})
		s.loadChunks(tx)
	})
	if _, ok := s.testPacket().(*v2193.TextPacket); !ok {
		t.Fatal("a packet of the game waited with the chunks")
	}
	if pk := s.testPacket(); pk != nil {
		t.Fatalf("%T sent with the queue full", packetOf(pk))
	}
	queued.Store(16 << 10)
	do(t, w, s.loadChunks)
	if pk := s.testPacket(); pk != nil {
		t.Fatalf("%T sent with the queue at its bound", packetOf(pk))
	}

	// Room for one sub-chunk a tick.
	queued.Store(16<<10 - 1)
	for _, pos := range columns {
		do(t, w, s.loadChunks)
		sub, ok := s.testPacket().(*v2193.SubChunkPacket)
		if !ok || sub.Position != (v2193.SubChunkPos{pos[0], 0, pos[1]}) {
			t.Fatalf("sent %+v, want the sub-chunk of %v", sub, pos)
		}
		if len(sub.SubChunkEntries) != 1 || sub.SubChunkEntries[0].Result != v2193.SubChunkResultSuccess {
			t.Fatalf("sub-chunk of %v: %+v", pos, sub.SubChunkEntries)
		}
		if pk := s.testPacket(); pk != nil {
			t.Fatalf("%T sent past the room of a tick", packetOf(pk))
		}
	}
	queued.Store(16 << 10)
	do(t, w, s.loadChunks)
	if pk := s.testPacket(); pk != nil {
		t.Fatalf("%T loaded with the queue at its bound", packetOf(pk))
	}
	queued.Store(0)
	do(t, w, s.loadChunks)
	if _, ok := s.testPacket().(*v2193.LevelChunkPacket); !ok {
		t.Fatal("no chunk loaded once the queue drained")
	}
}

// cacheConn is a queueConn to a client that caches chunks.
type cacheConn struct{ queueConn }

func (cacheConn) ClientCacheEnabled() bool { return true }

// TestSubChunkAnswer answers a request with each sub-chunk as the world
// holds it: one past the height of the world out of bounds, one of a
// column not loaded not found, at most maxSubChunkOffsets, and the bytes
// of the payloads counted. A client that caches them is sent the blob of a
// sub-chunk by its hash, to ask for it, and the bytes of its block
// entities alone are counted.
func TestSubChunkAnswer(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	s, _ := gatedSession(t, w, []world.ChunkPos{{0, 0}})
	pk := &v2193.SubChunkRequestPacket{Offsets: []v2193.SubChunkOffset{{0, 0, 0}, {0, 100, 0}, {5, 0, 0}}}
	var size int
	do(t, w, func(tx *world.Tx) { size = s.answerSubChunks(pk, tx) })
	sub := s.testPacket().(*v2193.SubChunkPacket)
	results := []byte{v2193.SubChunkResultSuccess, v2193.SubChunkResultIndexOutOfBounds, v2193.SubChunkResultChunkNotFound}
	for i, e := range sub.SubChunkEntries {
		if e.Result != results[i] || e.Offset != pk.Offsets[i] {
			t.Fatalf("entry %d: %+v", i, e)
		}
	}
	if payload := sub.SubChunkEntries[0].RawPayload.Val; size == 0 || size != len(payload) {
		t.Fatalf("%d bytes counted for a payload of %d", size, len(payload))
	}

	many := &v2193.SubChunkRequestPacket{Offsets: make([]v2193.SubChunkOffset, maxSubChunkOffsets+1)}
	do(t, w, func(tx *world.Tx) { s.answerSubChunks(many, tx) })
	if sub := s.testPacket().(*v2193.SubChunkPacket); len(sub.SubChunkEntries) != maxSubChunkOffsets {
		t.Fatalf("%d sub-chunks answered of %d asked", len(sub.SubChunkEntries), len(many.Offsets))
	}

	s.conn, s.blobs = cacheConn{s.conn.(queueConn)}, map[uint64][]byte{}
	do(t, w, func(tx *world.Tx) { size = s.answerSubChunks(pk, tx) })
	e := s.testPacket().(*v2193.SubChunkPacket).SubChunkEntries[0]
	if !e.BlobHash.Set || len(s.blobs[e.BlobHash.Val]) == 0 || len(s.openChunkTransactions) != 1 {
		t.Fatalf("cached sub-chunk %+v sent with %d transactions open", e, len(s.openChunkTransactions))
	}
	if size != len(e.RawPayload.Val) {
		t.Fatalf("%d bytes counted for block entities of %d", size, len(e.RawPayload.Val))
	}
}

// pathConn is a connection that queues bytes for its client behind a
// window on a path of a round trip.
type pathConn struct {
	testConn
	queued, window int
	rtt            time.Duration
}

func (c pathConn) SendQueue() (queued, window int, rtt time.Duration) {
	return c.queued, c.window, c.rtt
}

// TestConnRoom bounds the bytes a connection queues for its client by a
// congestion window, or by what its path carries in a tick if more, and
// leaves a connection that does not tell without bound.
func TestConnRoom(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		queued, window int
		rtt            time.Duration
		room           int
	}{
		{0, 16 << 10, 100 * time.Millisecond, 16 << 10},
		{0, 16 << 10, 20 * time.Millisecond, 40 << 10},
		{0, 16 << 10, 50 * time.Millisecond, 16 << 10},
		{0, 16 << 10, 0, 16 << 10},
		{10 << 10, 16 << 10, 100 * time.Millisecond, 6 << 10},
		{20 << 10, 16 << 10, 100 * time.Millisecond, -4 << 10},
		{1 << 20, 0, 0, math.MaxInt},
	} {
		s := testSession("room")
		s.conn = pathConn{testConn: s.conn.(testConn), queued: c.queued, window: c.window, rtt: c.rtt}
		if room := s.connRoom(); room != c.room {
			t.Errorf("%d queued behind a window of %d over %v: room %d, want %d", c.queued, c.window, c.rtt, room, c.room)
		}
	}
	if room := testSession("untold").connRoom(); room != math.MaxInt {
		t.Errorf("a connection that does not tell its queue has room %d", room)
	}
}

// TestSubChunkQueueBounded asks for more sub-chunks than a session holds
// for a client whose queue is full, 16 Ki: the oldest requests are
// answered, in order, until those left are within the bound.
func TestSubChunkQueueBounded(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	s := testSession("asks")
	queued := new(atomic.Int64)
	queued.Store(1 << 20)
	s.conn = queueConn{testConn: s.conn.(testConn), queued: queued}
	offsets := make([]v2193.SubChunkOffset, 1024)
	do(t, w, func(tx *world.Tx) {
		for i := range 18 {
			pk := &v2193.SubChunkRequestPacket{Position: v2193.SubChunkPos{int32(i), 0, 0}, Offsets: offsets}
			if err := (&SubChunkRequestHandler{}).Handle(pk, s, tx, nil); err != nil {
				t.Fatal(err)
			}
		}
		s.loadChunks(tx)
	})
	for i := range int32(2) {
		if sub, ok := s.testPacket().(*v2193.SubChunkPacket); !ok || sub.Position[0] != i {
			t.Fatalf("answered %+v, want request %d", sub, i)
		}
	}
	if pk := s.testPacket(); pk != nil {
		t.Fatalf("%T sent with the requests left within the bound", packetOf(pk))
	}
}

// TestOutdatedSubChunkRequest answers a request queued for a dimension the
// player is no longer in with no sub-chunks, as the client expects.
func TestOutdatedSubChunkRequest(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	s, _ := gatedSession(t, w, []world.ChunkPos{{0, 0}})
	pk := subChunkRequest(world.ChunkPos{3, 4})
	pk.Dimension = 1
	do(t, w, func(tx *world.Tx) {
		if err := (&SubChunkRequestHandler{}).Handle(pk, s, tx, nil); err != nil {
			t.Fatal(err)
		}
		s.loadChunks(tx)
	})
	sub, ok := s.testPacket().(*v2193.SubChunkPacket)
	if !ok || sub.Dimension != 1 || sub.Position != pk.Position || sub.SubChunkEntries == nil || len(sub.SubChunkEntries) != 0 {
		t.Fatalf("answered %+v, want no sub-chunks of dimension 1", sub)
	}
}

// TestChunkRoomCountsChunksOwed counts against the room of a tick, as they
// go out compressed, the blobs a client was told of and did not answer
// for, and the chunks written in the tick: a blob the client misses moves
// from the one to the other, and one it has frees its room.
func TestChunkRoomCountsChunksOwed(t *testing.T) {
	t.Parallel()
	s := testSession("owed")
	s.blobs = map[uint64][]byte{1: make([]byte, 30<<10), 2: make([]byte, 3<<10)}
	if room := s.chunkRoom(100 << 10); room != 100<<10-11<<10 {
		t.Fatalf("room %d with 33 KiB of blobs owed", room)
	}
	status := &v2193.ClientCacheBlobStatusPacket{MissHashes: []uint64{1}, HitHashes: []uint64{2}}
	if err := (&ClientCacheBlobStatusHandler{}).Handle(status, s, nil, nil); err != nil {
		t.Fatal(err)
	}
	if room := s.chunkRoom(100 << 10); room != 100<<10-10<<10 {
		t.Fatalf("room %d with 30 KiB of blobs written", room)
	}
	if resp, ok := s.testPacket().(*v2193.ClientCacheMissResponsePacket); !ok || len(resp.Blobs) != 1 {
		t.Fatalf("sent %+v, want the blob missed", resp)
	}
}
