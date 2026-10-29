package interop

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"testing"
	"time"

	"github.com/df-mc/go-xsapi/v2/xal/nsal"
	"github.com/dm-vev/adamant/net/xbox/auth"
)

// TestXboxSignatureMatchesGoXSAPI signs requests to Xbox services with
// go-xsapi and checks them with the verification of net/xbox/auth, whose
// tests check its own signatures the same way: both sign the same bytes.
func TestXboxSignatureMatchesGoXSAPI(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		url, authorization string
		body               []byte
	}{
		{"https://sisu.xboxlive.com/authorize", "", []byte(`{"AccessToken":"t=x","AppId":"0000000048183522"}`)},
		{"https://xsts.auth.xboxlive.com/xsts/authorize", "", []byte(`{"RelyingParty":"http://playfab.xboxlive.com/"}`)},
		{"https://sessiondirectory.xboxlive.com/handles?include=relatedInfo", "XBL3.0 x=123;token", []byte(`{"type":"activity"}`)},
		{"https://device.auth.xboxlive.com/device/authenticate", "", nil},
	} {
		req, err := http.NewRequest(http.MethodPost, test.url, bytes.NewReader(test.body))
		if err != nil {
			t.Fatal(err)
		}
		if test.authorization != "" {
			req.Header.Set("Authorization", test.authorization)
		}
		now := time.Now()
		if err := nsal.AuthPolicy.Sign(req, test.body, key, now); err != nil {
			t.Fatal(err)
		}
		signedAt, err := auth.VerifySignature(req, test.body, &key.PublicKey)
		if err != nil {
			t.Fatalf("%s: go-xsapi signature does not verify: %v", test.url, err)
		}
		if d := signedAt.Sub(now); d > time.Microsecond || d < -time.Microsecond {
			t.Fatalf("%s: signed at %v, want %v", test.url, signedAt, now)
		}
	}
}
