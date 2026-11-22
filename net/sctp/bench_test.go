package sctp

import (
	"testing"
	"time"
)

func setupBenchPair(b *testing.B) (sender, receiver *Association, deliver func() int, received *int) {
	b.Helper()
	now := time.Unix(1_700_000_000, 0)
	var sToR, rToS [][]byte
	var recvCount int
	sender = New(Config{}, func(p []byte) {
		sToR = append(sToR, append([]byte(nil), p...))
	})
	receiver = New(Config{
		OnMessage: func(_ uint16, _ uint32, _ []byte) {
			recvCount++
		},
	}, func(p []byte) {
		rToS = append(rToS, append([]byte(nil), p...))
	})
	sender.Connect(now)
	for !sender.Established() || !receiver.Established() {
		for len(sToR) > 0 {
			p := sToR[0]
			sToR = sToR[1:]
			if err := receiver.HandlePacket(p, now); err != nil {
				b.Fatal(err)
			}
		}
		for len(rToS) > 0 {
			p := rToS[0]
			rToS = rToS[1:]
			if err := sender.HandlePacket(p, now); err != nil {
				b.Fatal(err)
			}
		}
	}

	// Switch to zero-copy ring queues for the steady-state benchmark loop.
	var sBufs, rBufs [][]byte
	sHead, rHead := 0, 0
	sender.send = func(p []byte) {
		var dst []byte
		if len(sBufs) < cap(sBufs) {
			dst = sBufs[:len(sBufs)+1][len(sBufs)][:0]
		}
		dst = append(dst[:0], p...)
		sBufs = append(sBufs, dst)
	}
	receiver.send = func(p []byte) {
		var dst []byte
		if len(rBufs) < cap(rBufs) {
			dst = rBufs[:len(rBufs)+1][len(rBufs)][:0]
		}
		dst = append(dst[:0], p...)
		rBufs = append(rBufs, dst)
	}
	deliver = func() int {
		moved := 0
		for sHead < len(sBufs) || rHead < len(rBufs) {
			for sHead < len(sBufs) {
				p := sBufs[sHead]
				sHead++
				moved++
				if err := receiver.HandlePacket(p, now); err != nil {
					b.Fatal(err)
				}
			}
			if sHead == len(sBufs) {
				sBufs, sHead = sBufs[:0], 0
			}
			for rHead < len(rBufs) {
				p := rBufs[rHead]
				rHead++
				moved++
				if err := sender.HandlePacket(p, now); err != nil {
					b.Fatal(err)
				}
			}
			if rHead == len(rBufs) {
				rBufs, rHead = rBufs[:0], 0
			}
		}
		return moved
	}
	return sender, receiver, deliver, &recvCount
}

func BenchmarkParsePacket(b *testing.B) {
	var pb packetBuilder
	pb.start(header{srcPort: 5000, dstPort: 5000, tag: 0x12345678})
	payload := make([]byte, 512)
	var head [12]byte
	pb.addChunk(chunkData, flagBegin|flagEnd, head[:], payload)
	pkt := append([]byte(nil), pb.finish()...)
	chunks := make([]chunk, 0, 4)

	b.ReportAllocs()
	b.SetBytes(int64(len(pkt)))
	b.ResetTimer()
	for b.Loop() {
		_, c, err := parsePacket(pkt, chunks)
		if err != nil || len(c) != 1 {
			b.Fatalf("parsePacket: len=%d err=%v", len(c), err)
		}
		chunks = c[:0]
	}
}

func BenchmarkSendReceiveSingleChunk(b *testing.B) {
	sender, receiver, deliver, recvCount := setupBenchPair(b)
	now := time.Unix(1_700_000_000, 0)
	msg := make([]byte, 256)
	for i := range msg {
		msg[i] = byte(i)
	}
	// Warm up buffers and SACK path.
	for range 4 {
		if err := sender.SendOwned(1, PPIDBinary, msg, SendOptions{}, now); err != nil {
			b.Fatal(err)
		}
		deliver()
	}
	receiver.HandleTimeout(now.Add(sackDelay))
	deliver()

	b.ReportAllocs()
	b.SetBytes(int64(len(msg)))
	b.ResetTimer()
	i := 0
	for b.Loop() {
		if err := sender.SendOwned(1, PPIDBinary, msg, SendOptions{}, now); err != nil {
			b.Fatal(err)
		}
		deliver()
		i++
		if i%2 == 0 {
			// Ensure SACKs keep clearing outstanding chunks without delay timer overhead.
			deliver()
		}
	}
	if *recvCount == 0 {
		b.Fatal("no messages received")
	}
}

func BenchmarkSendReceiveMultiChunk(b *testing.B) {
	sender, _, deliver, recvCount := setupBenchPair(b)
	now := time.Unix(1_700_000_000, 0)
	msg := make([]byte, 4096)
	for i := range msg {
		msg[i] = byte(i)
	}
	// Warm up buffers.
	for range 4 {
		if err := sender.SendOwned(1, PPIDBinary, msg, SendOptions{}, now); err != nil {
			b.Fatal(err)
		}
		deliver()
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(msg)))
	b.ResetTimer()
	for b.Loop() {
		if err := sender.SendOwned(1, PPIDBinary, msg, SendOptions{}, now); err != nil {
			b.Fatal(err)
		}
		deliver()
	}
	if *recvCount == 0 {
		b.Fatal("no messages received")
	}
}
