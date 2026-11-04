package interop

import (
	"bytes"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestConstantsMatchGophertunnel checks the value of every constant of
// v2193 named as in gophertunnel against gophertunnel's: the abilities
// are bits in its protocol package and, with the suffix Index, indices in
// its packet package.
func TestConstantsMatchGophertunnel(t *testing.T) {
	const (
		protocolPath = "github.com/sandertv/gophertunnel/minecraft/protocol"
		packetPath   = "github.com/sandertv/gophertunnel/minecraft/protocol/packet"
		v2193Path    = "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	)
	pkgs, err := packages.Load(&packages.Config{Mode: loadMode}, protocolPath, packetPath, v2193Path)
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string]*types.Scope{}
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			t.Fatalf("%s: %v", p.PkgPath, p.Errors)
		}
		scopes[p.PkgPath] = p.Types.Scope()
	}
	ours := scopes[v2193Path]
	compared := 0
	for _, name := range ours.Names() {
		c, ok := ours.Lookup(name).(*types.Const)
		if !ok {
			continue
		}
		theirs, from := gophertunnelConst(scopes[protocolPath], scopes[packetPath], name)
		if theirs == nil {
			continue
		}
		compared++
		if !constant.Compare(c.Val(), token.EQL, theirs.Val()) {
			t.Errorf("%s is %s, %s in gophertunnel's %s", name, c.Val().ExactString(), theirs.Val().ExactString(), from)
		}
	}
	if compared < 100 {
		t.Fatalf("compared %d constants, want the hundreds constimport writes", compared)
	}
}

// loadMode type-checks the packages from source, with the types of their
// imports: the export data of a newer toolchain may be past what
// go/packages reads ("export data version 5 is greater than maximum
// supported version 2").
const loadMode = packages.NeedTypes | packages.NeedName | packages.NeedSyntax | packages.NeedTypesInfo |
	packages.NeedImports | packages.NeedDeps

// gophertunnelConst returns gophertunnel's constant of a name of v2193 and
// the package it is in, nil if there is none.
func gophertunnelConst(protocol, packet *types.Scope, name string) (*types.Const, string) {
	if base, ok := strings.CutSuffix(name, "Index"); ok && strings.HasPrefix(name, "Ability") {
		if c, ok := packet.Lookup(base).(*types.Const); ok {
			return c, "packet"
		}
	}
	if strings.HasPrefix(name, "Ability") {
		if c, ok := protocol.Lookup(name).(*types.Const); ok {
			return c, "protocol"
		}
	}
	for _, s := range []struct {
		scope *types.Scope
		from  string
	}{{protocol, "protocol"}, {packet, "packet"}} {
		if c, ok := s.scope.Lookup(name).(*types.Const); ok {
			return c, s.from
		}
	}
	return nil, ""
}

// TestConstImportReproduces runs constimport again: it writes consts.go as
// it is, however often it runs.
func TestConstImportReproduces(t *testing.T) {
	out := filepath.Join(t.TempDir(), "consts.go")
	cmd := exec.Command("go", "run", "./cmd/constimport", "-o", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("constimport: %v\n%s", err, b)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../bedrock/proto/v2193/consts.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("constimport writes another consts.go than the one in v2193")
	}
}
