package interop

import (
	"context"
	"encoding/binary"
	"os"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/nethernet"
)

// TestDialBDS joins a Bedrock Dedicated Server with our dialer when
// BDS_ADDR names its signaling, such as http://127.0.0.1:19232. BDS runs
// libwebrtc: it takes the DTLS client role, opens SCTP itself, and answers
// RequestNetworkSettings on the reliable channel.
func TestDialBDS(t *testing.T) {
	addr := os.Getenv("BDS_ADDR")
	if addr == "" {
		t.Skip("BDS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := nethernet.Dialer{Key: p384(t)}.DialContext(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.PublicKey() == nil {
		t.Fatal("the answer of BDS carried no identity")
	}
	// An uncompressed batch of one RequestNetworkSettings: the length of
	// the packet, its ID and the protocol of 1.26.50.
	pk := binary.BigEndian.AppendUint32(binary.AppendUvarint(nil, 193), 2193)
	if err := c.Send(append(binary.AppendUvarint(nil, uint64(len(pk))), pk...)); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	msg, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	size, n := binary.Uvarint(msg)
	if n <= 0 || int(size) > len(msg)-n {
		t.Fatalf("malformed batch % x", msg)
	}
	header, _ := binary.Uvarint(msg[n:])
	// NetworkSettings, or PlayStatus if BDS runs another protocol.
	if id := header & 0x3ff; id != 143 && id != 2 {
		t.Fatalf("BDS answered packet %d: % x", id, msg)
	}
	t.Logf("BDS answered packet %d in %d bytes; RTT %v", header&0x3ff, len(msg), c.RTT())
}
