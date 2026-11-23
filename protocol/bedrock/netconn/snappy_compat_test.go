package netconn

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock"
	ourclient "github.com/dm-vev/adamant/net/bedrock/client"
	ourlogin "github.com/dm-vev/adamant/net/bedrock/login"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/golang/snappy"
)

func TestSnappyCompatWithGolangSnappy(t *testing.T) {
	var bs bedrock.Snappy
	rng := rand.New(rand.NewPCG(42, 99))

	cases := [][]byte{
		nil,
		{0},
		{1, 2, 3, 4},
		bytes.Repeat([]byte{0x5a}, 5000),
		bytes.Repeat([]byte("move actor absolute payload "), 300),
	}
	for _, n := range []int{15, 60, 61, 255, 256, 1024, 2048, 4096, 65535, 65536, 65537, 131072} {
		b := make([]byte, n)
		for i := range b {
			if i%3 == 0 {
				b[i] = byte(rng.Uint32())
			} else {
				b[i] = byte(i % 29)
			}
		}
		cases = append(cases, b)
	}

	for i, in := range cases {
		// bedrock.Snappy -> golang/snappy
		compB, err := bs.Compress([]byte{0x7f}, in)
		if err != nil || compB[0] != 0x7f {
			t.Fatalf("case %d: bedrock compress err=%v", i, err)
		}
		decG, err := snappy.Decode(nil, compB[1:])
		if err != nil || !bytes.Equal(decG, in) {
			t.Fatalf("case %d (%dB): golang/snappy decode err=%v len=%d", i, len(in), err, len(decG))
		}

		// golang/snappy -> bedrock.Snappy
		compG := snappy.Encode(nil, in)
		decB, err := bs.Decompress([]byte("P:"), compG, len(in))
		if err != nil || !bytes.HasPrefix(decB, []byte("P:")) || !bytes.Equal(decB[2:], in) {
			t.Fatalf("case %d (%dB): bedrock decompress err=%v len=%d", i, len(in), err, len(decB))
		}
		if len(in) > 0 {
			if _, err := bs.Decompress(nil, compG, len(in)-1); !errors.Is(err, bedrock.ErrTooLarge) {
				t.Fatalf("case %d: limit n-1 got %v, want ErrTooLarge", i, err)
			}
		}
	}
}

func TestNetconnSnappyEndToEnd(t *testing.T) {
	l := listen(t, Config{Compression: bedrock.Snappy{}})
	spawned := serve(t, l)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ourclient.Dial(ctx, "http://"+l.Addr().String(), ourclient.Config{Identity: ourlogin.Identity{DisplayName: "SnappyBot"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	srv := <-spawned
	msg := "snappy-" + string(bytes.Repeat([]byte("payload "), 80))
	if err := srv.WritePacket(&mc.TextPacket{TextType: mc.TextTypeRaw, Message: msg}); err != nil {
		t.Fatal(err)
	}
	if err := srv.Flush(); err != nil {
		t.Fatal(err)
	}
	for {
		pk, err := conn.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if text, ok := pk.(*mc.TextPacket); ok {
			if text.Message != msg {
				t.Fatalf("got message len=%d, want %d", len(text.Message), len(msg))
			}
			return
		}
	}
}
