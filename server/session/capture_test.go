package session

import "github.com/dm-vev/adamant/net/bedrock/proto"

// newCaptureSession returns a session that holds every packet it writes, for
// a test to take with taken. It has no connection.
func newCaptureSession() *Session {
	return &Session{holdPackets: 1}
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
