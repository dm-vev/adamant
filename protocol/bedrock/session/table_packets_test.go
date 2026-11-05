package session_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/nbt"
)

// tableProtocols are the protocols the packets of a sound or a particle are
// encoded for in the goldens of the tables: that of the model the session is
// compiled against, and 2193, whose clients are sent what
// protocol/bedrock/internal/versions converts them to.
func tableProtocols() []*versions.Protocol {
	return []*versions.Protocol{versions.Model(), oldProtocol()}
}

// encodeTablePackets returns the lines that tell the packets passed as each
// of the protocols is sent them: the ID and the bytes of each, or none.
func encodeTablePackets(pks []proto.Packet) []string {
	var lines []string
	for _, p := range tableProtocols() {
		var out []proto.Packet
		for _, pk := range pks {
			if p.Converts() {
				out = append(out, p.Out(pk)...)
			} else {
				out = append(out, pk)
			}
		}
		if len(out) == 0 {
			lines = append(lines, fmt.Sprintf("  v%d none", p.Number))
		}
		for _, pk := range out {
			lines = append(lines, packetLine(p, pk))
		}
	}
	return lines
}

// packetLine returns the line of a golden of the tables for a packet as the
// protocol passed is sent it: its ID and bytes. The NBT of a generic level
// event is written as the compound it holds, with the keys in order: the
// session encodes it from a map, whose order is not the same twice.
func packetLine(p *versions.Protocol, pk proto.Packet) string {
	var event int32
	var data []byte
	switch g := pk.(type) {
	case *mc.LevelEventGenericPacket:
		event, data = g.EventID, g.SerialisedEventData
	case *v2193.LevelEventGenericPacket:
		event, data = g.EventID, g.SerialisedEventData
	default:
		return fmt.Sprintf("  v%d id=%d %x", p.Number, pk.ID(), encode(pk))
	}
	// The payload is the compound without its header and end tag.
	var compound map[string]any
	whole := append(append([]byte{10, 0}, data...), 0)
	if err := nbt.UnmarshalEncoding(whole, &compound, nbt.NetworkLittleEndian); err != nil {
		return fmt.Sprintf("  v%d id=%d event=%d undecodable %x: %v", p.Number, pk.ID(), event, data, err)
	}
	return fmt.Sprintf("  v%d id=%d event=%d nbt=%v", p.Number, pk.ID(), event, compound)
}

// tablePacketLines returns the lines of a golden of the tables for the sound
// or particle labelled: the packets play returns, as encodeTablePackets
// writes them, or a panic of play written as a line.
func tablePacketLines(label string, play func() []proto.Packet) (lines []string) {
	lines = []string{label}
	defer func() {
		if r := recover(); r != nil {
			lines = append(lines, fmt.Sprint("  panic: ", r))
		}
	}()
	return append(lines, encodeTablePackets(play())...)
}

// checkTableGolden compares the lines passed with the golden file passed, or
// writes the file if update is set. what names the flag that updates it in
// the message of a difference.
func checkTableGolden(t *testing.T, file string, update bool, what string, got []string) {
	t.Helper()
	out := strings.Join(got, "\n") + "\n"
	if update {
		if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) == out {
		return
	}
	t.Errorf("the packets of the %s differ from %s (run with -update-%s to see the change as a diff)", what, file, what)
	wl, gl := strings.Split(string(want), "\n"), strings.Split(out, "\n")
	for i := 0; i < min(len(wl), len(gl)); i++ {
		if wl[i] != gl[i] {
			t.Fatalf("first difference at line %d:\ngolden: %s\ngot:    %s", i+1, wl[i], gl[i])
		}
	}
	t.Fatalf("golden has %d lines, got %d", len(wl), len(gl))
}
