package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"

	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/df-mc/dragonfly/server/query"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/nethernet/lan"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// listenerFunc returns the NetherNet listener of the server, the standard
// listener used when UserConfig.Config() is called.
func (uc UserConfig) listenerFunc(conf Config) (Listener, error) {
	return uc.netherNetListener(conf)
}

func (uc UserConfig) netherNetListener(conf Config) (Listener, error) {
	identityFile := uc.Network.IdentityFile
	if identityFile == "" {
		identityFile = "server-key.pem"
	}
	key, err := loadServerIdentityKey(identityFile)
	if err != nil {
		return nil, fmt.Errorf("load NetherNet identity: %w", err)
	}
	var allow func(net.Addr, login.IdentityData, login.ClientData) (string, bool)
	if conf.Allower != nil {
		allow = conf.Allower.Allow
	}
	listen := nethernet.ListenConfig{Key: key, AllowAnonymous: conf.AuthDisabled}
	if uc.Network.EnableQuery {
		// Server lists send query requests to the port of the game, which
		// is the port of the NetherNet sockets.
		local, err := netip.ParseAddrPort(uc.Network.Address)
		if err != nil {
			local = netip.AddrPortFrom(netip.IPv4Unspecified(), 19132)
			if _, port, err := net.SplitHostPort(uc.Network.Address); err == nil {
				if p, err := strconv.ParseUint(port, 10, 16); err == nil {
					local = netip.AddrPortFrom(netip.IPv4Unspecified(), uint16(p))
				}
			}
		}
		responder, err := query.NewResponder(local, conf.Log)
		if err != nil {
			return nil, fmt.Errorf("create query responder: %w", err)
		}
		listen.Datagram = func(b []byte, from netip.AddrPort, reply func([]byte)) {
			responder.Answer(b, from, reply)
		}
	}
	l, err := netconn.Config{
		Log:                    conf.Log,
		MaximumPlayers:         conf.MaxPlayers,
		StatusProvider:         conf.StatusProvider,
		AuthenticationDisabled: conf.AuthDisabled,
		ResourcePacks:          conf.Resources,
		TexturePacksRequired:   conf.ResourcesRequired,
		Allow:                  allow,
		Compression:            batchCompression(conf.Compression),
	}.Listen(listen, uc.Network.Address)
	if err != nil {
		return nil, fmt.Errorf("create NetherNet listener: %w", err)
	}
	conf.Log.Info("NetherNet listener running.", "addr", l.Addr())
	if uc.Xbox.Enabled {
		tokenFile := uc.Xbox.TokenFile
		if tokenFile == "" {
			tokenFile = "xbox-token.json"
		}
		go publishXbox(l, tokenFile, uc.Xbox.AcceptFriends, conf.Log)
	}
	if uc.Network.LAN {
		publishLAN(l, conf, "")
	}
	return netherNetListener{l}, nil
}

// publishLAN answers the clients on the network that look for games with
// the status of the listener and serves the signaling of their joins, on
// addr, port 7551 if empty. A port taken by another game leaves the server
// out of the LAN tab.
func publishLAN(l *netconn.Listener, conf Config, addr string) *lan.Listener {
	ll, err := lan.Config{Addr: addr, Data: func() lan.ServerData {
		st := l.ServerStatus()
		return lan.ServerData{
			ServerName:            st.ServerName,
			Protocol:              v2193.Protocol,
			Version:               v2193.Version,
			LevelName:             st.ServerSubName,
			PlayerCount:           int32(st.PlayerCount),
			MaxPlayerCount:        int32(st.MaxPlayers),
			AcceptsOnlineAuth:     !conf.AuthDisabled,
			AcceptsSelfSignedAuth: conf.AuthDisabled,
			ConnectionType:        lan.ConnectionTypeLAN,
		}
	}}.Listen()
	if err != nil {
		conf.Log.Warn("The server does not show on LAN.", "err", err)
		return nil
	}
	conf.Log.Info("The server shows on LAN.", "addr", ll.Addr(), "network-id", ll.NetworkID())
	go func() {
		<-l.Closed()
		_ = ll.Close()
	}()
	go func() {
		if err := l.Serve(ll); err != nil && !errors.Is(err, net.ErrClosed) {
			conf.Log.Warn("LAN signaling ended.", "err", err)
		}
	}()
	return ll
}

// batchCompression returns the compression of the batches of package
// bedrock that stands for a compression of gophertunnel: flate at level 1
// unless none was asked for.
func batchCompression(c packet.Compression) bedrock.Compression {
	if c != nil && c.EncodeCompression() == packet.CompressionAlgorithmNone {
		return bedrock.None{}
	}
	return bedrock.Flate{Level: 1}
}

// netherNetListener is a netconn.Listener as the server listens on it.
type netherNetListener struct {
	*netconn.Listener
}

// Accept returns the next client that logged in.
func (l netherNetListener) Accept() (session.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Disconnect disconnects a client with a message.
func (l netherNetListener) Disconnect(conn session.Conn, reason string) error {
	return l.Listener.Disconnect(conn.(*netconn.Conn), reason)
}

// loadServerIdentityKey preserves the server identity across restarts. A corrupt
// existing key is an error, never a reason to silently replace a trusted identity.
func loadServerIdentityKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return decodeServerIdentityKey(data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}

	// Publish a complete, private file without overwriting an identity created
	// concurrently by another process. The temporary file is on the same filesystem.
	f, err := os.CreateTemp(filepath.Dir(path), ".server-key-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if err := pem.Encode(f, &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Link(f.Name(), path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return decodeServerIdentityKey(data)
	}
	return key, nil
}

func decodeServerIdentityKey(data []byte) (*ecdsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "EC PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("expected one PEM EC PRIVATE KEY")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse server identity: %w", err)
	}
	if key.Curve != elliptic.P384() {
		return nil, errors.New("server identity must use the P-384 curve")
	}
	return key, nil
}
