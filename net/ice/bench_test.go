package ice

import (
	"crypto/hmac"
	"crypto/sha1"
	"net/netip"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/stun"
)

func BenchmarkHandleSTUNRequest(b *testing.B) {
	now := time.Unix(1_700_000_000, 0)
	peerAddr := netip.MustParseAddrPort("198.51.100.7:40000")
	server := Credentials{Ufrag: "srvf", Pwd: "server-password-0123456789"}
	client := Credentials{Ufrag: "clif", Pwd: "client-password-0123456789"}
	var sent int
	agent := New(Config{Local: server, Remote: client}, func(pkt []byte, _ netip.AddrPort) {
		sent += len(pkt)
	})
	agent.Start(now)

	// Build a valid BindingRequest from the controlling peer.
	key := hmac.New(sha1.New, []byte(server.Pwd))
	tx := [12]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	bld := stun.NewBuilder(nil, stun.BindingRequest, tx)
	bld.AddString(stun.AttrUsername, server.Ufrag+":"+client.Ufrag)
	bld.AddUint32(stun.AttrPriority, priority)
	bld.AddUint64(stun.AttrICEControlling, 0x123456789abcdef0)
	bld.AddFlag(stun.AttrUseCandidate)
	bld.AddIntegrity(key)
	bld.AddFingerprint()
	req := bld.Bytes()

	// Warm up pair registration & triggered check so steady-state request handling is benchmarked.
	if !agent.HandleSTUN(req, peerAddr, now) {
		b.Fatal("warmup HandleSTUN failed")
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(req)))
	b.ResetTimer()
	for b.Loop() {
		if !agent.HandleSTUN(req, peerAddr, now) {
			b.Fatal("HandleSTUN failed")
		}
	}
	if sent == 0 {
		b.Fatal("nothing sent")
	}
}
