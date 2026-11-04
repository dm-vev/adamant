package anticheat

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

// javaTick is one tick of a player moving forward, as Java's
// LivingEntity.aiStep and travel move it: written apart from the
// envelopes, so that a mistake in one does not hide in the other.
type javaTick struct {
	// friction is the friction of the block below, speed the movement speed
	// attribute, input the forward input (0.98 held, Java's aiStep scaling).
	friction, speed, input float64
	sprint, jump           bool
	// jumpBoost and jumpFactor change the jump; ceiling, if above 0, is the
	// height above the ground the head hits.
	jumpBoost  int
	jumpFactor float64
	ceiling    float64
	gravity    float64
}

// javaPlayer is the state Java keeps of a player between ticks.
type javaPlayer struct {
	vx, vy, y float64
	onGround  bool
}

// tick moves the player a tick and returns the move: x forward, y up.
func (j *javaPlayer) tick(t javaTick) (dx, dy float64) {
	if t.jump && j.onGround {
		factor := t.jumpFactor
		if factor == 0 {
			factor = 1
		}
		j.vy = 0.42*factor + 0.1*float64(t.jumpBoost)
		if t.sprint {
			j.vx += 0.2
		}
	}
	fr, accel := 0.91, 0.02
	if t.sprint {
		accel = 0.025999999
	}
	if j.onGround {
		fr = t.friction * 0.91
		accel = t.speed * (0.21600002 / (t.friction * t.friction * t.friction))
	}
	j.vx += accel * t.input
	dx, dy = j.vx, j.vy
	j.onGround = false
	if j.y+dy <= 0 {
		dy, j.onGround = -j.y, true
	}
	if t.ceiling > 0 && j.y+dy+1.8 > t.ceiling {
		dy = t.ceiling - 1.8 - j.y
	}
	j.y += dy
	if dy != j.vy {
		j.vy = 0
	}
	g := t.gravity
	if g == 0 {
		g = 0.08
	}
	j.vy = (j.vy - g) * 0.98
	j.vx *= fr
	return dx, dy
}

// TestHorizontalAllowanceHoldsJava runs Java's travel and checks every move
// against the envelope of the state before it, with no tolerance: walking,
// sprinting on stone, ice, blue ice and slime, and sprint-jumping on ice
// under a trapdoor, the fastest a player goes on foot.
func TestHorizontalAllowanceHoldsJava(t *testing.T) {
	sprint := 0.1 * 1.3
	for _, tc := range []struct {
		name     string
		tick     javaTick
		ticks    int
		terminal float64
	}{
		{"walking on stone", javaTick{friction: 0.6, speed: 0.1, input: 0.98}, 60, 0.2158},
		{"sprinting on stone", javaTick{friction: 0.6, speed: sprint, input: 0.98, sprint: true}, 60, 0.2806},
		{"sprinting on ice", javaTick{friction: 0.98, speed: sprint, input: 0.98, sprint: true}, 200, 0.2702},
		{"sprinting on blue ice", javaTick{friction: 0.989, speed: sprint, input: 0.98, sprint: true}, 200, 0.2845},
		{"sprinting on slime", javaTick{friction: 0.8, speed: sprint, input: 0.98, sprint: true}, 100, 0.1976},
		{"sprint-jumping on stone", javaTick{friction: 0.6, speed: sprint, input: 0.98, sprint: true, jump: true}, 200, 0},
		{"sprint-jumping on ice under a trapdoor", javaTick{friction: 0.98, speed: sprint, input: 0.98, sprint: true, jump: true, ceiling: 2}, 200, 0},
		{"sprint-jumping on ice under a raised trapdoor", javaTick{friction: 0.98, speed: sprint, input: 0.98, sprint: true, jump: true, ceiling: 2.1875}, 200, 0},
		{"sprint-jumping on blue ice under a trapdoor", javaTick{friction: 0.989, speed: sprint, input: 0.98, sprint: true, jump: true, ceiling: 2}, 200, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := javaPlayer{onGround: true}
			var s State
			prevGround, prevFriction := false, 0.0
			var dx float64
			for i := range tc.ticks {
				ground := j.onGround
				f := 0.6
				if ground {
					f = tc.tick.friction
				}
				var dy float64
				dx, dy = j.tick(tc.tick)
				s.Speed, s.Ground, s.Friction, s.Rises = tc.tick.speed, ground, f, dy > 0
				s.PrevGround, s.PrevFriction = prevGround, prevFriction
				if allowed := HorizontalAllowance(s); dx > allowed+1e-9 {
					t.Fatalf("tick %d: Java moved %.5f, the envelope allows %.5f (state %+v)", i, dx, allowed, s)
				}
				s.PrevHorizontal, prevGround, prevFriction = dx, ground, f
			}
			if tc.terminal > 0 && math.Abs(dx-tc.terminal) > 5e-4 {
				t.Errorf("Java's terminal speed %.5f, want %.4f", dx, tc.terminal)
			}
		})
	}
}

// TestHorizontalAllowanceIsTight checks that the envelope of a steady walk
// or sprint is within 3% of Java's terminal speed on stone: the input of 1
// a diagonal gives, against Java's 0.98 forward. On slippery blocks it is
// within 3% of what stone allows, as it grants that too, and above Java's.
func TestHorizontalAllowanceIsTight(t *testing.T) {
	for _, tc := range []struct {
		speed, friction, java, bound float64
	}{
		{0.1, 0.6, 0.2158, 0.2158}, {0.13, 0.6, 0.2806, 0.2806}, {0.13, 0.98, 0.2702, 0.2806},
		{0.13, 0.989, 0.2845, 0.2845}, {0.13, 0.8, 0.1976, 0.2806},
	} {
		s := State{Speed: tc.speed, Ground: true, Friction: tc.friction, PrevGround: true, PrevFriction: tc.friction}
		for range 1000 {
			s.PrevHorizontal = HorizontalAllowance(s)
		}
		if got := s.PrevHorizontal; got < tc.java || got > tc.bound*1.03 {
			t.Errorf("speed %v on friction %v: the envelope settles at %.4f, Java at %.4f, want at most %.4f", tc.speed, tc.friction, got, tc.java, tc.bound*1.03)
		}
	}
	// A player who may sprint is granted the sprint it starts.
	if walk, mayRun := HorizontalAllowance(State{Speed: 0.1, Ground: true}), HorizontalAllowance(State{Speed: 0.1, Ground: true, MaySprint: true}); math.Abs(mayRun-walk*1.3) > 1e-9 {
		t.Errorf("MaySprint grants %v of %v", mayRun, walk)
	}
	// The impulse of an explosion is added as it is.
	if got := HorizontalAllowance(State{Impulse: mgl64.Vec3{3, 5, 4}}); math.Abs(got-(AirAcceleration+5)) > 1e-9 {
		t.Errorf("an impulse of 5 allows %v", got)
	}
}

// TestVerticalAllowanceHoldsJava checks every move of Java's jumps against
// the envelope: the apex of a jump, 1.2522; Jump Boost II; a jump on honey;
// a jump under Slow Falling.
func TestVerticalAllowanceHoldsJava(t *testing.T) {
	for _, tc := range []struct {
		name string
		tick javaTick
		apex float64
		slow bool
	}{
		{"jump", javaTick{friction: 0.6, speed: 0.1, jump: true}, 1.2522, false},
		{"Jump Boost II", javaTick{friction: 0.6, speed: 0.1, jump: true, jumpBoost: 2}, 2.5168, false},
		{"jump on honey", javaTick{friction: 0.6, speed: 0.1, jump: true, jumpFactor: 0.5}, 0.3839, false},
		{"jump under Slow Falling", javaTick{friction: 0.6, speed: 0.1, jump: true, gravity: 0.01}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := javaPlayer{onGround: true}
			s := State{Step: 0.6, JumpBoost: tc.tick.jumpBoost, JumpFactor: tc.tick.jumpFactor, SlowFalling: tc.slow}
			apex := 0.0
			for i := range 60 {
				ground := j.onGround
				_, dy := j.tick(tc.tick)
				s.Ground = ground
				if allowed := VerticalAllowance(s); dy > allowed+1e-9 {
					t.Fatalf("tick %d: Java rose %.5f, the envelope allows %.5f", i, dy, allowed)
				}
				if dy < 0 {
					tc.tick.jump = false
				}
				apex = max(apex, j.y)
				s.PrevVertical = dy
			}
			if tc.apex > 0 && math.Abs(apex-tc.apex) > 5e-4 {
				t.Errorf("apex %.5f, want %.4f", apex, tc.apex)
			}
		})
	}
}

// TestVerticalAllowance checks the cases of the envelope Java's jumps do
// not reach: a fall allows no rise, a ladder 0.2, Levitation V 0.25, an
// upward bubble column 1.8, a bounce and an impulse their speed.
func TestVerticalAllowance(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    State
		want float64
	}{
		{"falling", State{PrevVertical: -0.5}, 0},
		{"rising after a jump", State{PrevVertical: 0.42}, (0.42 - 0.08) * 0.98},
		{"on a ladder", State{Climbing: true, PrevVertical: 0.2}, 0.42},
		{"Levitation V", State{Levitation: 5, PrevVertical: 0}, 0.25},
		{"in water", State{Liquid: true}, 0.7},
		{"leaving the water", State{PrevLiquid: true, PrevVertical: 0.1178}, 0.1178 * 0.8},
		{"in a bubble column", State{Bubbles: true}, 1.8},
		{"a bounce", State{Bounce: 1.5}, 1.5},
		{"knocked up while falling", State{PrevVertical: -1, Impulse: mgl64.Vec3{0, 0.4, 0}}, 0.4},
		{"a step", State{Ground: true, Step: 0.6}, 0.6},
	} {
		if got := VerticalAllowance(tc.s); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: allows %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := AirborneVertical(State{PrevVertical: -0.5, SlowFalling: true}); math.Abs(got-(-0.51*0.98)) > 1e-9 {
		t.Errorf("Slow Falling: %v", got)
	}
}

func TestHoverLimit(t *testing.T) {
	if got := HoverLimit(80, false); got != 80 {
		t.Errorf("hover limit %d, want 80", got)
	}
	if got := HoverLimit(80, true); got != 640 {
		t.Errorf("hover limit under Slow Falling %d, want 640", got)
	}
}

func TestAllowed(t *testing.T) {
	if got := Allowed(0.2, 0.3, 0.05); math.Abs(got-0.31) > 1e-9 {
		t.Errorf("Allowed = %v, want 0.31", got)
	}
}
