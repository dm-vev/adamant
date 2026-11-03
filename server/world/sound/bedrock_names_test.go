package sound_test

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// bedrockNamesFile lists the Bedrock sound names the game still plays by,
// relative to this package.
const bedrockNamesFile = "../../../testdata/sounds/bedrock_names.txt"

// bedrockSoundPrefixes are the prefixes of Bedrock's sound definitions that
// Java sound event IDs do not start with.
var bedrockSoundPrefixes = []string{"mob.", "random.", "bottle.", "leashknot.", "portal."}

// TestCustomSoundsUseJavaNames checks that the game names its sounds by their
// Java sound event IDs: every string literal of the non-test files of
// server/{entity,item,block,player,world} that is spelled as a Bedrock sound
// definition must be listed in bedrock_names.txt, and every name listed there
// must still be used.
func TestCustomSoundsUseJavaNames(t *testing.T) {
	listed := readBedrockNames(t)
	used := map[string][]string{}
	for _, dir := range []string{"entity", "item", "block", "player", "world"} {
		root := filepath.Join("..", "..", dir)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil || !hasBedrockSoundPrefix(s) {
					return true
				}
				used[s] = append(used[s], filepath.ToSlash(path))
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for name, files := range used {
		if !slices.Contains(listed, name) {
			t.Errorf("%q in %v is a Bedrock sound name: play the Java sound event ID and map it in the session's bedrockSoundNames, or list it in %s with the reason", name, slices.Compact(slices.Sorted(slices.Values(files))), bedrockNamesFile)
		}
	}
	for _, name := range listed {
		if _, ok := used[name]; !ok {
			t.Errorf("%q is listed in %s but no longer used: remove it", name, bedrockNamesFile)
		}
	}
}

func hasBedrockSoundPrefix(s string) bool {
	for _, p := range bedrockSoundPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// readBedrockNames reads the names of bedrock_names.txt, which holds one name
// a line, with blank lines and comments starting with # ignored.
func readBedrockNames(t *testing.T) []string {
	t.Helper()
	f, err := os.Open(bedrockNamesFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if slices.Contains(names, line) {
			t.Errorf("%s lists %q twice", bedrockNamesFile, line)
		}
		names = append(names, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}
