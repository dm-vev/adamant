package server

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/turn"
	"github.com/dm-vev/adamant/net/xbox/auth"
	"github.com/dm-vev/adamant/net/xbox/mpsd"
	"github.com/dm-vev/adamant/net/xbox/signaling"
	"github.com/dm-vev/adamant/net/xbox/social"
)

// xboxUpdateInterval paces the updates of the session friends see.
const xboxUpdateInterval = 30 * time.Second

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
// again once a connection drops or its token nears expiry.
func serveXboxSignaling(ctx context.Context, l *netconn.Listener, s *auth.Session, networkID uint64, legacy bool, log *slog.Logger) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		err := func() error {
			mc, err := s.MCToken(ctx)
			if err != nil {
				return err
			}
			conn, err := dialXboxSignaling(ctx, signaling.Config{Token: mc.Header, NetworkID: networkID}, legacy)
			if err != nil {
				return err
			}
			defer conn.Close()
			backoff = 5 * time.Second
			log.Info("Xbox Live signaling connected.")
			if cfg, ok := relayServer(ctx, conn.Credentials()); ok {
				// Friends on other networks reach a server behind NAT through
				// the TURN server of the service alone.
				rctx, cancel := context.WithTimeout(ctx, 40*time.Second)
				if err := l.Relay(rctx, cfg); err != nil {
					log.Warn("No relay on the TURN server of Xbox Live: friends behind other NATs may not join.", "err", err)
				} else {
					log.Debug("Relay allocated on the TURN server of Xbox Live.", "server", cfg.Server)
				}
				cancel()
			}
			served := make(chan struct{})
			go func() {
				_ = l.Serve(conn)
				close(served)
			}()
			// Past refreshing, the token is renewed by the next MCToken.
			renew := max(time.Until(mc.ValidUntil)-4*time.Minute, time.Minute)
			select {
			case <-served:
				return conn.Err()
			case <-ctx.Done():
			case <-time.After(renew):
			}
			_ = conn.Close()
			<-served
			return nil
		}()
		if err == nil || ctx.Err() != nil {
			continue
		}
		log.Warn("Xbox Live signaling failed.", "err", err, "retry", backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Minute)
	}
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
		st := l.ServerStatus()
		status := mpsd.Status{
			HostName:       st.ServerName,
			WorldName:      st.ServerSubName,
			Version:        v2193.Version,
			Protocol:       v2193.Protocol,
			MemberCount:    st.PlayerCount,
			MaxMemberCount: st.MaxPlayers,
		}
		if host == nil {
			mc, err := s.MCToken(ctx)
			if err == nil {
				host, err = mpsd.Publish(ctx, mpsd.Config{Token: token, NetherNetID: networkID, PlayerMessagingID: mc.PlayerMessagingID,
					LegacySignaling: opts.legacySignaling, FriendRequests: notify}, status)
			}
			friends.host.Store(host)
			if err != nil {
				if ctx.Err() == nil {
					log.Warn("Publishing on Xbox Live failed.", "err", err)
				}
			} else if x, err := s.XSTS(ctx, auth.XboxLive); err == nil {
				log.Info("Published on Xbox Live: friends of the account see the server.", "gamertag", x.Gamertag)
			}
		} else if err := host.Update(ctx, status); err != nil && ctx.Err() == nil {
			log.Warn("Updating the Xbox Live session failed; publishing it again.", "err", err)
			closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = host.Close(closeCtx)
			cancel()
			host = nil
			friends.host.Store(nil)
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
	people, err := f.client.FriendRequests(ctx)
	if err != nil {
		if ctx.Err() == nil {
			f.log.Debug("Reading the friend requests on Xbox Live failed.", "err", err)
		}
		return 0
	}
	for _, p := range people {
		friend, err := f.client.AddFriend(ctx, p.XUID)
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
			if err := f.client.Invite(ctx, p.XUID, host.Ref()); err != nil {
				f.log.Debug("Inviting a friend failed.", "gamertag", p.Gamertag, "err", err)
			}
		}
	}
	return 0
}
