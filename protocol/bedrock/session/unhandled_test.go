package session

import (
	"bytes"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// TestUnhandledPacketLog checks how a session logs a packet it has no
// handler for: nothing at the info level of the default config.toml, and
// at debug its type and size, never its fields, once a second at most,
// counting the packets left out.
func TestUnhandledPacketLog(t *testing.T) {
	pk := &mc.TextPacket{TextType: mc.TextTypeChat, Message: "a secret of the client"}

	var info bytes.Buffer
	s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(&info, &slog.HandlerOptions{Level: slog.LevelInfo}))}}
	if err := s.handlePacket(pk, nil, nil); err != nil {
		t.Fatal(err)
	}
	if info.Len() != 0 {
		t.Fatalf("logged at info level: %s", info.String())
	}

	var debug bytes.Buffer
	s = &Session{conf: Config{Log: slog.New(slog.NewTextHandler(&debug, &slog.HandlerOptions{Level: slog.LevelDebug}))}}
	for range 3 {
		if err := s.handlePacket(pk, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	lines := strings.Split(strings.TrimSpace(debug.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("logged %d lines of 3 packets within a second, want 1:\n%s", len(lines), debug.String())
	}
	size := "size=" + strconv.Itoa(packetSize(pk))
	for _, want := range []string{"level=DEBUG", `msg="unhandled packet"`, fmt.Sprintf("packet=%T", pk), size, "skipped=0"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("line %q lacks %q", lines[0], want)
		}
	}
	if strings.Contains(debug.String(), "secret") {
		t.Errorf("the fields of the packet were logged: %s", debug.String())
	}

	s.unhandled.last = time.Now().Add(-unhandledInterval)
	debug.Reset()
	if err := s.handlePacket(pk, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(debug.String(), "skipped=2") {
		t.Errorf("the next line does not count the 2 packets left out: %s", debug.String())
	}
}
