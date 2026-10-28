package nethernet

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dm-vev/adamant/net/dtls"
	"github.com/dm-vev/adamant/net/stun"
)

// Error codes of the signaling, as the game numbers them.
const (
	codeFailedToCreateAnswer         = 11
	codeFailedToSetRemoteDescription = 13
	codeIdentityNotAllowed           = 37
)

const (
	// defaultMaxPending bounds the connections that did not open yet.
	defaultMaxPending = 256
	// acceptQueue bounds the connections opened and not accepted yet.
	acceptQueue = 256
	// identityRefresh is how long a server identity token is handed out:
	// BDS issues tokens valid for a minute.
	identityRefresh = 30 * time.Second
	// maxSockets bounds the sockets sharing the port by default.
	maxSockets = 8
)

var (
	errBusy        = errors.New("nethernet: too many connections opening")
	errAcceptQueue = errors.New("nethernet: connections are not accepted fast enough")
	errAnonymous   = errors.New("nethernet: offer carries no identity")
)

// ListenConfig configures a Listener.
type ListenConfig struct {
	// Key is the P-384 key of the server identity. Clients that join over
	// plain HTTP trust it on first use, so a server keeps it across
	// restarts. A new key is made if nil.
	Key *ecdsa.PrivateKey
	// Certificate is the DTLS certificate of the connections. A new one is
	// made if empty.
	Certificate dtls.Certificate
	// Addresses are announced to clients besides the address a client
	// reached the server at: the public address of a server behind NAT,
	// for one.
	Addresses []netip.Addr
	// Sockets is how many UDP sockets share the port. Zero means one per
	// CPU, up to 8, where the kernel spreads the datagrams of different
	// peers over sockets; one elsewhere.
	Sockets int
	// AllowAnonymous accepts clients whose offer carries no identity.
	AllowAnonymous bool
	// VerifyToken checks the identity token of a client and returns the
	// key it names. Nil reads the cpk claim of the token without checking
	// who issued it: the Login of the client must carry the same key.
	VerifyToken func(token string) (*ecdsa.PublicKey, error)
	// MaxPending bounds the connections that did not open yet. Zero means
	// 256.
	MaxPending int
	// SendBuffer bounds the bytes queued on a connection. Zero means 16
	// MiB.
	SendBuffer int
	// TLSConfig makes the signaling HTTPS.
	TLSConfig *tls.Config
}

// Status is what a server tells clients that look it up before joining.
type Status struct {
	ServerName     string `json:"name"`
	Protocol       int    `json:"protocol"`
	Version        string `json:"version"`
	LevelName      string `json:"level"`
	PlayerCount    int    `json:"players"`
	MaxPlayerCount int    `json:"maxPlayers"`
	GameType       int    `json:"gameType"`
}

// Listener accepts NetherNet connections. It answers the HTTP signaling of
// clients on a TCP port and carries every connection over UDP sockets on
// the same port number, a goroutine reading each socket. A connection is
// found by the ICE username of its checks and then by the address they
// proved.
type Listener struct {
	cfg        ListenConfig
	maxPending int
	key        *ecdsa.PrivateKey
	cert       dtls.Certificate
	fps        []Fingerprint
	jws        string
	http       *http.Server
	socks      []*net.UDPConn
	addr       *net.UDPAddr
	port       uint16
	bound      netip.Addr
	ipv4       bool
	nextSock   atomic.Uint32

	ufragMu sync.RWMutex
	ufrags  map[string]*Conn
	addrs   addrTable

	mu      sync.Mutex
	conns   map[*Conn]struct{}
	pending int
	closed  bool

	accept    chan *Conn
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	status  atomic.Pointer[[]byte]
	identMu sync.Mutex
	ident   string
	identAt time.Time
}

// Listen listens for NetherNet clients on address, a host and port taken
// on TCP for the signaling and on UDP for the connections.
func (cfg ListenConfig) Listen(address string) (*Listener, error) {
	l := &Listener{
		cfg:        cfg,
		maxPending: cfg.MaxPending,
		key:        cfg.Key,
		cert:       cfg.Certificate,
		ufrags:     make(map[string]*Conn),
		conns:      make(map[*Conn]struct{}),
		accept:     make(chan *Conn, acceptQueue),
		done:       make(chan struct{}),
	}
	if l.maxPending == 0 {
		l.maxPending = defaultMaxPending
	}
	var err error
	if l.key == nil {
		if l.key, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader); err != nil {
			return nil, err
		}
	}
	if len(l.cert.Chain) == 0 {
		if l.cert, err = dtls.GenerateCertificate(); err != nil {
			return nil, err
		}
	}
	// Every connection shares the certificate, so its assertion is signed
	// once.
	l.fps = []Fingerprint{{Algorithm: "sha-256", Digest: dtls.Fingerprint(l.cert.Chain[0])}}
	if l.jws, err = signFingerprints(l.key, l.fps); err != nil {
		return nil, err
	}

	tcp, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		_ = tcp.Close()
		return nil, err
	}
	sockets := cfg.Sockets
	if sockets == 0 {
		sockets = min(runtime.GOMAXPROCS(0), maxSockets)
	}
	port := strconv.Itoa(tcp.Addr().(*net.TCPAddr).Port)
	if l.socks, err = listenUDP(net.JoinHostPort(host, port), sockets); err != nil {
		_ = tcp.Close()
		return nil, err
	}
	l.addr = l.socks[0].LocalAddr().(*net.UDPAddr)
	local := l.addr.AddrPort()
	l.bound, l.port = local.Addr().Unmap(), local.Port()
	l.ipv4 = l.bound.Is4()

	if cfg.TLSConfig != nil {
		tcp = tls.NewListener(tcp, cfg.TLSConfig)
	}
	l.http = &http.Server{
		Handler:           l,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	l.wg.Add(1 + len(l.socks))
	go func() {
		defer l.wg.Done()
		_ = l.http.Serve(tcp)
	}()
	for _, s := range l.socks {
		go l.read(s)
	}
	return l, nil
}

// Accept waits for the next connection whose channels opened.
func (l *Listener) Accept() (net.Conn, error) {
	c, err := l.AcceptConn()
	if err != nil {
		return nil, err
	}
	return c, nil
}

// AcceptConn is Accept returning the connection as a *Conn.
func (l *Listener) AcceptConn() (*Conn, error) {
	select {
	case c := <-l.accept:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

// Addr returns the UDP address the connections go over.
func (l *Listener) Addr() net.Addr { return l.addr }

// PublicKey returns the public key of the server identity.
func (l *Listener) PublicKey() *ecdsa.PublicKey { return &l.key.PublicKey }

// SetStatus sets the status the server tells clients that look it up.
func (l *Listener) SetStatus(s Status) {
	b, err := json.Marshal(s)
	if err == nil {
		l.status.Store(&b)
	}
}

// Close stops the signaling and closes every connection of the listener.
func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		conns := make([]*Conn, 0, len(l.conns))
		for c := range l.conns {
			conns = append(conns, c)
		}
		l.mu.Unlock()
		close(l.done)
		_ = l.http.Close()
		for _, c := range conns {
			c.abort(net.ErrClosed)
		}
		for _, s := range l.socks {
			_ = s.Close()
		}
		l.wg.Wait()
	})
	return nil
}

// ServeHTTP answers the signaling: GET /v1/join returns the status and
// POST /v1/join/{networkID} takes an SDP offer and returns the answer.
func (l *Listener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Connection", "close")
	switch {
	case r.URL.Path == "/v1/join" && r.Method == http.MethodGet:
		l.serveStatus(w)
	case strings.HasPrefix(r.URL.Path, "/v1/join/") && r.Method == http.MethodPost:
		l.serveOffer(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (l *Listener) serveStatus(w http.ResponseWriter) {
	b := l.status.Load()
	if b == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(*b)
}

func (l *Listener) serveOffer(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxDescription+1))
	switch {
	case err != nil || len(body) == 0:
		http.Error(w, "missing SDP offer", http.StatusBadRequest)
		return
	case len(body) > maxDescription:
		http.Error(w, "SDP offer too large", http.StatusRequestEntityTooLarge)
		return
	}
	now := time.Now()
	offer, err := parseDescription(string(body))
	if err != nil {
		signalError(w, codeFailedToSetRemoteDescription)
		return
	}
	pub, err := l.verifyIdentity(offer, now)
	if err != nil {
		signalError(w, codeIdentityNotAllowed)
		return
	}
	answer, err := l.open(offer, pub, l.candidates(r), now)
	switch {
	case errors.Is(err, errBusy) || errors.Is(err, net.ErrClosed):
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	case err != nil:
		signalError(w, codeFailedToCreateAnswer)
		return
	}
	w.Header().Set("Content-Type", "application/sdp")
	_, _ = io.WriteString(w, answer)
}

// signalError answers an offer with an error code, as BDS does.
func signalError(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = io.WriteString(w, strconv.Itoa(code))
}

// verifyIdentity returns the key the identity of an offer proves: its
// token names the key, which signed the fingerprints of the offer.
func (l *Listener) verifyIdentity(d *description, now time.Time) (*ecdsa.PublicKey, error) {
	if d.identity == "" {
		if l.cfg.AllowAnonymous {
			return nil, nil
		}
		return nil, errAnonymous
	}
	token, jws, err := decodeIdentity(d.identity)
	if err != nil {
		return nil, err
	}
	var pub *ecdsa.PublicKey
	if l.cfg.VerifyToken != nil {
		pub, err = l.cfg.VerifyToken(token)
	} else {
		pub, err = tokenKey(token, now)
	}
	if err != nil {
		return nil, err
	}
	if err := verifyFingerprints(pub, jws, d.fingerprints); err != nil {
		return nil, err
	}
	return pub, nil
}

// identity returns the identity attribute of the answers: a token issued
// for a minute, as BDS issues them, handed out for half of it.
func (l *Listener) identity(now time.Time) (string, error) {
	l.identMu.Lock()
	defer l.identMu.Unlock()
	if l.ident != "" && now.Sub(l.identAt) >= 0 && now.Sub(l.identAt) < identityRefresh {
		return l.ident, nil
	}
	token, err := serverToken(l.key, now)
	if err != nil {
		return "", err
	}
	if l.ident, err = encodeIdentity("self", token, l.jws); err != nil {
		return "", err
	}
	l.identAt = now
	return l.ident, nil
}

// open makes the connection an offer asks for and returns the answer. The
// server takes the DTLS client role unless the offer takes it, as BDS
// does.
func (l *Listener) open(offer *description, pub *ecdsa.PublicKey, candidates []netip.AddrPort, now time.Time) (string, error) {
	identity, err := l.identity(now)
	if err != nil {
		return "", err
	}
	setup := SetupActive
	if offer.setup == SetupActive {
		setup = SetupPassive
	}
	local := &description{
		ufrag:          randomString(8),
		pwd:            randomString(24),
		fingerprints:   l.fps,
		setup:          setup,
		maxMessageSize: maxMessageSize,
		sctpPort:       5000,
		identity:       identity,
		candidates:     candidates,
	}
	sock := l.socks[int(l.nextSock.Add(1))%len(l.socks)]
	c := newConn(connConfig{
		local:       local,
		remote:      offer,
		dtlsClient:  setup == SetupActive,
		certificate: l.cert,
		sendBuffer:  l.cfg.SendBuffer,
		out:         func(b []byte, to netip.AddrPort) { _, _ = sock.WriteToUDPAddrPort(b, to) },
		outBatch:    batchWriter(sock),
		localAddr:   l.addr,
	}, now)
	c.publicKey = pub
	c.onReady = l.ready
	c.onClose = l.remove

	l.mu.Lock()
	switch {
	case l.closed:
		err = net.ErrClosed
	case l.pending >= l.maxPending:
		err = errBusy
	default:
		l.ufragMu.Lock()
		if _, taken := l.ufrags[local.ufrag]; taken {
			err = errBusy
		} else {
			l.ufrags[local.ufrag] = c
		}
		l.ufragMu.Unlock()
	}
	if err == nil {
		c.ufrag = local.ufrag
		c.pendingSlot = true
		l.pending++
		l.conns[c] = struct{}{}
	}
	l.mu.Unlock()
	if err != nil {
		c.abort(err)
		return "", err
	}
	c.mu.Lock()
	c.schedule()
	c.release()
	return encodeDescription(local), nil
}

// candidates returns the addresses announced to the client of a request:
// the address the sockets are bound to or the one the client reached, the
// address the client asked for, which holds the public address of a server
// behind NAT, and the configured ones.
func (l *Listener) candidates(r *http.Request) []netip.AddrPort {
	var out []netip.AddrPort
	add := func(a netip.Addr, port uint16) {
		a = a.Unmap()
		if !a.IsValid() || a.IsUnspecified() || a.Zone() != "" || l.ipv4 && !a.Is4() {
			return
		}
		if ap := netip.AddrPortFrom(a, port); !slices.Contains(out, ap) {
			out = append(out, ap)
		}
	}
	if !l.bound.IsUnspecified() {
		add(l.bound, l.port)
	} else if la, ok := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr); ok {
		add(la.AddrPort().Addr(), l.port)
	}
	if host, port, err := net.SplitHostPort(r.Host); err == nil {
		if a, err := netip.ParseAddr(host); err == nil {
			// A port forwarded for TCP is forwarded for UDP alike.
			if p, err := strconv.ParseUint(port, 10, 16); err == nil && p != 0 {
				add(a, uint16(p))
			}
		}
	} else if a, err := netip.ParseAddr(strings.Trim(r.Host, "[]")); err == nil {
		add(a, l.port)
	}
	for _, a := range l.cfg.Addresses {
		add(a, l.port)
	}
	if len(out) == 0 {
		addrs, _ := net.InterfaceAddrs()
		for _, ia := range addrs {
			if n, ok := ia.(*net.IPNet); ok {
				if a, ok := netip.AddrFromSlice(n.IP); ok && !a.IsLinkLocalUnicast() {
					add(a, l.port)
				}
			}
		}
	}
	return out
}

// ready queues a connection whose channels opened for Accept. It is called
// with the connection locked.
func (l *Listener) ready(c *Conn) {
	l.mu.Lock()
	if c.pendingSlot {
		c.pendingSlot = false
		l.pending--
	}
	closed := l.closed
	l.mu.Unlock()
	if closed {
		c.fail(net.ErrClosed)
		return
	}
	select {
	case l.accept <- c:
	default:
		c.fail(errAcceptQueue)
	}
}

// remove forgets a connection whose transport ended. It is called with the
// connection locked.
func (l *Listener) remove(c *Conn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.conns[c]; !ok {
		return
	}
	delete(l.conns, c)
	if c.pendingSlot {
		c.pendingSlot = false
		l.pending--
	}
	l.ufragMu.Lock()
	if l.ufrags[c.ufrag] == c {
		delete(l.ufrags, c.ufrag)
	}
	l.ufragMu.Unlock()
	for _, a := range c.addrs {
		l.addrs.remove(a, c)
	}
	c.addrs = nil
}

// bind routes the datagrams from an address the peer proved to its
// connection.
func (l *Listener) bind(a netip.AddrPort, c *Conn) {
	if l.addrs.get(a) == c {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.conns[c]; !ok || len(c.addrs) >= maxPairs {
		return
	}
	c.addrs = append(c.addrs, a)
	l.addrs.set(a, c)
}

// maxPairs bounds the addresses of one connection, as the ICE agent does.
const maxPairs = 8

func (l *Listener) read(sock *net.UDPConn) {
	defer l.wg.Done()
	buf := make([]byte, 1<<16)
	var msg stun.Message
	for {
		n, from, err := sock.ReadFromUDPAddrPort(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		l.dispatch(buf[:n], netip.AddrPortFrom(from.Addr().Unmap(), from.Port()), &msg)
	}
}

// dispatch hands a datagram to its connection: a check by the ICE username
// it carries, anything else by the address it came from.
func (l *Listener) dispatch(b []byte, from netip.AddrPort, msg *stun.Message) {
	if len(b) >= 20 && b[0] < 2 && stun.Parse(b, msg) == nil && msg.Type == stun.BindingRequest {
		user, _ := msg.Get(stun.AttrUsername)
		local, _, _ := bytes.Cut(user, []byte{':'})
		l.ufragMu.RLock()
		c := l.ufrags[string(local)]
		l.ufragMu.RUnlock()
		if c != nil && c.handleDatagram(b, from, time.Now()) {
			l.bind(from, c)
		}
		return
	}
	if c := l.addrs.get(from); c != nil {
		c.handleDatagram(b, from, time.Now())
	}
}

// addrTable maps the addresses peers proved to their connections, in
// stripes so that the goroutines of the sockets rarely wait on each other.
type addrTable struct {
	stripes [16]struct {
		sync.RWMutex
		m map[netip.AddrPort]*Conn
	}
}

func (t *addrTable) stripe(a netip.AddrPort) int {
	b := a.Addr().As16()
	h := uint32(2166136261)
	for _, x := range b[8:] {
		h = (h ^ uint32(x)) * 16777619
	}
	h = (h ^ uint32(a.Port())) * 16777619
	return int(h % uint32(len(t.stripes)))
}

func (t *addrTable) get(a netip.AddrPort) *Conn {
	s := &t.stripes[t.stripe(a)]
	s.RLock()
	defer s.RUnlock()
	return s.m[a]
}

func (t *addrTable) set(a netip.AddrPort, c *Conn) {
	s := &t.stripes[t.stripe(a)]
	s.Lock()
	defer s.Unlock()
	if s.m == nil {
		s.m = make(map[netip.AddrPort]*Conn)
	}
	s.m[a] = c
}

// remove forgets an address unless another connection took it since.
func (t *addrTable) remove(a netip.AddrPort, c *Conn) {
	s := &t.stripes[t.stripe(a)]
	s.Lock()
	defer s.Unlock()
	if s.m[a] == c {
		delete(s.m, a)
	}
}
