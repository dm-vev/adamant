package bedrockadapter

import (
	"bytes"
	"cmp"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/packbuilder"
	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/protocol/bedrock/query"
	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server"
	"github.com/dm-vev/adamant/net/acme"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/nethernet/lan"
	"github.com/dm-vev/adamant/net/proxy"
)

// Listener returns the NetherNet listener of the adapter: a server.Listener
// for server.Config.Listeners. It reads conf.Log, conf.MaxPlayers,
// conf.StatusProvider, conf.Allower and conf.Blocks, and host for queries.
// Unless c.DisableResourceBuilding, it offers clients a resource pack of the
// custom blocks and items of conf.Blocks after c.Resources.
func (c Config) Listener(conf server.Config, host server.Host) (server.Listener, error) {
	packs := slices.Clone(c.Resources)
	if !c.DisableResourceBuilding {
		if pack, ok := packbuilder.BuildResourcePack(conf.Blocks, c.oldestProtocol().Version, conf.Log); ok {
			packs = append(packs, pack)
		}
	}
	c.Resources = packs
	l, err := c.netherNetListener(conf, host)
	if err != nil {
		return nil, err
	}
	return session.Listen(l, session.ListenConfig{Log: conf.Log, EmoteChatMuted: c.MuteEmoteChat, Blocks: conf.Blocks, Primary: c.Primary}), nil
}

// primaryProtocol returns the protocol the server speaks: Primary, or the
// newest the session speaks as its primary if the config names none.
func (c Config) primaryProtocol() *versions.Protocol {
	if c.Primary == nil {
		return versions.NewestPrimary()
	}
	return c.Primary
}

// oldestProtocol returns the oldest protocol clients may join with: the
// built resource pack must not need a newer game, or its clients refuse it.
// No protocols allow every one a config may name.
func (c Config) oldestProtocol() *versions.Protocol {
	if len(c.Protocols) == 0 {
		return versions.Configurable()[0]
	}
	return slices.MinFunc(c.Protocols, func(a, b *versions.Protocol) int { return cmp.Compare(a.Number, b.Number) })
}

// admissionHooks returns the Allow and BypassesPlayerLimit of netconn of
// the Allower of the server: nil for a nil Allower, and BypassesPlayerLimit
// nil for one that is no server.PlayerLimitBypasser.
func admissionHooks(a server.Allower) (allow func(net.Addr, login.Identity, login.ClientData, bool) (string, bool), bypass func(login.Identity, bool) bool) {
	if a == nil {
		return nil, nil
	}
	allow = func(addr net.Addr, id login.Identity, d login.ClientData, xboxLive bool) (string, bool) {
		return a.Allow(addr, session.IdentityOf(id, xboxLive), session.ProfileOf(d))
	}
	if b, ok := a.(server.PlayerLimitBypasser); ok {
		bypass = func(id login.Identity, xboxLive bool) bool {
			return b.BypassesPlayerLimit(session.IdentityOf(id, xboxLive))
		}
	}
	return allow, bypass
}

// netherNetListener returns the connections of the NetherNet listener of
// the server, admitted to it as the Config says. Queries are answered
// from the server host.
func (c Config) netherNetListener(conf server.Config, host server.Host) (session.ConnListener, error) {
	identityFile := c.Network.IdentityFile
	if identityFile == "" {
		identityFile = "server-key.pem"
	}
	key, err := loadServerIdentityKey(identityFile)
	if err != nil {
		return nil, fmt.Errorf("load NetherNet identity: %w", err)
	}
	allow, bypass := admissionHooks(conf.Allower)
	listen := nethernet.ListenConfig{
		Key:                  key,
		AllowAnonymous:       c.AuthDisabled,
		MaxPending:           unbounded(c.Network.MaxPendingConnections),
		MaxPendingPerAddress: unbounded(c.Network.MaxPendingConnectionsPerIP),
		MaxPendingPerNetwork: unbounded(c.Network.MaxPendingConnectionsPerNetwork),
	}
	var certs *acme.Manager
	if domain := c.Network.HTTPSDomain; domain != "" {
		cacheDir := c.Network.ACMECacheDir
		if cacheDir == "" {
			cacheDir = "acme"
		}
		certs = &acme.Manager{Directory: c.Network.ACMEDirectory, Domain: domain, Email: c.Network.ACMEEmail, CacheDir: cacheDir}
		listen.TLSConfig = &tls.Config{GetCertificate: certs.GetCertificate, MinVersion: tls.VersionTLS12}
	}
	if c.Network.EnableQuery {
		// Server lists send query requests to the port of the game, which
		// is the port of the NetherNet sockets.
		local, err := netip.ParseAddrPort(c.Network.Address)
		if err != nil {
			local = netip.AddrPortFrom(netip.IPv4Unspecified(), 19132)
			if _, port, err := net.SplitHostPort(c.Network.Address); err == nil {
				if p, err := strconv.ParseUint(port, 10, 16); err == nil {
					local = netip.AddrPortFrom(netip.IPv4Unspecified(), uint16(p))
				}
			}
		}
		query.RegisterProvider(func(addr string, port int) query.Data {
			return queryData(host, c.primaryProtocol().Version, addr, port)
		})
		responder, err := query.NewResponder(local, conf.Log)
		if err != nil {
			return nil, fmt.Errorf("create query responder: %w", err)
		}
		listen.Datagram = func(b []byte, from netip.AddrPort, reply func([]byte)) {
			responder.Answer(b, from, reply)
		}
	}
	l, err := netconn.Config{
		Log:                    conf.Log,
		MaximumPlayers:         conf.MaxPlayers,
		MaxLogins:              max(c.Network.MaxPendingLogins, 0),
		MaxPerAddress:          max(c.Network.MaxConnectionsPerIP, 0),
		StatusProvider:         netconnStatusProvider(conf.StatusProvider),
		AuthenticationDisabled: c.AuthDisabled,
		ResourcePacks:          c.Resources,
		TexturePacksRequired:   c.ResourcesRequired,
		Validate: func(id login.Identity, data login.ClientData) error {
			return validateLogin(id, data, c.NameCheck)
		},
		Allow:               allow,
		BypassesPlayerLimit: bypass,
		Compression:         batchCompression(c.Compression),
		Protocols:           c.Protocols,
		Primary:             c.Primary,
	}.Listen(listen, c.Network.Address)
	if err != nil {
		return nil, fmt.Errorf("create NetherNet listener: %w", err)
	}
	conf.Log.Info("NetherNet listener running.", "addr", l.Addr())
	if certs != nil {
		keepCertificate(l, certs, c.Network.ACMEChallengeAddress, conf.Log)
	}
	if c.Xbox.Enabled {
		tokenFile := c.Xbox.TokenFile
		if tokenFile == "" {
			tokenFile = "xbox-token.json"
		}
		go publishXbox(l, tokenFile, xboxOptions{acceptFriends: c.Xbox.AcceptFriends, legacySignaling: c.Xbox.LegacySignaling}, conf.Log)
	}
	if c.Network.LAN {
		publishLAN(l, c, conf.Log, "")
	}
	if c.Proxy.Address != "" {
		if err := serveProxies(l, c.Proxy.Address, c.Proxy.Secret, conf.Log); err != nil {
			_ = l.Close()
			return nil, err
		}
	}
	return netherNetListener{l}, nil
}

// keepCertificate obtains the certificate of the HTTPS signaling and keeps
// it renewed while the listener lives, answering the challenges of the
// authority on addr, ":80" if empty. Until it has one, or if it gets none,
// clients join over plain HTTP.
func keepCertificate(l *netconn.Listener, certs *acme.Manager, addr string, log *slog.Logger) {
	if addr == "" {
		addr = ":80"
	}
	ctx, cancel := context.WithCancel(context.Background())
	challenges := &http.Server{
		Handler:           certs.HTTPHandler(nil),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	if ln, err := net.Listen("tcp", addr); err != nil {
		log.Warn("The challenges of the certificate authority go unanswered: a cached certificate still serves.", "addr", addr, "err", err)
	} else {
		go func() { _ = challenges.Serve(ln) }()
	}
	go func() {
		<-l.Closed()
		cancel()
		_ = challenges.Close()
	}()
	go func() {
		if err := certs.Run(ctx); err != nil {
			if ctx.Err() == nil {
				log.Warn("The signaling has no certificate: clients join over plain HTTP.", "domain", certs.Domain, "err", err)
			}
			return
		}
		log.Info("The signaling is served over HTTPS.", "domain", certs.Domain)
	}()
}

// minProxySecret bounds the length of the secret of proxy links from below.
const minProxySecret = 32

// serveProxies takes the players that proxies open on links to addr,
// signed with secret, until the listener closes.
func serveProxies(l *netconn.Listener, addr, secret string, log *slog.Logger) error {
	if len(secret) < minProxySecret {
		return fmt.Errorf("the secret of proxy links must be %d characters long at least", minProxySecret)
	}
	pl, err := proxy.Listen(addr, []byte(secret))
	if err != nil {
		return fmt.Errorf("listen for proxies: %w", err)
	}
	log.Info("Proxies may carry players to the server.", "addr", pl.Addr())
	go func() {
		<-l.Closed()
		_ = pl.Close()
	}()
	go func() {
		if err := l.ServeProxy(pl); err != nil && !errors.Is(err, proxy.ErrClosed) {
			log.Warn("The links of proxies ended.", "err", err)
		}
	}()
	return nil
}

// publishLAN answers the clients on the network that look for games with
// the status of the listener and serves the signaling of their joins, on
// addr, port 7551 if empty. A port taken by another game leaves the server
// out of the LAN tab.
func publishLAN(l *netconn.Listener, c Config, log *slog.Logger, addr string) *lan.Listener {
	ll, err := lan.Config{Addr: addr, Data: func() lan.ServerData {
		st, p := l.ServerStatus(), l.Advertised()
		return lan.ServerData{
			ServerName:            st.ServerName,
			Protocol:              p.Number,
			Version:               p.Version,
			LevelName:             st.ServerSubName,
			PlayerCount:           int32(st.PlayerCount),
			MaxPlayerCount:        int32(st.MaxPlayers),
			AcceptsOnlineAuth:     !c.AuthDisabled,
			AcceptsSelfSignedAuth: c.AuthDisabled,
			ConnectionType:        lan.ConnectionTypeLAN,
		}
	}}.Listen()
	if err != nil {
		log.Warn("The server does not show on LAN.", "err", err)
		return nil
	}
	log.Info("The server shows on LAN.", "addr", ll.Addr(), "network-id", ll.NetworkID())
	go func() {
		<-l.Closed()
		_ = ll.Close()
	}()
	go func() {
		// Serve returns only once the signaling ends, never with nil.
		if err := l.Serve(ll); !errors.Is(err, net.ErrClosed) {
			log.Warn("LAN signaling ended.", "err", err)
		}
	}()
	return ll
}

// unbounded returns a bound of the NetherNet listener as the [Network]
// table gives it, where zero, or less, means no bound: the listener takes
// zero for its default.
func unbounded(n int) int {
	if n <= 0 {
		return math.MaxInt
	}
	return n
}

// batchCompression returns the compression of the batches: flate at level
// 1 unless another was asked for.
func batchCompression(c bedrock.Compression) bedrock.Compression {
	if c == nil {
		return bedrock.Flate{Level: 1}
	}
	return c
}

// netherNetListener is a netconn.Listener as the session listens on it.
type netherNetListener struct {
	*netconn.Listener
}

// Accept returns the next client that logged in.
func (l netherNetListener) Accept() (session.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Disconnect disconnects a client with a message.
func (l netherNetListener) Disconnect(conn session.Conn, reason string) error {
	return l.Listener.Disconnect(conn.(*netconn.Conn), reason)
}

// loadServerIdentityKey preserves the server identity across restarts. A corrupt
// existing key is an error, never a reason to silently replace a trusted identity.
func loadServerIdentityKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return decodeServerIdentityKey(data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}

	// Publish a complete, private file without overwriting an identity created
	// concurrently by another process. The temporary file is on the same filesystem.
	f, err := os.CreateTemp(filepath.Dir(path), ".server-key-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if err := pem.Encode(f, &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Link(f.Name(), path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return decodeServerIdentityKey(data)
	}
	return key, nil
}

func decodeServerIdentityKey(data []byte) (*ecdsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "EC PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("expected one PEM EC PRIVATE KEY")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse server identity: %w", err)
	}
	if key.Curve != elliptic.P384() {
		return nil, errors.New("server identity must use the P-384 curve")
	}
	return key, nil
}

// validateLogin holds the identity and the client data of a client that
// logged in to the rules the server keeps them to, those gophertunnel kept
// them to: a valid UUID and XUID, a name of Bedrock's form, checked as
// check says, a device and a language that exist, skins of the sizes they
// claim. The address the
// client names the server by is read, not resolved: a lookup of a name a
// client chose would let it hold the login on a slow resolver.
func validateLogin(id login.Identity, data login.ClientData, check NameCheck) error {
	if err := netconn.ValidateIdentity(id); err != nil {
		return err
	}
	// The rules of names do not ask who vouched for the identity.
	if err := validateName(session.IdentityOf(id, false), check); err != nil {
		return err
	}
	return netconn.ValidateClientData(data)
}
