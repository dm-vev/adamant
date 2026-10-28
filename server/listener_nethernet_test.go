package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestServerIdentityConcurrentCreationPreservesIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.pem")
	const clients = 8
	keys := make([]*ecdsa.PrivateKey, clients)
	errs := make([]error, clients)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range clients {
		wg.Go(func() {
			<-start
			keys[i], errs[i] = loadServerIdentityKey(path)
		})
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("create identity %d: %v", i, err)
		}
	}
	reloaded, err := loadServerIdentityKey(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, key := range keys {
		if !key.Equal(reloaded) {
			t.Fatalf("listener %d acquired a different identity from the persisted key", i)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("identity is not private: permissions %o", info.Mode().Perm())
	}
}

func TestServerIdentityRejectsInvalidExistingKeyWithoutReplacingIt(t *testing.T) {
	wrongCurve, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(wrongCurve)
	if err != nil {
		t.Fatal(err)
	}
	for name, original := range map[string][]byte{
		"malformed":   []byte("damaged identity"),
		"wrong_curve": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "identity.pem")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadServerIdentityKey(path); err == nil {
				t.Fatal("accepted an invalid identity")
			}
			stored, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stored, original) {
				t.Fatal("replaced an existing identity on decode failure")
			}
		})
	}
}

// TestNetherNetCarriesGophertunnelSession joins a gophertunnel listener
// over package nethernet with the gophertunnel dialer: network settings,
// login, resource packs, start game and spawn all go over the data
// channel, with Minecraft encryption off.
func TestNetherNetCarriesGophertunnelSession(t *testing.T) {
	network := netherNet{cfg: nethernet.ListenConfig{AllowAnonymous: true}}
	l, err := minecraft.ListenConfig{AuthenticationDisabled: true}.ListenNetwork(network, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	served := make(chan error, 1)
	texts := make(chan string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			served <- err
			return
		}
		conn := c.(*minecraft.Conn)
		defer conn.Close()
		if err := conn.StartGame(minecraft.GameData{WorldName: "nethernet"}); err != nil {
			served <- err
			return
		}
		served <- nil
		for {
			pk, err := conn.ReadPacket()
			if err != nil {
				return
			}
			if text, ok := pk.(*packet.Text); ok {
				texts <- text.Message
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := minecraft.Dialer{IdentityData: login.IdentityData{DisplayName: "Bot"}}.DialContextNetwork(ctx, network, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.DoSpawnContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	if got := conn.GameData().WorldName; got != "nethernet" {
		t.Fatalf("world name %q", got)
	}
	if err := conn.WritePacket(&packet.Text{TextType: packet.TextTypeChat, Message: "hello"}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-texts:
		if got != "hello" {
			t.Fatalf("server read %q", got)
		}
	case <-ctx.Done():
		t.Fatal("server read no text")
	}
}

func TestParsePong(t *testing.T) {
	s, ok := parsePong([]byte("MCPE;Adamant;975;1.26.52;3;20;123;world;Survival;0;19132;19132;0;0;"))
	want := nethernet.Status{ServerName: "Adamant", Protocol: 975, Version: "1.26.52", PlayerCount: 3, MaxPlayerCount: 20, LevelName: "world"}
	if !ok || s != want {
		t.Fatalf("parsed %+v, %v", s, ok)
	}
	if _, ok := parsePong([]byte("MCPE;broken")); ok {
		t.Fatal("parsed a short pong")
	}
}
