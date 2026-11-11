package player

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// updateBehaviour rewrites the transcripts of testdata/behaviour from what the code does now. It is for
// recording a change of behaviour that was meant, never for a pure move.
var updateBehaviour = flag.Bool("update-behaviour", false, "rewrite the behaviour transcripts of testdata/behaviour")

// transcript is what a behaviour test saw a player do: lines of text, in order, that a golden file holds.
type transcript struct {
	b strings.Builder
}

// linef adds a line.
func (t *transcript) linef(format string, args ...any) {
	fmt.Fprintf(&t.b, format, args...)
	t.b.WriteByte('\n')
}

// calls adds the calls of r under the title passed, one a line, and forgets them.
func (t *transcript) calls(title string, r *clienttest.Recorder) {
	for _, c := range r.Calls() {
		args := make([]string, len(c.Args))
		for i, a := range c.Args {
			args[i] = describeArg(a)
		}
		t.linef("%s %s(%s)", title, c.Method, strings.Join(args, ", "))
	}
	r.Reset()
}

// describeArg describes an argument of a call so that two runs of the same behaviour read alike: entities by
// type and position, numbers rounded, values by their type and fields.
func describeArg(a any) string {
	switch v := a.(type) {
	case world.Entity:
		pos := v.Position()
		return fmt.Sprintf("%s@(%.4f %.4f %.4f)", v.H().Type().EncodeEntity(), pos[0], pos[1], pos[2])
	case mgl64.Vec3:
		return fmt.Sprintf("(%.4f %.4f %.4f)", v[0], v[1], v[2])
	case float64:
		return fmt.Sprintf("%.4f", v)
	case time.Duration, int, int32, int64, bool, string:
		return fmt.Sprint(v)
	case *world.Tx:
		return "tx"
	case *world.EntityMove:
		return fmt.Sprintf("*world.EntityMove&%+v", *v)
	}
	if reflect.TypeOf(a).Kind() == reflect.Pointer {
		// A pointer prints its address, which differs from run to run.
		return fmt.Sprintf("%T", a)
	}
	return fmt.Sprintf("%T%+v", a, a)
}

// checkTranscript compares the transcript with testdata/behaviour/<name>.txt, or writes it with
// -update-behaviour.
func checkTranscript(t *testing.T, name string, tr *transcript) {
	t.Helper()
	path := filepath.Join("testdata", "behaviour", name+".txt")
	got := tr.b.String()
	if *updateBehaviour {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no transcript: %v (record it with -update-behaviour)", err)
	}
	if string(want) == got {
		return
	}
	wl, gl := strings.Split(string(want), "\n"), strings.Split(got, "\n")
	for i := 0; i < max(len(wl), len(gl)); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			t.Fatalf("%s differs from the transcript at line %d:\n got: %s\nwant: %s", name, i+1, g, w)
		}
	}
}

// traceHandler is a Handler that writes the events it is called with, in order, to a transcript, and cancels
// those named in cancel.
type traceHandler struct {
	NopHandler
	tr     *transcript
	cancel map[string]bool
	// scaleHurt multiplies the damage of HandleHurt when it is not zero.
	scaleHurt float64
	// attackForce and attackHeight replace the knock-back of an attack when they are not zero, and noCritical
	// makes it not critical.
	attackForce, attackHeight float64
	noCritical                bool
}

func (h *traceHandler) event(ctx *Context, name string, format string, args ...any) {
	h.tr.linef("event %s %s", name, fmt.Sprintf(format, args...))
	if ctx != nil && h.cancel[name] {
		ctx.Cancel()
	}
}

func (h *traceHandler) HandleAttackEntity(ctx *Context, e world.Entity, force, height *float64, critical *bool) {
	h.event(ctx, "AttackEntity", "%s force=%.4f height=%.4f critical=%v", describeArg(e), *force, *height, *critical)
	if h.attackForce != 0 {
		*force = h.attackForce
	}
	if h.attackHeight != 0 {
		*height = h.attackHeight
	}
	if h.noCritical {
		*critical = false
	}
}

func (h *traceHandler) HandleItemDamage(ctx *Context, i item.Stack, d *int) {
	h.event(ctx, "ItemDamage", "%T damage=%d", i.Item(), *d)
}

func (h *traceHandler) HandleHurt(ctx *Context, damage *float64, immune bool, immunity *time.Duration, src world.DamageSource) {
	h.event(ctx, "Hurt", "damage=%.4f immune=%v immunity=%v src=%T", *damage, immune, *immunity, src)
	if h.scaleHurt != 0 {
		*damage *= h.scaleHurt
	}
}

func (h *traceHandler) HandleHeal(ctx *Context, health *float64, src world.HealingSource) {
	h.event(ctx, "Heal", "health=%.4f src=%T", *health, src)
}

func (h *traceHandler) HandleDeath(p *Player, src world.DamageSource, keepInv *bool) {
	h.event(nil, "Death", "src=%T keepInv=%v", src, *keepInv)
}

func (h *traceHandler) HandleSetOnFire(ctx *Context, d *time.Duration) {
	h.event(ctx, "SetOnFire", "duration=%v", *d)
}

func (h *traceHandler) HandlePunchAir(ctx *Context) { h.event(ctx, "PunchAir", "") }

func (h *traceHandler) HandleSleep(ctx *Context, sendReminder *bool) {
	h.event(ctx, "Sleep", "reminder=%v", *sendReminder)
}

// digest describes the state of a player that a tick changes, on one line.
func digest(p *Player) string {
	var effects []string
	for _, e := range p.Effects() {
		effects = append(effects, fmt.Sprintf("%T:%d:%v", e.Type(), e.Level(), e.Duration()))
	}
	held, off := p.HeldItems()
	armour := p.Armour()
	return fmt.Sprintf("pos=%s vel=%s rot=%v hp=%.4f abs=%.4f food=%d sat=%.4f exh=%.4f ground=%v sleeping=%v gliding=%v swimming=%v sprint=%v sneak=%v fall=%.4f fire=%v air=%v dead=%v effects=%v held=%s off=%s armour=%d/%d/%d/%d using=%v breaking=%v blockdelay=%d strength=%d mode=%v",
		describeArg(p.Position()), describeArg(p.Velocity()), p.Rotation(), p.Health(), p.Absorption(), p.Food(), p.hunger.saturationLevel, p.hunger.exhaustionLevel,
		p.OnGround(), p.sleeping, p.Gliding(), p.Swimming(), p.Sprinting(), p.Sneaking(), p.fallDistance, p.OnFireDuration(), p.AirSupply(), p.Dead(), effects,
		describeStack(held), describeStack(off), armour.Helmet().Durability(), armour.Chestplate().Durability(), armour.Leggings().Durability(), armour.Boots().Durability(),
		p.usingItem, p.breaking, p.blockingDelayTicks, p.attackStrengthTicks, p.GameMode())
}

// describeStack describes an item stack by its name, count and durability.
func describeStack(s item.Stack) string {
	if s.Empty() {
		return "-"
	}
	name, meta := s.Item().EncodeItem()
	return fmt.Sprintf("%s/%d*%d(%d)", name, meta, s.Count(), s.Durability())
}

func (h *traceHandler) HandleItemUse(ctx *Context) { h.event(ctx, "ItemUse", "") }

func (h *traceHandler) HandleItemUseOnBlock(ctx *Context, pos cube.Pos, face cube.Face, clickPos mgl64.Vec3) {
	h.event(ctx, "ItemUseOnBlock", "pos=%v face=%v click=%s", pos, face, describeArg(clickPos))
}

func (h *traceHandler) HandleItemRelease(ctx *Context, i item.Stack, d time.Duration) {
	h.event(ctx, "ItemRelease", "%T duration=%v", i.Item(), d)
}

func (h *traceHandler) HandleItemConsume(ctx *Context, i item.Stack) {
	h.event(ctx, "ItemConsume", "%T", i.Item())
}

func (h *traceHandler) HandleBlockPlace(ctx *Context, pos cube.Pos, b world.Block) {
	h.event(ctx, "BlockPlace", "pos=%v %T", pos, b)
}
