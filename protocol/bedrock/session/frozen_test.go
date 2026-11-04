package session

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateFrozen records the outcomes of the sweeps of testdata/frozen again,
// from server/menu: only once a change of the outcomes is meant.
var updateFrozen = flag.Bool("update-frozen", false, "record the frozen outcomes of the sweeps of menus again")

// frozenSweep holds the outcomes of the cases of a sweep, in order, which
// must be those of testdata/frozen/<name>.txt: the outcomes the handlers of
// item stack requests that the session had before they moved into
// server/menu gave, the oracle of the sweep, recorded when they went.
type frozenSweep struct {
	name string
	// fields is the number of the fields of the label of a case, separated
	// by spaces, that name the group of the case: the file holds a digest of
	// each group of cases of the same fields, in order. 0 if it holds each
	// case.
	fields int
	lines  []string
}

// add adds a case and its outcome, which hold no newline.
func (f *frozenSweep) add(label, outcome string) {
	f.lines = append(f.lines, label+"\t"+outcome)
}

// render returns the lines of the file of the sweep: each case, or a digest
// of each group of cases.
func (f *frozenSweep) render() []string {
	if f.fields == 0 {
		return f.lines
	}
	var out, group []string
	key := ""
	flush := func() {
		if len(group) == 0 {
			return
		}
		h := sha256.Sum256([]byte(strings.Join(group, "\n") + "\n"))
		out = append(out, fmt.Sprintf("%s\t%d cases sha256:%s", key, len(group), hex.EncodeToString(h[:])))
	}
	for _, l := range f.lines {
		label, _, _ := strings.Cut(l, "\t")
		k := strings.Join(strings.Fields(label)[:f.fields], " ")
		if k != key || len(group) == 0 {
			flush()
			key, group = k, nil
		}
		group = append(group, l)
	}
	flush()
	return out
}

// check compares the outcomes of the sweep with those of its file, or
// records them there with -update-frozen.
func (f *frozenSweep) check(t *testing.T) {
	t.Helper()
	path := filepath.Join("testdata", "frozen", f.name+".txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var header, want []string
	for _, l := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if strings.HasPrefix(l, "#") {
			header = append(header, l)
		} else {
			want = append(want, l)
		}
	}
	got := f.render()
	if *updateFrozen {
		if err := os.WriteFile(path, []byte(strings.Join(append(header, got...), "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if len(got) != len(want) {
		t.Errorf("%s: %d lines of outcomes, %s holds %d", f.name, len(got), path, len(want))
	}
	shown := 0
	for i := range min(len(got), len(want)) {
		if got[i] != want[i] && shown < 10 {
			shown++
			t.Errorf("%s, line %d:\n want: %s\n  got: %s", f.name, i+1, want[i], got[i])
		}
	}
}
