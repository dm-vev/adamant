package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/sandertv/gophertunnel/minecraft"
)

// Listener is a source for connections that may be listened on by a Server using Server.listen. Proxies can use this to
// provide players from a different source.
type Listener interface {
	// Accept blocks until the next connection is established and returns it. An error is returned if the Listener was
	// closed using Close.
	Accept() (session.Conn, error)
	// Disconnect disconnects a connection from the Listener with a reason.
	Disconnect(conn session.Conn, reason string) error
	io.Closer
}

// listenerFunc may be used to return a *minecraft.Listener using a Config. It
// is the standard listener used when UserConfig.Config() is called.
func (uc UserConfig) listenerFunc(conf Config) (Listener, error) {
	cfg := minecraft.ListenConfig{
		MaximumPlayers:         conf.MaxPlayers,
		StatusProvider:         conf.StatusProvider,
		AuthenticationDisabled: conf.AuthDisabled,
		ResourcePacks:          conf.Resources,
		TexturePacksRequired:   conf.ResourcesRequired,
		Compression:            conf.Compression,
		Allow:                  conf.Allower.Allow,
	}
	if conf.Log.Enabled(context.Background(), slog.LevelDebug) {
		cfg.ErrorLog = conf.Log.With("net origin", "gophertunnel")
	}
	if uc.Network.Transport == "" || uc.Network.Transport == "nethernet" {
		return uc.netherNetListener(conf)
	}
	l, err := cfg.Listen("raknet", uc.Network.Address)
	if err != nil {
		return nil, fmt.Errorf("create minecraft listener: %w", err)
	}
	conf.Log.Info("Listener running.", "addr", l.Addr())
	return listener{l}, nil
}

// listener is a Listener implementation that wraps around a minecraft.Listener so that it can be listened on by
// Server.
type listener struct {
	*minecraft.Listener
}

// Accept blocks until the next connection is established and returns it. An error is returned if the Listener was
// closed using Close.
func (l listener) Accept() (session.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return rakConn{conn.(*minecraft.Conn)}, err
}

// Disconnect disconnects a connection from the Listener with a reason.
func (l listener) Disconnect(conn session.Conn, reason string) error {
	return l.Listener.Disconnect(conn.(rakConn).Conn, reason)
}

// rakConn carries a session over a gophertunnel connection, which runs on
// RakNet: gophertunnel logs the client in and batches packets, which go as
// bytes coded with the codecs of protocol 2193, as over NetherNet.
type rakConn struct {
	*minecraft.Conn
}

// ReadPacket decodes the next packet of the client.
func (c rakConn) ReadPacket() (proto.Packet, error) {
	raw, err := c.Conn.ReadBytes()
	if err != nil {
		return nil, err
	}
	// A packet decoded shares the bytes it was decoded from, which
	// gophertunnel may read the next batch into.
	pk, err := netconn.DecodePacket(append([]byte(nil), raw...))
	if err != nil {
		// gophertunnel drops a client that sends a packet it cannot decode.
		_ = c.Conn.Close()
		return nil, err
	}
	return pk, nil
}

// WritePacket adds a packet to the batch gophertunnel sends next.
func (c rakConn) WritePacket(pk proto.Packet) error {
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
	pk.Encode(&w)
	_, err := c.Conn.Write(w.B)
	return err
}

// StartGameContext starts the game of the client with gophertunnel, which
// takes the shield ID of the items there; the codecs take it too.
func (c rakConn) StartGameContext(ctx context.Context, data minecraft.GameData) error {
	for _, item := range data.Items {
		if item.Name == "minecraft:shield" {
			v2193.SetShieldID(int32(item.RuntimeID))
		}
	}
	return c.Conn.StartGameContext(ctx, data)
}
