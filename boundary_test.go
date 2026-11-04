package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// updateBoundary rewrites the allowlist of TestImportBoundary to exactly the violations that exist now.
var updateBoundary = flag.Bool("update-boundary", false, "rewrite testdata/boundary/allowlist.txt to the current import boundary violations")

const (
	// boundaryAllowlist is the path of the allowlist, relative to the repository root.
	boundaryAllowlist = "testdata/boundary/allowlist.txt"

	rootModule = "github.com/df-mc/dragonfly"
	netModule  = "github.com/dm-vev/adamant/net"
)

// boundaryHeader starts the allowlist file.
const boundaryHeader = `# Import boundary violations that still exist.
#
# Each line is "<file> <import path>": a file, relative to the repository root and written with slashes,
# followed by an import that the rules of boundary_test.go (see docs/protocol-boundary.md) forbid it.
#
# This list is a ratchet. It may only shrink as the protocol/game separation proceeds: an entry that is no
# longer a violation fails the test until it is removed. Adding an entry needs a justification in review.
#
# Regenerate it with:
#
#	go test -run TestImportBoundary -update-boundary .
#
# Blank lines and lines starting with # are ignored. Entries are sorted.
`

// under reports if p is dir or lies below dir. It matches whole path segments, so that "server/sessionfoo" is not
// below "server/session".
func under(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+"/")
}

// violation applies the import boundary rules to a single import. file is the slash-separated path of the
// importing file relative to the repository root. If the import breaks a rule, violation returns the name of the
// rule and true.
//
//   - R1: the game knows no protocol. Files under server/ import neither net/ (apart from the neutral net/nbt
//     and net/metrics) nor protocol/.
//   - R2: adapters stay apart. A file under protocol/<a>/ imports no protocol/<b> with b other than a.
//   - R3: protocol libraries know no game. Files under net/ import nothing from the root module.
func violation(file, importPath string) (rule string, bad bool) {
	switch {
	case under(file, "server"):
		switch {
		case under(importPath, netModule) && !under(importPath, netModule+"/nbt") && !under(importPath, netModule+"/metrics"):
			return "R1", true
		case under(importPath, rootModule+"/protocol"):
			return "R1", true
		}
	case under(file, "protocol"):
		parts := strings.Split(file, "/")
		if len(parts) < 3 {
			// A file directly in protocol/ belongs to no adapter.
			return "", false
		}
		own := rootModule + "/protocol/" + parts[1]
		if under(importPath, rootModule+"/protocol") && !under(importPath, own) {
			// The bare protocol package itself is not an adapter.
			if importPath == rootModule+"/protocol" {
				return "", false
			}
			return "R2", true
		}
	case under(file, "net"):
		if under(importPath, rootModule) {
			return "R3", true
		}
	}
	return "", false
}

// ruleHints holds a one-line hint for each rule.
var ruleHints = map[string]string{
	"R1": "the game core must not depend on a protocol: move the code to the adapter or use a neutral contract (net/nbt and net/metrics are the only net/ packages allowed)",
	"R2": "protocol adapters must not import each other: share code through the game or through net/",
	"R3": "protocol libraries in net/ must not depend on the game: pass the data in through the library's own types",
}

// violations walks the repository at root and returns the sorted, deduplicated violations as
// "<file> <import path>" lines, with the rule each one breaks.
func violations(root string) (lines []string, rules map[string]string, err error) {
	rules = make(map[string]string)
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case rel == ".":
				return nil
			case d.Name() == ".git" || d.Name() == "testdata":
				return fs.SkipDir
			case rel != "net":
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					// A separate module other than the root and net/.
					return fs.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		for _, spec := range f.Imports {
			ip, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return fmt.Errorf("parse %s: import %s: %w", rel, spec.Path.Value, err)
			}
			if rule, bad := violation(rel, ip); bad {
				line := rel + " " + ip
				if _, ok := rules[line]; !ok {
					lines = append(lines, line)
				}
				rules[line] = rule
			}
		}
		return nil
	})
	sort.Strings(lines)
	return lines, rules, err
}

// readAllowlist reads the allowlist entries, in file order.
func readAllowlist(p string) ([]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	return entries, sc.Err()
}

func TestImportBoundary(t *testing.T) {
	current, rules, err := violations(".")
	if err != nil {
		t.Fatal(err)
	}

	if *updateBoundary {
		var b strings.Builder
		b.WriteString(boundaryHeader)
		b.WriteString("\n")
		for _, line := range current {
			b.WriteString(line + "\n")
		}
		if err := os.MkdirAll(path.Dir(boundaryAllowlist), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(boundaryAllowlist, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d violations to %s", len(current), boundaryAllowlist)
		return
	}

	allowed, err := readAllowlist(boundaryAllowlist)
	if err != nil {
		t.Fatalf("read allowlist: %v (generate it with: go test -run TestImportBoundary -update-boundary .)", err)
	}
	allowedSet := make(map[string]bool, len(allowed))
	for i, entry := range allowed {
		if len(strings.Fields(entry)) != 2 {
			t.Errorf("%s: malformed entry %q, want \"<file> <import path>\"", boundaryAllowlist, entry)
		}
		if allowedSet[entry] {
			t.Errorf("%s: duplicate entry %q", boundaryAllowlist, entry)
		}
		if i > 0 && allowed[i-1] > entry {
			t.Errorf("%s: entry %q is out of order, regenerate with -update-boundary", boundaryAllowlist, entry)
		}
		allowedSet[entry] = true
	}

	currentSet := make(map[string]bool, len(current))
	for _, line := range current {
		currentSet[line] = true
		if !allowedSet[line] {
			rule := rules[line]
			file, imp, _ := strings.Cut(line, " ")
			t.Errorf("%s imports %s: breaks %s: %s", file, imp, rule, ruleHints[rule])
		}
	}
	for _, entry := range allowed {
		if !currentSet[entry] {
			t.Errorf("stale allowlist entry %q: it is no longer a violation, remove it from %s", entry, boundaryAllowlist)
		}
	}
}

func TestBoundaryRules(t *testing.T) {
	const (
		nbt        = netModule + "/nbt"
		bedrockTxt = netModule + "/bedrock/text"
	)
	for _, tc := range []struct {
		name, file, imp, rule string
	}{
		{"net/nbt from the game", "server/world/chunk.go", nbt, ""},
		{"below net/nbt from the game", "server/world/chunk.go", nbt + "/sub", ""},
		{"net/nbt prefix of another segment", "server/world/chunk.go", nbt + "x", "R1"},
		{"net/metrics from the game", "server/world/stats.go", netModule + "/metrics", ""},
		{"net/metrics prefix of another segment", "server/world/stats.go", netModule + "/metricsx", "R1"},
		{"net/bedrock/text from server/item", "server/item/diamond.go", bedrockTxt, "R1"},
		{"net module root from the game", "server/server.go", netModule, "R1"},
		{"file in server/session is not exempt", "server/session/session.go", netModule + "/bedrock/proto/v2193", "R1"},
		{"file in server/netconn is not exempt", "server/netconn/conn.go", netModule + "/bedrock", "R1"},
		{"net from protocol/bedrock/netconn", "protocol/bedrock/netconn/conn.go", netModule + "/bedrock", ""},
		{"protocol/bedrock/session from server/player", "server/player/player.go", rootModule + "/protocol/bedrock/session", "R1"},
		{"below protocol/bedrock from server/player", "server/player/player.go", rootModule + "/protocol/bedrock/netconn", "R1"},
		{"server/x importing protocol/bedrock/internal", "server/x/x.go", rootModule + "/protocol/bedrock/internal/bedrockdata", "R1"},
		{"protocol/bedrock from server/x", "server/x/x.go", rootModule + "/protocol/bedrock", "R1"},
		{"protocol itself from server/x", "server/x/x.go", rootModule + "/protocol", "R1"},
		{"protocol/java from protocol/bedrock", "protocol/bedrock/session.go", rootModule + "/protocol/java", "R2"},
		{"protocol/java importing protocol/bedrock/text", "protocol/java/status.go", rootModule + "/protocol/bedrock/text", "R2"},
		{"protocol/bedrock importing net/java", "protocol/bedrock/listener.go", netModule + "/java", ""},
		{"below protocol/java from protocol/bedrock", "protocol/bedrock/text/text.go", rootModule + "/protocol/java/text", "R2"},
		{"protocol/bedrock/sub from protocol/bedrock", "protocol/bedrock/session.go", rootModule + "/protocol/bedrock/sub", ""},
		{"protocol/bedrock from protocol/bedrock/sub", "protocol/bedrock/sub/x.go", rootModule + "/protocol/bedrock", ""},
		{"protocol/bedrockfoo from protocol/bedrock", "protocol/bedrock/session.go", rootModule + "/protocol/bedrockfoo", "R2"},
		{"adapter importing the game and net", "protocol/java/login.go", rootModule + "/server/world", ""},
		{"adapter importing net", "protocol/java/login.go", netModule + "/java", ""},
		{"file directly in protocol", "protocol/doc.go", rootModule + "/protocol/java", ""},
		{"game from net/bedrock", "net/bedrock/x.go", rootModule + "/server/world", "R3"},
		{"root module from net", "net/nethernet/x.go", rootModule, "R3"},
		{"protocol from net", "net/java/x.go", rootModule + "/protocol/java", "R3"},
		{"dragonfly prefix of another segment from net", "net/bedrock/x.go", rootModule + "x/y", ""},
		{"net importing net", "net/bedrock/x.go", netModule + "/nbt", ""},
		{"net importing the standard library", "net/bedrock/x.go", "net/http", ""},
		{"file in network is not in net", "network/x.go", rootModule + "/server", ""},
		{"main.go is unrestricted", "main.go", rootModule + "/protocol/bedrock/session", ""},
		{"main.go imports net", "main.go", netModule + "/bedrock/login", ""},
		{"cmd is unrestricted", "cmd/tool/main.go", rootModule + "/protocol/bedrock", ""},
		{"cmd imports protocol/bedrock/session", "cmd/tool/main.go", rootModule + "/protocol/bedrock/session", ""},
		{"scripts are unrestricted", "scripts/x/main.go", netModule + "/bedrock", ""},
		{"game importing the game", "server/player/player.go", rootModule + "/server/world", ""},
		{"game importing the standard library", "server/player/player.go", "net", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule, bad := violation(tc.file, tc.imp)
			if rule != tc.rule || bad != (tc.rule != "") {
				t.Errorf("violation(%q, %q) = (%q, %v), want rule %q", tc.file, tc.imp, rule, bad, tc.rule)
			}
		})
	}
}
