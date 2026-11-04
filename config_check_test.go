package main

import (
	"bytes"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/config"
	"github.com/pelletier/go-toml"
)

// The tests of this file hold readConfigFile to its checks of config.toml:
// the keys and tables no part reads, the bounds of the values, and the one
// report of all the problems of a file.

// readCheckedConfig writes the config.toml passed in a directory of its own
// and reads it as readConfig does, returning the error and what compose
// logs.
func readCheckedConfig(t *testing.T, file string) (string, error) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.toml", []byte(file), 0644); err != nil {
		t.Fatal(err)
	}
	f, err := readConfigFile("config.toml")
	if err != nil {
		return "", err
	}
	var logged bytes.Buffer
	conf, _, _, err := f.compose(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if err != nil {
		t.Fatalf("%q: compose: %v", file, err)
	}
	closeProviders(t, conf)
	return logged.String(), nil
}

// TestConfigUnknownKeys checks that the keys and the tables no part reads
// are warned of once each, with their lines, and refused with StrictConfig.
func TestConfigUnknownKeys(t *testing.T) {
	const file = `[Players]
  MaxChunkRaduis = 9
  maxcount = 3

[AntiCheet]
  Enabled = true
  Mode = "kick"

[World]
  folder = "realm"
  [World.Extra]
    Key = 1

[[Hooks]]
  Name = "a"
[[Hooks]]
  Name = "b"
`
	want := []string{
		"line 2: unknown key Players.MaxChunkRaduis: nothing reads it, did you mean MaximumChunkRadius?",
		"line 5: unknown table AntiCheet: nothing reads it, did you mean AntiCheat?",
		"line 11: unknown table World.Extra: nothing reads it",
		"line 14: unknown table Hooks: nothing reads it",
	}
	logged, err := readCheckedConfig(t, file)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for l := range strings.Lines(logged) {
		lines = append(lines, l)
	}
	if len(lines) != len(want) {
		t.Errorf("logged %d lines, want %d:\n%s", len(lines), len(want), logged)
	}
	for _, w := range want {
		if !strings.Contains(logged, `level=WARN msg="config.toml: `+w+`"`) {
			t.Errorf("logged\n%s\nwant a warning %q", logged, w)
		}
	}

	_, err = readCheckedConfig(t, "[Server]\n  StrictConfig = true\n"+file)
	if err == nil {
		t.Fatal("strict: no error")
	}
	wantErr := "config.toml has 4 problems:"
	for _, w := range want {
		line, rest, _ := strings.Cut(strings.TrimPrefix(w, "line "), ":")
		n, _ := strconv.Atoi(line)
		wantErr += "\n\tline " + strconv.Itoa(n+2) + ":" + rest
	}
	if err.Error() != wantErr {
		t.Errorf("strict: error\n%v\nwant\n%s", err, wantErr)
	}
}

// TestConfigKnownKeysAgreeWithGoToml checks that the keys scanKeys finds no
// part reads are those go-toml leaves undecoded in every part: the keys the
// strict decoder of go-toml reports for each part, that no other part
// decodes.
func TestConfigKnownKeysAgreeWithGoToml(t *testing.T) {
	for _, file := range []string{
		everyKeyChanged,
		"[Players]\nMaxChunkRaduis = 9\nmaxcount = 3\nMAXIMUMCHUNKRADIUS = 4\n[Server]\nname = \"x\"\nauthEnabled = true\nZone = 1\n",
		"[Nope]\nA = 1\n[Nope.Deeper]\nB = 2\n[World]\nFolder = \"w\"\nfolder = \"v\"\n",
		"Top = 1\n[Java]\nAddress = \":1\"\nPort = 2\n",
	} {
		tree, err := toml.Load(file)
		if err != nil {
			t.Fatal(err)
		}
		f := defaultFileConfig()
		var undecoded map[string]bool
		for _, p := range f.parts() {
			keys := map[string]bool{}
			if err := toml.NewDecoder(strings.NewReader(file)).Strict(true).Decode(p); err != nil {
				list, ok := strings.CutPrefix(err.Error(), "undecoded keys: [")
				if !ok {
					t.Fatalf("%q: %v", file, err)
				}
				for _, k := range strings.Fields(strings.TrimSuffix(list, "]")) {
					k, err := strconv.Unquote(k)
					if err != nil {
						t.Fatal(err)
					}
					keys[k] = true
				}
			}
			if undecoded == nil {
				undecoded = keys
			}
			for k := range undecoded {
				if !keys[k] {
					delete(undecoded, k)
				}
			}
		}
		var want []string
		for k := range undecoded {
			want = append(want, k)
		}
		slices.Sort(want)

		// go-toml names the keys of an unknown table one by one, which
		// scanKeys names by their table.
		var got []string
		for _, pr := range scanKeys(tree, f.parts()).unknown {
			msg := strings.TrimPrefix(pr.Message, "unknown ")
			kind, rest, _ := strings.Cut(msg, " ")
			key, _, _ := strings.Cut(rest, ":")
			if kind == "key" {
				got = append(got, key)
				continue
			}
			for k := range undecoded {
				if strings.HasPrefix(k, key+".") {
					got = append(got, k)
				}
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%q: unknown keys %q, want those go-toml decodes in no part, %q", file, got, want)
		}
	}
}

// TestConfigBounds checks that each bound of a key refuses the values just
// outside it and takes those at its edges, and that the values the server
// falls back from are warned of, and refused with StrictConfig.
func TestConfigBounds(t *testing.T) {
	type value struct {
		toml string
		// want is the problem with the value, empty if it is taken.
		want string
	}
	for _, tc := range []struct {
		table, key string
		values     []value
		// warning is set if the problems are warnings.
		warning bool
		// setup prepares the directory the file is read in.
		setup func(t *testing.T)
	}{
		{table: "Permissions", key: "OperatorLevel", values: []value{
			{"-1", "Permissions.OperatorLevel = -1: want 0 to 4"}, {"0", ""}, {"4", ""},
			{"5", "Permissions.OperatorLevel = 5: want 0 to 4"},
		}},
		{table: "Permissions", key: "DefaultLevel", values: []value{
			{"0", ""}, {"4", ""}, {"5", "Permissions.DefaultLevel = 5: want 0 to 4"},
		}},
		{table: "Permissions", key: "CommandBlockLevel", values: []value{
			{"0", ""}, {"2", ""}, {"-1", "Permissions.CommandBlockLevel = -1: want 0 to 4"},
		}},
		{table: "Permissions", key: "RoleLevel", values: []value{
			{"3", ""}, {"7", "Permissions.RoleLevel = 7: want 0 to 4"},
		}},
		{table: "Permissions", key: "DefaultRole", values: []value{
			{`"member"`, ""}, {`" Visitor "`, ""}, {`""`, ""},
			{`"operator"`, `Permissions.DefaultRole = "operator": want "member" or "visitor"`},
		}},
		{table: "Bans", key: "Message", values: []value{
			{`"Gone. {reason}"`, ""}, {`""`, "Bans.Message is empty: want the text a banned player is refused with"},
			{`"Gone {why}"`, "Bans.Message holds {why}: want only {reason}, {source}, {created}, {expires}"},
		}},
		{table: "Bans", key: "TimeFormat", values: []value{
			{`"2006-01-02"`, ""}, {`"yesterday"`, `Bans.TimeFormat = "yesterday": want a layout of Go's package time, such as "2006-01-02 at 15:04:05 MST"`},
		}},
		{table: "Audit", key: "MaxSize", values: []value{
			{`"64MiB"`, ""}, {`"1GB"`, ""}, {`"0"`, ""}, {`"512"`, ""}, {`" 2 gib "`, ""},
			{`"big"`, `Audit.MaxSize = "big": want a size such as "512MiB" or "1GiB"`},
			{`"64Mb/s"`, `Audit.MaxSize = "64Mb/s": want a size such as "512MiB" or "1GiB"`},
			{`"-1MiB"`, `Audit.MaxSize = "-1MiB": want a size such as "512MiB" or "1GiB"`},
		}},
		{table: "Audit", key: "Keep", values: []value{
			{"0", ""}, {"10", ""}, {"-1", "Audit.Keep = -1: want 0 or more"},
		}},
		{table: "Audit", key: "MinLevel", values: []value{
			{"0", ""}, {"4", ""}, {"5", "Audit.MinLevel = 5: want 0 to 4"},
		}},
		{table: "Audit", key: "File", values: []value{
			{`"audit.log"`, ""}, {`"keys"`, `Audit.File = "keys": it is a folder, want a file`},
		}, setup: func(t *testing.T) { mkdir(t, "keys") }},
		{table: "Permissions.Commands", key: "kick", values: []value{
			{"0", ""}, {"4", ""}, {"5", "Permissions.Commands.kick = 5: want 0 to 4"},
		}},
		{table: "Players", key: "MaxCount", values: []value{
			{"-1", "Players.MaxCount = -1: want 0 or more"}, {"0", ""}, {"1000000", ""},
		}},
		{table: "Players", key: "MaximumChunkRadius", values: []value{
			{"-1", "Players.MaximumChunkRadius = -1: want 0 to 96"}, {"0", ""}, {"96", ""},
			{"97", "Players.MaximumChunkRadius = 97: want 0 to 96"},
		}},
		{table: "World", key: "GeneratorWorkers", values: []value{
			{"-1", ""}, {"0", ""}, {"1024", ""}, {"1025", "World.GeneratorWorkers = 1025: want 1024 or less"},
		}},
		{table: "World", key: "GeneratorQueueSize", values: []value{{"-1", ""}, {"0", ""}, {"1000000000", ""}}},
		{table: "World", key: "SaveInterval", values: []value{
			{`"999ms"`, `World.SaveInterval = "999ms": want 1s or more, or "0s" to save only on /save-all and at stop`},
			{`"1s"`, ""}, {`"0s"`, ""}, {`"-1m"`, ""}, {`""`, ""}, {`"24h"`, ""},
			{`"5"`, `World.SaveInterval = "5": want a duration such as "90s" or "5m"`},
		}},
		{table: "World", key: "GeneratorProfile", values: []value{
			{`"many-cores"`, ""}, {`" Fast-Cores "`, ""}, {`""`, ""},
			{`"slow"`, `World.GeneratorProfile: unknown generator profile "slow": expected "auto", "fast-cores" or "many-cores"`},
		}},
		{table: "World", key: "DefaultDimension", warning: true, values: []value{
			{`"the_end"`, ""}, {`"Nether"`, ""},
			{`"moon"`, `World.DefaultDimension = "moon": want "overworld", "nether" or "end"; players spawn in the overworld`},
		}},
		{table: "Server", key: "ReconnectPolicy", warning: true, values: []value{
			{`"kick-joining"`, ""},
			{`"kick-both"`, `Server.ReconnectPolicy = "kick-both": want "kick-joining" or "kick-existing"; the server kicks the existing player`},
		}},
		{table: "Network", key: "Address", values: []value{
			{`":0"`, ""}, {`":1"`, ""}, {`"127.0.0.1:65535"`, ""}, {`"[::1]:19132"`, ""}, {`""`, ""},
			{`":65536"`, `Network.Address = ":65536": want a host and a port of 0 to 65535, such as ":19132" or "127.0.0.1:19132"`},
			{`"19132"`, `Network.Address = "19132": want a host and a port of 0 to 65535, such as ":19132" or "127.0.0.1:19132"`},
			{`":-1"`, `Network.Address = ":-1": want a host and a port of 0 to 65535, such as ":19132" or "127.0.0.1:19132"`},
		}},
		{table: "Java", key: "Address", values: []value{
			{`":25565"`, ""}, {`"::25565"`, `Java.Address = "::25565": want a host and a port of 0 to 65535, such as ":19132" or "127.0.0.1:19132"`},
		}},
		{table: "Network", key: "ACMEChallengeAddress", values: []value{
			{`":80"`, ""}, {`"80"`, `Network.ACMEChallengeAddress = "80": want a host and a port of 0 to 65535, such as ":19132" or "127.0.0.1:19132"`},
		}},
		{table: "Network", key: "HTTPSDomain", values: []value{
			{`"play.example.invalid"`, ""},
			{`"https://play.example.invalid"`, `Network.HTTPSDomain = "https://play.example.invalid": want a domain alone, such as "play.example.com"`},
			{`"play.example.invalid:443"`, `Network.HTTPSDomain = "play.example.invalid:443": want a domain alone, such as "play.example.com"`},
		}},
		{table: "Network", key: "ACMEDirectory", values: []value{
			{`"https://acme.example.invalid/directory"`, ""},
			{`"acme.example.invalid"`, `Network.ACMEDirectory = "acme.example.invalid": want the URL of a directory, such as "https://acme-v02.api.letsencrypt.org/directory"`},
		}},
		{table: "Network", key: "IdentityFile", values: []value{
			{`"id.pem"`, ""}, {`"keys/id.pem"`, ""},
			{`"missing/id.pem"`, `Network.IdentityFile = "missing/id.pem": there is no folder missing to make it in`},
			{`"keys"`, `Network.IdentityFile = "keys": it is a folder, want a file`},
		}, setup: func(t *testing.T) { mkdir(t, "keys") }},
		{table: "World", key: "Folder", values: []value{
			{`"realm"`, ""}, {`"new/deep/realm"`, ""}, {`"keys"`, ""},
			{`"file.txt"`, `World.Folder = "file.txt": it is a file, want a folder`},
			{`"file.txt/realm"`, `World.Folder = "file.txt/realm": file.txt cannot hold a folder`},
		}, setup: func(t *testing.T) { mkdir(t, "keys"); writeFile(t, "file.txt") }},
		{table: "Whitelist", key: "File", values: []value{
			{`"allow.txt"`, ""}, {`"file.txt"`, ""},
			{`"keys"`, `Whitelist.File = "keys": it is a folder, want a file`},
		}, setup: func(t *testing.T) { mkdir(t, "keys"); writeFile(t, "file.txt") }},
		{table: "Backup", key: "Keep", values: []value{
			{"0", "Backup.Keep = 0: want 1 or more"}, {"1", ""}, {"1000", ""},
		}},
		{table: "Backup", key: "Interval", values: []value{
			{`"59s"`, `Backup.Interval = "59s": want 1m or more, or "0s" to schedule none`},
			{`"-1h"`, `Backup.Interval = "-1h": want 1m or more, or "0s" to schedule none`},
			{`"1m"`, ""}, {`"0s"`, ""}, {`""`, ""}, {`"168h"`, ""},
			{`"daily"`, `Backup.Interval = "daily": want a duration such as "90s" or "5m"`},
		}},
		{table: "Backup", key: "MaxAge", values: []value{
			{`"0s"`, ""}, {`"720h"`, ""}, {`"-1h"`, "Backup.MaxAge = -1h0m0s: want 0s or more"},
		}},
		{table: "Backup", key: "MinFreeSpace", values: []value{
			{`"0"`, ""}, {`"512MiB"`, ""}, {`"1 GB"`, ""}, {`""`, ""},
			{`"lots"`, `Backup.MinFreeSpace = "lots": want a size such as "512MiB" or "1GiB"`},
			{`"-1GiB"`, `Backup.MinFreeSpace = "-1GiB": want a size such as "512MiB" or "1GiB"`},
		}},
		{table: "Backup", key: "Format", values: []value{
			{`"folder"`, ""}, {`" ZIP "`, ""}, {`"tar"`, `Backup.Format = "tar": want "folder" or "zip"`},
		}},
		{table: "Backup", key: "Verify", values: []value{
			{`"record"`, ""}, {`" FULL "`, ""}, {`"off"`, ""},
			{`"deep"`, `Backup.Verify = "deep": want "record", "full" or "off"`},
		}},
		{table: "Backup", key: "Folder", values: []value{
			{`"archive"`, ""}, {`"../backups"`, ""}, {`"worlds"`, ""},
			{`"world"`, `Backup.Folder = "world": it lies inside World.Folder "world", whose copies would hold the backups`},
			{`"world/backups"`, `Backup.Folder = "world/backups": it lies inside World.Folder "world", whose copies would hold the backups`},
			{`"file.txt"`, `Backup.Folder = "file.txt": it is a file, want a folder`},
		}, setup: func(t *testing.T) { writeFile(t, "file.txt") }},
		{table: "Resources", key: "Folder", values: []value{
			{`"packs"`, ""}, {`"file.txt"`, `Resources.Folder = "file.txt": it is a file, want a folder`},
		}, setup: func(t *testing.T) { writeFile(t, "file.txt") }},
	} {
		for _, v := range tc.values {
			file := "[" + tc.table + "]\n  " + tc.key + " = " + v.toml + "\n"
			for _, strict := range []bool{false, true} {
				data := file
				if strict {
					data = "[Server]\n  StrictConfig = true\n" + file
					if tc.table == "Server" {
						data = "[Server]\n  StrictConfig = true\n  " + tc.key + " = " + v.toml + "\n"
					}
				}
				t.Chdir(t.TempDir())
				if tc.setup != nil {
					tc.setup(t)
				}
				if err := os.WriteFile("config.toml", []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
				f, err := readConfigFile("config.toml")
				var warnings []string
				for _, w := range f.warnings {
					warnings = append(warnings, w.String())
				}
				line := strings.Count(data, "\n")
				switch {
				case v.want == "":
					if err != nil || len(warnings) != 0 {
						t.Errorf("%q: error %v, warnings %q, want none", data, err, warnings)
					}
				case tc.warning && !strict:
					if want := "line " + strconv.Itoa(line) + ": " + v.want; err != nil || len(warnings) != 1 || warnings[0] != want {
						t.Errorf("%q: error %v, warnings %q, want the warning %q", data, err, warnings, want)
					}
				default:
					if want := "line " + strconv.Itoa(line) + ": " + v.want; err == nil || err.Error() != want {
						t.Errorf("%q: error %v, want %q", data, err, want)
					}
				}
			}
		}
	}
}

// TestConfigProxySecret checks that proxies are taken with a secret of 32
// characters at least, and that a short one is not printed.
func TestConfigProxySecret(t *testing.T) {
	for secret, want := range map[string]string{
		strings.Repeat("s", 31): "line 3: Proxy.Secret: want 32 characters or more, as proxies are taken at Proxy.Address",
		strings.Repeat("s", 32): "",
	} {
		t.Chdir(t.TempDir())
		file := "[Proxy]\n  Address = \"127.0.0.1:19133\"\n  Secret = \"" + secret + "\"\n"
		if err := os.WriteFile("config.toml", []byte(file), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := readConfigFile("config.toml")
		if got := errString(err); got != want {
			t.Errorf("a secret of %d: error %q, want %q", len(secret), got, want)
		}
	}
}

// TestConfigReportsEveryProblem checks that the problems of a file are
// reported together, in the order of their lines: those of decoding, of
// keys nothing reads with StrictConfig, of bounds and of keys that depend on
// each other.
func TestConfigReportsEveryProblem(t *testing.T) {
	const file = `[Java]
  Address = 25565

[Players]
  MaxCount = -2
  MaximumChunkRadius = 200

[Server]
  AuthEnabled = false
  IdentityKey = "xuid"
  NameCheck = "xbox"
  StrictConfig = true

[World]
  SaveInterval = "1ms"
  Sed = 4
`
	_, err := readCheckedConfig(t, file)
	const want = `config.toml has 7 problems:
	decode config: (2, 3): Can't convert 25565(int64) to string
	line 5: Players.MaxCount = -2: want 0 or more
	line 6: Players.MaximumChunkRadius = 200: want 0 to 96
	line 10: IdentityKey "xuid" needs AuthEnabled = true: without Xbox Live sign-in, the XUID of a player is what its client claims
	line 11: Server.NameCheck: unknown name check "xbox": want "lenient", "game" or "strict"
	line 15: World.SaveInterval = "1ms": want 1s or more, or "0s" to save only on /save-all and at stop
	line 16: unknown key World.Sed: nothing reads it, did you mean Seed?`
	if errString(err) != want {
		t.Errorf("error\n%v\nwant\n%s", err, want)
	}
	if e, ok := err.(*config.Error); !ok || len(e.Problems) != 7 {
		t.Errorf("error %T, want a *config.Error of 7 problems", err)
	}
}

// TestConfigChecksNameKnownKeys checks that the keys the Checks of the parts
// name, for a file of values each of them refuses, are keys of the file, so
// that their problems are reported at their lines.
func TestConfigChecksNameKnownKeys(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "file.txt")
	f := defaultFileConfig()
	data, err := marshalConfig(f.parts()...)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := toml.LoadBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	known := scanKeys(tree, f.parts()).lines

	s, b, j := &f.Server, &f.Bedrock, &f.Java
	s.Server.IdentityKey, s.Server.ReconnectPolicy = "x", "x"
	s.World.Folder, s.World.SaveChunks, s.World.SaveInterval, s.World.GeneratorProfile = "file.txt", "x", "x", "x"
	s.World.GeneratorWorkers, s.World.DefaultDimension = 1<<20, "x"
	s.Players.MaxCount, s.Players.MaximumChunkRadius, s.Players.Folder = -1, -1, "file.txt"
	s.Whitelist.File = "."
	b.Network.Address, b.Network.Transport, b.Network.IdentityFile = "x", "x", "."
	b.Network.HTTPSDomain, b.Network.ACMECacheDir, b.Network.ACMEDirectory, b.Network.ACMEChallengeAddress = "x/", "file.txt", "x", "x"
	b.Xbox.Enabled, b.Xbox.TokenFile = true, "."
	b.Proxy.Address, b.Proxy.Secret = ":1", "x"
	b.Server.NameCheck, b.Resources.Folder = "x", "file.txt"
	j.Java.Address = "x"

	var p config.Problems
	for _, part := range f.parts() {
		if c, ok := part.(interface{ Check(*config.Problems) }); ok {
			c.Check(&p)
		}
	}
	var keys []string
	for _, pr := range p.List() {
		keys = append(keys, pr.Key)
		if _, ok := known[pr.Key]; !ok {
			t.Errorf("%s names the key %q, which the file has not", pr.Message, pr.Key)
		}
	}
	if len(keys) != 24 {
		t.Errorf("%d problems of keys %q, want 24", len(keys), keys)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func mkdir(t *testing.T, name string) {
	t.Helper()
	if err := os.MkdirAll(name, 0755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, name string) {
	t.Helper()
	if err := os.WriteFile(name, nil, 0644); err != nil {
		t.Fatal(err)
	}
}
