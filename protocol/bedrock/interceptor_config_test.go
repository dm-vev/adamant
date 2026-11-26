package bedrockadapter

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/dm-vev/adamant/protocol/bedrock/session"
	"github.com/dm-vev/adamant/server"
	"github.com/dm-vev/adamant/net/bedrock/proto"
)

type noopInterceptor struct{}

func (noopInterceptor) OnIncoming(_ *session.Session, pk proto.Packet) (proto.Packet, bool) {
	return pk, true
}

func (noopInterceptor) OnOutgoing(_ *session.Session, pk proto.Packet) (proto.Packet, bool) {
	return pk, true
}

func TestConfigInterceptorsPassedToListener(t *testing.T) {
	dir := t.TempDir()
	ic := noopInterceptor{}
	cfg := Config{
		Network: NetworkConfig{
			Address:      "127.0.0.1:0",
			IdentityFile: filepath.Join(dir, "identity.pem"),
		},
		AuthDisabled:            true,
		DisableResourceBuilding: true,
		Interceptors:            []session.PacketInterceptor{ic},
	}
	ln, err := cfg.Listener(server.Config{Log: slog.New(slog.DiscardHandler)}, nil)
	if err != nil {
		t.Fatalf("Listener: %v", err)
	}
	defer func() { _ = ln.Close() }()
	sl, ok := ln.(*session.Listener)
	if !ok {
		t.Fatalf("expected *session.Listener, got %T", ln)
	}
	_ = sl
}
