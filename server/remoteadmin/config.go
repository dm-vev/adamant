package remoteadmin

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/config"
)

// DefaultAddress is the address the API listens on by default: of this
// machine alone.
const DefaultAddress = "localhost:19140"

// DefaultTokenFile is the file of the tokens by default, beside ops.json.
const DefaultTokenFile = "admin-tokens.txt"

// UserConfig is the [RemoteAdmin] table of config.toml: whether and where
// the remote admin API is served. It is off by default.
type UserConfig struct {
	RemoteAdmin struct {
		// Address is where the API listens, as host:port. On an address
		// other than one of this machine alone (localhost, 127.0.0.0/8,
		// ::1), CertFile and KeyFile are needed, unless AllowPlaintext.
		Address string
		// AllowPlaintext serves without TLS on an address other than one of
		// this machine alone: tokens then cross the network in clear.
		AllowPlaintext bool
		// AllowedHosts are the host names the Host of a request may be,
		// beside localhost, 127.0.0.1, ::1 and the host of Address: other
		// names are refused, as of a page of another site that rebound its
		// name to the server.
		AllowedHosts []string
		// CertFile and KeyFile are the certificate and the key of TLS, both
		// or neither, in PEM.
		CertFile string
		// CommandTimeout bounds how long a request waits for the output of
		// its command, 1s to 5m.
		CommandTimeout string
		// Enabled serves the API.
		Enabled bool
		KeyFile string
		// MaxFailures is how many failed sign-ins an address has a minute
		// before it is refused for a minute.
		MaxFailures int
		// TokenFile holds the tokens, one a line: name, level, token. A
		// relative path is beside ops.json. It is made, mode 0600, with one
		// token of level 4 if missing.
		TokenFile string
		// TrustedProxies are the addresses and networks whose
		// X-Forwarded-For is believed.
		TrustedProxies []string
	}
}

// DefaultConfig returns the [RemoteAdmin] table of the default file: off,
// on localhost:19140 when on.
func DefaultConfig() UserConfig {
	var c UserConfig
	r := &c.RemoteAdmin
	r.Address, r.TokenFile, r.CommandTimeout = DefaultAddress, DefaultTokenFile, DefaultCommandTimeout.String()
	r.MaxFailures, r.AllowedHosts, r.TrustedProxies = DefaultMaxFailures, []string{}, []string{}
	return c
}

// Check adds the problems of the [RemoteAdmin] table to p. Those of TLS
// are checked only if the API is enabled: the certificate is then loaded.
func (c UserConfig) Check(p *config.Problems) {
	r := c.RemoteAdmin
	config.Address(p, "RemoteAdmin.Address", "tcp", r.Address)
	if d, ok := config.Duration(p, "RemoteAdmin.CommandTimeout", r.CommandTimeout); ok && strings.TrimSpace(r.CommandTimeout) != "" {
		config.Range(p, "RemoteAdmin.CommandTimeout", d, time.Second, 5*time.Minute)
	}
	config.AtLeast(p, "RemoteAdmin.MaxFailures", r.MaxFailures, 1)
	if strings.TrimSpace(r.TokenFile) == "" {
		p.Errorf("RemoteAdmin.TokenFile", "RemoteAdmin.TokenFile is empty: want the file of the tokens, such as %q", DefaultTokenFile)
	}
	for _, s := range r.TrustedProxies {
		if _, err := parsePrefix(s); err != nil {
			p.Errorf("RemoteAdmin.TrustedProxies", "RemoteAdmin.TrustedProxies holds %q: want an address or a network, such as \"10.0.0.1\" or \"10.0.0.0/8\"", s)
		}
	}
	if (r.CertFile == "") != (r.KeyFile == "") {
		p.Errorf("RemoteAdmin.CertFile", "RemoteAdmin.CertFile and RemoteAdmin.KeyFile go together: set both, or neither")
		return
	}
	if !r.Enabled {
		return
	}
	if r.CertFile != "" {
		if _, err := tls.LoadX509KeyPair(r.CertFile, r.KeyFile); err != nil {
			p.Errorf("RemoteAdmin.CertFile", "RemoteAdmin.CertFile and KeyFile: %v", err)
		}
	} else if !r.AllowPlaintext && !Loopback(r.Address) {
		p.Errorf("RemoteAdmin.Address", "RemoteAdmin.Address on %s needs CertFile and KeyFile: tokens would cross the network in clear", r.Address)
	}
}

// parsePrefix parses an address or a network: an address alone is a
// network of its bits.
func parsePrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// Loopback reports whether the host of addr is of this machine alone:
// localhost, or an address of 127.0.0.0/8 or ::1. An empty host binds every
// interface, and is not.
func Loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// TokenPath returns the file of the tokens: TokenFile, beside ops.json,
// in the folder dir, if relative.
func (c UserConfig) TokenPath(dir string) string {
	path := strings.TrimSpace(c.RemoteAdmin.TokenFile)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// Listener serves the remote admin API.
type Listener struct {
	hs *http.Server
	ln net.Listener
	// done is closed once the goroutine that serves ended.
	done chan struct{}
}

// Addr returns the address the listener listens on.
func (l *Listener) Addr() net.Addr { return l.ln.Addr() }

// Close stops serving, ending the requests being served, and returns once
// the port is free and the goroutine that accepted connections ended.
func (l *Listener) Close() error {
	err := l.hs.Close()
	<-l.done
	return err
}

// Service is the remote admin API as a service of a server
// (server.Config.Services): the server starts it as it starts, and closes
// it, freeing its port, as it closes.
type Service struct {
	conf UserConfig
	opts ListenOptions

	mu sync.Mutex
	l  *Listener
}

// Service returns the service that serves the API as the table says, to
// pass in server.Config.Services. It serves nothing if the API is not
// enabled. Check must have passed, as for Listen.
func (c UserConfig) Service(o ListenOptions) *Service {
	return &Service{conf: c, opts: o}
}

// Start serves the API of srv, as Listen does.
func (s *Service) Start(srv *server.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.l != nil {
		return errors.New("remote admin: already serving")
	}
	l, err := s.conf.Listen(srv, s.opts)
	s.l = l
	return err
}

// Addr returns the address the service listens on, nil while it does not.
func (s *Service) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.l == nil {
		return nil
	}
	return s.l.Addr()
}

// Close stops serving, as Listener.Close does. The service may be started
// again, for another server.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.l == nil {
		return nil
	}
	err := s.l.Close()
	s.l = nil
	return err
}

var _ server.Service = (*Service)(nil)

// ListenOptions are what the program adds to the table to serve the API.
type ListenOptions struct {
	// Dir is the folder of ops.json, which a relative TokenFile is in.
	Dir string
	// Log is where the listener and the API log; slog.Default() if nil.
	Log *slog.Logger
}

// Listen serves the API of srv as the table says, in the background: it
// makes the file of tokens if missing, logging that it did and never the
// token, then listens on Address, with TLS 1.2 or newer if CertFile is
// set. It returns nil and no error if the API is not enabled. Check must
// have passed: a table it refuses may serve what it would not.
//
// The caller closes the Listener: it outlives srv otherwise. Service
// serves the API as a service of srv instead, which srv closes as it
// closes.
func (c UserConfig) Listen(srv *server.Server, o ListenOptions) (*Listener, error) {
	r := c.RemoteAdmin
	if !r.Enabled {
		return nil, nil
	}
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	path := c.TokenPath(o.Dir)
	if made, err := GenerateTokenFile(path); err != nil {
		return nil, fmt.Errorf("remote admin: %w", err)
	} else if made {
		log.Info("Remote admin token written to "+path+".", "file", path)
	}
	conf := Config{TokenFile: path, MaxFailures: r.MaxFailures, Log: log, Hosts: append([]string(nil), r.AllowedHosts...)}
	conf.CommandTimeout, _ = time.ParseDuration(strings.TrimSpace(r.CommandTimeout))
	if conf.CommandTimeout <= 0 {
		conf.CommandTimeout = DefaultCommandTimeout
	}
	if host, _, err := net.SplitHostPort(r.Address); err == nil && host != "" {
		conf.Hosts = append(conf.Hosts, host)
	}
	for _, s := range r.TrustedProxies {
		if p, err := parsePrefix(s); err == nil {
			conf.TrustedProxies = append(conf.TrustedProxies, p)
		}
	}
	h, err := New(srv, conf)
	if err != nil {
		return nil, fmt.Errorf("remote admin: %w", err)
	}
	hs := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      conf.CommandTimeout + 5*time.Second,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelDebug),
	}
	if r.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(r.CertFile, r.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("remote admin: %w", err)
		}
		hs.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	} else if !Loopback(r.Address) {
		log.Warn("Remote admin is served without TLS, as AllowPlaintext says: its tokens cross the network in clear.", "addr", r.Address)
	}
	ln, err := net.Listen("tcp", r.Address)
	if err != nil {
		return nil, fmt.Errorf("remote admin: %w", err)
	}
	if hs.TLSConfig != nil {
		ln = tls.NewListener(ln, hs.TLSConfig)
	}
	l := &Listener{hs: hs, ln: ln, done: make(chan struct{})}
	go func() {
		defer close(l.done)
		if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("Remote admin stopped serving: "+err.Error(), "addr", r.Address)
		}
	}()
	log.Info("Remote admin served.", "addr", ln.Addr().String(), "tls", hs.TLSConfig != nil)
	return l, nil
}
