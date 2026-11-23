package session

import (
	"slices"
	"sync"

	"github.com/dm-vev/adamant/net/bedrock/proto"
)

// PacketInterceptor inspects, rewrites, or cancels packets flowing through
// a Session. Returning false from OnIncoming or OnOutgoing drops the packet.
type PacketInterceptor interface {
	// OnIncoming is called before an incoming packet from the client is
	// dispatched to its handler. Returning false cancels handling.
	OnIncoming(s *Session, pk proto.Packet) (proto.Packet, bool)
	// OnOutgoing is called before an outgoing packet is queued or sent to the
	// client. Returning false cancels sending.
	OnOutgoing(s *Session, pk proto.Packet) (proto.Packet, bool)
}

type interceptorEntry struct {
	id uint64
	ic PacketInterceptor
}

// AddInterceptor dynamically registers ic on s and returns an idempotent
// function that removes it. Passing a nil interceptor or calling on nop returns
// a no-op remover.
func (s *Session) AddInterceptor(ic PacketInterceptor) func() {
	if s == nil || s == nop || ic == nil {
		return func() {}
	}
	id := s.addInterceptorEntry(ic)
	var once sync.Once
	return func() {
		once.Do(func() {
			s.removeInterceptor(id)
		})
	}
}

// SendRawPacket queues a raw protocol packet directly on s, passing through any
// active outgoing interceptors.
func (s *Session) SendRawPacket(pk proto.Packet) {
	if s == nil || s == nop || pk == nil {
		return
	}
	s.writePacket(pk)
}

func (s *Session) initInterceptors(ics []PacketInterceptor) {
	if len(ics) == 0 {
		return
	}
	entries := make([]interceptorEntry, 0, len(ics))
	for _, ic := range ics {
		if ic == nil {
			continue
		}
		s.nextInterceptorID++
		entries = append(entries, interceptorEntry{id: s.nextInterceptorID, ic: ic})
	}
	if len(entries) == 0 {
		return
	}
	s.interceptors.Store(&entries)
	s.hasInInterceptors.Store(true)
	s.hasOutInterceptors.Store(true)
}

func (s *Session) addInterceptorEntry(ic PacketInterceptor) uint64 {
	s.interceptorsMu.Lock()
	defer s.interceptorsMu.Unlock()
	s.nextInterceptorID++
	id := s.nextInterceptorID
	var cur []interceptorEntry
	if p := s.interceptors.Load(); p != nil {
		cur = *p
	}
	next := make([]interceptorEntry, len(cur)+1)
	copy(next, cur)
	next[len(cur)] = interceptorEntry{id: id, ic: ic}
	s.interceptors.Store(&next)
	s.hasInInterceptors.Store(true)
	s.hasOutInterceptors.Store(true)
	return id
}

func (s *Session) removeInterceptor(id uint64) {
	s.interceptorsMu.Lock()
	defer s.interceptorsMu.Unlock()
	p := s.interceptors.Load()
	if p == nil {
		return
	}
	cur := *p
	idx := slices.IndexFunc(cur, func(e interceptorEntry) bool { return e.id == id })
	if idx < 0 {
		return
	}
	if len(cur) == 1 {
		s.interceptors.Store(nil)
		s.hasInInterceptors.Store(false)
		s.hasOutInterceptors.Store(false)
		return
	}
	next := make([]interceptorEntry, 0, len(cur)-1)
	next = append(next, cur[:idx]...)
	next = append(next, cur[idx+1:]...)
	s.interceptors.Store(&next)
}

func (s *Session) runInInterceptors(pk proto.Packet) (proto.Packet, bool) {
	p := s.interceptors.Load()
	if p == nil {
		return pk, true
	}
	for _, entry := range *p {
		var ok bool
		pk, ok = entry.ic.OnIncoming(s, pk)
		if !ok || pk == nil {
			return nil, false
		}
	}
	return pk, true
}

func (s *Session) runOutInterceptors(pk proto.Packet) (proto.Packet, bool) {
	p := s.interceptors.Load()
	if p == nil {
		return pk, true
	}
	for _, entry := range *p {
		var ok bool
		pk, ok = entry.ic.OnOutgoing(s, pk)
		if !ok || pk == nil {
			return nil, false
		}
	}
	return pk, true
}
