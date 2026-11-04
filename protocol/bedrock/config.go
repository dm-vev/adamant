package bedrockadapter

import (
	"cmp"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/protocol/bedrock/query"
	"github.com/df-mc/dragonfly/server/config"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/resource"
)

// UserConfig is the part of config.toml the adapter owns: the [Network],
// [Xbox], [Proxy] and [Resources] tables, and the AuthEnabled,
// MuteEmoteChat and NameCheck keys of [Server], whose other keys the server
// owns. It may
// be serialised and is converted to a Config by UserConfig.Config.
type UserConfig struct {
	// Network holds settings related to network aspects of the server.
	Network NetworkConfig
	// Xbox holds the publishing of the server on Xbox Live.
	Xbox XboxConfig
	// Proxy holds the links that proxies carry their players to the server
	// over, beside the clients that join it themselves.
	Proxy ProxyConfig
	// Server holds the keys of the [Server] table the adapter owns.
	Server struct {
		// AuthEnabled controls whether players must be connected to Xbox Live
		// in order to join the server.
		AuthEnabled bool
		// MuteEmoteChat specifies if the player emote chat should be muted or not.
		MuteEmoteChat bool
		// NameCheck sets how the names players log in with are checked:
		// "lenient" admits a name if any one of its characters is one
		// Bedrock allows, as the server always did, so that §aSteve and
		// Steve! join; "game" also refuses names with a character other than
		// a letter, a digit, a space or an underscore, the names commands
		// can name; "strict" refuses names with any character Bedrock does
		// not allow. An empty value is "lenient".
		NameCheck string
	}
	Resources struct {
		// AutoBuildPack is if the server should automatically generate a
		// resource pack for custom features.
		AutoBuildPack bool
		// Folder controls the location where resource packs will be loaded
		// from.
		Folder string
		// Required is a boolean to force the client to load the resource pack
		// on join. If they do not accept, they'll have to leave the server.
		Required bool
	}
}

// NetworkConfig holds settings related to network aspects of the server:
// the [Network] table of config.toml.
type NetworkConfig struct {
	// Address is the address on which the server should listen. Players may
	// connect to this address in order to join.
	Address string
	// Transport names the transport players join over: "nethernet", the
	// only one, which an empty value also selects.
	Transport string
	// IdentityFile stores the NetherNet server's persistent P-384 identity key.
	// An empty value uses "server-key.pem".
	IdentityFile string
	// EnableQuery controls whether Bedrock Query responses are enabled. Query
	// requests arrive on the UDP port of the game, beside NetherNet.
	// When disabled, query packets are ignored (no reply is sent).
	EnableQuery bool
	// QueryPlugins controls whether the query response should list plugins.
	// When disabled, the query `plugins` field only contains the engine label.
	QueryPlugins bool
	// LAN shows the server in the LAN tab of clients on the same network
	// and lets them join from there, over UDP port 7551 as the game does.
	LAN bool
	// HTTPSDomain is the domain of the server for its signaling over
	// HTTPS: clients that join by an https:// address trust its
	// certificate rather than its key on first use. The server obtains
	// the certificate from Let's Encrypt and renews it, proving the domain
	// on TCP port 80, which must reach the server. Plain HTTP stays served
	// on the port of the game. An empty value serves plain HTTP alone.
	HTTPSDomain string
	// ACMEEmail is the contact of the account of the server at the
	// certificate authority, optional.
	ACMEEmail string
	// ACMECacheDir keeps the account and the certificate across restarts,
	// as the authority issues a domain few certificates a week. An empty
	// value uses "acme".
	ACMECacheDir string
	// ACMEDirectory is the directory URL of the certificate authority.
	// An empty value uses Let's Encrypt.
	ACMEDirectory string
	// ACMEChallengeAddress is where the server answers the challenges of
	// the authority, which asks on port 80: another address suits a port
	// forwarded to it. An empty value uses ":80".
	ACMEChallengeAddress string
	// Protocols lists the protocols of Bedrock Edition clients may join
	// with, by a version of each: "1.26.50" (or "26.50", "26.51", "26.52",
	// "1.26.52", "2193") names protocol 2193, "1.26.60" (or "26.60",
	// "26.61", "1.26.61", "2225") protocol 2225. Several names of one
	// protocol count once. An empty list allows the primary protocol alone.
	Protocols []string
	// PrimaryProtocol is the protocol the server speaks, one of Protocols:
	// the packets and fields it uses are those of that protocol, and clients
	// of the other protocols get them converted. An empty value is the
	// oldest of Protocols, or the default of the build if Protocols is
	// empty too. This build speaks 2193 or 2225 as its primary.
	PrimaryProtocol string
	// MaxPendingConnections bounds the NetherNet connections that did not
	// open yet, offers of clients still checking their way to the server:
	// once they are all taken, a new offer displaces the oldest offer
	// that never connected. Zero means no bound.
	MaxPendingConnections int
	// MaxPendingConnectionsPerIP and MaxPendingConnectionsPerNetwork
	// bound the connections not open yet of one source: an IPv4 address
	// or an IPv6 /64, and an IPv4 /24 or an IPv6 /48. Loopback is not
	// bounded. Zero means no bound.
	MaxPendingConnectionsPerIP, MaxPendingConnectionsPerNetwork int
	// MaxPendingLogins bounds the clients logging in at once, from the
	// opening of their connection to the end of their resource packs: a
	// client over it is told the server is full. Zero means no bound.
	MaxPendingLogins int
	// MaxConnectionsPerIP bounds the clients of one address, logging in or
	// playing: an IPv4 address or an IPv6 /64. A player of a proxy counts
	// against the address the proxy saw it at; loopback is not bounded.
	// Zero means no bound.
	MaxConnectionsPerIP int
}

// XboxConfig holds the publishing of the server on Xbox Live: the [Xbox]
// table of config.toml.
type XboxConfig struct {
	// Enabled publishes the server on Xbox Live through the account it
	// signs in as, so that friends of the account see it in their friends
	// list and join from there. It needs the NetherNet transport. The
	// account is signed in on the first start, with a code the console
	// shows to enter at a page of Microsoft.
	Enabled bool
	// TokenFile keeps the sign-in of the account across restarts. It holds
	// secrets. An empty value uses "xbox-token.json".
	TokenFile string
	// AcceptFriends takes the friend requests the account receives and
	// invites each new friend to the server, so that players add the
	// account and join.
	AcceptFriends bool
	// LegacySignaling has friends signal their joins over the protocol the
	// game spoke before JSON-RPC, a fallback for when the service of the
	// current one fails.
	LegacySignaling bool
}

// ProxyConfig holds the links that proxies carry their players to the
// server over, beside the clients that join it themselves: the [Proxy]
// table of config.toml.
type ProxyConfig struct {
	// Address is the TCP address proxies dial their links to, such as
	// "127.0.0.1:19133". An empty value takes no proxies. Links are not
	// encrypted: they are for a network the proxies and the server
	// trust.
	Address string
	// Secret is the secret the proxies sign the players they open with,
	// of 32 characters at least. Whoever knows it joins as anyone it
	// likes, so it must stay as secret as a password.
	Secret string
}

// Config holds the options of the NetherNet listener of the adapter, which
// clients of Bedrock Edition join through. UserConfig.Config makes it of
// the keys of config.toml the adapter owns.
type Config struct {
	Network NetworkConfig
	Xbox    XboxConfig
	Proxy   ProxyConfig
	// Protocols are the protocols clients may join with, oldest first, and
	// Primary is the one of them the server speaks, as Network names them.
	// No protocols allow the default of the build alone.
	Protocols []*versions.Protocol
	Primary   *versions.Protocol
	// Resources are the resource packs offered to clients, and
	// ResourcesRequired makes clients take them.
	Resources         []*resource.Pack
	ResourcesRequired bool
	// DisableResourceBuilding leaves out the resource pack the listener
	// otherwise builds of the custom blocks and items of the server and
	// offers after Resources. It is set if a pack of them exists already.
	DisableResourceBuilding bool
	// AuthDisabled lets clients join without Xbox Live. Note that this
	// should generally only be done for testing purposes or for local games:
	// allowing players to join without authentication is a security hazard.
	AuthDisabled bool
	// MuteEmoteChat mutes the emote chat of players.
	MuteEmoteChat bool
	// NameCheck is how strictly the names players log in with are held to
	// the rules of Bedrock Edition and of the game. The zero value is
	// NameCheckLenient.
	NameCheck NameCheck
	// Compression compresses the batches of the connections: bedrock.Flate
	// (at level 1 if nil) or bedrock.None.
	Compression bedrock.Compression
}

// Config converts a UserConfig to a Config. An error is returned if Check
// finds problems, all of them in one error, or if loading resources
// failed. It sets whether
// queries are answered, and with plugins, as the [Network] table says.
func (uc UserConfig) Config(log *slog.Logger) (Config, error) {
	var problems config.Problems
	uc.Check(&problems)
	if err := problems.Err(false); err != nil {
		return Config{}, err
	}
	transport, err := parseTransport(uc.Network.Transport)
	if err != nil {
		return Config{}, err
	}
	uc.Network.Transport = transport
	nameCheck, err := parseNameCheck(uc.Server.NameCheck)
	if err != nil {
		return Config{}, err
	}
	protocols, primary, err := parseProtocols(uc.Network.Protocols, uc.Network.PrimaryProtocol, versions.Parse, versions.Default(), versions.NewestPrimary())
	if err != nil {
		return Config{}, err
	}
	query.SetEnabled(uc.Network.EnableQuery)
	query.SetPluginListingEnabled(uc.Network.QueryPlugins)

	c := Config{
		Network:                 uc.Network,
		Xbox:                    uc.Xbox,
		Proxy:                   uc.Proxy,
		Protocols:               protocols,
		Primary:                 primary,
		ResourcesRequired:       uc.Resources.Required,
		DisableResourceBuilding: !uc.Resources.AutoBuildPack,
		AuthDisabled:            !uc.Server.AuthEnabled,
		MuteEmoteChat:           uc.Server.MuteEmoteChat,
		NameCheck:               nameCheck,
	}
	c.Resources, err = loadResources(uc.Resources.Folder)
	if err != nil {
		return c, fmt.Errorf("load resources: %w", err)
	}
	return c, nil
}

// parseProtocols returns the protocols the keys Protocols and
// PrimaryProtocol name by parse, oldest first and each once, and the
// primary: with def, the default of the build, for both keys empty, and
// model, the newest protocol the session speaks as its primary
// (versions.NewestPrimary), which bounds the primary.
func parseProtocols(names []string, primaryName string, parse func(string) (*versions.Protocol, error), def, model *versions.Protocol) ([]*versions.Protocol, *versions.Protocol, error) {
	var protocols []*versions.Protocol
	for _, name := range names {
		p, err := parse(name)
		if err != nil {
			return nil, nil, fmt.Errorf("[Network] Protocols: %w", err)
		}
		protocols = append(protocols, p)
	}
	slices.SortFunc(protocols, func(a, b *versions.Protocol) int { return cmp.Compare(a.Number, b.Number) })
	protocols = slices.Compact(protocols)

	primary := def
	if len(protocols) > 0 {
		primary = protocols[0]
	}
	if strings.TrimSpace(primaryName) != "" {
		p, err := parse(primaryName)
		if err != nil {
			return nil, nil, fmt.Errorf("[Network] PrimaryProtocol: %w", err)
		}
		primary = p
	}
	if len(protocols) == 0 {
		protocols = []*versions.Protocol{primary}
	}
	if !slices.Contains(protocols, primary) {
		return nil, nil, fmt.Errorf("[Network] PrimaryProtocol %s must be one of Protocols (%s)", primary, protocolList(protocols))
	}
	if primary.Number > model.Number {
		return nil, nil, fmt.Errorf("[Network] PrimaryProtocol %s needs a build whose session speaks %s: this one speaks %s", primary, primary, model)
	}
	return protocols, primary, nil
}

// checkProtocols adds the problems of the keys Protocols and PrimaryProtocol
// to p, each under its key: a protocol the build does not support, a primary
// that is none of Protocols, and a primary newer than model, the newest
// protocol the session speaks as its primary, which is the oldest of
// Protocols when PrimaryProtocol is empty.
func checkProtocols(p *config.Problems, names []string, primaryName string, parse func(string) (*versions.Protocol, error), model *versions.Protocol) {
	var protocols []*versions.Protocol
	ok := true
	for _, name := range names {
		v, err := parse(name)
		if err != nil {
			p.Add("Network.Protocols", err)
			ok = false
			continue
		}
		protocols = append(protocols, v)
	}
	if strings.TrimSpace(primaryName) == "" {
		if !ok || len(protocols) == 0 {
			return
		}
		oldest := slices.MinFunc(protocols, func(a, b *versions.Protocol) int { return cmp.Compare(a.Number, b.Number) })
		if oldest.Number > model.Number {
			p.Errorf("Network.Protocols", "Network.Protocols: the oldest, %s, is the primary, and this build's session speaks %s", oldest, model)
		}
		return
	}
	primary, err := parse(primaryName)
	if err != nil {
		p.Add("Network.PrimaryProtocol", err)
		return
	}
	if ok && len(protocols) > 0 && !slices.Contains(protocols, primary) {
		p.Errorf("Network.PrimaryProtocol", "Network.PrimaryProtocol = %q: want one of Network.Protocols (%s)", primaryName, protocolList(protocols))
	}
	if primary.Number > model.Number {
		p.Errorf("Network.PrimaryProtocol", "Network.PrimaryProtocol = %q: this build's session speaks %s", primaryName, model)
	}
}

// protocolList lists protocols by the version each tells its clients.
func protocolList(ps []*versions.Protocol) string {
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = p.Version
	}
	return strings.Join(s, ", ")
}

// Check adds the problems of the values of uc to p, as package
// server/config describes: the values Config refuses and those out of their
// bounds. A key of config.toml the adapter reads has its bounds declared
// here.
func (uc UserConfig) Check(p *config.Problems) {
	n := uc.Network
	config.Address(p, "Network.Address", "udp", n.Address)
	_, err := parseTransport(n.Transport)
	p.Add("Network.Transport", err)
	config.File(p, "Network.IdentityFile", cmp.Or(n.IdentityFile, "server-key.pem"))
	if n.HTTPSDomain != "" {
		// The certificate is of a name alone: a scheme, a port or a path
		// is no part of it.
		if _, _, err := net.SplitHostPort(n.HTTPSDomain); err == nil || strings.ContainsAny(n.HTTPSDomain, "/ ") {
			p.Errorf("Network.HTTPSDomain", "Network.HTTPSDomain = %q: want a domain alone, such as \"play.example.com\"", n.HTTPSDomain)
		}
		config.Folder(p, "Network.ACMECacheDir", cmp.Or(n.ACMECacheDir, "acme"))
	}
	if n.ACMEDirectory != "" {
		if u, err := url.Parse(n.ACMEDirectory); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			p.Errorf("Network.ACMEDirectory", "Network.ACMEDirectory = %q: want the URL of a directory, such as \"https://acme-v02.api.letsencrypt.org/directory\"", n.ACMEDirectory)
		}
	}
	config.Address(p, "Network.ACMEChallengeAddress", "tcp", n.ACMEChallengeAddress)

	if uc.Xbox.Enabled {
		config.File(p, "Xbox.TokenFile", cmp.Or(uc.Xbox.TokenFile, "xbox-token.json"))
	}
	if config.Address(p, "Proxy.Address", "tcp", uc.Proxy.Address) && uc.Proxy.Address != "" && len(uc.Proxy.Secret) < minProxySecret {
		// The secret is not to be printed.
		p.Errorf("Proxy.Secret", "Proxy.Secret: want %d characters or more, as proxies are taken at Proxy.Address", minProxySecret)
	}

	_, err = parseNameCheck(uc.Server.NameCheck)
	p.Add("Server.NameCheck", err)
	checkProtocols(p, n.Protocols, n.PrimaryProtocol, versions.Parse, versions.NewestPrimary())
	// The caps of connections take 0 for none, and nothing below.
	config.AtLeast(p, "Network.MaxPendingConnections", n.MaxPendingConnections, 0)
	config.AtLeast(p, "Network.MaxPendingConnectionsPerIP", n.MaxPendingConnectionsPerIP, 0)
	config.AtLeast(p, "Network.MaxPendingConnectionsPerNetwork", n.MaxPendingConnectionsPerNetwork, 0)
	config.AtLeast(p, "Network.MaxPendingLogins", n.MaxPendingLogins, 0)
	config.AtLeast(p, "Network.MaxConnectionsPerIP", n.MaxConnectionsPerIP, 0)
	config.ReadFolder(p, "Resources.Folder", uc.Resources.Folder)
}

// parseTransport parses the Network.Transport of a UserConfig: "nethernet",
// which an empty value is too.
func parseTransport(s string) (string, error) {
	transport := strings.ToLower(strings.TrimSpace(s))
	switch transport {
	case "", "nethernet":
		return "nethernet", nil
	case "raknet":
		return "", fmt.Errorf("network transport %q is no longer supported: players join over NetherNet", s)
	}
	return "", fmt.Errorf("unknown network transport %q", s)
}

// loadResources loads all resource packs found in a directory passed.
func loadResources(dir string) ([]*resource.Pack, error) {
	_ = os.MkdirAll(dir, 0777)

	resources, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	packs := make([]*resource.Pack, len(resources))
	for i, entry := range resources {
		packs[i], err = resource.ReadPath(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("compile resource (%v): %w", entry.Name(), err)
		}
	}
	return packs, nil
}

// DefaultConfig returns the default part of config.toml of the adapter.
func DefaultConfig() UserConfig {
	c := UserConfig{}
	c.Network.Address = ":19132"
	c.Network.Transport = "nethernet"
	c.Network.IdentityFile = "server-key.pem"
	c.Xbox.TokenFile = "xbox-token.json"
	c.Xbox.AcceptFriends = true
	c.Network.EnableQuery = false
	c.Network.QueryPlugins = false
	c.Network.Protocols = []string{versions.Default().Version}
	c.Network.PrimaryProtocol = versions.Default().Version
	c.Network.MaxPendingConnections = 256
	c.Network.MaxPendingConnectionsPerIP = 4
	c.Network.MaxPendingConnectionsPerNetwork = 32
	c.Network.MaxPendingLogins = 64
	c.Network.MaxConnectionsPerIP = 8
	c.Server.AuthEnabled = true
	c.Server.NameCheck = "lenient"
	c.Resources.AutoBuildPack = true
	c.Resources.Folder = "resources"
	c.Resources.Required = false
	return c
}
