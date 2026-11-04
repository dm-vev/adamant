package block_test

import (
	"bufio"
	"crypto/sha256"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
)

// updateRegistration rewrites the golden file of TestRegistrationOrder to what the registration of blocks and
// items leaves now.
var updateRegistration = flag.Bool("update-registration", false, "rewrite testdata/registration.txt to what the registration of blocks and items leaves now")

// registrationGolden holds the size and a hash of each part of what the registration of blocks and items
// leaves in the default registries.
const registrationGolden = "testdata/registration.txt"

// registrationParts returns, for each part of the default registries, its lines in the order the registries
// keep them: the block of every runtime ID, the custom blocks by name, the items sorted (the registry keeps
// no order of them) and the custom items in the order they were registered.
func registrationParts() map[string][]string {
	parts := map[string][]string{}
	reg := world.DefaultBlockRegistry
	for rid, b := range reg.Blocks() {
		name, props := b.EncodeBlock()
		parts["blocks"] = append(parts["blocks"], fmt.Sprintf("%d %T %s %v %d", rid, b, name, props, reg.BlockHash(b)))
	}
	custom := reg.CustomBlocks()
	for _, name := range slices.Sorted(maps.Keys(custom)) {
		parts["custom blocks"] = append(parts["custom blocks"], fmt.Sprintf("%s %T", name, custom[name]))
	}
	for _, it := range world.Items() {
		name, meta := it.EncodeItem()
		line := fmt.Sprintf("%s %d %T", name, meta, it)
		if b, ok := it.(world.Block); ok {
			// The state of a block item, which the item does not encode.
			bn, props := b.EncodeBlock()
			line += fmt.Sprintf(" %s %v %d", bn, props, reg.BlockHash(b))
		}
		parts["items"] = append(parts["items"], line)
	}
	slices.Sort(parts["items"])
	for i, it := range world.CustomItems() {
		parts["custom items"] = append(parts["custom items"], fmt.Sprintf("%d %T", i, it))
	}
	return parts
}

// TestRegistrationOrder requires the registration of blocks and items, which the init functions of the
// block and item packages run in a fixed order, to leave the default registries as the golden file records:
// the same block at every runtime ID, the same items and custom items in the same order.
func TestRegistrationOrder(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	var current []string
	for name, lines := range registrationParts() {
		h := sha256.Sum256([]byte(strings.Join(lines, "\n")))
		current = append(current, fmt.Sprintf("%s: %d %x", name, len(lines), h[:12]))
	}
	slices.Sort(current)
	if *updateRegistration {
		var b strings.Builder
		b.WriteString("# What the registration of blocks and items leaves in the default registries: \"<part>: <lines> <hash>\".\n")
		b.WriteString("# The parts are those of registration_test.go. Regenerate with:\n")
		b.WriteString("# go test -run TestRegistrationOrder -update-registration ./server/block\n\n")
		for _, l := range current {
			b.WriteString(l + "\n")
		}
		if err := os.WriteFile(registrationGolden, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(registrationGolden)
	if err != nil {
		t.Fatalf("read the golden file: %v (generate it with -update-registration)", err)
	}
	defer f.Close()
	var want []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := sc.Text(); l != "" && !strings.HasPrefix(l, "#") {
			want = append(want, l)
		}
	}
	if !slices.Equal(current, want) {
		t.Errorf("the registration of blocks and items changed:\n got %q\nwant %q", current, want)
	}
}
