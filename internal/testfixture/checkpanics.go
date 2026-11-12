package testfixture

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/internal/faultcheck"
)

// printedLines is how many lines of the stack RunCheckingPanics prints for a
// panic: the recovery site and the frames of the panic below it.
const printedLines = 28

// RunCheckingPanics runs the tests of m, for a TestMain to pass to os.Exit,
// and fails them when a recovery site swallowed a panic that no allow
// accepts. A test whose code panics inside a transaction would otherwise
// stop at the panic and still pass, with a log line nobody reads.
//
// Every panic recorded is printed with its site, its value and the top of its
// stack, which names the test. A test that panics on purpose panics with
// faultcheck.Expected; allow is for a test that checks what a panic of game
// code, not of the test, does, and should match as narrowly as it can.
func RunCheckingPanics(m *testing.M, allow ...func(faultcheck.Recovered) bool) int {
	faultcheck.Start()
	code := m.Run()
	var unexpected []faultcheck.Recovered
recorded:
	for _, r := range faultcheck.Take() {
		for _, ok := range allow {
			if ok(r) {
				continue recorded
			}
		}
		unexpected = append(unexpected, r)
	}
	if len(unexpected) == 0 {
		return code
	}
	// The same panic at the same place, of a test run in a loop, is printed
	// once.
	type key struct{ site, value, stack string }
	var order []key
	count := map[key]int{}
	for _, r := range unexpected {
		k := key{r.Site, fmt.Sprint(r.Value), firstLines(r.Stack, printedLines)}
		if count[k] == 0 {
			order = append(order, k)
		}
		count[k]++
	}
	_, _ = fmt.Fprintf(os.Stderr, "\nFAIL: %d panic(s) recovered by the code under test, which the tests that caused them did not see:\n", len(unexpected))
	for i, k := range order {
		times := ""
		if n := count[k]; n > 1 {
			times = fmt.Sprintf(" (%d times)", n)
		}
		_, _ = fmt.Fprintf(os.Stderr, "\n#%d site: %s%s\n   panic: %s\n   stack:\n%s", i+1, k.site, times, k.value, indent(k.stack))
	}
	return 1
}

// firstLines returns the first n lines of s.
func firstLines(s string, n int) string {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "")
}

// indent indents every line of s.
func indent(s string) string {
	return "     " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n     ") + "\n"
}
