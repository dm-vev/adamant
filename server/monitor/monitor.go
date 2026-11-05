// Package monitor serves what a program needs to watch a server: its metrics,
// the probes of its health and the Go profiles of the process, over HTTP. The
// [Monitoring] table of config.toml is UserConfig, and Service is the monitor
// as a service of a server (server.Config.Services), which a program that
// embeds the server adds to its Config, as it does the remote admin API.
package monitor

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/config"
	"github.com/dm-vev/adamant/net/metrics"
)

// UserConfig is the [Monitoring] table of config.toml: what the process
// serves over HTTP to be watched.
type UserConfig struct {
	Monitoring struct {
		// Address serves metrics, health and profiles over HTTP, as
		// host:port; "" serves nothing.
		Address string
		// Health serves /livez, /readyz and /healthz, which never need the
		// Token.
		Health bool
		// LiveTimeout is how long a tick of a world may take before /livez
		// fails, a duration above a minute; "" is 2 minutes.
		LiveTimeout string
		// Metrics serves /metrics, in the text format of Prometheus.
		Metrics bool
		// Pprof serves the Go profiles under /debug/pprof/.
		Pprof bool
		// ReadyTickAge is how long ago the last tick of a world may have
		// ended for /readyz to pass, a second or more; "" is 5 seconds.
		ReadyTickAge string
		// Token, if set, is the bearer token /metrics and /debug/pprof/
		// need.
		Token string
	}
}

// stuckTickLogged is how long a tick runs before the stacks of the process
// are logged: LiveTimeout must be above it, so that the stacks are in the
// log before the process is killed.
const stuckTickLogged = time.Minute

// DefaultConfig returns the [Monitoring] table of the default file:
// metrics, health and profiles on localhost:6060, as the program served
// them before it had the table.
func DefaultConfig() UserConfig {
	var c UserConfig
	m := &c.Monitoring
	m.Address, m.Health, m.Metrics, m.Pprof = "localhost:6060", true, true, true
	m.LiveTimeout, m.ReadyTickAge = "2m", "5s"
	return c
}

// Check adds the problems of the [Monitoring] table to p.
func (c UserConfig) Check(p *config.Problems) {
	m := c.Monitoring
	if config.Address(p, "Monitoring.Address", "tcp", m.Address) && m.Address != "" && m.Pprof && m.Token == "" && !loopback(m.Address) {
		p.Errorf("Monitoring.Address", "Monitoring.Pprof on %s needs Monitoring.Token: profiles show the memory and the command line of the process", m.Address)
	}
	if d, ok := config.Duration(p, "Monitoring.LiveTimeout", m.LiveTimeout); ok && strings.TrimSpace(m.LiveTimeout) != "" && d <= stuckTickLogged {
		p.Errorf("Monitoring.LiveTimeout", "Monitoring.LiveTimeout = %v: want more than %v, when the stacks of a stuck tick are logged", d, stuckTickLogged)
	}
	if d, ok := config.Duration(p, "Monitoring.ReadyTickAge", m.ReadyTickAge); ok && strings.TrimSpace(m.ReadyTickAge) != "" {
		config.AtLeast(p, "Monitoring.ReadyTickAge", d, time.Second)
	}
}

// Health returns the bounds of the probes of the table: its LiveTimeout and
// ReadyTickAge, 0 where they are empty, which the server takes as its
// defaults.
func (c UserConfig) Health() server.HealthOptions {
	var o server.HealthOptions
	o.LiveTimeout, _ = time.ParseDuration(strings.TrimSpace(c.Monitoring.LiveTimeout))
	o.ReadyTickAge, _ = time.ParseDuration(strings.TrimSpace(c.Monitoring.ReadyTickAge))
	return o
}

// loopback reports whether the host of addr is of this machine alone:
// localhost, or an address of 127.0.0.0/8 or ::1. An empty host binds every
// interface, and is not.
func loopback(addr string) bool {
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

// Options are what the program adds to the table to serve it.
type Options struct {
	// Log is where the monitor logs; slog.Default() if nil.
	Log *slog.Logger
}

// Service is the monitor as a service of a server (server.Config.Services):
// the metrics of the process and of the server, the probes of its health and
// the profiles, as the table says. The server starts it as it starts, and
// closes it, freeing its port, as it closes.
type Service struct {
	conf UserConfig
	log  *slog.Logger
	// srv is the server once it was made: until then, /readyz fails as
	// starting and /livez passes.
	srv atomic.Pointer[server.Server]

	mu sync.Mutex
	// tried is whether Serve ran since the last Close, so that Start does
	// not log a failure to listen twice.
	tried bool
	// hs is the HTTP server of the monitor once it serves, and done is
	// closed once the goroutine that serves it ended.
	hs   *http.Server
	ln   net.Listener
	done chan struct{}
}

// Service returns the service that serves the monitor as the table says, to
// pass in server.Config.Services. Check must have passed: a table it refuses
// may serve what it would not.
func (c UserConfig) Service(o Options) *Service {
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{conf: c, log: log}
}

// setServer makes the server the one the monitor serves the metrics and the
// health of.
func (s *Service) setServer(srv *server.Server) { s.srv.Store(srv) }

// Start makes srv the server the monitor serves and, if it does not serve
// yet, serves. A monitor that cannot listen is logged, and Start returns
// nil: it does not stop a game.
//
// A program that wants /livez to answer while the worlds load calls Serve
// before it makes the server.
func (s *Service) Start(srv *server.Server) error {
	s.setServer(srv)
	s.mu.Lock()
	tried := s.tried
	s.mu.Unlock()
	if !tried {
		s.Serve()
	}
	return nil
}

var _ server.Service = (*Service)(nil)

// handler returns the handler of the paths the table turns on, and the
// paths.
func (s *Service) handler() (http.Handler, []string) {
	c := s.conf.Monitoring
	mux := http.NewServeMux()
	var paths []string
	if c.Metrics {
		mux.Handle("GET /metrics", s.auth(http.HandlerFunc(s.metrics)))
		paths = append(paths, "/metrics")
	}
	if c.Health {
		mux.HandleFunc("GET /livez", s.live)
		mux.HandleFunc("GET /readyz", s.ready)
		mux.HandleFunc("GET /healthz", s.live)
		paths = append(paths, "/livez", "/readyz", "/healthz")
	}
	if c.Pprof {
		mux.Handle("/debug/pprof/", s.auth(http.HandlerFunc(pprof.Index)))
		mux.Handle("/debug/pprof/cmdline", s.auth(http.HandlerFunc(pprof.Cmdline)))
		mux.Handle("/debug/pprof/profile", s.auth(http.HandlerFunc(pprof.Profile)))
		mux.Handle("/debug/pprof/symbol", s.auth(http.HandlerFunc(pprof.Symbol)))
		mux.Handle("/debug/pprof/trace", s.auth(http.HandlerFunc(pprof.Trace)))
		paths = append(paths, "/debug/pprof/")
	}
	return mux, paths
}

// Serve listens on the address of the table and serves its paths in the
// background, and returns the address it listens on, nil if none. A
// failure to listen is logged, and the server runs without: a monitor that
// cannot bind does not stop a game. It does nothing, and returns the
// address, if the monitor serves already.
func (s *Service) Serve() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tried = true
	if s.hs != nil {
		return s.ln.Addr()
	}
	c := s.conf.Monitoring
	h, paths := s.handler()
	if c.Address == "" || len(paths) == 0 {
		return nil
	}
	ln, err := net.Listen("tcp", c.Address)
	if err != nil {
		s.log.Error("Monitoring not served: "+err.Error(), "addr", c.Address)
		return nil
	}
	if c.Metrics && c.Token == "" && !loopback(c.Address) {
		s.log.Warn("Metrics are served to the network without a token: set Monitoring.Token.", "addr", c.Address)
	}
	s.log.Info("Monitoring served.", "addr", ln.Addr().String(), "paths", strings.Join(paths, " "))
	hs := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		// A CPU profile takes 30 seconds by default.
		WriteTimeout: time.Minute,
		ErrorLog:     slog.NewLogLogger(s.log.Handler(), slog.LevelDebug),
	}
	done := make(chan struct{})
	s.hs, s.ln, s.done = hs, ln, done
	go func() {
		defer close(done)
		_ = hs.Serve(ln)
	}()
	return ln.Addr()
}

// Addr returns the address the monitor listens on, nil while it does not.
func (s *Service) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hs == nil {
		return nil
	}
	return s.ln.Addr()
}

// Close stops serving, as the server it serves closes, and returns once
// the port is free and the goroutine that served ended. The service may be
// started again, for another server.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tried = false
	s.srv.Store(nil)
	if s.hs == nil {
		return nil
	}
	err := s.hs.Close()
	<-s.done
	s.hs, s.ln = nil, nil
	return err
}

// auth returns h behind the token of the table, if it has one: a request
// without "Authorization: Bearer <token>" is answered 401.
func (s *Service) auth(h http.Handler) http.Handler {
	token := s.conf.Monitoring.Token
	if token == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="monitoring"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// metrics serves the metrics of the network stack and of the server.
func (s *Service) metrics(w http.ResponseWriter, r *http.Request) {
	regs := []*metrics.Registry{metrics.Default}
	if srv := s.srv.Load(); srv != nil {
		regs = append(regs, srv.Metrics())
	}
	metrics.Handler(regs...).ServeHTTP(w, r)
}

// live serves /livez.
func (s *Service) live(w http.ResponseWriter, r *http.Request) {
	report := server.HealthReport{OK: true}
	if srv := s.srv.Load(); srv != nil {
		report = srv.Live(s.conf.Health())
	}
	writeReport(w, r, report)
}

// ready serves /readyz.
func (s *Service) ready(w http.ResponseWriter, r *http.Request) {
	report := server.HealthReport{Checks: []server.HealthCheck{{Name: "server/listening", Detail: "starting"}}}
	if srv := s.srv.Load(); srv != nil {
		report = srv.Ready(s.conf.Health())
	}
	writeReport(w, r, report)
}

// writeReport writes a report as plain text: 200 if it passed, 503 with
// its checks otherwise, and its checks always with ?verbose.
func writeReport(w http.ResponseWriter, r *http.Request, report server.HealthReport) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if !report.OK {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_, verbose := r.URL.Query()["verbose"]
	_, _ = w.Write([]byte(report.String(verbose)))
}
