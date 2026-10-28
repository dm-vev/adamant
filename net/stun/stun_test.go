package stun

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"net/netip"
	"strings"
	"testing"
)

// The test vectors of RFC 5769, section 2.
const (
	vectorPassword = "VOkJxbRl1RmTxUk/WvJxBt"
	vectorRequest  = `
00 01 00 58 21 12 a4 42 b7 e7 a7 01 bc 34 d6 86 fa 87 df ae
80 22 00 10 53 54 55 4e 20 74 65 73 74 20 63 6c 69 65 6e 74
00 24 00 04 6e 00 01 ff 80 29 00 08 93 2f f9 b1 51 26 3b 36
00 06 00 09 65 76 74 6a 3a 68 36 76 59 20 20 20
00 08 00 14 9a ea a7 0c bf d8 cb 56 78 1e f2 b5 b2 d3 f2 49 c1 b5 71 a2
80 28 00 04 e5 7a 3b cf`
	vectorResponse4 = `
01 01 00 3c 21 12 a4 42 b7 e7 a7 01 bc 34 d6 86 fa 87 df ae
80 22 00 0b 74 65 73 74 20 76 65 63 74 6f 72 20
00 20 00 08 00 01 a1 47 e1 12 a6 43
00 08 00 14 2b 91 f5 99 fd 9e 90 c3 8c 74 89 f9 2a f9 ba 53 f0 6b e7 d7
80 28 00 04 c0 7d 4c 96`
	vectorResponse6 = `
01 01 00 48 21 12 a4 42 b7 e7 a7 01 bc 34 d6 86 fa 87 df ae
80 22 00 0b 74 65 73 74 20 76 65 63 74 6f 72 20
00 20 00 14 00 02 a1 47 01 13 a9 fa a5 d3 f1 79 bc 25 f4 b5 be d2 b9 d9
00 08 00 14 a3 82 95 4e 4b e6 7b f1 17 84 c9 7c 82 92 c2 75 bf e3 ed 41
80 28 00 04 c8 fb 0b 4c`
)

func decodeVector(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestVectorsDecode(t *testing.T) {
	key := hmac.New(sha1.New, []byte(vectorPassword))
	request := decodeVector(t, vectorRequest)
	var m Message
	if err := Parse(request, &m); err != nil {
		t.Fatal(err)
	}
	if m.Type != BindingRequest {
		t.Fatalf("type %#x", m.Type)
	}
	if user, _ := m.Get(AttrUsername); string(user) != "evtj:h6vY" {
		t.Fatalf("username %q", user)
	}
	if p, _ := m.Uint32(AttrPriority); p != 0x6e0001ff {
		t.Fatalf("priority %#x", p)
	}
	if tie, _ := m.Uint64(AttrICEControlled); tie != 0x932ff9b151263b36 {
		t.Fatalf("tie breaker %#x", tie)
	}
	if !m.CheckIntegrity(key) || !m.CheckFingerprint() {
		t.Fatal("request integrity or fingerprint does not match")
	}

	for _, c := range []struct {
		raw  string
		addr netip.AddrPort
	}{
		{vectorResponse4, netip.MustParseAddrPort("192.0.2.1:32853")},
		{vectorResponse6, netip.MustParseAddrPort("[2001:db8:1234:5678:11:2233:4455:6677]:32853")},
	} {
		if err := Parse(decodeVector(t, c.raw), &m); err != nil {
			t.Fatal(err)
		}
		if addr, ok := m.XORAddress(); !ok || addr != c.addr {
			t.Fatalf("address %v, want %v", addr, c.addr)
		}
		if !m.CheckIntegrity(key) || !m.CheckFingerprint() {
			t.Fatalf("response for %v: integrity or fingerprint does not match", c.addr)
		}
	}
}

func TestVectorsEncode(t *testing.T) {
	key := hmac.New(sha1.New, []byte(vectorPassword))
	var tx [12]byte
	copy(tx[:], decodeVector(t, "b7 e7 a7 01 bc 34 d6 86 fa 87 df ae"))

	b := NewBuilder(nil, BindingSuccess, tx)
	b.Add(AttrSoftware, []byte("test vector"))
	b.AddXORAddress(netip.MustParseAddrPort("[2001:db8:1234:5678:11:2233:4455:6677]:32853"))
	b.AddIntegrity(key)
	b.AddFingerprint()
	want := decodeVector(t, vectorResponse6)
	// The vector pads SOFTWARE with spaces, the builder with zeroes.
	got := b.Bytes()
	if len(got) != len(want) {
		t.Fatalf("length %d, want %d", len(got), len(want))
	}
	var m Message
	if err := Parse(got, &m); err != nil {
		t.Fatal(err)
	}
	if !m.CheckIntegrity(key) || !m.CheckFingerprint() {
		t.Fatal("encoded message does not verify")
	}
	got[43] = ' '
	b = NewBuilder(nil, BindingSuccess, tx)
	b.Add(AttrSoftware, []byte("test vector"))
	b.buf[len(b.buf)-1] = ' '
	b.AddXORAddress(netip.MustParseAddrPort("[2001:db8:1234:5678:11:2233:4455:6677]:32853"))
	b.AddIntegrity(key)
	b.AddFingerprint()
	if !bytes.Equal(b.Bytes(), want) {
		t.Fatalf("encoded\n%x\nwant\n%x", b.Bytes(), want)
	}
}

func TestBuilderAppendsToBuffer(t *testing.T) {
	key := hmac.New(sha1.New, []byte("pwd"))
	prefix := []byte{1, 2, 3}
	b := NewBuilder(prefix, BindingRequest, [12]byte{9})
	b.AddString(AttrUsername, "abc:defgh")
	b.AddUint32(AttrPriority, 7)
	b.AddFlag(AttrUseCandidate)
	b.AddIntegrity(key)
	b.AddFingerprint()
	out := b.Bytes()
	if !bytes.Equal(out[:3], prefix) {
		t.Fatal("prefix lost")
	}
	var m Message
	if err := Parse(out[3:], &m); err != nil {
		t.Fatal(err)
	}
	if !m.Has(AttrUseCandidate) || !m.CheckIntegrity(key) || !m.CheckFingerprint() {
		t.Fatal("message does not verify")
	}
	if u, _ := m.Get(AttrUsername); string(u) != "abc:defgh" {
		t.Fatalf("username %q", u)
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	request := decodeVector(t, vectorRequest)
	var m Message
	for i := range len(request) {
		if Parse(request[:i], &m) == nil && i < 20 {
			t.Fatalf("parsed a %d byte prefix", i)
		}
	}
	bad := bytes.Clone(request)
	bad[len(bad)-1] ^= 1
	if err := Parse(bad, &m); err != nil || m.CheckFingerprint() {
		t.Fatal("corrupted fingerprint verified")
	}
	bad = bytes.Clone(request)
	bad[30] ^= 1
	if err := Parse(bad, &m); err != nil || m.CheckIntegrity(hmac.New(sha1.New, []byte(vectorPassword))) {
		t.Fatal("corrupted message passed the integrity check")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte{0, 1, 0, 0, 0x21, 0x12, 0xa4, 0x42, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	key := hmac.New(sha1.New, []byte(vectorPassword))
	f.Fuzz(func(t *testing.T, b []byte) {
		var m Message
		if Parse(b, &m) != nil {
			return
		}
		m.Get(AttrUsername)
		m.XORAddress()
		m.CheckFingerprint()
		m.CheckIntegrity(key)
	})
}
