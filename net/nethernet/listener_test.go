package nethernet

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/dtls"
	"github.com/dm-vev/adamant/net/stun"
)

func listen(t *testing.T, cfg ListenConfig) *Listener {
	t.Helper()
	l, err := cfg.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func newDialer(t *testing.T) Dialer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return Dialer{Key: key}
}

func dial(t *testing.T, d Dialer, l *Listener) *Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := d.DialContext(ctx, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func accept(t *testing.T, l *Listener) *Conn {
	t.Helper()
	got := make(chan *Conn, 1)
	go func() {
		c, err := l.AcceptConn()
		if err != nil {
			t.Error(err)
		}
		got <- c
	}()
	select {
	case c := <-got:
		if c == nil {
			t.FailNow()
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	case <-time.After(10 * time.Second):
		t.Fatal("no connection accepted")
		return nil
	}
}

func read(t *testing.T, c *Conn) []byte {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	msg, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestListenAndDial(t *testing.T) {
	l := listen(t, ListenConfig{Sockets: 4})
	d := newDialer(t)
	cli := dial(t, d, l)
	srv := accept(t, l)
	if !srv.PublicKey().Equal(&d.Key.PublicKey) {
		t.Fatal("server conn does not carry the key of the client identity")
	}
	if !cli.PublicKey().Equal(l.PublicKey()) {
		t.Fatal("client conn does not carry the key of the server identity")
	}
	for i, size := range []int{1, 1000, 100000, 1 << 20} {
		if err := cli.Send(message(i, size)); err != nil {
			t.Fatal(err)
		}
		if got := read(t, srv); !bytes.Equal(got, message(i, size)) {
			t.Fatalf("server read %d bytes, want %d", len(got), size)
		}
		if err := srv.Send(message(100+i, size)); err != nil {
			t.Fatal(err)
		}
		if got := read(t, cli); !bytes.Equal(got, message(100+i, size)) {
			t.Fatalf("client read %d bytes, want %d", len(got), size)
		}
	}
	if err := cli.Send([]byte("last")); err != nil {
		t.Fatal(err)
	}
	_ = cli.Close()
	if got := read(t, srv); string(got) != "last" {
		t.Fatalf("read %q before the close", got)
	}
	_ = srv.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := srv.ReadMessage(); !errors.Is(err, errPeerClosed) {
		t.Fatalf("read after the client closed: %v", err)
	}
}

// TestManyDials opens connections at once over sockets sharing the port:
// each datagram must reach its own connection.
func TestManyDials(t *testing.T) {
	l := listen(t, ListenConfig{Sockets: 4})
	const n = 40
	d := newDialer(t)
	conns := make([]*Conn, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			c, err := d.DialContext(ctx, l.Addr().String())
			if err != nil {
				t.Error(err)
				return
			}
			conns[i] = c
		})
	}
	go func() {
		for {
			c, err := l.AcceptConn()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				for {
					msg, err := c.ReadMessage()
					if err != nil {
						return
					}
					if c.Send(msg) != nil {
						return
					}
				}
			}()
		}
	}()
	wg.Wait()
	if t.Failed() {
		return
	}
	for i, c := range conns {
		wg.Go(func() {
			defer c.Close()
			want := message(i, 5000+i)
			if err := c.Send(want); err != nil {
				t.Error(err)
				return
			}
			_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
			got, err := c.ReadMessage()
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("conn %d echo: %d bytes, %v", i, len(got), err)
			}
		})
	}
	wg.Wait()
}

func TestStatus(t *testing.T) {
	l := listen(t, ListenConfig{})
	get := func() (string, string) {
		resp, err := http.Get("http://" + l.Addr().String() + "/v1/join")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %s", resp.Status)
		}
		return resp.Header.Get("Content-Type"), string(b)
	}
	if _, body := get(); body != "" {
		t.Fatalf("status before one was set: %q", body)
	}
	want := Status{ServerName: "Adamant", Protocol: 975, Version: "1.26.52", PlayerCount: 3, MaxPlayerCount: 20, LevelName: "world"}
	l.SetStatus(want)
	typ, body := get()
	var got Status
	if err := json.Unmarshal([]byte(body), &got); err != nil || got != want || typ != "application/json" {
		t.Fatalf("status %s %q: %+v, %v", typ, body, got, err)
	}
	if !strings.Contains(body, `"maxPlayers":20`) {
		t.Fatalf("status JSON %s", body)
	}
}

func TestAnonymousOffers(t *testing.T) {
	l := listen(t, ListenConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := Dialer{}.DialContext(ctx, l.Addr().String())
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(codeIdentityNotAllowed)) {
		t.Fatalf("anonymous dial: %v", err)
	}
	open := listen(t, ListenConfig{AllowAnonymous: true})
	cli := dial(t, Dialer{}, open)
	srv := accept(t, open)
	if srv.PublicKey() != nil {
		t.Fatal("anonymous conn has a key")
	}
	if err := cli.Send([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, srv); string(got) != "hi" {
		t.Fatalf("read %q", got)
	}
}

// TestForgedIdentity signs the fingerprint with a key other than the one
// the token names: the offer is refused.
func TestForgedIdentity(t *testing.T) {
	l := listen(t, ListenConfig{})
	d := newDialer(t)
	other := newDialer(t)
	token, err := clientToken(other.Key, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	d.Token = token
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := d.DialContext(ctx, l.Addr().String()); err == nil || !strings.Contains(err.Error(), fmt.Sprint(codeIdentityNotAllowed)) {
		t.Fatalf("dial with a token of another key: %v", err)
	}
}

// TestVouch asks Vouch about the token of each offer whose identity
// verified, and answers the offer whatever it says: one it does not vouch
// for opens all the same, not vouched for. An offer whose identity does
// not verify is refused before Vouch is asked.
func TestVouch(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	signed := ""
	l := listen(t, ListenConfig{Vouch: func(token string) error {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, token)
		if token != signed {
			return errors.New("not signed by the service")
		}
		return nil
	}})
	var tokens []string
	for _, vouched := range []bool{false, true} {
		d := newDialer(t)
		token, err := clientToken(d.Key, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		d.Token = token
		tokens = append(tokens, token)
		if vouched {
			mu.Lock()
			signed = token
			mu.Unlock()
		}
		dial(t, d, l)
		if srv := accept(t, l); srv.vouched != vouched {
			t.Fatalf("connection vouched for: %v, want %v", srv.vouched, vouched)
		}
	}
	forged := newDialer(t)
	forged.Token = tokens[0]
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := forged.DialContext(ctx, l.Addr().String()); err == nil {
		t.Fatal("took a token of another key")
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(asked, tokens) {
		t.Fatalf("Vouch asked about %d tokens, want the %d of the offers whose identity verified", len(asked), len(tokens))
	}
}

// TestPendingBound sends offers that never connect: past MaxPending the
// listener refuses one that ranks below every connection opening, and
// takes offers again once those expired.
func TestPendingBound(t *testing.T) {
	l := listen(t, ListenConfig{MaxPending: 2, AllowAnonymous: true, Vouch: func(string) error { return nil }})
	vouched := identityOffer(t, newDialer(t).Key)
	post := func(offer string) int {
		resp, err := http.Post("http://"+l.Addr().String()+"/v1/join/1", "application/sdp", strings.NewReader(offer))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	for i, step := range []struct {
		offer string
		want  int
	}{{vouched, 200}, {vouched, 200}, {idleOffer(), 503}} {
		if got := post(step.offer); got != step.want {
			t.Fatalf("offer %d: status %d, want %d", i, got, step.want)
		}
	}
	expireAll(l)
	if got := post(idleOffer()); got != 200 {
		t.Fatalf("offer after the pending ones closed: status %d", got)
	}
}

// idleOffer returns an anonymous offer of a client that never connects.
func idleOffer() string {
	cert, _ := dtls.GenerateCertificate()
	return encodeDescription(&description{
		ufrag: "abcd", pwd: randomString(24), setup: SetupActpass, maxMessageSize: maxMessageSize, sctpPort: 5000,
		fingerprints: []Fingerprint{{"sha-256", dtls.Fingerprint(cert.Chain[0])}},
	})
}

// identityOffer returns an offer of a client of key that never connects,
// with the self-signed token of the key.
func identityOffer(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	d, err := parseDescription(idleOffer())
	if err != nil {
		t.Fatal(err)
	}
	token, err := clientToken(key, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	jws, err := signFingerprints(key, d.fingerprints)
	if err != nil {
		t.Fatal(err)
	}
	if d.identity, err = encodeIdentity("", token, jws); err != nil {
		t.Fatal(err)
	}
	return encodeDescription(d)
}

// bindingRequest returns a check of the peer of the end of a connection
// with ufrag, signed with pwd.
func bindingRequest(ufrag, pwd string) []byte {
	b := stun.NewBuilder(nil, stun.BindingRequest, [12]byte{1})
	b.AddString(stun.AttrUsername, ufrag+":abcd")
	b.AddUint64(stun.AttrICEControlling, 1)
	b.AddIntegrity(hmac.New(sha1.New, []byte(pwd)))
	b.AddFingerprint()
	return b.Bytes()
}

// pendingOffer is the connection an offer posted to a listener opened.
type pendingOffer struct {
	l      *Listener
	c      *Conn
	answer *description
}

// postOffer posts an offer to l from addr and returns the status of the
// answer and the connection opened, if any.
func postOffer(t *testing.T, l *Listener, offer, addr string) (int, pendingOffer) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/join/1", strings.NewReader(offer))
	r.RemoteAddr = addr
	w := httptest.NewRecorder()
	l.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		return w.Code, pendingOffer{}
	}
	answer, err := parseDescription(w.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	l.ufragMu.RLock()
	defer l.ufragMu.RUnlock()
	return w.Code, pendingOffer{l, l.ufrags[answer.ufrag], answer}
}

// prove has the peer of the connection prove an address with a check.
func (p pendingOffer) prove(from netip.AddrPort) {
	var msg stun.Message
	p.l.dispatch(bindingRequest(p.answer.ufrag, p.answer.pwd), from, nil, &msg)
}

// TestPendingDisplaced fills the slots of connections opening and sends
// more offers: each displaces the oldest connection of the lowest rank,
// unless that one ranks above it, and is refused then. A connection ranks
// 2 if Vouch vouched for its offer, and 1 more once a check proved an
// address of its peer. A connection displaced closes and gives its slot
// back.
func TestPendingDisplaced(t *testing.T) {
	key := newDialer(t).Key
	l := listen(t, ListenConfig{AllowAnonymous: true, MaxPending: 2, Vouch: func(string) error { return nil }})
	pending := connectionsPending.Load()
	unvouched, vouched := idleOffer(), identityOffer(t, key)
	offers := map[string]pendingOffer{}
	post := func(name, offer string, want int) {
		t.Helper()
		code, p := postOffer(t, l, offer, "127.0.0.1:1000")
		if code != want {
			t.Fatalf("offer %s: status %d, want %d", name, code, want)
		}
		offers[name] = p
	}
	prove := func(name string) {
		offers[name].prove(netip.AddrPortFrom(netip.MustParseAddr("192.0.2.1"), 5000+uint16(name[0])))
	}
	displaced := func(want ...string) {
		t.Helper()
		var got []string
		for name, p := range offers {
			if p.c != nil && errors.Is(p.c.Err(), errDisplaced) {
				got = append(got, name)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("displaced %v, want %v", got, want)
		}
	}
	post("a", unvouched, 200)
	post("b", unvouched, 200)
	post("c", unvouched, 200)
	displaced("a")
	prove("b")
	post("d", unvouched, 200)
	displaced("a", "c")
	post("e", vouched, 200)
	displaced("a", "c", "d")
	post("f", unvouched, 503)
	post("g", vouched, 200)
	displaced("a", "b", "c", "d")
	prove("g")
	post("h", vouched, 200)
	displaced("a", "b", "c", "d", "e")
	prove("h")
	post("i", vouched, 503)
	displaced("a", "b", "c", "d", "e")

	l.mu.Lock()
	slots, conns := l.pending, len(l.conns)
	l.mu.Unlock()
	if slots != 2 || conns != 2 || connectionsPending.Load() != pending+2 {
		t.Fatalf("%d slots taken, %d connections, %d counted pending; want 2 of each", slots, conns, connectionsPending.Load()-pending)
	}
}

// TestPendingDisplacedAfterSourceBound bounds the connections opening of
// a source before it displaces any: an offer of a source at its bound is
// refused as such, even if it would displace one. A connection displaced
// gives back the counts of its sources.
func TestPendingDisplacedAfterSourceBound(t *testing.T) {
	l := listen(t, ListenConfig{AllowAnonymous: true, MaxPending: 1, MaxPendingPerAddress: 1, Vouch: func(string) error { return nil }})
	vouched := identityOffer(t, newDialer(t).Key)
	for i, step := range []struct {
		offer, from string
		want        int
	}{
		{idleOffer(), "203.0.113.5:1000", 200},
		{vouched, "203.0.113.5:1001", 429},
		{vouched, "203.0.113.6:1000", 200},
		{idleOffer(), "203.0.113.5:1002", 503},
	} {
		if got, _ := postOffer(t, l, step.offer, step.from); got != step.want {
			t.Fatalf("offer %d from %s: status %d, want %d", i, step.from, got, step.want)
		}
	}
}

// TestOpenNotDisplaced displaces only connections opening: one open,
// though it ranks below them, stays open.
func TestOpenNotDisplaced(t *testing.T) {
	key := newDialer(t).Key
	token, err := clientToken(key, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	l := listen(t, ListenConfig{AllowAnonymous: true, MaxPending: 1, Vouch: func(tok string) error {
		if tok != token {
			return errors.New("not signed by the service")
		}
		return nil
	}})
	cli := dial(t, newDialer(t), l)
	srv := accept(t, l)
	vouched := identityOffer(t, key)
	for i := range 2 {
		if got, _ := postOffer(t, l, vouched, "127.0.0.1:1000"); got != 200 {
			t.Fatalf("offer %d: status %d", i, got)
		}
	}
	if err := cli.Send([]byte("still open")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, srv); string(got) != "still open" || srv.Err() != nil {
		t.Fatalf("read %q; closed with %v", got, srv.Err())
	}
}

// TestDisplacedAsItOpens has the channels of a connection open just after
// an offer displaced it: it closes, and is not accepted nor counted open.
func TestDisplacedAsItOpens(t *testing.T) {
	l := listen(t, ListenConfig{AllowAnonymous: true})
	open := connectionsOpen.Load()
	_, p := postOffer(t, l, idleOffer(), "127.0.0.1:1000")
	l.mu.Lock()
	l.displace(p.c)
	l.mu.Unlock()
	p.c.mu.Lock()
	l.ready(p.c)
	p.c.release()
	if !errors.Is(p.c.Err(), errDisplaced) || connectionsOpen.Load() != open || len(l.accept) != 0 {
		t.Fatalf("closed with %v, %d counted open, %d to accept", p.c.Err(), connectionsOpen.Load()-open, len(l.accept))
	}
}

// TestPendingFlood keeps every slot of the connections opening taken with
// offers that never connect, each from a network of its own and none
// vouched for, as a botnet would: a client vouched for joins all the
// same, and so does one not vouched for, whose checks prove its address
// before the flood displaces it.
func TestPendingFlood(t *testing.T) {
	vouched := newDialer(t)
	token, err := clientToken(vouched.Key, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	vouched.Token = token
	l := listen(t, ListenConfig{AllowAnonymous: true, Vouch: func(tok string) error {
		if tok != token {
			return errors.New("not signed by the service")
		}
		return nil
	}})
	offer := idleOffer()
	post := func(i int) int {
		code, _ := postOffer(t, l, offer, fmt.Sprintf("10.%d.%d.1:1000", i>>8&255, i&255))
		return code
	}
	for i := range defaultMaxPending {
		if got := post(i); got != 200 {
			t.Fatalf("offer %d of the flood: status %d", i, got)
		}
	}
	// A thousand offers a second go on, each displacing the oldest of the
	// flood: every slot is taken anew within a quarter of a second.
	stop := make(chan struct{})
	var flood sync.WaitGroup
	var taken atomic.Int64
	flood.Go(func() {
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for i := defaultMaxPending; ; i++ {
			select {
			case <-stop:
				return
			case <-tick.C:
			}
			if post(i) == 200 {
				taken.Add(1)
			}
		}
	})
	stopped := false
	stopFlood := func() {
		if !stopped {
			stopped = true
			close(stop)
			flood.Wait()
		}
	}
	t.Cleanup(stopFlood)
	for _, client := range []struct {
		name string
		d    Dialer
	}{{"vouched for", vouched}, {"not vouched for", newDialer(t)}} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		c, err := client.d.DialContext(ctx, l.Addr().String())
		cancel()
		if err != nil {
			t.Fatalf("client %s: %v", client.name, err)
		}
		t.Cleanup(func() { _ = c.Close() })
		accept(t, l)
	}
	stopFlood()
	// The flood holds every slot but those the clients gave back as they
	// opened, each counted against its two sources.
	l.mu.Lock()
	slots, sources := l.pending, len(l.sources)
	l.mu.Unlock()
	if taken.Load() == 0 || slots < defaultMaxPending-2 || sources != 2*slots {
		t.Fatalf("the flood took %d offers while the clients joined and holds %d slots of %d sources; want some, %d at least and twice the slots",
			taken.Load(), slots, sources, defaultMaxPending-2)
	}
}

// expireAll closes the connections of a listener as their open timeout
// does.
func expireAll(l *Listener) {
	l.mu.Lock()
	conns := make([]*Conn, 0, len(l.conns))
	for c := range l.conns {
		conns = append(conns, c)
	}
	l.mu.Unlock()
	for _, c := range conns {
		c.abort(errOpenTimeout)
	}
}

// TestPendingBoundPerSource sends offers that never connect from a few
// sources: past its bound on an address, an IPv6 /64 taken as one, or on
// a network, an IPv4 /24 or an IPv6 /48, a source is refused while others
// are taken, and taken again once its connections expired. Loopback is
// not bounded, and offers over a signaling service count against the
// network ID of their sender.
func TestPendingBoundPerSource(t *testing.T) {
	l := listen(t, ListenConfig{AllowAnonymous: true, MaxPendingPerAddress: 2, MaxPendingPerNetwork: 3})
	pending := connectionsPending.Load()
	offer := idleOffer()
	post := func(from string) int {
		r := httptest.NewRequest(http.MethodPost, "/v1/join/1", strings.NewReader(offer))
		r.RemoteAddr = from
		w := httptest.NewRecorder()
		l.ServeHTTP(w, r)
		return w.Code
	}
	steps := []struct {
		from string
		want int
	}{
		{"203.0.113.5:1000", 200}, {"203.0.113.5:1001", 200}, {"203.0.113.5:1002", 429},
		{"203.0.113.6:1000", 200}, {"203.0.113.7:1000", 429},
		{"198.51.100.1:1000", 200},
		{"[2001:db8:1:2::1]:1000", 200}, {"[2001:db8:1:2::2]:1000", 200}, {"[2001:db8:1:2::3]:1000", 429},
		{"[2001:db8:1:3::1]:1000", 200}, {"[2001:db8:1:4::1]:1000", 429},
		{"127.0.0.1:1000", 200}, {"127.0.0.1:1001", 200}, {"127.0.0.1:1002", 200},
	}
	for i, s := range steps {
		if got := post(s.from); got != s.want {
			t.Fatalf("offer %d from %s: status %d, want %d", i, s.from, got, s.want)
		}
	}
	// One of the two of an address expires: one more is taken, not two.
	if got := post("198.51.100.1:1001"); got != 200 {
		t.Fatalf("second offer of an address: status %d", got)
	}
	l.mu.Lock()
	var first *Conn
	for c := range l.conns {
		if slices.Contains(c.sources, "198.51.100.1/32") {
			first = c
			break
		}
	}
	l.mu.Unlock()
	first.abort(errOpenTimeout)
	for i, want := range []int{200, 429} {
		if got := post("198.51.100.1:1002"); got != want {
			t.Fatalf("offer %d after one of the address expired: status %d, want %d", i, got, want)
		}
	}
	expireAll(l)
	if got := connectionsPending.Load(); got != pending {
		t.Fatalf("%d connections counted pending once all expired, want %d", got, pending)
	}
	l.ufragMu.RLock()
	routed := len(l.ufrags)
	l.ufragMu.RUnlock()
	if routed != 0 {
		t.Fatalf("%d ufrags still route checks once their connections closed", routed)
	}
	if got := post("203.0.113.5:1004"); got != 200 {
		t.Fatalf("offer after the pending ones closed: status %d", got)
	}

	var router signalRouter
	server, client := router.join("1234"), router.join("5678")
	go func() { _ = l.Serve(server) }()
	// A trickled candidate is taken without a reply.
	if err := client.WriteSignal(Signal{Type: SignalCandidate, ConnectionID: 7, Data: "candidate:1 1 udp 1 192.0.2.9 4000 typ host", NetworkID: "1234"}); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{SignalAnswer, SignalAnswer, SignalError} {
		if err := client.WriteSignal(Signal{Type: SignalOffer, ConnectionID: uint64(i), Data: offer, NetworkID: "1234"}); err != nil {
			t.Fatal(err)
		}
		if reply, err := client.ReadSignal(); err != nil || reply.Type != want {
			t.Fatalf("offer %d over signaling: %s, %v; want %s", i, reply.Type, err, want)
		}
	}
}

func TestListenerClose(t *testing.T) {
	l := listen(t, ListenConfig{})
	cli := dial(t, newDialer(t), l)
	accept(t, l)
	_ = l.Close()
	if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("accept after close: %v", err)
	}
	_ = cli.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := cli.ReadMessage(); !errors.Is(err, errPeerClosed) {
		t.Fatalf("client read after the listener closed: %v", err)
	}
}

// TestCandidates announces the address a client reached, the address it
// asked for, which a server behind NAT is known by, and configured ones.
func TestCandidates(t *testing.T) {
	request := func(host string, local string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/join/1", nil)
		r.Host = host
		if local != "" {
			r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, net.TCPAddrFromAddrPort(mustAddr(local))))
		}
		return r
	}
	public := netip.MustParseAddr("198.51.100.7")
	bound := listen(t, ListenConfig{Addresses: []netip.Addr{public}})
	port := bound.port
	got := bound.candidates(request("203.0.113.5:25565", ""))
	want := []netip.AddrPort{
		netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port),
		mustAddr("203.0.113.5:25565"),
		netip.AddrPortFrom(public, port),
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("bound listener announces %v, want %v", got, want)
	}

	all, err := ListenConfig{}.Listen(":0")
	if err != nil {
		t.Fatal(err)
	}
	defer all.Close()
	got = all.candidates(request("[2001:db8::5]:19132", "192.0.2.1:19132"))
	want = []netip.AddrPort{
		netip.AddrPortFrom(netip.MustParseAddr("192.0.2.1"), all.port),
		mustAddr("[2001:db8::5]:19132"),
	}
	if !all.ipv4 && fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("listener on every address announces %v, want %v", got, want)
	}

	// A socket bound to an IPv4 address cannot reach IPv6 peers. (Go
	// listens on 0.0.0.0 with a dual-stack socket.)
	got = bound.candidates(request("[2001:db8::5]:19132", ""))
	if want := []netip.AddrPort{netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port), netip.AddrPortFrom(public, port)}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("IPv4 listener announces %v, want %v", got, want)
	}
}

// TestPanicFailsOneConn panics in a layer above the transport as it takes
// a message, on the goroutine that read the datagram: the connection fails,
// and the listener goes on with others.
func TestPanicFailsOneConn(t *testing.T) {
	l := listen(t, ListenConfig{Sockets: 1})
	cli := dial(t, newDialer(t), l)
	srv := accept(t, l)
	srv.SetReceiver(func([]byte) { panic("a bug of the layer above") })
	if err := cli.Send([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-srv.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the connection whose layer panicked did not close")
	}
	if !strings.Contains(srv.Err().Error(), "panic") {
		t.Fatalf("closed with %v", srv.Err())
	}
	other := dial(t, newDialer(t), l)
	next := accept(t, l)
	if err := other.Send([]byte("still here")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, next); string(got) != "still here" {
		t.Fatalf("read %q", got)
	}
}

// TestDatagram answers a datagram of another protocol on the port of the
// connections, as the query protocol of server lists, and still lets a
// client join beside it.
func TestDatagram(t *testing.T) {
	l := listen(t, ListenConfig{AllowAnonymous: true, Datagram: func(b []byte, from netip.AddrPort, reply func([]byte)) {
		if b[0] == 0xfe {
			reply(append([]byte("answer:"), b...))
		}
		if b[0] == 0xff {
			panic("a broken handler")
		}
	}})
	sock, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(netip.MustParseAddrPort(l.Addr().String())))
	if err != nil {
		t.Fatal(err)
	}
	defer sock.Close()
	_ = sock.SetReadDeadline(time.Now().Add(5 * time.Second))
	for _, b := range [][]byte{{0xff}, {0xfe, 0xfd, 9}} {
		if _, err := sock.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	buf := make([]byte, 64)
	n, err := sock.Read(buf)
	if err != nil || string(buf[:n]) != "answer:\xfe\xfd\x09" {
		t.Fatalf("answer %q, %v", buf[:n], err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Dialer{}.DialContext(ctx, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}

// TestPendingSlotFreedOnce opens a connection and closes it: the slot it
// held pending is given back once, when it opened, not again when it
// closed.
func TestPendingSlotFreedOnce(t *testing.T) {
	l := listen(t, ListenConfig{MaxPending: 1, AllowAnonymous: true, Vouch: func(string) error { return nil }})
	open := connectionsOpen.Load()
	cli := dial(t, newDialer(t), l)
	srv := accept(t, l)
	if got := connectionsOpen.Load(); got != open+1 {
		t.Fatalf("%d connections counted open with one opened, want %d", got, open+1)
	}
	_ = cli.Close()
	srv.abort(net.ErrClosed)
	if got := connectionsOpen.Load(); got != open {
		t.Fatalf("%d connections counted open once it closed, want %d", got, open)
	}
	for i := range l.addrs.stripes {
		st := &l.addrs.stripes[i]
		st.RLock()
		routed := len(st.m)
		st.RUnlock()
		if routed != 0 {
			t.Fatalf("%d addresses still route to closed connections", routed)
		}
	}
	// An offer vouched for takes the one slot; one not vouched for ranks
	// below it.
	for i, step := range []struct {
		offer string
		want  int
	}{{identityOffer(t, newDialer(t).Key), 200}, {idleOffer(), 503}} {
		resp, err := http.Post("http://"+l.Addr().String()+"/v1/join/1", "application/sdp", strings.NewReader(step.offer))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != step.want {
			t.Fatalf("offer %d after a connection opened and closed: status %d, want %d", i, resp.StatusCode, step.want)
		}
	}
}

// TestExtraCandidates covers the candidates of the relay announced beside
// those of the sockets: the address the TURN server saw unless the sockets
// have it, then the relayed one, none without a candidate to base them on.
func TestExtraCandidates(t *testing.T) {
	host := netip.MustParseAddrPort("192.0.2.1:19132")
	reflexive := netip.MustParseAddrPort("198.51.100.1:40000")
	relayed := netip.MustParseAddrPort("203.0.113.1:50000")
	srflx := candidate{addr: reflexive, typ: "srflx", base: host}
	relay := candidate{addr: relayed, typ: "relay", base: reflexive}
	cases := []struct {
		name       string
		candidates []netip.AddrPort
		reflexive  netip.AddrPort
		want       []candidate
	}{
		{"both", []netip.AddrPort{host}, reflexive, []candidate{srflx, relay}},
		{"reflexive announced", []netip.AddrPort{host, reflexive}, reflexive, []candidate{relay}},
		{"no reflexive", []netip.AddrPort{host}, netip.AddrPort{}, []candidate{{addr: relayed, typ: "relay"}}},
		{"no candidates", nil, reflexive, nil},
	}
	for _, c := range cases {
		if got := extraCandidates(c.candidates, c.reflexive, relayed); !slices.Equal(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestOfferBody covers the body of an offer over HTTP: an empty one and
// one past the size of a description are refused as such, one of that
// size is read.
func TestOfferBody(t *testing.T) {
	l := listen(t, ListenConfig{AllowAnonymous: true})
	post := func(body string) int {
		r := httptest.NewRequest(http.MethodPost, "/v1/join/1", strings.NewReader(body))
		w := httptest.NewRecorder()
		l.ServeHTTP(w, r)
		return w.Code
	}
	for _, c := range []struct {
		name string
		body string
		want int
	}{
		{"empty", "", http.StatusBadRequest},
		{"too large", strings.Repeat("a", maxDescription+1), http.StatusRequestEntityTooLarge},
		{"largest", strings.Repeat("a", maxDescription), http.StatusInternalServerError},
	} {
		if got := post(c.body); got != c.want {
			t.Errorf("%s offer: status %d, want %d", c.name, got, c.want)
		}
	}
}

// TestAnswerSetup covers the DTLS role of an answer: the server takes the
// client role unless the offer takes it, as BDS does.
func TestAnswerSetup(t *testing.T) {
	l := listen(t, ListenConfig{AllowAnonymous: true})
	for _, c := range []struct{ offer, want Setup }{{SetupActpass, SetupActive}, {SetupActive, SetupPassive}, {SetupPassive, SetupActive}} {
		d, err := parseDescription(idleOffer())
		if err != nil {
			t.Fatal(err)
		}
		d.setup = c.offer
		answer, _, err := l.answer(encodeDescription(d), nil, nil, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if got, err := parseDescription(answer); err != nil || got.setup != c.want {
			t.Errorf("answer to an offer of %s: %v, %v; want %s", c.offer, got.setup, err, c.want)
		}
	}
}
