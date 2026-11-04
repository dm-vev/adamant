package netconn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// TestStartGameEndsWithItsContext checks that a client that asked for its
// chunk radius and never says it initialised is let go of once the
// context of StartGameContext ends, and still reads the Disconnect the
// server then sends it, as the server sends one that timed out.
func TestStartGameEndsWithItsContext(t *testing.T) {
	l := listen(t, Config{})
	link := proxyLink(t, l)
	b := openProxiedFrom(t, link, "Slow", "203.0.113.7:4242")
	readUntil(t, b, v2193.IDResourcePacksInfo)
	writeRaw(t, b, &v2193.ResourcePackClientResponsePacket{Response: v2193.PackResponseAllPacksDownloaded})
	readUntil(t, b, v2193.IDResourcePackStack)
	writeRaw(t, b, &v2193.ResourcePackClientResponsePacket{Response: v2193.PackResponseCompleted})

	c, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	began := time.Now()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		done <- c.StartGameContext(ctx, gameData())
	}()
	readUntil(t, b, v2193.IDStartGame)
	writeRaw(t, b, &v2193.RequestChunkRadiusPacket{ChunkRadius: 8})
	readUntil(t, b, v2193.IDPlayStatus)
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("StartGameContext returned %v, want the deadline", err)
		}
		if waited := time.Since(began); waited < 100*time.Millisecond {
			t.Fatalf("StartGameContext returned after %v, before the deadline", waited)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("StartGameContext waits past its context")
	}
	_ = l.Disconnect(c, "Timed out.")
	if d := readUntil(t, b, v2193.IDDisconnect).(*v2193.DisconnectPacket); d.Message != "Timed out." {
		t.Fatalf("disconnect %q", d.Message)
	}
}
