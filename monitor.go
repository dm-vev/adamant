package main

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/config"
	"github.com/dm-vev/adamant/net/metrics"
)

// monitorConfig is the part of config.toml of what the process serves
// over HTTP to be watched: the [Monitoring] table.
type monitorConfig struct {
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

// defaultMonitorConfig returns the [Monitoring] table of the default file:
// metrics, health and profiles on localhost:6060, as the program served
// them before it had the table.
func defaultMonitorConfig() monitorConfig {
	var c monitorConfig
	m := &c.Monitoring
	m.Address, m.Health, m.Metrics, m.Pprof = "localhost:6060", true, true, true
	m.LiveTimeout, m.ReadyTickAge = "2m", "5s"
	return c
}

// Check adds the problems of the [Monitoring] table to p.
func (c monitorConfig) Check(p *config.Problems) {
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

// health returns the bounds of the probes of the table.
func (c monitorConfig) health() server.HealthOptions {
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

// monitor serves what config.toml's [Monitoring] says: the metrics of the
// process and of the server, the probes of its health and its profiles.
type monitor struct {
	conf monitorConfig
	log  *slog.Logger
	// srv is the server once it was made: until then, /readyz fails as
	// starting and /livez passes.
	srv atomic.Pointer[server.Server]
	// hs is the HTTP server of the monitor once it serves.
	hs *http.Server
}

// newMonitor returns the monitor of the table c, logging through log.
func newMonitor(c monitorConfig, log *slog.Logger) *monitor {
	return &monitor{conf: c, log: log}
}

// setServer makes the server the one the monitor serves the metrics and the
// health of.
func (m *monitor) setServer(srv *server.Server) { m.srv.Store(srv) }

// handler returns the handler of the paths the table turns on, and the
// paths.
func (m *monitor) handler() (http.Handler, []string) {
	c := m.conf.Monitoring
	mux := http.NewServeMux()
	var paths []string
	if c.Metrics {
		mux.Handle("GET /metrics", m.auth(http.HandlerFunc(m.metrics)))
		paths = append(paths, "/metrics")
	}
	if c.Health {
		mux.HandleFunc("GET /livez", m.live)
		mux.HandleFunc("GET /readyz", m.ready)
		mux.HandleFunc("GET /healthz", m.live)
		paths = append(paths, "/livez", "/readyz", "/healthz")
	}
	if c.Pprof {
		mux.Handle("/debug/pprof/", m.auth(http.HandlerFunc(pprof.Index)))
		mux.Handle("/debug/pprof/cmdline", m.auth(http.HandlerFunc(pprof.Cmdline)))
		mux.Handle("/debug/pprof/profile", m.auth(http.HandlerFunc(pprof.Profile)))
		mux.Handle("/debug/pprof/symbol", m.auth(http.HandlerFunc(pprof.Symbol)))
		mux.Handle("/debug/pprof/trace", m.auth(http.HandlerFunc(pprof.Trace)))
		paths = append(paths, "/debug/pprof/")
	}
	return mux, paths
}

// serve listens on the address of the table and serves its paths in the
// background, and returns the address it listens on, nil if none. A
// failure to listen is logged, and the server runs without: a monitor that
// cannot bind does not stop a game.
func (m *monitor) serve() net.Addr {
	c := m.conf.Monitoring
	h, paths := m.handler()
	if c.Address == "" || len(paths) == 0 {
		return nil
	}
	ln, err := net.Listen("tcp", c.Address)
	if err != nil {
		m.log.Error("Monitoring not served: "+err.Error(), "addr", c.Address)
		return nil
	}
	if c.Metrics && c.Token == "" && !loopback(c.Address) {
		m.log.Warn("Metrics are served to the network without a token: set Monitoring.Token.", "addr", c.Address)
	}
	m.log.Info("Monitoring served.", "addr", ln.Addr().String(), "paths", strings.Join(paths, " "))
	hs := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		// A CPU profile takes 30 seconds by default.
		WriteTimeout: time.Minute,
		ErrorLog:     slog.NewLogLogger(m.log.Handler(), slog.LevelDebug),
	}
	m.hs = hs
	go func() { _ = hs.Serve(ln) }()
	return ln.Addr()
}

// close stops serving.
func (m *monitor) close() error {
	if m.hs == nil {
		return nil
	}
	return m.hs.Close()
}

// auth returns h behind the token of the table, if it has one: a request
// without "Authorization: Bearer <token>" is answered 401.
func (m *monitor) auth(h http.Handler) http.Handler {
	token := m.conf.Monitoring.Token
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
func (m *monitor) metrics(w http.ResponseWriter, r *http.Request) {
	regs := []*metrics.Registry{metrics.Default}
	if srv := m.srv.Load(); srv != nil {
		regs = append(regs, srv.Metrics())
	}
	metrics.Handler(regs...).ServeHTTP(w, r)
}

// live serves /livez.
func (m *monitor) live(w http.ResponseWriter, r *http.Request) {
	report := server.HealthReport{OK: true}
	if srv := m.srv.Load(); srv != nil {
		report = srv.Live(m.conf.health())
	}
	writeReport(w, r, report)
}

// ready serves /readyz.
func (m *monitor) ready(w http.ResponseWriter, r *http.Request) {
	report := server.HealthReport{Checks: []server.HealthCheck{{Name: "server/listening", Detail: "starting"}}}
	if srv := m.srv.Load(); srv != nil {
		report = srv.Ready(m.conf.health())
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
