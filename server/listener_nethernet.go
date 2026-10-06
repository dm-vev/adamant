package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/df-mc/go-nethernet"
	"github.com/df-mc/go-nethernet/endpoint"
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
	socket, err := net.Listen("tcp", uc.Network.Address)
	if err != nil {
		return nil, fmt.Errorf("listen for NetherNet signaling: %w", err)
	}
	signaling := (endpoint.HandlerConfig{Logger: conf.Log}).New()
	network := minecraft.NetherNet{
		Signaling: signaling,
		Log:       conf.Log,
		ListenConfig: nethernet.ListenConfig{
			AllowAnonymous:    conf.AuthDisabled,
			DisableTrickleICE: true,
			IssueServerIdentity: func(context.Context) (*nethernet.Identity, error) {
				return nethernet.GenerateServerIdentity(key, "self")
			},
		},
	}
	l, err := cfg.ListenNetwork(network, signaling.NetworkID())
	if err != nil {
		_ = socket.Close()
		return nil, fmt.Errorf("create NetherNet listener: %w", err)
	}
	httpServer := &http.Server{
		Handler:           signaling,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		if err := httpServer.Serve(socket); err != nil && !errors.Is(err, http.ErrServerClosed) {
			conf.Log.Error("NetherNet signaling stopped", "err", err)
			_ = l.Close()
		}
	}()
	conf.Log.Info("NetherNet listener running.", "addr", socket.Addr())
	return &netherNetServerListener{listener: listener{l}, signaling: httpServer}, nil
}

type netherNetServerListener struct {
	listener
	signaling *http.Server
}

func (l *netherNetServerListener) Close() error {
	return errors.Join(l.listener.Close(), l.signaling.Close())
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
