package login

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
)

// verify checks a compact JWS signed with ES384 by key, naming it in x5u,
// and decodes its claims.
func verify(t *testing.T, key *ecdsa.PublicKey, jws string, claims any) {
	t.Helper()
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		t.Fatalf("JWS of %d parts", len(parts))
	}
	header, _ := b64.DecodeString(parts[0])
	var h struct{ Alg, X5U string }
	if err := json.Unmarshal(header, &h); err != nil || h.Alg != "ES384" {
		t.Fatalf("header %s: %v", header, err)
	}
	if pub, _ := MarshalPublicKey(key); h.X5U != pub {
		t.Fatalf("x5u %q names another key", h.X5U)
	}
	sig, _ := b64.DecodeString(parts[2])
	sum := sha512.Sum384([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 96 || !ecdsa.Verify(key, sum[:], new(big.Int).SetBytes(sig[:48]), new(big.Int).SetBytes(sig[48:])) {
		t.Fatal("signature does not verify")
	}
	payload, _ := b64.DecodeString(parts[1])
	if err := json.Unmarshal(payload, claims); err != nil {
		t.Fatalf("claims %s: %v", payload, err)
	}
}

func TestEncodeOffline(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	id := Identity{DisplayName: "Bot", Identity: NewUUID(), PlayFabID: "abc"}
	data := DefaultClientData("127.0.0.1:19132", "Bot", "1.26.50")
	now := time.Unix(1_800_000_000, 0)
	b, err := EncodeOffline(id, data, key, now)
	if err != nil {
		t.Fatal(err)
	}
	part := func() []byte {
		n := binary.LittleEndian.Uint32(b)
		p := b[4 : 4+n]
		b = b[4+n:]
		return p
	}
	var req request
	if err := json.Unmarshal(part(), &req); err != nil {
		t.Fatal(err)
	}
	raw := string(part())
	if len(b) != 0 {
		t.Fatalf("%d bytes after the request", len(b))
	}
	if req.Certificate != `{"chain":[""]}` || req.AuthenticationType != selfSigned {
		t.Fatalf("request %+v", req)
	}
	var claims tokenClaims
	verify(t, &key.PublicKey, req.Token, &claims)
	pub, _ := MarshalPublicKey(&key.PublicKey)
	want := tokenClaims{Expiry: now.Unix() + 6*3600, NotBefore: now.Unix() - 6*3600, Audience: multiplayerAudience,
		PlayFabID: "abc", ClientPublicKey: pub, DisplayName: "Bot", Identity: id.Identity}
	if claims != want {
		t.Fatalf("claims %+v, want %+v", claims, want)
	}
	var got ClientData
	verify(t, &key.PublicKey, raw, &got)
	if !reflect.DeepEqual(got, data) {
		t.Fatalf("client data %+v, want %+v", got, data)
	}
}

func TestNewUUID(t *testing.T) {
	u := NewUUID()
	if len(u) != 36 || u[14] != '4' || !strings.ContainsRune("89ab", rune(u[19])) || u == NewUUID() {
		t.Fatalf("UUID %s", u)
	}
}
