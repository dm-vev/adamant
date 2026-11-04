package remoteadmin

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/cmd/builtin"
	"github.com/df-mc/dragonfly/server/world"
)

// The tokens of the tests, of the levels 4 and 2.
const (
	ownerToken  = "owner-0123456789abcdef0123456789abcdef"
	helperToken = "helper-0123456789abcdef0123456789abcde"
)

// remoteTest is a server with the remote admin API, its handler, its
// audit log and its file of tokens.
type remoteTest struct {
	srv    *server.Server
	h      *Handler
	tokens string
	audit  string
}

// newRemoteTest returns a server of the defaults of config.toml, with no
// saved data, and the handler of its API, of the tokens owner (4) and
// helper (2).
func newRemoteTest(t *testing.T) *remoteTest {
	t.Helper()
	dir := t.TempDir()
	uc := server.DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.World.DisableNether, uc.World.DisableEnd = true, true
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	conf.Log = slog.New(slog.DiscardHandler)
	conf.Generator = func(world.Dimension, int64) world.Generator { return world.NopGenerator{} }
	rt := &remoteTest{srv: conf.New(), tokens: filepath.Join(dir, "admin-tokens.txt"), audit: filepath.Join(dir, "audit.log")}
	builtin.Register(rt.srv)
	t.Cleanup(func() {
		cmd.SetPermissions(nil)
		cmd.SetObserver(nil)
		cmd.SetAdminFeedback(nil)
	})
	rt.srv.Listen()
	t.Cleanup(func() { _ = rt.srv.Close() })
	rt.writeTokens(t, "# tokens\nowner 4 "+ownerToken+"\nhelper 2 "+helperToken+"\n")
	if rt.h, err = New(rt.srv, Config{TokenFile: rt.tokens, Hosts: []string{"admin.example.invalid:19140"}, Log: slog.New(slog.DiscardHandler),
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}); err != nil {
		t.Fatal(err)
	}
	rt.h.reloadEvery = 0
	return rt
}

// writeTokens writes the file of tokens, of mode 0600.
func (rt *remoteTest) writeTokens(t *testing.T, data string) {
	t.Helper()
	if err := os.WriteFile(rt.tokens, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// do serves a request from the address passed with the token passed, ""
// for none, and returns the answer.
func (rt *remoteTest) do(t *testing.T, method, path, from, tok, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
	r.RemoteAddr = from
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	w := httptest.NewRecorder()
	rt.h.ServeHTTP(w, r)
	return w
}

// command runs a command with the token passed and returns its answer.
func (rt *remoteTest) command(t *testing.T, tok, body string) (int, commandAnswer) {
	t.Helper()
	w := rt.do(t, http.MethodPost, "/v1/command", "127.0.0.1:50000", tok, body)
	var a commandAnswer
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	return w.Code, a
}

// TestRemoteAuth checks the sign-in: no header and a wrong token are 401;
// past MaxFailures failures of an address, every request of it is 429,
// with the right token too, while other addresses are served; past 60
// failures a minute in all, every address but loopback is 429; a changed
// file of tokens is read again; X-Forwarded-For counts behind a trusted
// proxy only.
func TestRemoteAuth(t *testing.T) {
	rt := newRemoteTest(t)
	clock := time.Date(2026, 11, 4, 14, 0, 0, 0, time.UTC)
	rt.h.now = func() time.Time { return clock }
	if w := rt.do(t, "GET", "/v1/status", "198.51.100.1:1", "", ""); w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" {
		t.Errorf("no header: %d", w.Code)
	}
	if w := rt.do(t, "GET", "/v1/status", "198.51.100.1:1", ownerToken, ""); w.Code != 200 {
		t.Errorf("the owner's token: %d %s", w.Code, w.Body)
	}
	for i := range DefaultMaxFailures - 1 {
		if w := rt.do(t, "GET", "/v1/status", "198.51.100.1:1", "wrong", ""); w.Code != 401 {
			t.Fatalf("failure %d: %d", i+2, w.Code)
		}
	}
	if w := rt.do(t, "GET", "/v1/status", "198.51.100.1:1", ownerToken, ""); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Errorf("the right token after %d failures: %d, Retry-After %q", DefaultMaxFailures, w.Code, w.Header().Get("Retry-After"))
	}
	if w := rt.do(t, "GET", "/v1/status", "198.51.100.2:1", ownerToken, ""); w.Code != 200 {
		t.Errorf("another address: %d", w.Code)
	}
	clock = clock.Add(2 * time.Minute)
	if w := rt.do(t, "GET", "/v1/status", "198.51.100.1:1", ownerToken, ""); w.Code != 200 {
		t.Errorf("the address a minute later: %d", w.Code)
	}

	// X-Forwarded-For behind a trusted proxy names the client.
	for range DefaultMaxFailures {
		r := httptest.NewRequest("GET", "http://localhost/v1/status", nil)
		r.RemoteAddr, r.Header["X-Forwarded-For"] = "10.1.2.3:1", []string{"203.0.113.50, 10.9.9.9"}
		rt.h.ServeHTTP(httptest.NewRecorder(), r)
	}
	if w := rt.do(t, "GET", "/v1/status", "10.1.2.3:1", ownerToken, ""); w.Code != 200 {
		t.Errorf("the trusted proxy itself after its client failed: %d", w.Code)
	}
	if until, blocked := rt.h.limit.blocked(netip.MustParseAddr("203.0.113.50"), clock); !blocked || until.IsZero() {
		t.Error("the client behind the proxy is not blocked")
	}

	// 60 failures a minute block all but loopback.
	for i := range globalFailures {
		rt.do(t, "GET", "/v1/status", netip.AddrFrom4([4]byte{192, 0, 2, byte(i)}).String()+":1", "wrong", "")
	}
	if w := rt.do(t, "GET", "/v1/status", "198.51.100.9:1", ownerToken, ""); w.Code != 429 {
		t.Errorf("a new address past 60 failures a minute: %d", w.Code)
	}
	if w := rt.do(t, "GET", "/v1/status", "127.0.0.1:1", ownerToken, ""); w.Code != 200 {
		t.Errorf("loopback past 60 failures a minute: %d", w.Code)
	}

	// The file is read again when it changes.
	clock = clock.Add(2 * time.Minute)
	const rotated = "rotated-0123456789abcdef0123456789abcdef"
	rt.writeTokens(t, "owner 4 "+rotated+"\n")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(rt.tokens, future, future); err != nil {
		t.Fatal(err)
	}
	if w := rt.do(t, "GET", "/v1/status", "127.0.0.1:1", ownerToken, ""); w.Code != 401 {
		t.Errorf("the old token after the file changed: %d", w.Code)
	}
	if w := rt.do(t, "GET", "/v1/status", "127.0.0.1:1", rotated, ""); w.Code != 200 {
		t.Errorf("the new token: %d", w.Code)
	}
	if w := rt.do(t, "GET", "/v1/status", "127.0.0.1:1", rotated, ""); w.Code != 200 {
		t.Errorf("the new token again: %d", w.Code)
	}
}

// TestTokenFile checks the file of tokens: GenerateTokenFile makes it once,
// of mode 0600, with an owner token of 40 letters and digits; a file read
// by others is reported; short tokens, bad levels and twice a name are
// errors.
func TestTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin-tokens.txt")
	if made, err := GenerateTokenFile(path); !made || err != nil {
		t.Fatalf("GenerateTokenFile = %v, %v", made, err)
	}
	if made, err := GenerateTokenFile(path); made || err != nil {
		t.Fatalf("GenerateTokenFile of a file there = %v, %v", made, err)
	}
	if open, err := TokenFileTooOpen(path); open || err != nil {
		t.Errorf("the generated file is open to others: %v, %v", open, err)
	}
	tokens, err := readTokens(path)
	if err != nil || len(tokens) != 1 || tokens[0].name != "owner" || tokens[0].level != cmd.LevelOwner {
		t.Fatalf("the generated tokens: %+v, %v", tokens, err)
	}
	data, _ := os.ReadFile(path)
	line := strings.Fields(strings.Split(strings.TrimSpace(string(data)), "\n")[1])
	if len(line[2]) != GeneratedTokenLength || strings.Trim(line[2], tokenAlphabet) != "" {
		t.Errorf("the generated token %q", line[2])
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if open, _ := TokenFileTooOpen(path); !open {
		t.Error("a file of mode 0644 is not too open")
	}
	for _, bad := range []string{"a 4 short", "a 5 " + ownerToken, "a x " + ownerToken, "a 4 " + ownerToken + "\na 3 " + helperToken, "a 4"} {
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readTokens(path); err == nil {
			t.Errorf("%q reads", bad)
		} else if strings.Contains(err.Error(), ownerToken) {
			t.Errorf("the error names the token: %v", err)
		}
	}
}

// slowCommand sends its output after it returned, once released, as
// /save-all does once the world was saved.
type slowCommand struct{ release chan struct{} }

func (c slowCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	o.Print("started")
	if d, ok := src.(cmd.Deferrer); ok {
		done := d.Defer()
		go func() {
			<-c.release
			out := &cmd.Output{}
			out.Print("§afinished")
			src.SendCommandOutput(out)
			done()
		}()
	}
}

// TestRemoteCommand checks POST /v1/command and the limits of requests:
// the output of a command, its later output waited for, pending past the
// wait; a token of level 2 told /ban does not exist; a body over 64 KiB,
// a request past 8 in flight and an unknown Host refused; the endpoints
// above the level of a token refused; and each remote command recorded.
func TestRemoteCommand(t *testing.T) {
	rt := newRemoteTest(t)
	if code, a := rt.command(t, ownerToken, `{"command": "list"}`); code != 200 || len(a.Output) != 1 || !strings.HasPrefix(a.Output[0].Text, "There are 0/") || a.Pending {
		t.Errorf("/list: %d %+v", code, a)
	}
	if code, a := rt.command(t, helperToken, `{"command": "/ban Griefer"}`); code != 200 || len(a.Output) != 1 || !a.Output[0].Error ||
		!strings.Contains(a.Output[0].Text, "Unknown command") {
		t.Errorf("/ban from level 2: %d %+v", code, a)
	}

	release := make(chan struct{})
	cmd.Register(cmd.New("slowtest", "", nil, slowCommand{release: release}))
	go func() { time.Sleep(50 * time.Millisecond); close(release) }()
	if _, a := rt.command(t, ownerToken, `{"command": "slowtest"}`); len(a.Output) != 2 || a.Output[1].Text != "finished" || a.Pending {
		t.Errorf("a command of later output: %+v", a)
	}
	cmd.Register(cmd.New("slowtest", "", nil, slowCommand{release: make(chan struct{})}))
	if _, a := rt.command(t, ownerToken, `{"command": "slowtest", "wait": "50ms"}`); len(a.Output) != 1 || !a.Pending {
		t.Errorf("a command whose output did not come: %+v", a)
	}

	if code, _ := rt.command(t, ownerToken, `{"command": "say `+strings.Repeat("x", MaxBody)+`"}`); code != 413 {
		t.Errorf("a body over 64 KiB: %d", code)
	}
	if code, _ := rt.command(t, ownerToken, `{"cmd": 5`); code != 400 {
		t.Errorf("a bad body: %d", code)
	}
	for range MaxInFlight {
		rt.h.inFlight <- struct{}{}
	}
	if w := rt.do(t, "GET", "/v1/status", "127.0.0.1:1", ownerToken, ""); w.Code != 503 {
		t.Errorf("the request past %d in flight: %d", MaxInFlight, w.Code)
	}
	for range MaxInFlight {
		<-rt.h.inFlight
	}
	r := httptest.NewRequest("GET", "http://evil.example.invalid/v1/status", nil)
	r.Header.Set("Authorization", "Bearer "+ownerToken)
	w := httptest.NewRecorder()
	rt.h.ServeHTTP(w, r)
	if w.Code != 421 {
		t.Errorf("an unknown Host: %d", w.Code)
	}
	r.Host = "admin.example.invalid"
	w = httptest.NewRecorder()
	rt.h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("a Host of Hosts: %d", w.Code)
	}
	for path, want := range map[string][2]int{"/v1/players": {200, 200}, "/v1/bans": {403, 200}, "/v1/operators": {403, 200}, "/v1/nothing": {404, 404}} {
		for i, tok := range []string{helperToken, ownerToken} {
			if w := rt.do(t, "GET", path, "127.0.0.1:1", tok, ""); w.Code != want[i] {
				t.Errorf("GET %s with the token of level %d: %d, want %d", path, []int{2, 4}[i], w.Code, want[i])
			}
		}
	}

	if err := rt.srv.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(rt.audit)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var remote, reads int
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for s.Scan() {
		var r audit.Record
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Actor.Kind == audit.KindRemote && r.Actor.Token != "" && r.Action != "read" {
			remote++
		}
		if r.Action == "read" {
			reads++
		}
	}
	if remote < 4 || reads < 2 {
		t.Errorf("%d records of remote commands and %d of reads, want 4 or more and 2 or more", remote, reads)
	}
}
