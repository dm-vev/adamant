package nethernet

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func mustAddr(s string) netip.AddrPort { return netip.MustParseAddrPort(s) }

// bdsAnswer is the answer BDS 1.26.52.3 gave a probe, shortened to one
// candidate.
const bdsAnswer = "v=0\r\n" +
	"o=- 8734063096739538688 2 IN IP4 127.0.0.1\r\n" +
	"s=-\r\n" +
	"t=0 0\r\n" +
	"a=group:BUNDLE 0\r\n" +
	"a=extmap-allow-mixed\r\n" +
	"a=msid-semantic: WMS\r\n" +
	"m=application 19232 UDP/DTLS/SCTP webrtc-datachannel\r\n" +
	"c=IN IP4 172.17.0.1\r\n" +
	"a=candidate:2241237996 1 udp 2122194687 172.17.0.1 19232 typ host generation 0 network-id 1 network-cost 50\r\n" +
	"a=candidate:1911476613 1 udp 2122265343 fd01::1a34:3d79:fb04:ac2 19232 typ host generation 0 network-id 3 network-cost 50\r\n" +
	"a=ice-ufrag:RQKo\r\n" +
	"a=ice-pwd:ucVHibc9q7d2UNXN/f3rpnTU\r\n" +
	"a=ice-options:trickle\r\n" +
	"a=fingerprint:sha-256 3E:73:C6:AC:DE:9B:18:0E:74:6A:27:8C:CA:E4:52:D9:4D:A6:EE:81:AC:D4:0A:B3:3E:12:9C:78:98:DF:CB:A2\r\n" +
	"a=setup:active\r\n" +
	"a=mid:0\r\n" +
	"a=sctp-port:5000\r\n" +
	"a=max-message-size:262144\r\n"

func TestParseBDSAnswer(t *testing.T) {
	d, err := parseDescription(bdsAnswer)
	if err != nil {
		t.Fatal(err)
	}
	if d.ufrag != "RQKo" || d.pwd != "ucVHibc9q7d2UNXN/f3rpnTU" || d.setup != SetupActive || d.maxMessageSize != 262144 || d.sctpPort != 5000 {
		t.Fatalf("parsed %+v", d)
	}
	if len(d.candidates) != 2 || d.candidates[0].String() != "172.17.0.1:19232" || d.candidates[1].Port() != 19232 {
		t.Fatalf("candidates %v", d.candidates)
	}
	if len(d.fingerprints) != 1 || d.fingerprints[0].Algorithm != "sha-256" || !strings.HasPrefix(d.fingerprints[0].Digest, "3E:73") {
		t.Fatalf("fingerprints %v", d.fingerprints)
	}
}

func TestParseRepeatedCandidatePrefix(t *testing.T) {
	addr, ok := parseCandidate("candidate:candidate:2878742611 1 udp 2130706431 127.0.0.1 57520 typ host generation 0 ufrag x")
	if !ok || addr.String() != "127.0.0.1:57520" {
		t.Fatalf("%v %v", addr, ok)
	}
	if _, ok := parseCandidate("candidate:1 1 udp 1 abc.local 1234 typ host"); ok {
		t.Fatal("accepted an mDNS candidate")
	}
}

func TestDescriptionRoundTrip(t *testing.T) {
	d := &description{
		ufrag: "abcd", pwd: randomString(24), setup: SetupActive, maxMessageSize: 262144, sctpPort: 5000,
		fingerprints: []Fingerprint{{"sha-256", "AA:BB"}},
		identity:     "eyJ9",
	}
	d.candidates = append(d.candidates, mustAddr("203.0.113.1:19132"), mustAddr("[2001:db8::1]:19132"))
	got, err := parseDescription(encodeDescription(d))
	if err != nil {
		t.Fatal(err)
	}
	if got.ufrag != d.ufrag || got.pwd != d.pwd || got.setup != d.setup || got.identity != d.identity || len(got.candidates) != 2 {
		t.Fatalf("round trip %+v", got)
	}
}

func TestIdentityRoundTrip(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	now := time.Now()
	fps := []Fingerprint{{"sha-256", "3E:73:C6"}}
	for _, makeToken := range []func() (string, error){
		func() (string, error) { return serverToken(key, now) },
		func() (string, error) { return clientToken(key, "Bot", now) },
	} {
		token, err := makeToken()
		if err != nil {
			t.Fatal(err)
		}
		jws, err := signFingerprints(key, fps)
		if err != nil {
			t.Fatal(err)
		}
		attr, err := encodeIdentity("self", token, jws)
		if err != nil {
			t.Fatal(err)
		}
		gotToken, gotJWS, err := decodeIdentity(attr)
		if err != nil {
			t.Fatal(err)
		}
		pub, err := tokenKey(gotToken, now)
		if err != nil {
			t.Fatal(err)
		}
		if !pub.Equal(&key.PublicKey) {
			t.Fatal("cpk is not the signing key")
		}
		if err := verifyFingerprints(pub, gotJWS, fps); err != nil {
			t.Fatal(err)
		}
		if verifyFingerprints(pub, gotJWS, []Fingerprint{{"sha-256", "00"}}) == nil {
			t.Fatal("assertion verified for other fingerprints")
		}
	}
}

// TestBDSIdentityToken decodes the identity BDS put in an answer: its
// token names the key that signed the fingerprints of that answer.
func TestBDSIdentityToken(t *testing.T) {
	const attr = "eyJhc3NlcnRpb24iOiJ7XCJmaW5nZXJwcmludHNcIjpcImV5SmhiR2NpT2lKRlV6TTROQ0o5Li5IeDYtZWtRbk5vVDVyTVlCQ0gwdzVtbjRTZUdyNWd1UENBVF8xZGpXY2ltX3NJWHYxbFc5UWFMYmNBZkdRc1I3dTFTNTRMd19UMkw2OVgwRF9WZ21ZSDlRdC1sNWRxWnF0bzZnNjV0NHpxN01lblNsYzF0bktFMW80SUdaVVV0OFwiLFwidG9rZW5cIjpcImV5SmhiR2NpT2lKRlV6TTROQ0lzSW5nMWRTSTZJazFJV1hkRlFWbElTMjlhU1hwcU1FTkJVVmxHU3pSRlJVRkRTVVJaWjBGRmNsWmljRkpRZFdGeFNWazVWVTR2UTBwTFpUTjJjWGh5ZG1Oc1VWWjFhbEpQT0UxV1REVjNUemhYVm1wT1NUTk5VRXd2VDBZMll6TmFVSE5GYWtaaVpVbGFXVkZNTDJkeVpVTkpWRnBxTW1aaFEydFdLek0wUmxCVmRtbG1SVGhqTjBSdGJVSlNRVmczY0VoTGIwVmFSREZsWmsxUFlXMUNWbTVJY1RBNFNIa2lmUW8uZXlKamNHc2lPbnNpWTNKMklqb2lVQzB6T0RRaUxDSnJkSGtpT2lKRlF5SXNJbmdpT2lKeVZtSndVbEIxWVhGSldUbFZUbDlEU2t0bE0zWnhlSEoyWTJ4UlZuVnFVazg0VFZaTU5YZFBPRmRXYWs1Sk0wMVFURjlQUmpaak0xcFFjMFZxUm1KbElpd2llU0k2SWtsYVdWRk1YMmR5WlVOSlZGcHFNbVpoUTJ0V0xUTTBSbEJWZG1sbVJUaGpOMFJ0YlVKU1FWZzNjRWhMYjBWYVJERmxaazFQWVcxQ1ZtNUljVEE0U0hraWZTd2laWGh3SWpveE56a3pNakE0TVRnd0xDSnBZWFFpT2pFM09UTXlNRGd4TWpCOUNnLllJYzJNdWRCcmdtSDN5UUpUUWNqYzhFUllaa2ltRlFrMy1fM1VMMHFNcXRORUxDWU93aldnZTNUWnlGMkU2bndhZEx3MGI1Tk8zZTBzVDF0QlVRQWlSSV83aXc0eUg2cnBobVhLeXJVVVJ0N1ljUTZWVVgxNVpvNE9oU3FqaVdHXCJ9IiwiaWRwIjp7ImRvbWFpbiI6InNlbGYiLCJwcm90b2NvbCI6ImRlZmF1bHQifX0="
	token, jws, err := decodeIdentity(attr)
	if err != nil {
		t.Fatal(err)
	}
	// The token was issued for a minute in 2026: read it as of then.
	pub, err := tokenKey(token, time.Unix(1793208150, 0))
	if err != nil {
		t.Fatal(err)
	}
	fps := []Fingerprint{{"sha-256", "3E:73:C6:AC:DE:9B:18:0E:74:6A:27:8C:CA:E4:52:D9:4D:A6:EE:81:AC:D4:0A:B3:3E:12:9C:78:98:DF:CB:A2"}}
	if err := verifyFingerprints(pub, jws, fps); err != nil {
		t.Fatalf("BDS fingerprint assertion: %v", err)
	}
}
