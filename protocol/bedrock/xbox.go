package bedrockadapter

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/turn"
	"github.com/dm-vev/adamant/net/xbox/auth"
	"github.com/dm-vev/adamant/net/xbox/mpsd"
	"github.com/dm-vev/adamant/net/xbox/signaling"
	"github.com/dm-vev/adamant/net/xbox/social"
)

// xboxUpdateInterval paces the updates of the session friends see.
const xboxUpdateInterval = 30 * time.Second

// xboxCallTimeout bounds each call to the services of Xbox Live, which
// would otherwise wait on a service that stalls for as long as the server
// runs.
var xboxCallTimeout = 30 * time.Second

// publishXbox publishes the server on Xbox Live until its listener closes:
// it signs the account in, serves the offers friends send over the
// signaling service and keeps the session friends see the server by up to
// date.
// xboxOptions are the options of the publishing on Xbox Live.
type xboxOptions struct {
	acceptFriends, legacySignaling bool
}

func publishXbox(l *netconn.Listener, tokenFile string, opts xboxOptions, log *slog.Logger) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-l.Closed()
		cancel()
	}()
	s, err := xboxSession(ctx, tokenFile, log)
	if err != nil {
		if ctx.Err() == nil {
			log.Error("Xbox Live sign-in failed.", "err", err)
		}
		return
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	// A positive network ID, as the game makes them.
	networkID := binary.BigEndian.Uint64(b[:])>>1 | 1
	go serveXboxSignaling(ctx, l, s, networkID, opts.legacySignaling, log)
	keepXboxSession(ctx, l, s, networkID, opts, log)
}

// xboxSignaling is a connection to the signaling service of either
// protocol.
type xboxSignaling interface {
	nethernet.Signaling
	Credentials() []signaling.ICEServer
	Close() error
	Err() error
}

// dialXboxSignaling connects to the signaling service over the protocol
// asked for.
func dialXboxSignaling(ctx context.Context, cfg signaling.Config, legacy bool) (xboxSignaling, error) {
	if legacy {
		return signaling.DialLegacy(ctx, cfg)
	}
	return signaling.Dial(ctx, cfg)
}

// xboxSession signs the account in from the token file, or with the device
// code flow if the file holds no sign-in, which it then keeps.
func xboxSession(ctx context.Context, tokenFile string, log *slog.Logger) (*auth.Session, error) {
	save := func(snap auth.Snapshot) {
		if err := saveXboxToken(tokenFile, snap); err != nil {
			log.Error("Keeping the Xbox Live sign-in failed.", "file", tokenFile, "err", err)
		}
	}
	var s *auth.Session
	data, err := os.ReadFile(tokenFile)
	switch {
	case err == nil:
		var snap auth.Snapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			return nil, err
		}
		if s, err = auth.Android.Restore(snap); err != nil {
			return nil, err
		}
	case errors.Is(err, os.ErrNotExist):
		dc, err := auth.Android.StartLogin(ctx)
		if err != nil {
			return nil, err
		}
		log.Warn("Sign the Xbox Live account of the server in: open the page and enter the code.", "page", dc.VerificationURI, "code", dc.UserCode)
		if s, err = dc.Wait(ctx); err != nil {
			return nil, err
		}
		save(s.Snapshot())
	default:
		return nil, err
	}
	s.OnRefresh(save)
	return s, nil
}

// saveXboxToken writes a sign-in to a file only its owner reads, replacing
// the old one at once.
func saveXboxToken(path string, snap auth.Snapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".xbox-token-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// serveXboxSignaling keeps a connection to the signaling service, over
// which friends send their offers, until the context ends: it connects
// again once a connection drops, once the relay on the TURN server it
// handed out failed, for fresh credentials, and with a new token once its
// token nears expiry.
func serveXboxSignaling(ctx context.Context, l *netconn.Listener, s *auth.Session, networkID uint64, legacy bool, log *slog.Logger) {
	x := &xboxSignalingLoop{
		l:     l,
		token: s.MCToken,
		dial: func(ctx context.Context, token string) (xboxSignaling, error) {
			return dialXboxSignaling(ctx, signaling.Config{Token: token, NetworkID: networkID}, legacy)
		},
		after:   time.After,
		log:     log,
		backoff: 5 * time.Second,
	}
	x.run(ctx)
}

// xboxListener is the listener the signaling of Xbox Live serves, with a
// relay on the TURN server the service hands out.
type xboxListener interface {
	Serve(s nethernet.Signaling) error
	Relay(ctx context.Context, cfg turn.Config) error
	RelayDone() <-chan struct{}
	RelayErr() error
}

// xboxSignalingLoop is serveXboxSignaling, with the token, the connection
// to the service and the timers it takes.
type xboxSignalingLoop struct {
	l     xboxListener
	token func(ctx context.Context) (*auth.MCToken, error)
	dial  func(ctx context.Context, token string) (xboxSignaling, error)
	after func(d time.Duration) <-chan time.Time
	log   *slog.Logger
	// backoff is how long to wait before connecting again after a failure.
	backoff time.Duration
}

// run keeps a connection to the signaling service until the context ends.
func (x *xboxSignalingLoop) run(ctx context.Context) {
	var mc *auth.MCToken
	for ctx.Err() == nil {
		var err error
		if mc == nil {
			mc, err = x.token(ctx)
		}
		if err == nil {
			mc, err = x.serve(ctx, mc)
		}
		if err != nil && ctx.Err() == nil {
			x.wait(ctx, err)
		}
	}
}

// serve connects to the signaling service with the token mc and serves
// the offers that come over the connection until it ends, or the relay on
// the TURN server it handed out fails, or the token nears expiry. Then it
// returns the next token, fetched while the connection still served: if
// none can be had, the connection serves on until it ends.
func (x *xboxSignalingLoop) serve(ctx context.Context, mc *auth.MCToken) (*auth.MCToken, error) {
	conn, err := x.dial(ctx, mc.Header)
	if err != nil {
		return nil, err
	}
	x.backoff = 5 * time.Second
	x.log.Info("Xbox Live signaling connected.")
	relayDone := x.relay(ctx, conn)
	served := make(chan struct{})
	go func() {
		_ = x.l.Serve(conn)
		close(served)
	}()
	defer func() {
		_ = conn.Close()
		<-served
	}()
	// Past refreshing, the token is renewed by the next MCToken.
	renew := x.after(max(time.Until(mc.ValidUntil)-4*time.Minute, time.Minute))
	for {
		select {
		case <-served:
			return nil, conn.Err()
		case <-ctx.Done():
			return nil, nil
		case <-relayDone:
			return nil, fmt.Errorf("relay on the TURN server failed: %w", x.l.RelayErr())
		case <-renew:
			next, err := x.token(ctx)
			if err == nil {
				return next, nil
			}
			x.log.Warn("Renewing the token of the Xbox Live signaling failed: it serves on until it drops.", "err", err)
			renew = nil
		}
	}
}

// relay makes a relay on the TURN server the signaling service handed
// out, or keeps the one made earlier: friends on other networks reach a
// server behind NAT through it alone. It returns the channel closed once
// the relay failed, nil without one.
func (x *xboxSignalingLoop) relay(ctx context.Context, conn xboxSignaling) <-chan struct{} {
	cfg, ok := relayServer(ctx, conn.Credentials())
	if !ok {
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if err := x.l.Relay(rctx, cfg); err != nil {
		x.log.Warn("No relay on the TURN server of Xbox Live: friends behind other NATs may not join.", "err", err)
		return nil
	}
	x.log.Debug("Relay on the TURN server of Xbox Live.", "server", cfg.Server)
	return x.l.RelayDone()
}

// wait tells why the signaling failed and waits before connecting again,
// twice as long each time up to five minutes.
func (x *xboxSignalingLoop) wait(ctx context.Context, err error) {
	x.log.Warn("Xbox Live signaling failed.", "err", err, "retry", x.backoff)
	select {
	case <-ctx.Done():
	case <-x.after(x.backoff):
	}
	x.backoff = min(x.backoff*2, 5*time.Minute)
}

// keepXboxSession publishes the session friends see the server by and
// updates it with the status of the server until the context ends, when it
// removes the session.
func keepXboxSession(ctx context.Context, l *netconn.Listener, s *auth.Session, networkID uint64, opts xboxOptions, log *slog.Logger) {
	token := func(ctx context.Context) (string, string, error) {
		x, err := s.XSTS(ctx, auth.XboxLive)
		if err != nil {
			return "", "", err
		}
		return x.Header(), x.XUID, nil
	}
	friends := &xboxFriends{
		client:   social.Client{Token: func(ctx context.Context) (string, error) { h, _, err := token(ctx); return h, err }},
		requests: make(chan struct{}, 1),
		log:      log,
	}
	var notify func()
	if opts.acceptFriends {
		notify = friends.notify
		go friends.run(ctx)
	}
	var host *mpsd.Host
	t := time.NewTicker(xboxUpdateInterval)
	defer t.Stop()
	for {
		st, p := l.ServerStatus(), l.Advertised()
		status := mpsd.Status{
			HostName:       st.ServerName,
			WorldName:      st.ServerSubName,
			Version:        p.Version,
			Protocol:       int(p.Number),
			MemberCount:    st.PlayerCount,
			MaxMemberCount: st.MaxPlayers,
		}
		if host == nil {
			callCtx, cancel := context.WithTimeout(ctx, xboxCallTimeout)
			mc, err := s.MCToken(callCtx)
			cancel()
			if err == nil {
				callCtx, cancel = context.WithTimeout(ctx, xboxCallTimeout)
				host, err = mpsd.Publish(callCtx, mpsd.Config{Token: token, NetherNetID: networkID, PlayerMessagingID: mc.PlayerMessagingID,
					LegacySignaling: opts.legacySignaling, FriendRequests: notify}, status)
				cancel()
			}
			friends.host.Store(host)
			if err != nil {
				if ctx.Err() == nil {
					log.Warn("Publishing on Xbox Live failed.", "err", err)
				}
			} else {
				callCtx, cancel = context.WithTimeout(ctx, xboxCallTimeout)
				if x, err := s.XSTS(callCtx, auth.XboxLive); err == nil {
					log.Info("Published on Xbox Live: friends of the account see the server.", "gamertag", x.Gamertag)
				}
				cancel()
			}
		} else {
			callCtx, cancel := context.WithTimeout(ctx, xboxCallTimeout)
			err := host.Update(callCtx, status)
			cancel()
			if err != nil && ctx.Err() == nil {
				log.Warn("Updating the Xbox Live session failed; publishing it again.", "err", err)
				closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = host.Close(closeCtx)
				cancel()
				host = nil
				friends.host.Store(nil)
			}
		}
		select {
		case <-ctx.Done():
			if host != nil {
				closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = host.Close(closeCtx)
				cancel()
			}
			return
		case <-t.C:
		}
	}
}

// relayServer returns the first TURN server over UDP of those the signaling
// service handed out, resolved, with its credentials.
func relayServer(ctx context.Context, servers []signaling.ICEServer) (turn.Config, bool) {
	for _, srv := range servers {
		for _, u := range srv.URLs {
			rest, ok := strings.CutPrefix(u, "turn:")
			if !ok {
				continue
			}
			hostport, query, _ := strings.Cut(rest, "?")
			if query != "" && query != "transport=udp" {
				continue
			}
			host, port, err := net.SplitHostPort(hostport)
			if err != nil {
				host, port = hostport, "3478"
			}
			p, err := strconv.ParseUint(port, 10, 16)
			if err != nil {
				continue
			}
			addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
			if err != nil || len(addrs) == 0 {
				continue
			}
			return turn.Config{Server: netip.AddrPortFrom(addrs[0].Unmap(), uint16(p)), Username: srv.Username, Password: srv.Credential}, true
		}
	}
	return turn.Config{}, false
}

// xboxFriends takes the friend requests of the account and invites the new
// friends to the session of the server.
type xboxFriends struct {
	client   social.Client
	host     atomic.Pointer[mpsd.Host]
	requests chan struct{}
	log      *slog.Logger
}

// friendCheckInterval paces the checks for friend requests the real-time
// activity did not tell of.
const friendCheckInterval = 5 * time.Minute

// notify asks for the friend requests to be taken.
func (f *xboxFriends) notify() {
	select {
	case f.requests <- struct{}{}:
	default:
	}
}

// run takes the friend requests when told of them and every few minutes
// until the context ends, waiting as long as the service asks after it
// refused for too many.
func (f *xboxFriends) run(ctx context.Context) {
	t := time.NewTicker(friendCheckInterval)
	defer t.Stop()
	f.notify()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-f.requests:
		}
		if wait := f.accept(ctx); wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
				f.notify()
			}
		}
	}
}

// accept adds the people who sent a friend request as friends and invites
// them. It returns how long to wait if the service refused for too many.
func (f *xboxFriends) accept(ctx context.Context) time.Duration {
	callCtx, cancel := context.WithTimeout(ctx, xboxCallTimeout)
	people, err := f.client.FriendRequests(callCtx)
	cancel()
	if err != nil {
		if ctx.Err() == nil {
			f.log.Debug("Reading the friend requests on Xbox Live failed.", "err", err)
		}
		return 0
	}
	for _, p := range people {
		callCtx, cancel := context.WithTimeout(ctx, xboxCallTimeout)
		friend, err := f.client.AddFriend(callCtx, p.XUID)
		cancel()
		var limited *social.RateLimited
		if errors.As(err, &limited) {
			return limited.After
		}
		if err != nil {
			f.log.Debug("Adding a friend on Xbox Live failed.", "gamertag", p.Gamertag, "err", err)
			continue
		}
		if !friend {
			continue
		}
		f.log.Info("Added a friend on Xbox Live.", "gamertag", p.Gamertag)
		if host := f.host.Load(); host != nil {
			callCtx, cancel := context.WithTimeout(ctx, xboxCallTimeout)
			err := f.client.Invite(callCtx, p.XUID, host.Ref())
			cancel()
			if err != nil {
				f.log.Debug("Inviting a friend failed.", "gamertag", p.Gamertag, "err", err)
			}
		}
	}
	return 0
}
