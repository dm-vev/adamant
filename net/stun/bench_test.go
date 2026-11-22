package stun

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"net/netip"
	"strings"
	"testing"
)

func BenchmarkParseAndVerify(b *testing.B) {
	key := hmac.New(sha1.New, []byte(vectorPassword))
	raw, err := hex.DecodeString(strings.Join(strings.Fields(vectorRequest), ""))
	if err != nil {
		b.Fatal(err)
	}
	var m Message
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for b.Loop() {
		if err := Parse(raw, &m); err != nil {
			b.Fatal(err)
		}
		if !m.CheckFingerprint() || !m.CheckIntegrity(key) {
			b.Fatal("verification failed")
		}
	}
}

func BenchmarkBuildBindingRequest(b *testing.B) {
	key := hmac.New(sha1.New, []byte(vectorPassword))
	tx := [12]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	buf := make([]byte, 0, 256)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		bld := NewBuilder(buf[:0], BindingRequest, tx)
		bld.AddString(AttrUsername, "evtj:h6vY")
		bld.AddUint32(AttrPriority, 0x6e0001ff)
		bld.AddUint64(AttrICEControlling, 0x932ff9b151263b36)
		bld.AddFlag(AttrUseCandidate)
		bld.AddIntegrity(key)
		bld.AddFingerprint()
		buf = bld.Bytes()
	}
}

func BenchmarkBuildBindingSuccess(b *testing.B) {
	key := hmac.New(sha1.New, []byte(vectorPassword))
	tx := [12]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	addr := netip.MustParseAddrPort("192.0.2.1:32853")
	buf := make([]byte, 0, 256)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		bld := NewBuilder(buf[:0], BindingSuccess, tx)
		bld.AddXORAddress(addr)
		bld.AddIntegrity(key)
		bld.AddFingerprint()
		buf = bld.Bytes()
	}
}
