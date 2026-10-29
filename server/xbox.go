package server

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/xbox/auth"
	"github.com/dm-vev/adamant/net/xbox/mpsd"
	"github.com/dm-vev/adamant/net/xbox/signaling"
)

// xboxUpdateInterval paces the updates of the session friends see.
const xboxUpdateInterval = 30 * time.Second

// publishXbox publishes the server on Xbox Live until its listener closes:
// it signs the account in, serves the offers friends send over the
// signaling service and keeps the session friends see the server by up to
// date.
func publishXbox(l *netconn.Listener, tokenFile string, log *slog.Logger) {
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
	go serveXboxSignaling(ctx, l, s, networkID, log)
	keepXboxSession(ctx, l, s, networkID, log)
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
func serveXboxSignaling(ctx context.Context, l *netconn.Listener, s *auth.Session, networkID uint64, log *slog.Logger) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		err := func() error {
			mc, err := s.MCToken(ctx)
			if err != nil {
				return err
			}
			conn, err := signaling.Dial(ctx, signaling.Config{Token: mc.Header, NetworkID: networkID})
			if err != nil {
				return err
			}
			defer conn.Close()
			backoff = 5 * time.Second
			log.Info("Xbox Live signaling connected.")
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
func keepXboxSession(ctx context.Context, l *netconn.Listener, s *auth.Session, networkID uint64, log *slog.Logger) {
	token := func(ctx context.Context) (string, string, error) {
		x, err := s.XSTS(ctx, auth.XboxLive)
		if err != nil {
			return "", "", err
		}
		return x.Header(), x.XUID, nil
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
				host, err = mpsd.Publish(ctx, mpsd.Config{Token: token, NetherNetID: networkID, PlayerMessagingID: mc.PlayerMessagingID}, status)
			}
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
