package interop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/login"
	gtlogin "github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// TestOfflineLoginParses has gophertunnel, which servers parse logins
// with, read the request of an offline client.
func TestOfflineLoginParses(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	id := login.Identity{DisplayName: "Bot One", Identity: login.NewUUID(), PlayFabID: "0123456789abcdef"}
	data := login.DefaultClientData("127.0.0.1:19132", "Bot One", "1.26.50")
	req, err := login.EncodeOffline(id, data, key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	identity, client, auth, err := gtlogin.Parse(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if identity.DisplayName != id.DisplayName || identity.Identity != id.Identity || identity.XUID != "" || identity.PlayFabID != id.PlayFabID {
		t.Fatalf("identity %+v", identity)
	}
	if auth.XBOXLiveAuthenticated || !auth.PublicKey.Equal(&key.PublicKey) {
		t.Fatalf("authentication %+v", auth)
	}
	if client.SkinID != data.SkinID || string(client.DeviceID) != data.DeviceID || client.SkinImageWidth != 64 ||
		client.ServerAddress != data.ServerAddress || client.ThirdPartyName != "Bot One" || int(client.DeviceOS) != login.DeviceAndroid {
		t.Fatalf("client data %+v", client)
	}
}
