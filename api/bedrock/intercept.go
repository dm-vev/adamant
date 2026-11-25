// Package bedrock provides Bedrock-specific packet interception, multi-protocol
// raw packet injection, resource pack helpers, and player-to-session lookup
// utilities for Adamant.
package bedrock

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	bedrockadapter "github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/resource"
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

// SessionProtocol returns the negotiated Bedrock protocol number of s (e.g.
// 2168, 2169, 2193, 2225), or 0 when s is nil.
func SessionProtocol(s *session.Session) int32 {
	if s == nil {
		return 0
	}
	return s.ProtocolNumber()
}

// SessionGameVersion returns the Bedrock game version string of s's negotiated
// protocol (e.g. "1.26.60"), or "" when s is nil.
func SessionGameVersion(s *session.Session) string {
	if s == nil {
		return ""
	}
	return s.GameVersion()
}

// ProtocolVersion returns the negotiated Bedrock protocol number for player p,
// or (0, false) if p is not backed by a Bedrock session.
func ProtocolVersion(p *player.Player) (int32, bool) {
	s, ok := SessionOf(p)
	if !ok {
		return 0, false
	}
	n := s.ProtocolNumber()
	return n, n != 0
}

// GameVersion returns the negotiated Bedrock game version string for player p,
// or ("", false) if p is not backed by a Bedrock session.
func GameVersion(p *player.Player) (string, bool) {
	s, ok := SessionOf(p)
	if !ok {
		return "", false
	}
	v := s.GameVersion()
	return v, v != ""
}

type protocolFilterInterceptor struct {
	inner     PacketInterceptor
	protocols []int32
	minProto  int32
	maxProto  int32
	useRange  bool
}

func (f protocolFilterInterceptor) matches(s *session.Session) bool {
	n := SessionProtocol(s)
	if f.useRange {
		if f.minProto > 0 && n < f.minProto {
			return false
		}
		if f.maxProto > 0 && n > f.maxProto {
			return false
		}
		return n != 0
	}
	return slices.Contains(f.protocols, n)
}

func (f protocolFilterInterceptor) OnIncoming(s *session.Session, pk proto.Packet) (proto.Packet, bool) {
	if f.inner == nil || !f.matches(s) {
		return pk, true
	}
	return f.inner.OnIncoming(s, pk)
}

func (f protocolFilterInterceptor) OnOutgoing(s *session.Session, pk proto.Packet) (proto.Packet, bool) {
	if f.inner == nil || !f.matches(s) {
		return pk, true
	}
	return f.inner.OnOutgoing(s, pk)
}

// ForProtocols wraps ic so it only intercepts sessions speaking one of the
// specified Bedrock protocol numbers (e.g. 2168, 2169).
func ForProtocols(ic PacketInterceptor, protocols ...int32) PacketInterceptor {
	return protocolFilterInterceptor{
		inner:     ic,
		protocols: slices.Clone(protocols),
	}
}

// ForProtocolRange wraps ic so it only intercepts sessions whose protocol
// number falls within [minProto, maxProto] (0 leaves a bound open).
func ForProtocolRange(ic PacketInterceptor, minProto, maxProto int32) PacketInterceptor {
	return protocolFilterInterceptor{
		inner:    ic,
		minProto: minProto,
		maxProto: maxProto,
		useRange: true,
	}
}

// SendVersionedPacket calls build with s's negotiated protocol number and
// queues the returned packet when non-nil, giving plugins per-protocol control.
func SendVersionedPacket(s *session.Session, build func(protocol int32) proto.Packet) bool {
	if s == nil || build == nil {
		return false
	}
	pk := build(s.ProtocolNumber())
	if pk == nil {
		return false
	}
	s.SendRawPacket(pk)
	return true
}

// SendPlayerVersionedPacket calls build with p's negotiated Bedrock protocol
// number and sends the resulting packet if non-nil.
func SendPlayerVersionedPacket(p *player.Player, build func(protocol int32) proto.Packet) bool {
	s, ok := SessionOf(p)
	if !ok {
		return false
	}
	return SendVersionedPacket(s, build)
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

// LoadResourcePack compiles and returns a Bedrock resource pack from a zip,
// mcpack, or directory path.
func LoadResourcePack(path string) (*resource.Pack, error) {
	return resource.ReadPath(path)
}

// LoadResourcePacksDir compiles all resource packs found inside dir.
func LoadResourcePacksDir(dir string) ([]*resource.Pack, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read resource packs dir: %w", err)
	}
	packs := make([]*resource.Pack, 0, len(entries))
	for _, entry := range entries {
		p, err := resource.ReadPath(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("load resource pack %q: %w", entry.Name(), err)
		}
		packs = append(packs, p)
	}
	return packs, nil
}

// WithResourcePacks returns a copy of conf with packs appended and
// ResourcesRequired set to required.
func WithResourcePacks(conf bedrockadapter.Config, required bool, packs ...*resource.Pack) bedrockadapter.Config {
	conf.ResourcesRequired = required
	for _, p := range packs {
		if p != nil {
			conf.Resources = append(conf.Resources, p)
		}
	}
	return conf
}
