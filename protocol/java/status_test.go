package javaadapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/java"
)

// TestListenerShowsToJava lists the server for a client of Java Edition
// with the name and players of the host, as one of Bedrock Edition, and
// tells one that joins where to join.
func TestListenerShowsToJava(t *testing.T) {
	addr := freeAddr(t)
	host := testHost{status: server.Status{Name: "Adamant", MaxPlayers: 20}}
	l, err := Config{Address: addr}.Listener(server.Config{Log: slog.New(slog.DiscardHandler)}, host)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	st := queryStatus(t, addr)
	if st.Description.Text != "Adamant" || st.Version.Protocol != -1 ||
		!strings.HasPrefix(st.Version.Name, "Bedrock ") || st.Players.Online != 0 || st.Players.Max != 20 {
		t.Fatalf("status %+v", st)
	}

	nc, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	_ = nc.SetDeadline(time.Now().Add(5 * time.Second))
	c := java.NewConn(nc)
	hs := java.AppendVarInt(nil, 774)
	hs = java.AppendString(hs, "localhost")
	hs = append(hs, 0x63, 0xdd, 2)
	_ = c.WritePacket(0x00, hs)
	_ = c.WritePacket(0x00, append(java.AppendString(nil, "Steve"), make([]byte, 16)...))
	id, fields, err := c.ReadPacket()
	if err != nil || id != 0x00 || !strings.Contains(java.NewReader(fields).String(), "Bedrock Edition") {
		t.Fatalf("login answered %#x %q, %v", id, fields, err)
	}
}

// TestStatusNamesTheJoinableVersions checks that the status and the refusal
// of a client of Java Edition name the versions of Bedrock Edition that may
// join, as the host lists them, and not the release the server implements.
func TestStatusNamesTheJoinableVersions(t *testing.T) {
	addr := freeAddr(t)
	host := testHost{status: server.Status{Name: "Adamant"}, primary: "1.26.60", joinable: []string{"26.50–26.52", "26.60–26.61"}}
	l, err := Config{Address: addr}.Listener(server.Config{Log: slog.New(slog.DiscardHandler)}, host)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	const want = "1.26.50–1.26.52, 1.26.60–1.26.61"
	if st := queryStatus(t, addr); st.Version.Name != "Bedrock "+want || st.Version.Protocol != -1 {
		t.Errorf("version %+v, want Bedrock %s", st.Version, want)
	}
	if got := loginRefusal(t, addr); !strings.Contains(got, "Bedrock Edition "+want+".") {
		t.Errorf("refusal %q does not name %s", got, want)
	}
}

// TestStatusNamesThePrimaryWithoutJoinable checks that a host that lists no
// joinable version is named by its primary.
func TestStatusNamesThePrimaryWithoutJoinable(t *testing.T) {
	addr := freeAddr(t)
	l, err := Config{Address: addr}.Listener(server.Config{Log: slog.New(slog.DiscardHandler)}, testHost{primary: "1.26.50"})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if st := queryStatus(t, addr); st.Version.Name != "Bedrock 1.26.50" {
		t.Errorf("version name %q, want Bedrock 1.26.50", st.Version.Name)
	}
}

// TestVersionNameFitsTheStatus checks that the name of a host with many
// versions is cut, so that the status stays within the 32767 characters a
// client of Java Edition reads, and stays valid UTF-8.
func TestVersionNameFitsTheStatus(t *testing.T) {
	var joinable []string
	for i := range 2000 {
		joinable = append(joinable, fmt.Sprintf("%d.%d–%d.%d", i, i, i+1, i+1))
	}
	name := versionName("1.26.60", joinable)
	if len(name) > maxVersionName || !utf8.ValidString(name) || !strings.HasPrefix(name, "Bedrock 1.0.0–1.1.1, ") {
		t.Errorf("name of %d bytes %q", len(name), name)
	}
}

// loginRefusal returns the message a client of Java Edition that logs in
// to addr is refused with.
func loginRefusal(t *testing.T, addr string) string {
	t.Helper()
	nc, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	_ = nc.SetDeadline(time.Now().Add(5 * time.Second))
	c := java.NewConn(nc)
	hs := java.AppendVarInt(nil, 774)
	hs = java.AppendString(hs, "localhost")
	hs = append(hs, 0x63, 0xdd, 2)
	_ = c.WritePacket(0x00, hs)
	_ = c.WritePacket(0x00, append(java.AppendString(nil, "Steve"), make([]byte, 16)...))
	id, fields, err := c.ReadPacket()
	if err != nil || id != 0x00 {
		t.Fatalf("login answered %#x %q, %v", id, fields, err)
	}
	return java.NewReader(fields).String()
}

// TestListenerCountsThePlayersOfTheHost checks that the status counts the
// players of the host, and that a maximum of 0 shows one more than them.
func TestListenerCountsThePlayersOfTheHost(t *testing.T) {
	addr := freeAddr(t)
	host := testHost{status: server.Status{Name: "Adamant", PlayerCount: 3}}
	l, err := Config{Address: addr}.Listener(server.Config{Log: slog.New(slog.DiscardHandler)}, host)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if st := queryStatus(t, addr); st.Players.Online != 3 || st.Players.Max != 4 {
		t.Fatalf("status %+v, want 3 of 4 players", st)
	}
}

// TestListenerAcceptsNoJoin checks that Accept blocks until the listener is
// closed, and that it then fails.
func TestListenerAcceptsNoJoin(t *testing.T) {
	l, err := Config{Address: "127.0.0.1:0"}.Listener(server.Config{Log: slog.New(slog.DiscardHandler)}, testHost{})
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan error, 1)
	go func() {
		_, err := l.Accept()
		accepted <- err
	}()
	select {
	case err := <-accepted:
		t.Fatalf("Accept returned %v before Close", err)
	case <-time.After(50 * time.Millisecond):
	}
	_ = l.Close()
	select {
	case err := <-accepted:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Accept after Close returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return after Close")
	}
}

// TestListenerFailsOnABoundAddress checks that a listener refuses an address
// it cannot listen on.
func TestListenerFailsOnABoundAddress(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	if _, err := (Config{Address: taken.Addr().String()}).Listener(server.Config{Log: slog.New(slog.DiscardHandler)}, testHost{}); err == nil ||
		!strings.HasPrefix(err.Error(), "listen for Java Edition: ") {
		t.Fatalf("listening on a bound address: %v", err)
	}
}

// javaStatus is the part of the status of the server a test reads.
type javaStatus struct {
	Version struct {
		Name     string
		Protocol int32
	}
	Players     struct{ Online, Max int }
	Description struct{ Text string }
}

// queryStatus asks the listener on addr for its status as a client of Java
// Edition does.
func queryStatus(t *testing.T, addr string) javaStatus {
	t.Helper()
	status, _, err := java.QueryStatus(addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var st javaStatus
	if err := json.Unmarshal(status, &st); err != nil {
		t.Fatalf("status %s: %v", status, err)
	}
	return st
}

// freeAddr returns a TCP address on the loopback that nothing listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()
	return addr
}

// testHost is a Host of a fixed status, with no players and no world.
type testHost struct {
	status   server.Status
	primary  string
	joinable []string
}

func (h testHost) Status() server.Status  { return h.status }
func (h testHost) PlayerNames() []string  { return nil }
func (h testHost) World() *world.World    { return nil }
func (h testHost) WhitelistEnabled() bool { return false }
func (h testHost) GameVersions() (string, []string) {
	if h.primary == "" {
		return "1.26.60", nil
	}
	return h.primary, h.joinable
}
