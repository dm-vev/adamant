package query

import (
	"context"
	"net"

	"github.com/sandertv/go-raknet"
	"github.com/sandertv/gophertunnel/minecraft"
	"log/slog"
)

const (
	queryTypeHandshake   = 0x09
	queryTypeInformation = 0x00
)

var (
	querySplitNum  = [...]byte{'S', 'P', 'L', 'I', 'T', 'N', 'U', 'M', 0x00}
	queryPlayerKey = [...]byte{0x00, 0x01, 'p', 'l', 'a', 'y', 'e', 'r', '_', 0x00, 0x00}
	queryVersion   = [...]byte{0xfe, 0xfd}
)

// init replaces the default RakNet implementation so that the query specific
// packet handling can be injected transparently.
func init() {
	minecraft.RegisterNetwork("raknet", func(l *slog.Logger) minecraft.Network {
		return rakNetNetwork{log: l}
	})
}

// rakNetNetwork is a wrapper that installs a query-aware packet listener while
// delegating all other behaviour to the standard RakNet implementation.
type rakNetNetwork struct {
	log *slog.Logger
}

// DialContext forwards dial requests to the default RakNet dialer.
func (r rakNetNetwork) DialContext(ctx context.Context, address string) (net.Conn, error) {
	return raknet.Dialer{ErrorLog: r.log.With("net origin", "raknet")}.DialContext(ctx, address)
}

// PingContext forwards ping requests to the default RakNet dialer.
func (r rakNetNetwork) PingContext(ctx context.Context, address string) ([]byte, error) {
	return raknet.Dialer{ErrorLog: r.log.With("net origin", "raknet")}.PingContext(ctx, address)
}

// Listen wraps the standard RakNet listener so that query packets are
// intercepted before they reach the upstream handler.
func (r rakNetNetwork) Listen(address string) (minecraft.NetworkListener, error) {
	lc := raknet.ListenConfig{
		ErrorLog: r.log.With("net origin", "raknet"),
		UpstreamPacketListener: &packetListener{
			log: r.log.With("net origin", "raknet"),
		},
	}
	return lc.Listen(address)
}

// packetListener produces query aware UDP connections for the RakNet listener.
type packetListener struct {
	log *slog.Logger
}

// ListenPacket implements the minecraft.PacketListener interface.
func (l *packetListener) ListenPacket(network, address string) (net.PacketConn, error) {
	conn, err := net.ListenPacket(network, address)
	if err != nil {
		return nil, err
	}
	local, _ := net.ResolveUDPAddr(network, conn.LocalAddr().String())
	host := ""
	if local != nil && local.IP != nil {
		host = local.IP.String()
		if host == "" || local.IP.IsUnspecified() {
			host = "0.0.0.0"
		}
	}
	port := 0
	if local != nil {
		port = local.Port
	}
	return &packetConn{
		PacketConn: conn,
		log:        l.log,
		host:       host,
		port:       port,
	}, nil
}
