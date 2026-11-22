package dtls

import (
	"testing"
	"time"
)

func BenchmarkRecordSealOpen(b *testing.B) {
	key := make([]byte, 16)
	iv := make([]byte, 4)
	for i := range key {
		key[i] = byte(i + 1)
	}
	for i := range iv {
		iv[i] = byte(0xa0 + i)
	}
	writer, err := newCipherState(key, iv)
	if err != nil {
		b.Fatal(err)
	}
	reader, err := newCipherState(key, iv)
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{64, 1100} {
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i)
		}
		dst := make([]byte, 0, size+recordHeaderSize+sealOverhead)
		b.Run(benchSizeName(size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			var seq uint64
			for b.Loop() {
				seq++
				rec := writer.seal(dst[:0], contentApplicationData, 1, seq, payload)
				h, err := parseRecordHeader(rec)
				if err != nil {
					b.Fatal(err)
				}
				plain, err := reader.open(h, rec[recordHeaderSize:])
				if err != nil || len(plain) != size {
					b.Fatalf("open: len=%d err=%v", len(plain), err)
				}
			}
		})
	}
}

func BenchmarkConnWriteAndHandleDatagram(b *testing.B) {
	cCert, err := GenerateCertificate()
	if err != nil {
		b.Fatal(err)
	}
	sCert, err := GenerateCertificate()
	if err != nil {
		b.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	var clientConn, serverConn *Conn
	var cInbox, sInbox [][]byte
	acceptAll := func([][]byte) error { return nil }
	clientConn = Client(Config{Certificate: cCert, VerifyPeer: acceptAll}, func(d []byte) {
		sInbox = append(sInbox, append([]byte(nil), d...))
	})
	serverConn = Server(Config{Certificate: sCert, VerifyPeer: acceptAll}, func(d []byte) {
		cInbox = append(cInbox, append([]byte(nil), d...))
	})
	clientConn.Start(now)
	serverConn.Start(now)
	for !clientConn.Established() || !serverConn.Established() {
		for len(sInbox) > 0 {
			d := sInbox[0]
			sInbox = sInbox[1:]
			if err := serverConn.HandleDatagram(d, now, func([]byte) {}); err != nil {
				b.Fatal(err)
			}
		}
		for len(cInbox) > 0 {
			d := cInbox[0]
			cInbox = cInbox[1:]
			if err := clientConn.HandleDatagram(d, now, func([]byte) {}); err != nil {
				b.Fatal(err)
			}
		}
	}

	var wire []byte
	clientConn.send = func(d []byte) {
		wire = append(wire[:0], d...)
	}
	payload := make([]byte, 1100)
	for i := range payload {
		payload[i] = byte(i)
	}
	var gotLen int
	deliver := func(p []byte) { gotLen = len(p) }

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for b.Loop() {
		if err := clientConn.Write(payload); err != nil {
			b.Fatal(err)
		}
		if err := serverConn.HandleDatagram(wire, now, deliver); err != nil {
			b.Fatal(err)
		}
		if gotLen != len(payload) {
			b.Fatalf("got %d bytes, want %d", gotLen, len(payload))
		}
	}
}

func benchSizeName(n int) string {
	switch n {
	case 64:
		return "64B"
	case 1100:
		return "1100B"
	default:
		return "other"
	}
}
