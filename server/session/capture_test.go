package session

import "github.com/dm-vev/adamant/net/bedrock/proto"

// newCaptureSession returns a session that holds every packet it writes, for
// a test to take with taken, those it writes to its connection at once too.
func newCaptureSession() *Session {
	s := &Session{holdPackets: 1}
	s.conn = captureConn{s: s}
	return s
}

// captureConn is the connection of a capture session: what is written to it
// is held by the session, after the packets the session held so far.
type captureConn struct {
	benchmarkConn
	s *Session
}

func (c captureConn) WritePacket(pk proto.Packet) error {
	c.s.packetsMu.Lock()
	defer c.s.packetsMu.Unlock()
	c.s.heldPackets = append(c.s.heldPackets, pk)
	return nil
}

// taken returns the packets the session held since it was last called, and
// forgets them.
func (s *Session) taken() []proto.Packet {
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	pks := s.heldPackets
	s.heldPackets = nil
	return pks
}
