package bedrockadapter

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/turn"
	"github.com/dm-vev/adamant/net/xbox/auth"
	"github.com/dm-vev/adamant/net/xbox/mpsd"
	"github.com/dm-vev/adamant/net/xbox/signaling"
	"github.com/dm-vev/adamant/net/xbox/social"
)

// TestXboxTokenFile keeps a sign-in in a file only its owner reads and
// signs in from it on the next start without asking the user.
func TestXboxTokenFile(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	snap := auth.Snapshot{RefreshToken: "refresh", ProofKey: base64.StdEncoding.EncodeToString(der), DeviceID: "{device}"}
	path := filepath.Join(t.TempDir(), "xbox-token.json")
	if err := saveXboxToken(path, snap); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v, want 0600", info.Mode().Perm())
	}
	snap.RefreshToken = "rotated"
	if err := saveXboxToken(path, snap); err != nil {
		t.Fatal(err)
	}
	s, err := xboxSession(context.Background(), path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got != snap {
		t.Fatalf("restored %+v, want %+v", got, snap)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("%d files beside the token file", len(entries)-1)
	}
}

// TestRelayServer takes the first TURN server over UDP the signaling
// service hands out, with its credentials, and passes over STUN servers
// and TURN over TCP.
func TestRelayServer(t *testing.T) {
	cfg, ok := relayServer(context.Background(), []signaling.ICEServer{
		{URLs: []string{"stun:192.0.2.1:3478"}},
		{URLs: []string{"turn:192.0.2.2:443?transport=tcp", "turn:192.0.2.3:3479?transport=udp"}, Username: "u", Credential: "p"},
		{URLs: []string{"turn:192.0.2.4"}, Username: "x", Credential: "y"},
	})
	if !ok || cfg.Server != netip.MustParseAddrPort("192.0.2.3:3479") || cfg.Username != "u" || cfg.Password != "p" {
		t.Fatalf("relay server %+v, %v", cfg, ok)
	}
	if cfg, ok := relayServer(context.Background(), []signaling.ICEServer{{URLs: []string{"turn:192.0.2.4"}}}); !ok || cfg.Server.Port() != 3478 {
		t.Fatalf("default port: %+v, %v", cfg, ok)
	}
	if _, ok := relayServer(context.Background(), []signaling.ICEServer{{URLs: []string{"stun:192.0.2.1"}}}); ok {
		t.Fatal("a STUN server taken for a relay")
	}
}

// TestXboxFriendsAccept adds the people who sent the account a friend
// request, and stops for as long as the service asks once it refuses for
// too many.
func TestXboxFriendsAccept(t *testing.T) {
	var added []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/users/me/people/friendrequests(received)":
			_, _ = io.WriteString(w, `{"people":[{"xuid":"2535400000000002","gamertag":"A"},{"xuid":"2535400000000003","gamertag":"B"},{"xuid":"2535400000000004","gamertag":"C"}]}`)
		case strings.HasPrefix(r.URL.Path, "/users/me/people/friends/v2/"):
			if len(added) == 2 {
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			added = append(added, r.URL.Path)
			_, _ = io.WriteString(w, `{"isFriend":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := &xboxFriends{
		client: social.Client{Token: func(context.Context) (string, error) { return "XBL3.0 x=1;t", nil },
			Endpoints: social.Endpoints{Social: srv.URL, PeopleHub: srv.URL, SessionDirectory: srv.URL}},
		requests: make(chan struct{}, 1),
		log:      slog.New(slog.DiscardHandler),
	}
	if wait := f.accept(context.Background()); wait != 30*time.Second || len(added) != 2 {
		t.Fatalf("waits %v after adding %v", wait, added)
	}
}

// TestFriendsStalledService takes a friend request through social services
// that stall one of the requests of it, with a context that lives as long
// as the server: each request gives up rather than holding up the friends
// of the account for good.
func TestFriendsStalledService(t *testing.T) {
	old := xboxCallTimeout
	xboxCallTimeout = 200 * time.Millisecond
	defer func() { xboxCallTimeout = old }()
	for _, stall := range []string{"friendrequests", "friends/v2", "handles"} {
		t.Run(stall, func(t *testing.T) {
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, stall) {
					<-release
				}
				switch {
				case strings.Contains(r.URL.Path, "friendrequests"):
					_, _ = w.Write([]byte(`{"people":[{"xuid":"2535400000000002","gamertag":"Friend"}]}`))
				case strings.Contains(r.URL.Path, "friends/v2"):
					_, _ = w.Write([]byte(`{"isFriend":true}`))
				default:
					w.WriteHeader(http.StatusCreated)
				}
			}))
			defer srv.Close()
			defer close(release)
			f := &xboxFriends{
				client: social.Client{
					Token:     func(context.Context) (string, error) { return "XBL3.0 x=1;token", nil },
					Endpoints: social.Endpoints{Social: srv.URL, PeopleHub: srv.URL, SessionDirectory: srv.URL},
				},
				requests: make(chan struct{}, 1),
				log:      slog.New(slog.DiscardHandler),
			}
			f.host.Store(&mpsd.Host{})
			done := make(chan struct{})
			go func() {
				f.accept(context.Background())
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("taking a friend request still waits on a stalled service after 5s")
			}
		})
	}
}

// fakeXboxConn is a connection to the signaling service of a test, made
// with a token, which ends once closed or once the test ends it.
type fakeXboxConn struct {
	token string
	creds []signaling.ICEServer
	done  chan struct{}
	once  sync.Once
	err   error
}

func newFakeXboxConn(token string, creds []signaling.ICEServer) *fakeXboxConn {
	return &fakeXboxConn{token: token, creds: creds, done: make(chan struct{})}
}

func (c *fakeXboxConn) ReadSignal() (nethernet.Signal, error) {
	<-c.done
	return nethernet.Signal{}, c.err
}

func (c *fakeXboxConn) WriteSignal(nethernet.Signal) error { return nil }
func (c *fakeXboxConn) Credentials() []signaling.ICEServer { return c.creds }
func (c *fakeXboxConn) Close() error                       { c.end(errors.New("closed")); return nil }

func (c *fakeXboxConn) Err() error {
	select {
	case <-c.done:
		return c.err
	default:
		return nil
	}
}

func (c *fakeXboxConn) end(err error) { c.once.Do(func() { c.err = err; close(c.done) }) }

func (c *fakeXboxConn) closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// fakeXboxListener serves the signaling of a test until it ends, and
// holds a relay that fails when the test closes failed: the next Relay
// makes a new one.
type fakeXboxListener struct {
	relays chan turn.Config
	mu     sync.Mutex
	failed chan struct{}
}

func (l *fakeXboxListener) Serve(s nethernet.Signaling) error {
	for {
		if _, err := s.ReadSignal(); err != nil {
			return err
		}
	}
}

func (l *fakeXboxListener) Relay(ctx context.Context, cfg turn.Config) error {
	l.mu.Lock()
	select {
	case <-l.failed:
		l.failed = make(chan struct{})
	default:
	}
	l.mu.Unlock()
	select {
	case l.relays <- cfg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *fakeXboxListener) RelayDone() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.failed
}

func (l *fakeXboxListener) RelayErr() error {
	select {
	case <-l.RelayDone():
		return errors.New("turn: refresh failed with error 401")
	default:
		return nil
	}
}

// xboxLoopTest runs serveXboxSignaling with a token, a service and timers
// the test drives: the loop asks for a token on asks and waits for the
// reply, hands the connections it makes to dialed, and the timers it sets
// to timers, which fire when the test sends on them.
type xboxLoopTest struct {
	t      *testing.T
	l      *fakeXboxListener
	asks   chan chan *auth.MCToken
	dialed chan *fakeXboxConn
	timers chan xboxTimer
	log    strings.Builder
	cancel context.CancelFunc
	done   chan struct{}
}

type xboxTimer struct {
	d    time.Duration
	fire chan time.Time
}

func runXboxLoop(t *testing.T) *xboxLoopTest {
	x := &xboxLoopTest{
		t:      t,
		l:      &fakeXboxListener{relays: make(chan turn.Config), failed: make(chan struct{})},
		asks:   make(chan chan *auth.MCToken),
		dialed: make(chan *fakeXboxConn),
		timers: make(chan xboxTimer),
		done:   make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	x.cancel = cancel
	dials := 0
	loop := &xboxSignalingLoop{
		l: x.l,
		token: func(ctx context.Context) (*auth.MCToken, error) {
			reply := make(chan *auth.MCToken)
			select {
			case x.asks <- reply:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			select {
			case mc := <-reply:
				if mc == nil {
					return nil, errors.New("token refused")
				}
				return mc, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		dial: func(ctx context.Context, token string) (xboxSignaling, error) {
			dials++
			c := newFakeXboxConn(token, []signaling.ICEServer{{URLs: []string{"turn:127.0.0.1:3478"}, Username: "user " + strconv.Itoa(dials), Credential: "p"}})
			select {
			case x.dialed <- c:
				return c, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		after: func(d time.Duration) <-chan time.Time {
			tm := xboxTimer{d, make(chan time.Time, 1)}
			select {
			case x.timers <- tm:
			case <-ctx.Done():
			}
			return tm.fire
		},
		log:     slog.New(slog.NewTextHandler(&x.log, nil)),
		backoff: 5 * time.Second,
	}
	go func() {
		loop.run(ctx)
		close(x.done)
	}()
	t.Cleanup(x.stop)
	return x
}

// stop ends the loop and waits for it to return.
func (x *xboxLoopTest) stop() {
	x.cancel()
	<-x.done
}

func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("no %s", what)
		panic("unreachable")
	}
}

// token answers the next ask for a token with mc, nil for a refusal.
func (x *xboxLoopTest) token(mc *auth.MCToken) {
	x.t.Helper()
	receive(x.t, x.asks, "ask for a token") <- mc
}

func mcToken(name string) *auth.MCToken {
	return &auth.MCToken{Header: name, ValidUntil: time.Now().Add(time.Hour)}
}

// TestXboxSignalingRenewsTokenFirst renews the token of the signaling
// while its connection still serves, and switches to a new connection
// with the new token only then; if the token cannot be renewed, the
// connection serves until it drops.
func TestXboxSignalingRenewsTokenFirst(t *testing.T) {
	x := runXboxLoop(t)
	x.token(mcToken("token 1"))
	first := receive(t, x.dialed, "connection")
	receive(t, x.l.relays, "relay")
	renew := receive(t, x.timers, "renewal")
	if renew.d < 55*time.Minute || renew.d > 56*time.Minute {
		t.Fatalf("renewal in %v of a token valid for an hour", renew.d)
	}
	renew.fire <- time.Now()
	reply := receive(t, x.asks, "ask for the new token")
	if first.closed() {
		t.Fatal("the connection closed before the new token was fetched")
	}
	reply <- nil
	// The connection drops by itself later.
	first.end(errors.New("dropped"))
	backoff := receive(t, x.timers, "wait to connect again")
	if first.err.Error() != "dropped" {
		t.Fatalf("connection ended with %v once the token could not be renewed", first.err)
	}
	backoff.fire <- time.Now()
	x.token(mcToken("token 2"))
	second := receive(t, x.dialed, "connection")
	receive(t, x.l.relays, "relay")
	receive(t, x.timers, "renewal").fire <- time.Now()
	reply = receive(t, x.asks, "ask for the new token")
	if second.closed() {
		t.Fatal("the connection closed before the new token was fetched")
	}
	reply <- mcToken("token 3")
	third := receive(t, x.dialed, "connection")
	if third.token != "token 3" || !second.closed() {
		t.Fatalf("connected with %q, the old connection closed %v", third.token, second.closed())
	}
}

// TestXboxSignalingBacksOff waits five seconds after a failure to connect
// to the signaling service, twice as long after each next one up to five
// minutes, and five seconds again once it connected.
func TestXboxSignalingBacksOff(t *testing.T) {
	x := runXboxLoop(t)
	for _, want := range []time.Duration{5, 10, 20, 40, 80, 160, 300, 300} {
		x.token(nil)
		backoff := receive(t, x.timers, "wait to connect again")
		if backoff.d != want*time.Second {
			t.Fatalf("waits %v, want %v", backoff.d, want*time.Second)
		}
		backoff.fire <- time.Now()
	}
	x.token(mcToken("token"))
	receive(t, x.dialed, "connection").end(errors.New("dropped"))
	receive(t, x.l.relays, "relay")
	receive(t, x.timers, "renewal")
	if backoff := receive(t, x.timers, "wait to connect again"); backoff.d != 5*time.Second {
		t.Fatalf("waits %v once connected, want 5s", backoff.d)
	}
}

// TestXboxSignalingReconnectsOnRelayFailure connects to the signaling
// service again once the allocation of the relay failed, which brings
// fresh TURN credentials to make a new one with, and tells why.
func TestXboxSignalingReconnectsOnRelayFailure(t *testing.T) {
	x := runXboxLoop(t)
	x.token(mcToken("token"))
	first := receive(t, x.dialed, "connection")
	if cfg := receive(t, x.l.relays, "relay"); cfg.Username != "user 1" {
		t.Fatalf("relay with %+v", cfg)
	}
	receive(t, x.timers, "renewal")
	close(x.l.failed)
	backoff := receive(t, x.timers, "wait to connect again once the relay failed")
	if backoff.d != 5*time.Second || !first.closed() {
		t.Fatalf("waits %v, the connection closed %v", backoff.d, first.closed())
	}
	backoff.fire <- time.Now()
	x.token(mcToken("token"))
	receive(t, x.dialed, "connection")
	if cfg := receive(t, x.l.relays, "relay"); cfg.Username != "user 2" {
		t.Fatalf("relay with %+v, want the fresh credentials", cfg)
	}
	receive(t, x.timers, "renewal")
	x.stop()
	if !strings.Contains(x.log.String(), "refresh failed with error 401") {
		t.Fatalf("the failure of the relay not told: %s", x.log.String())
	}
}
