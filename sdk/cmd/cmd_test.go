package cmd_test

import (
	"slices"
	"strings"
	"testing"

	sdkcmd "github.com/dm-vev/adamant/sdk/cmd"
	corecmd "github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type testSource struct {
	name     string
	level    corecmd.Level
	messages []string
	errors   []string
}

func (s *testSource) Name() string                   { return s.name }
func (s *testSource) Position() mgl64.Vec3           { return mgl64.Vec3{} }
func (s *testSource) PermissionLevel() corecmd.Level { return s.level }
func (s *testSource) SendCommandOutput(o *corecmd.Output) {
	for _, m := range o.Messages() {
		s.messages = append(s.messages, m.String())
	}
	for _, e := range o.Errors() {
		s.errors = append(s.errors, e.Error())
	}
}

type typedGreet struct {
	Target string `cmd:"target"`
}

var lastTypedTarget string

func (g typedGreet) Run(_ corecmd.Source, o *corecmd.Output, _ *world.Tx) {
	lastTypedTarget = g.Target
	o.Printf("hi %s", g.Target)
}

func TestRegisterTypedAndSubcommandGroups(t *testing.T) {
	var simpleArgs []string
	c := sdkcmd.Register("sdk_echo",
		sdkcmd.Desc("echoes arguments"),
		sdkcmd.Aliases("secho"),
		sdkcmd.RunOutput(func(_ sdkcmd.Source, out *sdkcmd.Output, args []string, _ *world.Tx) {
			simpleArgs = args
			out.Printf("echo:%s", strings.Join(args, ","))
		}),
	)

	src := &testSource{name: "Tester", level: corecmd.LevelAdmin}
	c.Execute("one two three", src, nil)
	if !slices.Equal(simpleArgs, []string{"one", "two", "three"}) {
		t.Fatalf("simpleArgs = %v", simpleArgs)
	}
	if len(src.messages) != 1 || src.messages[0] != "echo:one,two,three" {
		t.Fatalf("messages = %v", src.messages)
	}

	tc := sdkcmd.RegisterTyped("sdk_greet", "typed greet", nil, typedGreet{})
	tc.Execute("Alice", src, nil)
	if lastTypedTarget != "Alice" {
		t.Fatalf("lastTypedTarget = %q, want Alice", lastTypedTarget)
	}

	var branch string
	var branchArgs []string
	grp := sdkcmd.Group("sdk_kit",
		sdkcmd.Sub("list", sdkcmd.RunOutput(func(_ sdkcmd.Source, out *sdkcmd.Output, args []string, _ *world.Tx) {
			branch = "list"
			branchArgs = args
			out.Print("listed")
		})),
		sdkcmd.Sub("give",
			sdkcmd.MinLevel(corecmd.LevelAdmin),
			sdkcmd.Run(func(_ sdkcmd.Source, args []string, _ *world.Tx) {
				branch = "give"
				branchArgs = args
			}),
		),
	).Desc("kit management").Aliases("skit").Register()

	// Verify native client autocomplete parameter descriptors are populated.
	params := grp.Params(src)
	if len(params) != 2 {
		t.Fatalf("expected 2 subcommand overloads in Params, got %d", len(params))
	}

	grp.Execute("list vip", src, nil)
	if branch != "list" || !slices.Equal(branchArgs, []string{"vip"}) {
		t.Fatalf("list branch = (%q, %v)", branch, branchArgs)
	}

	// Non-admin should be denied `give`.
	lowSrc := &testSource{name: "Guest", level: corecmd.LevelAll}
	branch = ""
	grp.Execute("give starter", lowSrc, nil)
	if branch != "" {
		t.Fatalf("expected give denied for LevelAny, got branch=%q", branch)
	}

	grp.Execute("give starter", src, nil)
	if branch != "give" || !slices.Equal(branchArgs, []string{"starter"}) {
		t.Fatalf("give branch = (%q, %v)", branch, branchArgs)
	}
}
