// Package bedrock provides Bedrock-specific packet interception, raw packet
// injection, and player-to-session lookup helpers for Dragonfly.
package bedrock

import (
	bedrockadapter "github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/dm-vev/adamant/net/bedrock/proto"
)

// PacketInterceptor inspects, rewrites, or drops packets flowing through a
// Bedrock session.
type PacketInterceptor = session.PacketInterceptor

// FuncInterceptor adapts optional incoming/outgoing packet callbacks into a
// PacketInterceptor. Nil callbacks pass packets through unchanged.
type FuncInterceptor struct {
	OnIn  func(s *session.Session, pk proto.Packet) (proto.Packet, bool)
	OnOut func(s *session.Session, pk proto.Packet) (proto.Packet, bool)
}

// OnIncoming implements session.PacketInterceptor.
func (f FuncInterceptor) OnIncoming(s *session.Session, pk proto.Packet) (proto.Packet, bool) {
	if f.OnIn != nil {
		return f.OnIn(s, pk)
	}
	return pk, true
}

// OnOutgoing implements session.PacketInterceptor.
func (f FuncInterceptor) OnOutgoing(s *session.Session, pk proto.Packet) (proto.Packet, bool) {
	if f.OnOut != nil {
		return f.OnOut(s, pk)
	}
	return pk, true
}

// InterceptSession registers ic on s and returns an idempotent removal func.
func InterceptSession(s *session.Session, ic PacketInterceptor) func() {
	if s == nil || ic == nil {
		return func() {}
	}
	return s.AddInterceptor(ic)
}

// InterceptPlayer attaches ic to p's Bedrock session if p is backed by one.
func InterceptPlayer(p *player.Player, ic PacketInterceptor) (func(), bool) {
	s, ok := SessionOf(p)
	if !ok {
		return func() {}, false
	}
	return InterceptSession(s, ic), true
}

// SendRawPacket queues a raw protocol packet on s, passing through outgoing
// interceptors.
func SendRawPacket(s *session.Session, pk proto.Packet) {
	if s == nil || pk == nil {
		return
	}
	s.SendRawPacket(pk)
}

// SendPlayerRawPacket queues a raw protocol packet on p's Bedrock session if p
// is backed by one.
func SendPlayerRawPacket(p *player.Player, pk proto.Packet) bool {
	s, ok := SessionOf(p)
	if !ok {
		return false
	}
	SendRawPacket(s, pk)
	return true
}

// SessionOf returns the Bedrock *session.Session backing p, or (nil, false)
// when p is nil, simulated, or driven by a non-Bedrock client.
func SessionOf(p *player.Player) (*session.Session, bool) {
	if p == nil || p.H() == nil {
		return nil, false
	}
	c := p.Data().Client
	s, ok := c.(*session.Session)
	return s, ok && s != nil
}

// WithInterceptors returns a copy of conf with ics appended to its initial
// session interceptors.
func WithInterceptors(conf bedrockadapter.Config, ics ...PacketInterceptor) bedrockadapter.Config {
	for _, ic := range ics {
		if ic != nil {
			conf.Interceptors = append(conf.Interceptors, ic)
		}
	}
	return conf
}
