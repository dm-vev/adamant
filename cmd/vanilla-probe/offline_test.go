package main

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/df-mc/go-nethernet"
	"github.com/df-mc/go-nethernet/endpoint"
	"github.com/pion/webrtc/v4"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

func TestOfflineIdentityBoundToLogin(t *testing.T) {
	settings := webrtc.SettingEngine{}
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetIPFilter(net.IP.IsLoopback)
	api := webrtc.NewAPI(webrtc.WithSettingEngine(settings))
	signaling := endpoint.NewHandler()
	httpServer := httptest.NewServer(signaling)
	defer httpServer.Close()
	listener, err := (minecraft.ListenConfig{AuthenticationDisabled: true}).ListenNetwork(minecraft.NetherNet{
		Signaling: signaling,
		// Requiring an SDP identity also catches regression to an anonymous offer.
		ListenConfig: nethernet.ListenConfig{API: api, AllowAnonymous: false},
	}, signaling.NetworkID())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	dialer := minecraft.Dialer{IdentityData: login.IdentityData{DisplayName: "VanillaProbe"}}
	network := minecraft.NetherNet{Signaling: endpoint.NewClient(), Dialer: nethernet.Dialer{API: api}}
	if err := bindOfflineIdentity(&dialer, &network); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	accepted := make(chan login.IdentityData, 1)
	errors := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errors <- err
			return
		}
		defer conn.Close()
		serverConn := conn.(*minecraft.Conn)
		if err := serverConn.StartGameContext(ctx, minecraft.GameData{EntityUniqueID: 1, EntityRuntimeID: 1}); err != nil {
			errors <- err
			return
		}
		accepted <- serverConn.IdentityData()
		<-ctx.Done()
	}()
	client, err := dialer.DialContextNetwork(ctx, network, httpServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	select {
	case identity := <-accepted:
		if identity.DisplayName != "VanillaProbe" || identity.Identity != dialer.IdentityData.Identity || identity.XUID != "" {
			t.Fatalf("server accepted wrong offline identity: %+v", identity)
		}
	case err := <-errors:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
