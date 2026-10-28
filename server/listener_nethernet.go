package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/sandertv/gophertunnel/minecraft"
)

func (uc UserConfig) netherNetListener(conf Config, cfg minecraft.ListenConfig) (Listener, error) {
	identityFile := uc.Network.IdentityFile
	if identityFile == "" {
		identityFile = "server-key.pem"
	}
	key, err := loadServerIdentityKey(identityFile)
	if err != nil {
		return nil, fmt.Errorf("load NetherNet identity: %w", err)
	}
	network := netherNet{cfg: nethernet.ListenConfig{Key: key, AllowAnonymous: conf.AuthDisabled}}
	l, err := cfg.ListenNetwork(network, uc.Network.Address)
	if err != nil {
		return nil, fmt.Errorf("create NetherNet listener: %w", err)
	}
	conf.Log.Info("NetherNet listener running.", "addr", l.Addr())
	return listener{l}, nil
}

// netherNet carries gophertunnel connections over package nethernet: the
// signaling and every connection share one port, without goroutines per
// connection below gophertunnel.
type netherNet struct {
	cfg nethernet.ListenConfig
}

// DialContext opens an anonymous connection.
func (n netherNet) DialContext(ctx context.Context, address string) (net.Conn, error) {
	c, err := nethernet.Dialer{}.DialContext(ctx, address)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// DialContextIdentity opens a connection whose identity is the token and
// key the Login of an authenticated client carries.
func (n netherNet) DialContextIdentity(ctx context.Context, address, token string, key *ecdsa.PrivateKey) (net.Conn, error) {
	c, err := nethernet.Dialer{Key: key, Token: token}.DialContext(ctx, address)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// PingContext is not supported: gophertunnel would take the port of the
// connection from the answer.
func (n netherNet) PingContext(context.Context, string) ([]byte, error) {
	return nil, errors.New("nethernet: ping is not supported")
}

// Listen listens for NetherNet clients on address.
func (n netherNet) Listen(address string) (minecraft.NetworkListener, error) {
	l, err := n.cfg.Listen(address)
	if err != nil {
		return nil, err
	}
	var id [8]byte
	_, _ = rand.Read(id[:])
	return &netherNetListener{Listener: l, id: int64(binary.LittleEndian.Uint64(id[:]))}, nil
}

// netherNetListener is a nethernet.Listener as gophertunnel listens on it.
type netherNetListener struct {
	*nethernet.Listener
	id int64
}

// ID returns the ID of the listener.
func (l *netherNetListener) ID() int64 { return l.id }

// PongData takes the status gophertunnel announces, in the form of a RakNet
// pong, and serves it to clients that look the server up.
func (l *netherNetListener) PongData(data []byte) {
	if s, ok := parsePong(data); ok {
		l.SetStatus(s)
	}
}

// parsePong reads a RakNet pong: MCPE;name;protocol;version;players;max;
// id;sub name;game mode;game mode number;...
func parsePong(data []byte) (nethernet.Status, bool) {
	f := strings.Split(string(data), ";")
	if len(f) < 9 || f[0] != "MCPE" {
		return nethernet.Status{}, false
	}
	protocol, err1 := strconv.Atoi(f[2])
	players, err2 := strconv.Atoi(f[4])
	maxPlayers, err3 := strconv.Atoi(f[5])
	if err1 != nil || err2 != nil || err3 != nil {
		return nethernet.Status{}, false
	}
	s := nethernet.Status{ServerName: f[1], Protocol: protocol, Version: f[3], PlayerCount: players, MaxPlayerCount: maxPlayers, LevelName: f[7]}
	switch strings.ToLower(f[8]) {
	case "creative":
		s.GameType = 1
	case "adventure":
		s.GameType = 2
	}
	return s, true
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
