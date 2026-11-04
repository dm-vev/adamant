// Package remoteadmin serves the admin commands and the lists of a server
// over HTTP and JSON, to clients that present a bearer token of a level.
//
// The API is versioned under /v1/. POST /v1/command runs a command as the
// console would, at the level of the token, and answers its output, that
// sent after the command returned included. The GET endpoints read the
// status, the players and the lists. Every change goes through
// /v1/command, so that it has the permission check, the audit record and
// the output of a typed command.
//
// Tokens are named, each of a level, in a file of their own (see
// GenerateTokenFile). They are compared in constant time over SHA-256, and
// failed sign-ins are limited per address and in all. This package serves
// a handler; the program binds it to an address, with TLS off loopback.
package remoteadmin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/internal/httpauth"
	"github.com/df-mc/dragonfly/server/world"
)

// Defaults of a Config.
const (
	DefaultMaxFailures    = 5
	DefaultCommandTimeout = 10 * time.Second
	// MaxBody is the largest body of a request.
	MaxBody = 64 << 10
	// MaxInFlight is how many requests are served at once; one more is
	// answered 503 at once.
	MaxInFlight = 8
)

// Config is how a Handler serves.
type Config struct {
	// TokenFile is the file of the tokens, one a line: name, level, token.
	// It is read again when it changes, checked at most every 5 seconds.
	TokenFile string
	// MaxFailures is how many failed sign-ins an address has a minute
	// before it is refused for a minute. Zero or less is
	// DefaultMaxFailures.
	MaxFailures int
	// CommandTimeout bounds how long a request waits for the output of its
	// command. Zero or less is DefaultCommandTimeout.
	CommandTimeout time.Duration
	// Hosts are the host names, with or without a port, requests may name
	// in their Host header beside localhost, 127.0.0.1 and ::1; any other
	// is answered 421, as of a page of another site rebinding its name.
	Hosts []string
	// TrustedProxies are the addresses whose X-Forwarded-For is believed:
	// the client is then the right-most address of it no proxy holds.
	TrustedProxies []netip.Prefix
	// Log is where the handler logs; slog.Default() if nil.
	Log *slog.Logger
}

// Handler serves the remote admin API of a server.
type Handler struct {
	srv      *server.Server
	conf     Config
	limit    *limiter
	inFlight chan struct{}
	now      func() time.Time
	// reloadEvery is how often the file of tokens is checked for a change.
	reloadEvery time.Duration

	tokens  atomic.Pointer[[]token]
	tokenMu sync.Mutex
	checked time.Time
	modTime time.Time
}

// New returns the handler of the API of srv, of the tokens of
// conf.TokenFile, which must exist.
func New(srv *server.Server, conf Config) (*Handler, error) {
	if conf.MaxFailures <= 0 {
		conf.MaxFailures = DefaultMaxFailures
	}
	if conf.CommandTimeout <= 0 {
		conf.CommandTimeout = DefaultCommandTimeout
	}
	if conf.Log == nil {
		conf.Log = slog.Default()
	}
	h := &Handler{srv: srv, conf: conf, limit: newLimiter(conf.MaxFailures), inFlight: make(chan struct{}, MaxInFlight),
		now: time.Now, reloadEvery: 5 * time.Second}
	info, err := os.Stat(conf.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("read the remote admin tokens: %w", err)
	}
	tokens, err := readTokens(conf.TokenFile)
	if err != nil {
		return nil, err
	}
	h.tokens.Store(&tokens)
	h.checked, h.modTime = h.now(), info.ModTime()
	return h, nil
}

// apiError is the body of an answer of an error.
type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// fail answers an error.
func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: msg, Code: code})
}

// writeJSON answers v as JSON.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// route is an endpoint: its method, the level it needs, and what serves
// it.
type route struct {
	method string
	level  cmd.Level
	serve  func(h *Handler, w http.ResponseWriter, r *http.Request, src *source)
}

// routes are the endpoints of the API by path.
var routes = map[string]route{
	"/v1/command":     {http.MethodPost, cmd.LevelAll, (*Handler).serveCommand},
	"/v1/status":      {http.MethodGet, cmd.LevelAll, (*Handler).serveStatus},
	"/v1/players":     {http.MethodGet, cmd.LevelGameMaster, (*Handler).servePlayers},
	"/v1/operators":   {http.MethodGet, cmd.LevelAdmin, listOf(func(s *server.Server) (any, error) { return s.OperatorList() })},
	"/v1/permissions": {http.MethodGet, cmd.LevelAdmin, listOf(func(s *server.Server) (any, error) { return s.RoleEntries() })},
	"/v1/bans":        {http.MethodGet, cmd.LevelAdmin, listOf(func(s *server.Server) (any, error) { return s.BanList(false) })},
	"/v1/ip-bans":     {http.MethodGet, cmd.LevelAdmin, listOf(func(s *server.Server) (any, error) { return s.BanList(true) })},
	"/v1/whitelist":   {http.MethodGet, cmd.LevelAdmin, listOf(func(s *server.Server) (any, error) { return s.WhitelistEntries() })},
}

// ServeHTTP serves a request: at most MaxInFlight at once, of a Host it
// knows, from an address not blocked, with a token of the level of the
// endpoint.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case h.inFlight <- struct{}{}:
		defer func() { <-h.inFlight }()
	default:
		fail(w, http.StatusServiceUnavailable, "busy", "too many requests in flight")
		return
	}
	if !h.knownHost(r.Host) {
		fail(w, http.StatusMisdirectedRequest, "bad_request", "unknown host")
		return
	}
	addr := h.clientAddr(r)
	now := h.now()
	if until, blocked := h.limit.blocked(addr, now); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(until.Sub(now).Seconds())+1))
		fail(w, http.StatusTooManyRequests, "rate_limited", "too many failed sign-ins")
		return
	}
	t, ok := h.authenticate(r, now)
	if !ok {
		if n := h.limit.fail(addr, now); n > 0 {
			h.srv.Audit(audit.Record{Actor: audit.Actor{Kind: audit.KindRemote, Address: addr.String()}, Action: "auth-failed",
				Command: r.Method + " " + r.URL.Path, Result: audit.ResultDenied, Count: n})
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="remote admin"`)
		fail(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is needed")
		return
	}
	rt, ok := routes[r.URL.Path]
	switch {
	case !ok:
		fail(w, http.StatusNotFound, "not_found", "no such endpoint")
		return
	case r.Method != rt.method:
		w.Header().Set("Allow", rt.method)
		fail(w, http.StatusMethodNotAllowed, "bad_request", "use "+rt.method)
		return
	case t.level < rt.level:
		fail(w, http.StatusForbidden, "forbidden", fmt.Sprintf("this endpoint needs a token of level %v", rt.level))
		return
	}
	src := &source{token: t, address: addr.String(), log: h.conf.Log}
	if r.Method == http.MethodGet && rt.level >= cmd.LevelAdmin {
		h.srv.Audit(audit.Record{Actor: src.AuditActor(), Action: "read", Command: r.Method + " " + r.URL.Path, Result: audit.ResultOK})
	}
	rt.serve(h, w, r, src)
}

// authenticate returns the token the request presents, if any matches.
func (h *Handler) authenticate(r *http.Request, now time.Time) (token, bool) {
	presented, ok := httpauth.Bearer(r)
	if !ok {
		return token{}, false
	}
	h.reload(now)
	tokens := *h.tokens.Load()
	hashes := make([]httpauth.Hash, len(tokens))
	for i, t := range tokens {
		hashes[i] = t.hash
	}
	i, ok := httpauth.Match(presented, hashes)
	if !ok {
		return token{}, false
	}
	return tokens[i], true
}

// reload reads the file of tokens again if it changed, checking at most
// every reloadEvery. A file that cannot be read keeps the tokens as they
// were.
func (h *Handler) reload(now time.Time) {
	h.tokenMu.Lock()
	defer h.tokenMu.Unlock()
	// A clock that went back since the last check counts as due.
	if d := now.Sub(h.checked); d >= 0 && d < h.reloadEvery {
		return
	}
	h.checked = now
	info, err := os.Stat(h.conf.TokenFile)
	if err != nil || info.ModTime().Equal(h.modTime) {
		return
	}
	tokens, err := readTokens(h.conf.TokenFile)
	if err != nil {
		h.conf.Log.Warn("Read the remote admin tokens again: "+err.Error()+"; the tokens stay as they were.", "file", h.conf.TokenFile)
		return
	}
	h.modTime = info.ModTime()
	h.tokens.Store(&tokens)
	h.conf.Log.Info("Read the remote admin tokens again.", "file", h.conf.TokenFile, "tokens", len(tokens))
}

// knownHost reports whether the Host of a request is one the API answers.
func (h *Handler) knownHost(host string) bool {
	name := host
	if n, _, err := net.SplitHostPort(host); err == nil {
		name = n
	}
	name = strings.ToLower(strings.Trim(name, "[]"))
	if name == "localhost" || name == "127.0.0.1" || name == "::1" {
		return true
	}
	for _, k := range h.conf.Hosts {
		if n, _, err := net.SplitHostPort(k); err == nil {
			k = n
		}
		if strings.EqualFold(strings.Trim(k, "[]"), name) {
			return true
		}
	}
	return false
}

// clientAddr returns the address of the client of a request: of its peer,
// or, for a peer of TrustedProxies, the right-most address of its
// X-Forwarded-For that no trusted proxy holds.
func (h *Handler) clientAddr(r *http.Request) netip.Addr {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	addr := peer.Addr().Unmap()
	if !h.trusted(addr) {
		return addr
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for _, hop := range slices.Backward(hops) {
		a, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			break
		}
		if a = a.Unmap(); !h.trusted(a) {
			return a
		}
	}
	return addr
}

// trusted reports whether a is of a trusted proxy.
func (h *Handler) trusted(a netip.Addr) bool {
	for _, p := range h.conf.TrustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// commandRequest is the body of POST /v1/command.
type commandRequest struct {
	// Command is the command line, with or without its leading slash.
	Command string `json:"command"`
	// Wait is how long to wait for the output, such as "10s", at most
	// CommandTimeout; CommandTimeout if empty.
	Wait string `json:"wait"`
}

// commandAnswer is the answer of POST /v1/command.
type commandAnswer struct {
	Output []Line `json:"output"`
	// Pending reports that the command said more output follows, which did
	// not come within the wait: it is logged, and recorded, when it comes.
	Pending bool `json:"pending"`
}

// serveCommand runs a command as the console does, in a transaction of the
// default world, at the level of the token, and answers its output.
func (h *Handler) serveCommand(w http.ResponseWriter, r *http.Request, src *source) {
	var req commandRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
	if err := dec.Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "bad_request", fmt.Sprintf("the body is larger than %d bytes", MaxBody))
			return
		}
		fail(w, http.StatusBadRequest, "bad_request", "want {\"command\": \"...\"}: "+err.Error())
		return
	}
	line := strings.TrimSpace(req.Command)
	if line == "" {
		fail(w, http.StatusBadRequest, "bad_request", "no command")
		return
	}
	wait := h.conf.CommandTimeout
	if req.Wait != "" {
		d, err := time.ParseDuration(req.Wait)
		if err != nil || d < 0 {
			fail(w, http.StatusBadRequest, "bad_request", "wait: want a duration such as \"10s\"")
			return
		}
		wait = min(d, h.conf.CommandTimeout)
	}
	if !strings.HasPrefix(line, "/") {
		line = "/" + line
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	select {
	case <-h.srv.World().Exec(func(tx *world.Tx) { cmd.ExecuteLine(src, line, tx, nil) }):
	case <-deadline.C:
		writeJSON(w, http.StatusOK, commandAnswer{Output: src.answer(), Pending: true})
		return
	}
	sent := make(chan struct{})
	go func() { src.pending.Wait(); close(sent) }()
	pending := false
	select {
	case <-sent:
	case <-deadline.C:
		pending = true
	}
	writeJSON(w, http.StatusOK, commandAnswer{Output: src.answer(), Pending: pending})
}

// status is the answer of GET /v1/status.
type status struct {
	Version      string  `json:"version"`
	Players      int     `json:"players"`
	MaxPlayers   int     `json:"maxPlayers"`
	Uptime       float64 `json:"uptimeSeconds"`
	AuditDropped uint64  `json:"auditDropped"`
}

// serveStatus answers what the server list and query show already, and the
// records the audit log dropped.
func (h *Handler) serveStatus(w http.ResponseWriter, _ *http.Request, _ *source) {
	s := status{Version: h.srv.Version(), Players: h.srv.PlayerCount(), MaxPlayers: h.srv.MaxPlayerCount(), AuditDropped: h.srv.AuditDropped()}
	if start := h.srv.StartTime(); !start.IsZero() {
		s.Uptime = h.now().Sub(start).Seconds()
	}
	writeJSON(w, http.StatusOK, s)
}

// playerInfo is a player of GET /v1/players.
type playerInfo struct {
	Name      string `json:"name"`
	XUID      string `json:"xuid,omitempty"`
	UUID      string `json:"uuid"`
	Dimension string `json:"dimension,omitempty"`
	Level     uint8  `json:"level"`
	Role      string `json:"role"`
	Address   string `json:"address,omitempty"`
}

// servePlayers answers the players online, with their addresses for a
// token of level 3 or more.
func (h *Handler) servePlayers(w http.ResponseWriter, _ *http.Request, src *source) {
	out := []playerInfo{}
	for _, p := range h.srv.Online() {
		perms := h.srv.PlayerPermissions(p.UUID)
		info := playerInfo{Name: p.Name, XUID: p.XUID, UUID: p.UUID.String(), Level: perms.Level, Role: perms.Role.String()}
		if w := p.Handle.World(); w != nil {
			info.Dimension = strings.ToLower(fmt.Sprint(w.Dimension()))
		}
		if p.Addr != nil && src.token.level >= cmd.LevelAdmin {
			info.Address = p.Addr.String()
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

// listOf returns the endpoint that answers the list read passed.
func listOf(read func(*server.Server) (any, error)) func(h *Handler, w http.ResponseWriter, r *http.Request, src *source) {
	return func(h *Handler, w http.ResponseWriter, _ *http.Request, _ *source) {
		list, err := read(h.srv)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "unavailable", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}
