package session

import "github.com/sandertv/gophertunnel/minecraft/protocol/packet"

// maxQueuedPackets bounds retained packets for a connection whose writer cannot
// keep up. The normal 256-packet channel is only a fast path, not a burst limit:
// chunk/entity publication may enqueue hundreds of packets in a single tick.
const maxQueuedPackets = 16 * 1024

// writePacket queues a packet without waiting for encoding or network I/O on the
// world owner. All accepted packets retain FIFO order, including during bursts.
func (s *Session) writePacket(pk packet.Packet) {
	if s == Nop {
		return
	}
	s.packetsMu.Lock()
	if s.closing.Load() {
		s.packetsMu.Unlock()
		return
	}
	if s.pendingHead == len(s.pendingPackets) {
		select {
		case s.packets <- pk:
			s.packetsMu.Unlock()
			return
		default:
		}
	}
	queued := len(s.packets) + len(s.pendingPackets) - s.pendingHead
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
	if len(s.pendingPackets) == cap(s.pendingPackets) && s.pendingHead > 0 {
		n := copy(s.pendingPackets, s.pendingPackets[s.pendingHead:])
		clear(s.pendingPackets[n:])
		s.pendingPackets = s.pendingPackets[:n]
		s.pendingHead = 0
	}
	s.pendingPackets = append(s.pendingPackets, pk)
	s.packetsMu.Unlock()
}

func (s *Session) sendPackets() {
	for {
		select {
		case <-s.closeBackground:
			return
		case pk := <-s.packets:
			if s.closing.Load() {
				return
			}
			s.refillPackets()
			s.connWriteMu.Lock()
			err := s.conn.WritePacket(pk)
			s.connWriteMu.Unlock()
			if err != nil {
				s.conf.Log.Debug("write session packet: " + err.Error())
				s.CloseConnection()
				return
			}
		}
	}
}

// refillPackets moves the oldest overflow packet into the slot just freed by
// the sole consumer. Producers must keep appending to the overflow until it is
// empty, so newly queued packets cannot overtake older ones.
func (s *Session) refillPackets() {
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	if s.pendingHead == len(s.pendingPackets) {
		return
	}
	select {
	case s.packets <- s.pendingPackets[s.pendingHead]:
	default:
		// A producer may have filled the freed slot before the first overflow
		// packet was queued. Retry after the next receive instead of waiting
		// on our own consumer while holding packetsMu.
		return
	}
	s.pendingPackets[s.pendingHead] = nil
	s.pendingHead++
	if s.pendingHead == len(s.pendingPackets) {
		s.pendingPackets = nil
		s.pendingHead = 0
	}
}

// clearPacketQueue releases payloads retained by a session that is closing.
func (s *Session) clearPacketQueue() {
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	s.pendingPackets = nil
	s.pendingHead = 0
	for {
		select {
		case <-s.packets:
		default:
			return
		}
	}
}
