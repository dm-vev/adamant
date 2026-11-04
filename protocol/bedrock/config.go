package bedrockadapter

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/df-mc/dragonfly/protocol/bedrock/query"
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

// Config converts a UserConfig to a Config. An error is returned if the
// transport is not NetherNet, the name check is unknown or loading
// resources failed. It sets whether
// queries are answered, and with plugins, as the [Network] table says.
func (uc UserConfig) Config(log *slog.Logger) (Config, error) {
	transport := strings.ToLower(strings.TrimSpace(uc.Network.Transport))
	if transport == "" {
		transport = "nethernet"
	}
	if transport == "raknet" {
		return Config{}, fmt.Errorf("network transport %q is no longer supported: players join over NetherNet", uc.Network.Transport)
	}
	if transport != "nethernet" {
		return Config{}, fmt.Errorf("unknown network transport %q", uc.Network.Transport)
	}
	uc.Network.Transport = transport
	nameCheck, err := parseNameCheck(uc.Server.NameCheck)
	if err != nil {
		return Config{}, err
	}
	query.SetEnabled(uc.Network.EnableQuery)
	query.SetPluginListingEnabled(uc.Network.QueryPlugins)

	c := Config{
		Network:                 uc.Network,
		Xbox:                    uc.Xbox,
		Proxy:                   uc.Proxy,
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
	c.Server.AuthEnabled = true
	c.Server.NameCheck = "lenient"
	c.Resources.AutoBuildPack = true
	c.Resources.Folder = "resources"
	c.Resources.Required = false
	return c
}
