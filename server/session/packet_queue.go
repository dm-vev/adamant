package session

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/nethernet"
)

// writeQueued writes a packet taken from the queue to the connection. A
// packet that does not encode, as a Text without a message, is a bug of
// the server: it is dropped and logged rather than taking the server down
// from this goroutine.
func (s *Session) writeQueued(pk proto.Packet) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.conf.Log.Error("drop session packet that does not encode", "packet", fmt.Sprintf("%T", pk), "panic", r)
			err = nil
		}
	}()
	return s.conn.WritePacket(pk)
}

// maxQueuedPackets bounds retained packets for a connection whose writes
// cannot keep up. Chunk and entity publication may queue hundreds of packets
// in a single tick, so it is far above what a tick shows.
const maxQueuedPackets = 16 * 1024

// writePacket queues a packet without waiting for encoding or network I/O on
// the world owner. Packets keep their order. Outside a batch the session is
// handed to the flushers at once; within one, when the batch ends.
func (s *Session) writePacket(pk proto.Packet) {
	if s == Nop {
		return
	}
	s.packetsMu.Lock()
	if s.closing.Load() {
		s.packetsMu.Unlock()
		return
	}
	if queued := len(s.pendingPackets) + len(s.heldPackets); queued >= maxQueuedPackets {
		// Stop accepting packets immediately. Close off-owner because closing a
		// real connection may flush buffered data and wait for network I/O.
		closeConn := s.closing.CompareAndSwap(false, true)
		s.packetsMu.Unlock()
		if closeConn {
			backlogClosed.Inc()
			s.conf.Log.Warn("closing session due to outgoing packet backlog", "queued", queued)
			go s.CloseConnection()
		}
		return
	}
	if s.holdPackets > 0 {
		s.heldPackets = append(s.heldPackets, pk)
		s.packetsMu.Unlock()
		return
	}
	s.pendingPackets = append(s.pendingPackets, pk)
	submit := s.scheduleFlushLocked()
	s.packetsMu.Unlock()
	if submit {
		flushers.submit(s)
	}
}

// scheduleFlushLocked marks the session as waiting for a flusher and
// reports whether it must be handed to one: not if one has it already.
func (s *Session) scheduleFlushLocked() bool {
	if s.flushing || s.conn == nil {
		return false
	}
	s.flushing = true
	return true
}

// BeginBatch holds the packets queued until EndBatch, so that a flusher
// takes what a tick of the world of the session shows at once rather than
// packet by packet.
func (s *Session) BeginBatch() {
	if s == Nop {
		return
	}
	s.packetsMu.Lock()
	s.holdPackets++
	s.packetsMu.Unlock()
}

// EndBatch hands the packets held since BeginBatch to a flusher.
func (s *Session) EndBatch() {
	if s == Nop {
		return
	}
	s.flushBlockUpdates()
	s.packetsMu.Lock()
	if s.holdPackets--; s.holdPackets > 0 || len(s.heldPackets) == 0 {
		s.packetsMu.Unlock()
		return
	}
	held := s.heldPackets
	if s.closing.Load() {
		clear(held)
		s.heldPackets = held[:0]
		s.packetsMu.Unlock()
		return
	}
	s.pendingPackets = append(s.pendingPackets, held...)
	clear(held)
	s.heldPackets = held[:0]
	if s.tickEnded == 0 {
		s.tickEnded = nanotime()
	}
	submit := s.scheduleFlushLocked()
	s.packetsMu.Unlock()
	if submit {
		flushers.submit(s)
	}
}

// flush writes the packets queued to the connection and flushes them as a
// batch, again while more were queued meanwhile. One flusher at a time has
// a session.
func (s *Session) flush() {
	for {
		s.packetsMu.Lock()
		batch := s.pendingPackets
		if len(batch) == 0 || s.closing.Load() {
			s.flushing = false
			s.packetsMu.Unlock()
			return
		}
		s.pendingPackets = s.flushSpare
		s.flushSpare = nil
		ended := s.tickEnded
		s.tickEnded = 0
		s.packetsMu.Unlock()

		start := nanotime()
		err := s.writeBatch(batch)
		done := nanotime()
		flushes.Inc()
		flushSeconds.Observe(float64(done-start) / 1e9)
		flushPackets.Observe(float64(len(batch)))
		if ended != 0 {
			tickToSend.Observe(float64(done-ended) / 1e9)
		}
		clear(batch)
		if cap(batch) <= maxKeptBatch {
			s.flushSpare = batch[:0]
		}
		if err != nil {
			s.conf.Log.Debug("write session packets: " + err.Error())
			s.packetsMu.Lock()
			s.flushing = false
			s.packetsMu.Unlock()
			s.CloseConnection()
			return
		}
	}
}

// maxKeptBatch bounds the packets of the array of a batch a session keeps
// for the next: the rare burst of a spawn is not kept for good.
const maxKeptBatch = 1024

// writeBatch writes packets to the connection and flushes them: what a tick
// showed goes out as one batch, when the tick ends rather than on a timer of
// the connection.
func (s *Session) writeBatch(batch []proto.Packet) error {
	s.connWriteMu.Lock()
	defer s.connWriteMu.Unlock()
	for _, pk := range batch {
		if err := s.writeQueued(pk); err != nil {
			return err
		}
	}
	return s.conn.Flush()
}

// clearPacketQueue releases payloads retained by a session that is closing.
func (s *Session) clearPacketQueue() {
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	s.pendingPackets = nil
	s.heldPackets = nil
}

// flushers write the packets queued for sessions to their connections: a
// goroutine a CPU shared by all sessions rather than one a session, each
// taking the sessions handed to it in turn. They start with the first
// session handed to them.
var flushers flushPool

// flushPool is a queue of sessions to flush with the goroutines taking from
// it.
type flushPool struct {
	start sync.Once
	mu    sync.Mutex
	cond  sync.Cond
	queue []*Session
	head  int
}

// submit queues s to be flushed.
func (p *flushPool) submit(s *Session) {
	p.start.Do(func() {
		p.cond.L = &p.mu
		for range max(runtime.GOMAXPROCS(0), 1) {
			go p.work()
		}
	})
	p.mu.Lock()
	p.queue = append(p.queue, s)
	p.mu.Unlock()
	p.cond.Signal()
}

// flushRound bounds the sessions a flusher takes at once, whose datagrams
// go out together once it flushed them all.
const flushRound = 16

func (p *flushPool) work() {
	batch := nethernet.NewSendBatch()
	round := make([]*Session, 0, flushRound)
	for {
		p.mu.Lock()
		for p.head == len(p.queue) {
			p.cond.Wait()
		}
		n := min(len(p.queue)-p.head, flushRound)
		round = append(round[:0], p.queue[p.head:p.head+n]...)
		clear(p.queue[p.head : p.head+n])
		if p.head += n; p.head == len(p.queue) {
			p.queue, p.head = p.queue[:0], 0
		}
		p.mu.Unlock()
		for _, s := range round {
			p.flush(s, batch)
		}
		clear(round)
		batch.Flush()
	}
}

// sendBatcher is a connection whose datagrams a nethernet.SendBatch may
// gather.
type sendBatcher interface {
	SetSendBatch(b *nethernet.SendBatch)
}

// flush flushes s, gathering its datagrams in batch; a panic of its
// connection closes the session rather than the process.
func (p *flushPool) flush(s *Session, batch *nethernet.SendBatch) {
	if sb, ok := s.conn.(sendBatcher); ok {
		sb.SetSendBatch(batch)
		defer sb.SetSendBatch(nil)
	}
	defer func() {
		if v := recover(); v != nil {
			s.conf.Log.Error("panic flushing session packets", "panic", v)
			s.packetsMu.Lock()
			s.flushing = false
			s.packetsMu.Unlock()
			s.CloseConnection()
		}
	}()
	s.flush()
}

// nanotime returns a monotonic time in nanoseconds for the metrics.
func nanotime() int64 { return int64(time.Since(processStart)) }

var processStart = time.Now()
