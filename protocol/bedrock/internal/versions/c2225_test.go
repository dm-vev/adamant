package versions

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/convert/c2225"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// corpusPacket is a packet of the golden snapshots of the session: its ID,
// whether the client sent it, and its fields, in protocol 2193.
type corpusPacket struct {
	file   string
	id     uint32
	client bool
	fields []byte
}

// packetLine opens a packet of a snapshot: ">>" one the session sent, "<<"
// one its client sent.
var packetLine = regexp.MustCompile(`^(>>|<<) \w+ id=(\d+) len=(\d+)`)

// goldenCorpus reads the packets of the golden snapshots whose bytes the
// snapshot holds whole: it cuts the bytes of large ones.
func goldenCorpus(t *testing.T) []corpusPacket {
	t.Helper()
	files, err := filepath.Glob("../../session/testdata/golden/*.snapshot")
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden snapshots: %v", err)
	}
	var out []corpusPacket
	for _, f := range files {
		file, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		var cur *corpusPacket
		var size int
		var b strings.Builder
		flush := func() {
			if cur == nil {
				return
			}
			raw, err := hex.DecodeString(b.String())
			if err == nil && len(raw) == size {
				cur.fields = raw
				out = append(out, *cur)
			}
			cur = nil
			b.Reset()
		}
		sc := bufio.NewScanner(file)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if m := packetLine.FindStringSubmatch(line); m != nil {
				flush()
				id, _ := strconv.Atoi(m[2])
				size, _ = strconv.Atoi(m[3])
				cur = &corpusPacket{file: filepath.Base(f), id: uint32(id), client: m[1] == "<<"}
				continue
			}
			if text := strings.TrimSpace(line); cur != nil && strings.HasPrefix(text, "| ") {
				b.WriteString(strings.TrimPrefix(text, "| "))
			}
		}
		flush()
		_ = file.Close()
	}
	return out
}

func encodePacket(pk proto.Packet) []byte {
	var w wire.Writer
	pk.Encode(&w)
	return w.B
}

// decodeAs decodes the bytes of a packet with the codec of a protocol.
func decodeAs(newPacket func(uint32) proto.Packet, id uint32, b []byte) (proto.Packet, bool) {
	pk := newPacket(id)
	if pk == nil {
		return nil, false
	}
	r := wire.NewReader(b)
	pk.Decode(r)
	return pk, r.Err() == nil && r.Len() == 0
}

// TestC2225RoundTripsTheGoldenCorpus takes every packet of the golden
// snapshots, those the session sent and those its client sent, through the
// step from 2193 to 2225 and back. What Up gives is valid in 2225: it
// encodes, and decodes as 2225 again to the same bytes; Down of it gives
// the packet of 2193 again, byte for byte. A packet of the same layout
// goes as it is, and its bytes decode as 2225 too.
func TestC2225RoundTripsTheGoldenCorpus(t *testing.T) {
	corpus := goldenCorpus(t)
	if len(corpus) < 1000 {
		t.Fatalf("%d packets in the corpus", len(corpus))
	}
	var changed, same []uint32
	for _, p := range corpus {
		pk, ok := decodeAs(v2193.New, p.id, p.fields)
		if !ok {
			t.Errorf("%s: packet %d does not decode as 2193", p.file, p.id)
			continue
		}
		want := encodePacket(pk)
		ups := c2225.Up(pk)
		if len(ups) != 1 {
			t.Errorf("%s: packet %d goes to 2225 as %d packets", p.file, p.id, len(ups))
			continue
		}
		up := ups[0]
		b := encodePacket(up)
		again, ok := decodeAs(v2225.New, up.ID(), b)
		if !ok || !bytes.Equal(encodePacket(again), b) {
			t.Errorf("%s: packet %d of 2225 (%T) does not decode as 2225 to the same bytes", p.file, p.id, up)
			continue
		}
		if c2225.Same(p.id) {
			if up != pk {
				t.Errorf("%s: packet %d of the same layout was converted", p.file, p.id)
			}
			if !slices.Contains(same, p.id) {
				same = append(same, p.id)
			}
			continue
		}
		if !slices.Contains(changed, p.id) {
			changed = append(changed, p.id)
		}
		downs := c2225.Down(again)
		if len(downs) != 1 || !bytes.Equal(encodePacket(downs[0]), want) {
			t.Errorf("%s: packet %d comes back from 2225 otherwise\nwant % x", p.file, p.id, head(want))
		}
	}
	slices.Sort(changed)
	t.Logf("%d packets: %d IDs of the same layout, %d that changed: %v", len(corpus), len(same), len(changed), changed)
	// StartGame is too large for the snapshots to hold its bytes whole:
	// c2225's own tests convert one.
	for _, id := range []uint32{12, 13, 30, 44, 58, 63, 144, 147} {
		if !slices.Contains(changed, id) {
			t.Errorf("the corpus has no packet %d, which 2225 changed", id)
		}
	}
}

func head(b []byte) []byte {
	if len(b) > 64 {
		return b[:64]
	}
	return b
}
