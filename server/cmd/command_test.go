package cmd

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// commandRecord holds what the last run test command was called with.
var commandRecord []string

type modeEnum string

func (modeEnum) Type() string { return "Mode" }
func (modeEnum) Options(Source) []string {
	return []string{"survival", "creative"}
}

type setRunner struct {
	Set   SubCommand `cmd:"set"`
	Mode  modeEnum
	Count Optional[int]
}

func (c setRunner) Run(_ Source, o *Output, _ *world.Tx) {
	n, ok := c.Count.Load()
	commandRecord = []string{"set", string(c.Mode), strings.Repeat("+", n), boolWord(ok)}
	o.Printf("set")
}

type sayRunner struct {
	Say     SubCommand `cmd:"say"`
	Message Varargs
}

func (c sayRunner) Run(_ Source, _ *Output, _ *world.Tx) {
	commandRecord = []string{"say", string(c.Message)}
}

type quotedRunner struct {
	Quote SubCommand `cmd:"quote"`
	Text  string
	Flag  Optional[bool]
}

func (c quotedRunner) Run(_ Source, _ *Output, _ *world.Tx) {
	commandRecord = []string{"quote", c.Text, boolWord(c.Flag.LoadOr(false))}
}

// opOnly is a runnable an Allower limits to sources at y >= 100.
type opOnly struct {
	Secret SubCommand `cmd:"secret"`
}

func (opOnly) Allow(src Source) bool { return src.Position()[1] >= 100 }
func (opOnly) Run(_ Source, _ *Output, _ *world.Tx) {
	commandRecord = []string{"secret"}
}

func boolWord(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func newTestCommand() Command {
	return New("Test", "test command", []string{"T", "tst"}, setRunner{}, sayRunner{}, quotedRunner{}, opOnly{})
}

// run executes args through the command as src and returns the output.
func run(c Command, args string, src testSource) (*Output, []string) {
	commandRecord = nil
	out := &Output{}
	src.out = out
	c.Execute(args, src, nil)
	return out, commandRecord
}

func TestCommandIdentity(t *testing.T) {
	c := newTestCommand()
	if c.Name() != "test" {
		t.Errorf("name %q, want lowercase", c.Name())
	}
	aliases := c.Aliases()
	if strings.Join(aliases, ",") != "t,tst,test" {
		t.Errorf("aliases %q", aliases)
	}
	aliases[0] = "changed"
	if c.Aliases()[0] != "t" {
		t.Error("Aliases returned the command's own slice")
	}
	want := "/test set <Mode: Mode> [Count: int]\n/test say <Message: text>\n/test quote <Text: string> [Flag: bool]\n/test secret"
	if c.Usage() != want || c.String() != want {
		t.Errorf("usage:\n%s\nwant:\n%s", c.Usage(), want)
	}
	if got := len(c.Params(testSource{})); got != 3 {
		t.Errorf("a non-operator sees %d overloads, want 3 without the secret one", got)
	}
	if got := len(c.Runnables(testSource{pos: [3]float64{0, 100, 0}})); got != 4 {
		t.Errorf("an operator sees %d runnables, want 4", got)
	}
	if params := c.Params(testSource{})[0]; len(params) != 3 || !params[2].Optional || params[1].Optional {
		t.Errorf("set params = %+v", params)
	}
}

// Commands dispatch to the first overload that parses, with case-insensitive
// literals and enum options (Bedrock and Brigadier literals are matched as
// typed, but the enum's canonical spelling is kept), optional trailing
// arguments, quoted strings and greedy text.
func TestCommandDispatch(t *testing.T) {
	c := newTestCommand()
	for _, tc := range []struct {
		args string
		want string
	}{
		{"set creative", "set creative  no"},
		{"SET Creative 3", "set creative +++ yes"},
		{"set survival 0", "set survival  yes"},
		// StringArgumentType.greedyString keeps the rest of the line as typed.
		{"say hello  there world", "say hello  there world"},
		{"say", "say "},
		{`quote "two words"`, "quote two words no"},
		{`quote one true`, "quote one yes"},
	} {
		out, got := run(c, tc.args, testSource{})
		if strings.Join(got, " ") != tc.want || out.ErrorCount() != 0 {
			t.Errorf("%q ran %q with errors %v; want %q", tc.args, got, out.Errors(), tc.want)
		}
	}
}

// A failed parse runs nothing and reports an error, and an Allower hides its
// overload as an unknown command (commands.generic.unknown), as Java hides
// commands the source lacks the permission level for.
func TestCommandErrors(t *testing.T) {
	c := newTestCommand()
	for _, args := range []string{
		"",
		"set",
		"set hardcore",
		"set creative many",
		"set creative 1 2",
		"quote one maybe",
		"unknown",
		"secret",
	} {
		out, got := run(c, args, testSource{})
		if got != nil || out.ErrorCount() == 0 {
			t.Errorf("%q ran %q with errors %v", args, got, out.Errors())
		}
	}
	out, _ := run(New("secret", "", nil, opOnly{}), "secret", testSource{})
	if !strings.Contains(out.Errors()[len(out.Errors())-1].Error(), "Unknown command") {
		t.Errorf("a disallowed source got %v, want an unknown command", out.Errors())
	}
	if out, got := run(c, "secret", testSource{pos: [3]float64{0, 100, 0}}); got == nil || out.ErrorCount() != 0 {
		t.Errorf("an allowed source could not run secret: %v", out.Errors())
	}
	// The error comes from the overload that parsed furthest: "set creative x"
	// fails on its count, not with the usage of the say overload.
	out, _ = run(c, "set creative x", testSource{})
	if !strings.Contains(out.Errors()[len(out.Errors())-1].Error(), "'x' is not a valid number") {
		t.Errorf("errors %v, want the number error of the set overload", out.Errors())
	}
}

func TestCommandSignatureChecks(t *testing.T) {
	for name, r := range map[string]Runnable{
		"required after optional": badOrder{},
		"non-string enum":         badEnum{},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: New did not panic", name)
				}
			}()
			New("bad", "", nil, r)
		}()
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Execute with a nil source did not panic")
			}
		}()
		newTestCommand().Execute("", nil, nil)
	}()
}

type badOrder struct {
	A Optional[int]
	B int
}

func (badOrder) Run(Source, *Output, *world.Tx) {}

type intEnum int

func (intEnum) Type() string            { return "IntEnum" }
func (intEnum) Options(Source) []string { return nil }

type badEnum struct{ E intEnum }

func (badEnum) Run(Source, *Output, *world.Tx) {}

// ExecuteLine runs a full chat line: it needs the leading slash, looks the
// command up by any alias, reports unknown commands, and lets a hook see the
// split arguments and veto the run.
func TestExecuteLine(t *testing.T) {
	Register(newTestCommand())
	src := testSource{}
	for _, tc := range []struct {
		line string
		want string
	}{
		{"/test say hi", "say hi"},
		{"  /  tst  say hi", "say hi"},
		{"/t set creative", "set creative  no"},
		{"test say hi", ""},
		{"/", ""},
		{"", ""},
	} {
		commandRecord = nil
		ExecuteLine(src, tc.line, nil, nil)
		if got := strings.Join(commandRecord, " "); got != tc.want {
			t.Errorf("%q ran %q, want %q", tc.line, got, tc.want)
		}
	}

	out := &Output{}
	ExecuteLine(testSource{out: out}, "/nosuchcommand_xyz", nil, nil)
	if out.ErrorCount() != 1 || !strings.Contains(out.Errors()[0].Error(), "nosuchcommand_xyz") {
		t.Errorf("unknown command errors %v", out.Errors())
	}

	var seen []string
	commandRecord = nil
	ExecuteLine(src, `/test quote "a b" true`, nil, func(c Command, args []string) bool {
		seen = args
		return false
	})
	if commandRecord != nil {
		t.Error("a vetoing hook did not stop the command")
	}
	if strings.Join(seen, "|") != "quote|a b|true" {
		t.Errorf("hook saw %q", seen)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("ExecuteLine with a nil source did not panic")
			}
		}()
		ExecuteLine(nil, "/test", nil, nil)
	}()
}

// A player-name target matches case-insensitively, and selectors need a
// world to select from.
func TestTargetArguments(t *testing.T) {
	steve := &selectorTarget{name: "Steve"}
	got, err := parser{}.parsePlayer("steve", []NamedTarget{steve})
	if err != nil || got != steve {
		t.Fatalf("parsePlayer = %v, %v", got, err)
	}
	if _, err := (parser{}).parsePlayer("alex", []NamedTarget{steve}); err == nil {
		t.Fatal("an absent player was found")
	}
	if _, err := ResolveTargets("@a", testSource{}, nil); err == nil {
		t.Fatal("a selector resolved without a world")
	}
	if _, err := ResolveTargets("Steve", testSource{}, nil); err == nil {
		t.Fatal("a player resolved without a world")
	}
}
