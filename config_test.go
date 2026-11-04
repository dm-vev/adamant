package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/protocol/bedrock/query"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/google/uuid"
	"github.com/pelletier/go-toml"
)

// The tests of this file hold config.toml to its layout and its meaning:
// whichever part of the program reads a key, a new default file has the
// bytes it always had, and a file means what it always meant. The bytes and
// the meanings below were recorded from the readConfig that read the file
// into a single server.UserConfig. The exceptions are the keys added since:
// IdentityKey, whose line a new default file has, and a file that lacks it,
// with AuthEnabled = true, now keys players by XUID; SaveChunks of [World],
// whose line a new default file has, and a file that lacks it saves the
// chunks that changed, as the server did before the key; Protocols and
// PrimaryProtocol of [Network], whose lines a new default file has, and a
// file that lacks them allows and speaks protocol 2193, as before;
// StrictConfig of [Server], whose line a new default file has, and a file
// that lacks it warns of the keys nothing reads, which the server ignored
// before; the connection caps of [Network], whose lines a new default file
// has, and a file that lacks them takes their defaults, where the server had
// the NetherNet defaults and no other caps before; and SpawnTimeout and
// SpawnTimeoutMessage of [Server], whose lines a new default file has, and a
// file that lacks them gives a client a minute to spawn, where it had for
// ever before; and LogLevel of [Server], whose line a new default file has,
// and a file that lacks it logs at info, where the program logged at debug
// before.

// TestDefaultConfigFile checks that readConfig writes the default file, when
// there is none, as testdata/config/default.toml holds it.
func TestDefaultConfigFile(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "config", "default.toml"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	conf, err := readConfig(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	closeProviders(t, conf)
	got, err := os.ReadFile("config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("default config.toml:\n%s\nwant:\n%s", got, want)
	}
	f := defaultFileConfig()
	if merged, err := marshalConfig(f.parts()...); err != nil || !bytes.Equal(merged, want) {
		t.Errorf("default parts marshal to:\n%s\n%v, want:\n%s", merged, err, want)
	}
}

// TestMarshalConfigMergesParts checks that parts with keys of the same table
// are written as go-toml writes one struct that holds all their keys, and that two parts
// may not write the same key.
func TestMarshalConfigMergesParts(t *testing.T) {
	type a struct {
		Server struct{ Name, Zone string }
		World  struct{ Folder string }
	}
	type b struct {
		Server struct{ Auth bool }
		Java   struct{ Address string }
	}
	type whole struct {
		Java   struct{ Address string }
		Server struct {
			Name, Zone string
			Auth       bool
		}
		World struct{ Folder string }
	}
	var pa a
	var pb b
	var w whole
	pa.Server.Name, pa.Server.Zone, pa.World.Folder, pb.Server.Auth, pb.Java.Address = "n", "z", "f", true, ":1"
	w.Server.Name, w.Server.Zone, w.World.Folder, w.Server.Auth, w.Java.Address = "n", "z", "f", true, ":1"
	got, err := marshalConfig(&pa, &pb)
	if err != nil {
		t.Fatal(err)
	}
	want, err := toml.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("merged parts:\n%s\nwant:\n%s", got, want)
	}
	if _, err := marshalConfig(&pa, &pa); err == nil || err.Error() != "two parts of the config write Server.Name" {
		t.Errorf("a key two parts write: error %v", err)
	}
}

// TestConfigFileKeepsItsMeaning reads the config.toml of the repository and
// a file that sets every key to a value other than its default, and checks
// every setting the server and its listeners take from them.
func TestConfigFileKeepsItsMeaning(t *testing.T) {
	checkedIn, err := os.ReadFile("config.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		file []byte
		want configMeaning
	}{
		{"checked in", checkedIn, configMeaning{
			"Name": "Dragonfly Server", "AuthDisabled": false, "MuteEmoteChat": false, "NameCheck": "lenient", "IdentityKey": "xuid",
			"ReconnectPolicy": server.ReconnectKickExisting, "JoinMessage": true, "QuitMessage": true,
			"SpawnTimeout": "1m0s", "SpawnTimeoutMessage": server.DefaultSpawnTimeoutMessage, "LogLevel": "INFO",
			"MaxPlayers": 0, "MaxChunkRadius": 32,
			"Resources": 0, "ResourcesRequired": false, "DisableResourceBuilding": false,
			"OverworldSeed": int64(0), "GeneratorProfile": "auto", "GeneratorWorkers": 0, "GeneratorQueueSize": 0, "SaveInterval": "5m0s",
			"Regions": false, "DisableOverworld": false, "DisableNether": false, "DisableEnd": false,
			"DefaultDimension": "Overworld", "PortalDisabledMessage": "The %s dimension is disabled on this server.", "SaveChunks": "modified",
			"WorldProvider": "*mcdb.DB", "WorldFolder": true, "PlayerProvider": "*playerdb.Provider", "PlayerFolder": true,
			"WhitelistFile": "white-list.txt", "Stranger": "allowed",
			"Query": false, "QueryPlugins": false,
			"Network.Address": ":19132", "Network.IdentityFile": "server-key.pem", "Network.EnableQuery": false,
			"Network.LAN": false, "Network.HTTPSDomain": "", "Network.ACMEEmail": "", "Network.ACMECacheDir": "",
			"Network.ACMEDirectory": "", "Network.ACMEChallengeAddress": "",
			"Network.Protocols": []int32{2193}, "Network.PrimaryProtocol": int32(2193),
			"Network.MaxPendingConnections": 256, "Network.MaxPendingConnectionsPerIP": 4, "Network.MaxPendingConnectionsPerNetwork": 32,
			"Network.MaxPendingLogins": 64, "Network.MaxConnectionsPerIP": 8,
			"Xbox.Enabled": false, "Xbox.TokenFile": "xbox-token.json", "Xbox.AcceptFriends": true, "Xbox.LegacySignaling": false,
			"Proxy.Address": "", "Proxy.Secret": "",
			"Java.Address": "", "Java.Message": "",
		}},
		{"every key changed", []byte(everyKeyChanged), configMeaning{
			"Name": "Changed", "AuthDisabled": true, "MuteEmoteChat": true, "NameCheck": "strict", "IdentityKey": "name",
			"ReconnectPolicy": server.ReconnectKickJoining, "JoinMessage": false, "QuitMessage": false,
			"SpawnTimeout": "15s", "SpawnTimeoutMessage": "Spawn faster.", "LogLevel": "WARN",
			"MaxPlayers": 7, "MaxChunkRadius": 9,
			"Resources": 0, "ResourcesRequired": true, "DisableResourceBuilding": true,
			"OverworldSeed": int64(42), "GeneratorProfile": "many-cores", "GeneratorWorkers": 3, "GeneratorQueueSize": 5, "SaveInterval": "1m30s",
			"Regions": true, "DisableOverworld": true, "DisableNether": true, "DisableEnd": false,
			"DefaultDimension": "End", "PortalDisabledMessage": "Closed.", "SaveChunks": "all",
			"WorldProvider": "<nil>", "WorldFolder": false, "PlayerProvider": "<nil>", "PlayerFolder": false,
			"WhitelistFile": "allow.txt", "Stranger": "Members only.",
			"Query": true, "QueryPlugins": true,
			"Network.Address": "127.0.0.1:19140", "Network.IdentityFile": "id.pem", "Network.EnableQuery": true,
			"Network.LAN": true, "Network.HTTPSDomain": "play.example.invalid", "Network.ACMEEmail": "ops@example.invalid",
			"Network.ACMECacheDir": "certs", "Network.ACMEDirectory": "https://acme.example.invalid/directory",
			"Network.ACMEChallengeAddress": ":8080",
			"Network.Protocols":            []int32{2193}, "Network.PrimaryProtocol": int32(2193),
			"Network.MaxPendingConnections": 100, "Network.MaxPendingConnectionsPerIP": 2, "Network.MaxPendingConnectionsPerNetwork": 0,
			"Network.MaxPendingLogins": 10, "Network.MaxConnectionsPerIP": 3,
			"Xbox.Enabled": true, "Xbox.TokenFile": "token.json", "Xbox.AcceptFriends": false, "Xbox.LegacySignaling": true,
			"Proxy.Address": "127.0.0.1:19133", "Proxy.Secret": "0123456789abcdef0123456789abcdef",
			"Java.Address": ":25565", "Java.Message": "Use Bedrock.",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := loadMeaning(t, tc.file)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				var keys []string
				for k := range tc.want {
					keys = append(keys, k)
				}
				for k := range got {
					if _, ok := tc.want[k]; !ok {
						keys = append(keys, k)
					}
				}
				sort.Strings(keys)
				for _, k := range keys {
					if !reflect.DeepEqual(got[k], tc.want[k]) {
						t.Errorf("%s = %#v, want %#v", k, got[k], tc.want[k])
					}
				}
			}
		})
	}
}

// TestConfigFileErrors checks the errors of files the server refuses: a
// value of the wrong type in a key of the server and in one of a listener,
// and the transports it no longer or never knew.
func TestConfigFileErrors(t *testing.T) {
	for file, want := range map[string]string{
		"[Server]\nName = 5\n":                                    "decode config: (2, 1): Can't convert 5(int64) to string",
		"[Network]\nLAN = \"yes\"\n":                              "decode config: (2, 1): Can't convert yes(string) to bool",
		"[Network]\nTransport = \"raknet\"\n":                     `line 2: Network.Transport: network transport "raknet" is no longer supported: players join over NetherNet`,
		"[Network]\nTransport = \"tcp\"\n":                        `line 2: Network.Transport: unknown network transport "tcp"`,
		"[Server]\nNameCheck = \"xbox\"\n":                        `line 2: Server.NameCheck: unknown name check "xbox": want "lenient", "game" or "strict"`,
		"[Server]\nIdentityKey = \"uuid\"\n":                      `line 2: Server.IdentityKey: unknown IdentityKey "uuid": use "auto", "xuid" or "name"`,
		"[Server]\nAuthEnabled = false\nIdentityKey = \"xuid\"\n": `line 3: IdentityKey "xuid" needs AuthEnabled = true: without Xbox Live sign-in, the XUID of a player is what its client claims`,
		"[World]\nSaveChunks = \"changed\"\n":                     `line 2: World.SaveChunks: unknown SaveChunks "changed": use "modified" or "all"`,
		"[World]\nSaveInterval = \"often\"\n":                     `line 2: World.SaveInterval = "often": want a duration such as "90s" or "5m"`,
		"[Network]\nProtocols = [\"1.26.40\"]\n":                  `line 2: Network.Protocols: protocol "1.26.40" is not supported: this build supports 1.26.50 (26.50–26.52)`,
		"[Network]\nPrimaryProtocol = \"26.70\"\n":                `line 2: Network.PrimaryProtocol: protocol "26.70" is not supported: this build supports 1.26.50 (26.50–26.52)`,
		"[Network]\nProtocols = \"1.26.50\"\n":                    "decode config: (2, 1): Can't convert 1.26.50(string) to []string(slice)",
		"[Server]\nSpawnTimeout = \"soon\"\n":                     `line 2: Server.SpawnTimeout = "soon": want a duration such as "90s" or "5m"`,
		"[Server]\nLogLevel = \"loud\"\n":                         `line 2: Server.LogLevel: unknown LogLevel "loud": use "debug", "info", "warn" or "error"`,
	} {
		_, err := loadMeaning(t, []byte(file))
		if err == nil || err.Error() != want {
			t.Errorf("%q: error %v, want %q", file, err, want)
		}
	}
}

// TestIdentityKeyResolves checks how compose resolves IdentityKey with
// AuthEnabled, and what it logs: "auto" keys by XUID with sign-in and by
// name without, in any case and with spaces around, and name mode warns.
func TestIdentityKeyResolves(t *testing.T) {
	for _, tc := range []struct {
		file, want, log string
	}{
		{"[Server]\nAuthEnabled = true\n", "xuid", `level=INFO msg="Players are keyed by their Xbox accounts (IdentityKey = \"xuid\")."`},
		{"[Server]\nAuthEnabled = false\n", "name", `level=WARN msg="Xbox Live sign-in is off (AuthEnabled = false): players are keyed by name, and anyone can join as any name, an operator's included. Use this only on a network you trust."`},
		{"[Server]\nAuthEnabled = false\nIdentityKey = \" Name \"\n", "name", "sign-in is off"},
		{"[Server]\nIdentityKey = \"XUID\"\n", "xuid", "keyed by their Xbox accounts"},
		{"[Server]\nIdentityKey = \"\"\n", "xuid", "keyed by their Xbox accounts"},
		{"[Server]\nAuthEnabled = true\nIdentityKey = \"name\"\n", "name", `level=WARN msg="IdentityKey = \"name\": a player who renames their Xbox account loses operator status, leaves the whitelist and escapes a ban by name."`},
	} {
		t.Chdir(t.TempDir())
		if err := os.WriteFile("config.toml", []byte(tc.file), 0644); err != nil {
			t.Fatal(err)
		}
		var logged bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		}}))
		f, err := readConfigFile("config.toml")
		if err != nil {
			t.Fatal(err)
		}
		conf, _, _, err := f.compose(log)
		if err != nil {
			t.Fatalf("%q: %v", tc.file, err)
		}
		closeProviders(t, conf)
		if got := conf.IdentityKey.String(); got != tc.want {
			t.Errorf("%q: IdentityKey %s, want %s", tc.file, got, tc.want)
		}
		if !strings.Contains(logged.String(), tc.log) {
			t.Errorf("%q: logged\n%s\nwant a line with %s", tc.file, logged.String(), tc.log)
		}
	}
}

// everyKeyChanged is a config.toml with every key set to a value other than
// its default.
const everyKeyChanged = `
[Java]
  Address = ":25565"
  Message = "Use Bedrock."

[Network]
  ACMECacheDir = "certs"
  ACMEChallengeAddress = ":8080"
  ACMEDirectory = "https://acme.example.invalid/directory"
  ACMEEmail = "ops@example.invalid"
  Address = "127.0.0.1:19140"
  EnableQuery = true
  HTTPSDomain = "play.example.invalid"
  IdentityFile = "id.pem"
  LAN = true
  MaxConnectionsPerIP = 3
  MaxPendingConnections = 100
  MaxPendingConnectionsPerIP = 2
  MaxPendingConnectionsPerNetwork = 0
  MaxPendingLogins = 10
  PrimaryProtocol = "2193"
  Protocols = ["26.52"]
  QueryPlugins = true
  Transport = " NetherNet "

[Players]
  Folder = "people"
  MaxCount = 7
  MaximumChunkRadius = 9
  SaveData = false

[Proxy]
  Address = "127.0.0.1:19133"
  Secret = "0123456789abcdef0123456789abcdef"

[Resources]
  AutoBuildPack = false
  Folder = "packs"
  Required = true

[Server]
  AuthEnabled = false
  DisableJoinQuitMessages = true
  IdentityKey = "name"
  LogLevel = "Warn"
  MuteEmoteChat = true
  Name = "Changed"
  NameCheck = "strict"
  ReconnectPolicy = "kick-joining"
  SpawnTimeout = "15s"
  SpawnTimeoutMessage = "Spawn faster."
  StrictConfig = true

[Whitelist]
  Enabled = true
  File = "allow.txt"
  Reason = "Members only."

[World]
  DefaultDimension = "end"
  DisableEnd = false
  DisableNether = true
  DisableOverworld = true
  Folder = "realm"
  GeneratorProfile = "many-cores"
  GeneratorQueueSize = 5
  GeneratorWorkers = 3
  PortalDisabledMessage = "Closed."
  Regions = true
  SaveChunks = " All "
  SaveData = false
  SaveInterval = "90s"
  Seed = 42

[Xbox]
  AcceptFriends = false
  Enabled = true
  LegacySignaling = true
  TokenFile = "token.json"
`

// configMeaning is what a config.toml means: every setting the server and
// its listeners take from it, by name.
type configMeaning map[string]any

// serverMeaning returns the settings of conf, the Config readConfig
// composed, in a directory where it was read alone.
func serverMeaning(t *testing.T, conf server.Config, log *slog.Logger) configMeaning {
	t.Helper()
	if conf.Log != log {
		t.Error("the Config does not log to the logger passed")
	}
	if len(conf.Listeners) == 0 {
		t.Error("the Config has no listener")
	}
	// The stranger signed in with Xbox Live, as a server keyed by XUID
	// refuses any other.
	stranger := "allowed"
	if msg, ok := conf.Allower.Allow(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, client.Identity{Name: "Stranger", UUID: uuid.New(), XUID: "2535400000000999", Authenticated: true}, client.Profile{}); !ok {
		stranger = msg
	}
	whitelist := ""
	for _, name := range []string{"white-list.txt", "allow.txt"} {
		if _, err := os.Stat(name); err == nil {
			whitelist = name
		}
	}
	return configMeaning{
		"Name": conf.Name, "IdentityKey": conf.IdentityKey.String(),
		"ReconnectPolicy": conf.ReconnectPolicy, "JoinMessage": !conf.JoinMessage.Zero(), "QuitMessage": !conf.QuitMessage.Zero(),
		"SpawnTimeout": conf.SpawnTimeout.String(), "SpawnTimeoutMessage": conf.SpawnTimeoutMessage,
		"MaxPlayers": conf.MaxPlayers, "MaxChunkRadius": conf.MaxChunkRadius,
		"OverworldSeed": conf.OverworldSeed, "GeneratorProfile": conf.GeneratorProfile.String(),
		"GeneratorWorkers": conf.GeneratorWorkers, "GeneratorQueueSize": conf.GeneratorQueueSize, "SaveInterval": conf.SaveInterval.String(),
		"Regions": conf.Regions, "DisableOverworld": conf.DisableOverworld, "DisableNether": conf.DisableNether, "DisableEnd": conf.DisableEnd,
		"DefaultDimension": fmt.Sprint(conf.DefaultDimension), "PortalDisabledMessage": conf.PortalDisabledMessage, "SaveChunks": conf.SaveChunks.String(),
		"WorldProvider": fmt.Sprintf("%T", conf.WorldProvider), "WorldFolder": exists("world") || exists("realm"),
		"PlayerProvider": fmt.Sprintf("%T", conf.PlayerProvider), "PlayerFolder": exists("players") || exists("people"),
		"WhitelistFile": whitelist, "Stranger": stranger,
		"Query": query.Enabled(), "QueryPlugins": query.PluginListingEnabled(),
	}
}

// exists reports whether a file of the name passed exists.
func exists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// closeProviders closes the world and player providers of conf.
func closeProviders(t *testing.T, conf server.Config) {
	t.Helper()
	for _, p := range []any{conf.WorldProvider, conf.PlayerProvider} {
		if c, ok := p.(io.Closer); ok && p != nil {
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		}
	}
}

// loadMeaning reads the config.toml passed, in a directory of its own, as
// readConfig reads it, and returns its meaning.
func loadMeaning(t *testing.T, data []byte) (configMeaning, error) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.toml", data, 0644); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	f, err := readConfigFile("config.toml")
	if err != nil {
		return nil, err
	}
	conf, bc, jc, err := f.compose(log)
	if err != nil {
		return nil, err
	}
	m := serverMeaning(t, conf, log)
	level, _ := f.Log.level()
	m["LogLevel"] = level.String()
	closeProviders(t, conf)
	// The settings of the listeners are those of the Configs of the adapters.
	m["AuthDisabled"], m["MuteEmoteChat"] = bc.AuthDisabled, bc.MuteEmoteChat
	m["NameCheck"] = map[bedrockadapter.NameCheck]string{
		bedrockadapter.NameCheckLenient: "lenient", bedrockadapter.NameCheckGame: "game", bedrockadapter.NameCheckStrict: "strict",
	}[bc.NameCheck]
	m["Resources"], m["ResourcesRequired"], m["DisableResourceBuilding"] = len(bc.Resources), bc.ResourcesRequired, bc.DisableResourceBuilding
	n, x, p, j := bc.Network, bc.Xbox, bc.Proxy, jc
	m["Network.Address"], m["Network.IdentityFile"], m["Network.EnableQuery"] = n.Address, n.IdentityFile, n.EnableQuery
	m["Network.LAN"], m["Network.HTTPSDomain"], m["Network.ACMEEmail"], m["Network.ACMECacheDir"] = n.LAN, n.HTTPSDomain, n.ACMEEmail, n.ACMECacheDir
	m["Network.ACMEDirectory"], m["Network.ACMEChallengeAddress"] = n.ACMEDirectory, n.ACMEChallengeAddress
	var protocols []int32
	for _, p := range bc.Protocols {
		protocols = append(protocols, p.Number)
	}
	m["Network.Protocols"], m["Network.PrimaryProtocol"] = protocols, bc.Primary.Number
	m["Network.MaxPendingConnections"], m["Network.MaxPendingConnectionsPerIP"] = n.MaxPendingConnections, n.MaxPendingConnectionsPerIP
	m["Network.MaxPendingConnectionsPerNetwork"], m["Network.MaxPendingLogins"] = n.MaxPendingConnectionsPerNetwork, n.MaxPendingLogins
	m["Network.MaxConnectionsPerIP"] = n.MaxConnectionsPerIP
	m["Xbox.Enabled"], m["Xbox.TokenFile"], m["Xbox.AcceptFriends"], m["Xbox.LegacySignaling"] = x.Enabled, x.TokenFile, x.AcceptFriends, x.LegacySignaling
	m["Proxy.Address"], m["Proxy.Secret"] = p.Address, p.Secret
	m["Java.Address"], m["Java.Message"] = j.Address, j.Message
	return m, nil
}

// TestLogLevel checks how LogLevel of [Server] reads: in any case, with
// spaces around, info if empty.
func TestLogLevel(t *testing.T) {
	for value, want := range map[string]slog.Level{
		"": slog.LevelInfo, "info": slog.LevelInfo, " DEBUG ": slog.LevelDebug, "Warn": slog.LevelWarn,
		"warning": slog.LevelWarn, "error": slog.LevelError,
	} {
		var c logConfig
		c.Server.LogLevel = value
		if got, err := c.level(); err != nil || got != want {
			t.Errorf("LogLevel %q = %v, %v, want %v", value, got, err, want)
		}
	}
}

// TestDefaultLogLevelHidesDebug checks that readConfig of the default file
// sets the default logger of slog to info, which leaves out the debug
// lines, as of packets the server has no handler for, and that a file of
// LogLevel = "debug" logs them.
func TestDefaultLogLevelHidesDebug(t *testing.T) {
	old := slog.SetLogLoggerLevel(slog.LevelDebug)
	t.Cleanup(func() { slog.SetLogLoggerLevel(old) })
	t.Chdir(t.TempDir())
	conf, err := readConfig(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	closeProviders(t, conf)
	if slog.Default().Enabled(t.Context(), slog.LevelDebug) || !slog.Default().Enabled(t.Context(), slog.LevelInfo) {
		t.Fatal("the default config.toml does not log at info")
	}
	if !conf.Log.Enabled(t.Context(), slog.LevelInfo) || conf.Log.Enabled(t.Context(), slog.LevelDebug) {
		t.Fatal("the logger of the server does not log at info")
	}

	data, err := os.ReadFile("config.toml")
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`LogLevel = "info"`), []byte(`LogLevel = "debug"`), 1)
	if err := os.WriteFile("config.toml", data, 0644); err != nil {
		t.Fatal(err)
	}
	conf, err = readConfig(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	closeProviders(t, conf)
	if !conf.Log.Enabled(t.Context(), slog.LevelDebug) {
		t.Fatal(`LogLevel = "debug" does not log debug lines`)
	}
}
