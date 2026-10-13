package session

import "time"

// LastActivity reports the time the most recent client packet was handled.
func (s *Session) LastActivity() time.Time {
	if s == nil {
		return time.Time{}
	}
	return time.Unix(0, s.lastActivity.Load())
}
