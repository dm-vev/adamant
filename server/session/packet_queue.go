package session

import (
	"fmt"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// queuedPacket is a packet the session sends: a packet.Packet of
// gophertunnel, or a proto.Packet of the generated codecs of protocol 2193,
// which a connection that implements protoWriter encodes itself.
type queuedPacket interface {
	ID() uint32
}

// protoWriter is a connection that writes packets of the generated codecs.
type protoWriter interface {
	WriteProto(pk proto.Packet) error
}

// protoPacket carries a packet of the generated codecs over a connection
// that takes only gophertunnel's: its encoding goes as raw bytes.
type protoPacket struct{ proto.Packet }

func (p protoPacket) Marshal(io protocol.IO) {
	var w wire.Writer
	p.Encode(&w)
	io.Bytes(&w.B)
}

// writeQueued writes a packet taken from the queue to the connection. A
// packet that does not encode, as a Text without a message, is a bug of
// the server: it is dropped and logged rather than taking the server down
// from this goroutine.
func (s *Session) writeQueued(pk queuedPacket) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.conf.Log.Error("drop session packet that does not encode", "packet", fmt.Sprintf("%T", pk), "panic", r)
			err = nil
		}
	}()
	switch pk := pk.(type) {
	case packet.Packet:
		return s.conn.WritePacket(pk)
	case proto.Packet:
		if w, ok := s.conn.(protoWriter); ok {
			return w.WriteProto(pk)
		}
		return s.conn.WritePacket(protoPacket{pk})
	}
	return nil
}

// maxQueuedPackets bounds retained packets for a connection whose writer cannot
// keep up. The normal 256-packet channel is only a fast path, not a burst limit:
// chunk/entity publication may enqueue hundreds of packets in a single tick.
const maxQueuedPackets = 16 * 1024

// writePacket queues a packet without waiting for encoding or network I/O on the
// world owner. All accepted packets retain FIFO order, including during bursts.
// Every packet in pendingPackets was queued after every packet in the channel.
func (s *Session) writePacket(pk queuedPacket) {
	if s == Nop {
		return
	}
	s.packetsMu.Lock()
	if s.closing.Load() {
		s.packetsMu.Unlock()
		return
	}
	if s.holdPackets == 0 && len(s.pendingPackets) == 0 {
		select {
		case s.packets <- pk:
			s.packetsMu.Unlock()
			return
		default:
		}
	}
	queued := len(s.packets) + len(s.pendingPackets) + len(s.heldPackets)
	if queued >= maxQueuedPackets {
		// Stop accepting packets immediately. Close off-owner because closing a
		// real connection may flush buffered data and wait for network I/O.
		closeConn := s.closing.CompareAndSwap(false, true)
		s.packetsMu.Unlock()
		if closeConn {
			s.conf.Log.Warn("closing session due to outgoing packet backlog", "queued", queued)
			go s.CloseConnection()
		}
		return
	}
	if s.holdPackets > 0 {
		s.heldPackets = append(s.heldPackets, pk)
	} else {
		s.pendingPackets = append(s.pendingPackets, pk)
	}
	s.packetsMu.Unlock()
}

// BeginBatch holds the packets queued until EndBatch, so that the writer
// wakes once for what a tick of the world of the session shows rather than
// for every packet.
func (s *Session) BeginBatch() {
	if s == Nop {
		return
	}
	s.packetsMu.Lock()
	s.holdPackets++
	s.packetsMu.Unlock()
}

// EndBatch hands the packets held since BeginBatch to the writer: the first
// through the channel, waking the writer, and the rest through the overflow,
// which the writer takes at once.
func (s *Session) EndBatch() {
	if s == Nop {
		return
	}
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	if s.holdPackets--; s.holdPackets > 0 || len(s.heldPackets) == 0 {
		return
	}
	held := s.heldPackets
	if s.closing.Load() {
		clear(held)
		s.heldPackets = held[:0]
		return
	}
	rest := held
	if len(s.pendingPackets) == 0 {
		select {
		case s.packets <- held[0]:
			rest = held[1:]
		default:
		}
	}
	s.pendingPackets = append(s.pendingPackets, rest...)
	clear(held)
	s.heldPackets = held[:0]
}

func (s *Session) sendPackets() {
	var batch []queuedPacket
	for {
		select {
		case <-s.closeBackground:
			return
		case pk := <-s.packets:
			batch = append(batch[:0], pk)
		}
		// Take the rest of the channel and then the overflow, queued after it,
		// while no packet can be queued.
		s.packetsMu.Lock()
		for drained := false; !drained; {
			select {
			case pk := <-s.packets:
				batch = append(batch, pk)
			default:
				drained = true
			}
		}
		batch = append(batch, s.pendingPackets...)
		clear(s.pendingPackets)
		s.pendingPackets = s.pendingPackets[:0]
		s.packetsMu.Unlock()
		if s.closing.Load() {
			return
		}
		s.connWriteMu.Lock()
		for i, pk := range batch {
			err := s.writeQueued(pk)
			batch[i] = nil
			if err != nil {
				s.connWriteMu.Unlock()
				s.conf.Log.Debug("write session packet: " + err.Error())
				s.CloseConnection()
				return
			}
		}
		// What a tick showed goes out as one batch, when the tick ends
		// rather than on a timer of the connection.
		err := s.conn.Flush()
		s.connWriteMu.Unlock()
		if err != nil {
			s.conf.Log.Debug("flush session packets: " + err.Error())
			s.CloseConnection()
			return
		}
	}
}

// clearPacketQueue releases payloads retained by a session that is closing.
func (s *Session) clearPacketQueue() {
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	s.pendingPackets = nil
	s.heldPackets = nil
	for {
		select {
		case <-s.packets:
		default:
			return
		}
	}
}
