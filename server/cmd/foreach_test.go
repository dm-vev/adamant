package cmd

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTallyAndFeedback(t *testing.T) {
	// Empty results: adds failed error, result is 0.
	o := &Output{}
	TallyOf(nil).Feedback(o, ErrNoPlayerFound,
		func(who string) string { return "one " + who },
		func(n string) string { return "many " + n },
	)
	if o.ErrorCount() != 1 || o.Errors()[0] != ErrNoPlayerFound {
		t.Fatalf("empty errors = %v, want [%v]", o.Errors(), ErrNoPlayerFound)
	}
	if o.Result() != 0 {
		t.Fatalf("empty result = %d, want 0", o.Result())
	}

	// Non-empty results where all values are 0: still fails, tracks Count/Only.
	zeroRes := []Result[int]{{Name: "Alice", Value: 0}}
	tally := TallyOf(zeroRes)
	if tally.Count != 1 || tally.Only != "Alice" || tally.NonZero != 0 || tally.OnlyNonZero != "" {
		t.Fatalf("zero tally = %+v", tally)
	}
	o = &Output{}
	tally.Feedback(o, ErrNoEntityFound,
		func(who string) string { return "one " + who },
		func(n string) string { return "many " + n },
	)
	if o.ErrorCount() != 1 || o.Errors()[0] != ErrNoEntityFound {
		t.Fatalf("zero errors = %v, want [%v]", o.Errors(), ErrNoEntityFound)
	}

	// One non-zero target alongside a zero target: single feedback, Total=5.
	oneRes := []Result[int]{{Name: "Alice", Value: 0}, {Name: "Bob", Value: 5}}
	tally = TallyOf(oneRes)
	if tally.Count != 2 || tally.Only != "" || tally.NonZero != 1 || tally.OnlyNonZero != "Bob" || tally.Total != 5 {
		t.Fatalf("one non-zero tally = %+v", tally)
	}
	o = &Output{}
	tally.Feedback(o, ErrNoPlayerFound,
		func(who string) string { return "Gave to " + who },
		func(n string) string { return "Gave to " + n + " players" },
	)
	if o.ErrorCount() != 0 || o.MessageCount() != 1 || o.Messages()[0].String() != "Gave to Bob" || o.Result() != 5 {
		t.Fatalf("single output = msgs:%v errs:%v res:%d", o.Messages(), o.Errors(), o.Result())
	}

	// Multiple non-zero targets: multiple feedback with NonZero count, Total=7.
	multiRes := []Result[int]{{Name: "Alice", Value: 3}, {Name: "Bob", Value: 4}}
	o = &Output{}
	TallyOf(multiRes).Feedback(o, ErrNoPlayerFound,
		func(who string) string { return "one " + who },
		func(n string) string { return fmt.Sprintf("Gave to %s players", n) },
	)
	if o.ErrorCount() != 0 || o.MessageCount() != 1 || o.Messages()[0].String() != "Gave to 2 players" || o.Result() != 7 {
		t.Fatalf("multi output = msgs:%v errs:%v res:%d", o.Messages(), o.Errors(), o.Result())
	}
}

type selectorParamCmd struct {
	Targets Players             `cmd:"targets"`
	Extra   Optional[OneEntity] `cmd:"extra"`
	ran     *int
}

func (c selectorParamCmd) Run(src Source, o *Output, tx *world.Tx) {
	*c.ran++
	ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, _ Target) int { return 1 },
		func(_ Source, o *Output, _ *world.Tx, rs []Result[int]) {
			o.Printf("matched %d", len(rs))
		},
	)
}

func TestSelectorParameterParsingAndParams(t *testing.T) {
	var ran int
	c := New("seltest", "", nil, selectorParamCmd{ran: &ran})

	// Usage and Params must look identical to []Target for client/console compatibility.
	if got := c.Usage(); got != "/seltest <targets: target> [extra: target]" {
		t.Fatalf("Usage() = %q, want %q", got, "/seltest <targets: target> [extra: target]")
	}
	params := c.Params(testSource{})
	if len(params) != 1 || len(params[0]) != 2 {
		t.Fatalf("Params() = %+v", params)
	}
	for i, p := range params[0] {
		if _, ok := p.Value.([]Target); !ok {
			t.Errorf("param[%d].Value type = %T, want []cmd.Target", i, p.Value)
		}
	}

	hasErr := func(o *Output, substr string) bool {
		for _, e := range o.Errors() {
			if strings.Contains(e.Error(), substr) {
				return true
			}
		}
		return false
	}

	// Players refuses @e at parse time without calling Run.
	src := newDeferSource()
	c.Execute("@e", src, nil)
	if ran != 0 {
		t.Fatalf("Run was called %d times on invalid selector, want 0", ran)
	}
	if src.count() != 1 || !hasErr(src.outputs[0], "Only players may be affected") {
		t.Fatalf("expected players-only error, got %v", src.outputs[0].Errors())
	}

	// Optional OneEntity refuses @e at parse time.
	src = newDeferSource()
	c.Execute("@a @e", src, nil)
	if ran != 0 {
		t.Fatalf("Run was called %d times on non-single extra selector, want 0", ran)
	}
	if src.count() != 1 || !hasErr(src.outputs[0], "Only one entity is allowed") {
		t.Fatalf("expected single-entity error, got %v", src.outputs[0].Errors())
	}

	// Valid selector executes Run and ForEach cleanly (with nil tx -> 0 results).
	src = newDeferSource()
	c.Execute("@a @n", src, nil)
	if ran != 1 {
		t.Fatalf("Run count = %d, want 1", ran)
	}
	if src.count() != 1 || src.outputs[0].ErrorCount() != 0 || src.outputs[0].Messages()[0].String() != "matched 0" {
		t.Fatalf("valid output = msgs:%v errs:%v", src.outputs[0].Messages(), src.outputs[0].Errors())
	}
}

func TestDisplayTypeNameAndTimeout(t *testing.T) {
	if got := displayTypeName("minecraft:iron_golem"); got != "Iron Golem" {
		t.Fatalf("displayTypeName(iron_golem) = %q, want Iron Golem", got)
	}
	if got := displayTypeName("minecraft:pig"); got != "Pig" {
		t.Fatalf("displayTypeName(pig) = %q, want Pig", got)
	}

	t.Cleanup(func() { SetCrossWorldTimeout(0) })
	SetCrossWorldTimeout(0)
	if got := getCrossWorldTimeout(); got != defaultCrossWorldTimeout {
		t.Fatalf("default timeout = %v, want %v", got, defaultCrossWorldTimeout)
	}
	SetCrossWorldTimeout(250 * time.Millisecond)
	if got := getCrossWorldTimeout(); got != 250*time.Millisecond {
		t.Fatalf("custom timeout = %v, want 250ms", got)
	}
	SetCrossWorldTimeout(-1)
	if got := getCrossWorldTimeout(); got != defaultCrossWorldTimeout {
		t.Fatalf("reset timeout = %v, want %v", got, defaultCrossWorldTimeout)
	}
}

type leveledNamedSource struct {
	name  string
	pos   mgl64.Vec3
	level Level
}

func (s leveledNamedSource) Name() string              { return s.name }
func (s leveledNamedSource) Position() mgl64.Vec3      { return s.pos }
func (s leveledNamedSource) PermissionLevel() Level    { return s.level }
func (s leveledNamedSource) SendCommandOutput(*Output) {}

func TestCaptureSourceAndContextRewrap(t *testing.T) {
	base := leveledNamedSource{name: "Console", pos: mgl64.Vec3{1, 2, 3}, level: LevelOwner}
	ctx := ExecutionContext{Position: mgl64.Vec3{10, 20, 30}}
	wrapped := WithContext(base, ctx)

	ref := captureSource(wrapped)
	resolved := ref.resolve(nil)
	ec, ok := ExecutionContextOf(resolved)
	if !ok || ec.Position != (mgl64.Vec3{10, 20, 30}) {
		t.Fatalf("resolved context = (%+v, %v), want pos {10,20,30}", ec, ok)
	}
	if SourceLevel(resolved) != LevelOwner {
		t.Fatalf("resolved level = %v, want LevelOwner", SourceLevel(resolved))
	}

	standIn := standInSource{name: "Alice", pos: mgl64.Vec3{4, 5, 6}, level: LevelGameMaster}
	if standIn.Name() != "Alice" || standIn.Position() != (mgl64.Vec3{4, 5, 6}) || standIn.PermissionLevel() != LevelGameMaster {
		t.Fatalf("unexpected standInSource fields: %+v", standIn)
	}
}
