package anticheat

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
)

// Rule holds the keys every check has, as config.toml writes them. A table
// embeds it, so its keys are those of the table.
type Rule struct {
	// Enabled switches the check on.
	Enabled bool
	// Action is what the game does once the violation level passed ActAt:
	// "log", "setback" (the movement checks), "cancel" (the action checks,
	// and Ascend), "throttle" (Timer) or "kick".
	Action string
	// ActAt is the violation level past which Action applies: 0 applies it
	// at the first violation.
	ActAt float64
	// KickAt is the violation level at which the player is kicked whatever
	// Action is; 0 never kicks.
	KickAt float64
	// KickMessage is the reason a kick shows, read with § codes; "" shows
	// the default of the check.
	KickMessage string
	// Decay is the violation level lost each second.
	Decay float64
	// ExemptGameModes are the game modes the check skips, named as
	// /gamemode takes them.
	ExemptGameModes []string
}

// MovedTooQuicklySettings are the keys of MovedTooQuickly beyond its Rule.
type MovedTooQuicklySettings struct {
	// Limit is how many blocks beyond the velocity the server knows a
	// player may move in a tick: Java's √100.
	Limit float64
	// LimitGliding is Limit while gliding: Java's √300.
	LimitGliding float64
}

// SpeedSettings are the keys of Speed beyond its Rule.
type SpeedSettings struct {
	// Tolerance is the part of the envelope allowed beyond it: 0.3 lets a
	// player go 30% faster than Java's physics.
	Tolerance float64
	// Epsilon is a flat slack, in blocks a tick, beyond the envelope.
	Epsilon float64
}

// FlySettings are the keys of Fly beyond its Rule.
type FlySettings struct {
	// HoverTicks is how many moves in a row a player may float before it
	// violates, Java's 80 at normal gravity.
	HoverTicks int
	// HoverDescent is the least a player must descend in a move for it not
	// to count as floating: Java's 0.03125.
	HoverDescent float64
	// ResendSupport resends the blocks below a hovering player before it
	// is set back, at most once every 200 ticks, as a ghost block heals.
	ResendSupport bool
}

// AscendSettings are the keys of Ascend beyond its Rule.
type AscendSettings struct {
	// Epsilon is a flat slack, in blocks a tick, beyond the envelope.
	Epsilon float64
	// StepHeight is the height a player on the ground steps up in a move.
	StepHeight float64
	// GroundMargin is how far, in blocks, the box of a player may be from
	// the edge of a block, to its sides, and the player still stand on it:
	// a Bedrock client sends its position as a float32.
	GroundMargin float64
	// GroundDepth is how far, in blocks, below a player a block may be for
	// it to stand on it, if its last move did not rise or its client said
	// it collided below in it: the shapes of blocks may differ between a
	// client and the server, as a single layer of snow does.
	GroundDepth float64
}

// NoClipSettings are the keys of NoClip beyond its Rule.
type NoClipSettings struct {
	// MinDepth is how deep, in blocks, a player must enter a block for it
	// to count: the shapes of blocks may differ between a client and the
	// server by less.
	MinDepth float64
	// ResendFirst resends the blocks a violation entered: while the level
	// is at most ActAt, the move is only logged, so a desync heals itself
	// and a cheat is set back from the next violation.
	ResendFirst bool
}

// PhaseSettings are the keys of Phase beyond its Rule.
type PhaseSettings struct {
	// Tolerance is how far, in blocks, a move may end from where the
	// collisions along its path stop it: Java's √0.0625.
	Tolerance float64
}

// ReachSettings are the keys of EntityReach and BlockReach beyond their
// Rule.
type ReachSettings struct {
	// Measure is what the reach is measured to from the eyes: "box", the
	// nearest point of the box of the target, as Java does; "centre", the
	// centre of a block; "position", the position of an entity.
	Measure string
	// Survival and Creative are the reach in those game modes; the other
	// modes take Survival.
	Survival, Creative float64
	// Buffer is added to the reach, Java's allowance for latency.
	Buffer float64
}

// BreakTimeSettings are the keys of BreakTime beyond its Rule.
type BreakTimeSettings struct {
	// Tolerance is the progress at which a finish is accepted, counting
	// the tick it comes in: Java's 0.7.
	Tolerance float64
	// Delay breaks a block finished early once its time is served, Java's
	// delayed destroy, rather than refusing the finish.
	Delay bool
	// InstantPerSecond and InstantBurst bound the blocks a player breaks
	// at once in survival: a token bucket of that rate and size.
	InstantPerSecond float64
	InstantBurst     int
}

// PlaceSettings are the keys of Place beyond its Rule.
type PlaceSettings struct {
	// PerSecond and Burst bound the blocks placed: a token bucket.
	PerSecond float64
	Burst     int
	// FaceEpsilon is how far, in blocks, the eyes may be behind the plane
	// of the face clicked.
	FaceEpsilon float64
	// LineOfSight refuses a place whose point clicked the eyes cannot see
	// past full blocks.
	LineOfSight bool
}

// AttackSettings are the keys of Attack beyond its Rule.
type AttackSettings struct {
	// PerSecond and Burst bound the attacks: a token bucket.
	PerSecond float64
	Burst     int
	// TargetsPerTick is how many living targets a player attacks in a
	// tick.
	TargetsPerTick int
}

// AttackAimSettings are the keys of AttackAim beyond its Rule.
type AttackAimSettings struct {
	// MinCos is the least cosine of the angle between the aim and the
	// direction to the target: BDS's 0.85. An aim whose ray hits the box of
	// the target, grown by HitboxMargin, needs no cosine.
	MinCos float64
	// HitboxMargin is how far, in blocks, a ray of the client may pass from
	// the box of the target and still hit it: the box the client saw is
	// where the target was a round trip ago.
	HitboxMargin float64
	// LineOfSight refuses an attack on a target whose centre the eyes
	// cannot see past full blocks.
	LineOfSight bool
}

// FastUseSettings are the keys of FastUse beyond its Rule.
type FastUseSettings struct {
	// MinTicks is the least ticks between two uses of an item without a
	// cooldown, Java's right-click delay.
	MinTicks int
	// Burst is how many uses may come closer together than MinTicks.
	Burst int
}

// TimerSettings are the keys of Timer beyond its Rule. They are in ticks.
type TimerSettings struct {
	// Tolerance is how many ticks a client may run ahead of time.
	Tolerance int
	// MaxBalance is the most ticks a client may make up after it fell
	// behind, such as after a stall of its connection.
	MaxBalance int
	// MaxQueue is the most ticks a move may wait while the client is ahead
	// of time; a move that waited longer is dropped, and the player set
	// back.
	MaxQueue int
}

// MenuSettings are the keys of Menu beyond its Rule.
type MenuSettings struct {
	// Buffer is added to the reach of blocks and entities for a menu to
	// stay open, Java's 4.
	Buffer float64
	// RequestsPerTick bounds the menu operations a player makes in a tick.
	RequestsPerTick int
}

// The tables of the checks in config.toml: the Rule and the settings of
// each.
type (
	MovedTooQuicklyTable struct {
		Rule
		MovedTooQuicklySettings
	}
	SpeedTable struct {
		Rule
		SpeedSettings
	}
	FlyTable struct {
		Rule
		FlySettings
	}
	AscendTable struct {
		Rule
		AscendSettings
	}
	NoClipTable struct {
		Rule
		NoClipSettings
	}
	PhaseTable struct {
		Rule
		PhaseSettings
	}
	ReachTable struct {
		Rule
		ReachSettings
	}
	BreakTimeTable struct {
		Rule
		BreakTimeSettings
	}
	PlaceTable struct {
		Rule
		PlaceSettings
	}
	AttackTable struct {
		Rule
		AttackSettings
	}
	AttackAimTable struct {
		Rule
		AttackAimSettings
	}
	FastUseTable struct {
		Rule
		FastUseSettings
	}
	TimerTable struct {
		Rule
		TimerSettings
	}
	MenuTable struct {
		Rule
		MenuSettings
	}
)

// Grace holds the ticks of a player during which the movement checks skip
// its moves, after what moved it other than its client.
type Grace struct {
	// Dismount follows getting off what it rode.
	Dismount int
	// GameMode follows a change of its game mode or abilities.
	GameMode int
	// Impulse is added to the round trip after each impulse the server
	// gives the player, and after an effect or a speed that ended: the
	// client applies them a round trip later.
	Impulse int
	// Join follows the first move after it spawned.
	Join int
	// Respawn follows a respawn.
	Respawn int
	// Teleport follows the acknowledgement of a teleport, and a move by
	// the server.
	Teleport int
	// WorldChange follows a change of world.
	WorldChange int
}

// UserConfig is the [AntiCheat] table of config.toml.
type UserConfig struct {
	// Enabled switches every check on or off.
	Enabled bool
	// ExemptOperators has operators skip every check.
	ExemptOperators bool
	// ExemptPlayers are players who skip every check: names, or XUIDs
	// written "xuid:<XUID>", as the admin lists take them.
	ExemptPlayers []string
	// LogInterval is the ticks between two lines a check logs of one
	// player: the first violation, then a summary.
	LogInterval int
	// MaxLatencyTicks caps the round trip, in ticks, that the latency
	// windows assume: 40, the 2 s round trip of a latency of 1 s.
	MaxLatencyTicks int
	// Grace holds the grace periods of the movement checks.
	Grace Grace

	MovedTooQuickly MovedTooQuicklyTable
	Speed           SpeedTable
	Fly             FlyTable
	Ascend          AscendTable
	NoClip          NoClipTable
	Phase           PhaseTable
	EntityReach     ReachTable
	BlockReach      ReachTable
	BreakTime       BreakTimeTable
	Place           PlaceTable
	Attack          AttackTable
	AttackAim       AttackAimTable
	FastUse         FastUseTable
	Timer           TimerTable
	Menu            MenuTable
}

// rule returns a pointer to the Rule of the table of the check passed.
func (uc *UserConfig) rule(c Check) *Rule {
	switch c {
	case MovedTooQuickly:
		return &uc.MovedTooQuickly.Rule
	case Speed:
		return &uc.Speed.Rule
	case Fly:
		return &uc.Fly.Rule
	case Ascend:
		return &uc.Ascend.Rule
	case NoClip:
		return &uc.NoClip.Rule
	case Phase:
		return &uc.Phase.Rule
	case EntityReach:
		return &uc.EntityReach.Rule
	case BlockReach:
		return &uc.BlockReach.Rule
	case BreakTime:
		return &uc.BreakTime.Rule
	case Place:
		return &uc.Place.Rule
	case Attack:
		return &uc.Attack.Rule
	case AttackAim:
		return &uc.AttackAim.Rule
	case FastUse:
		return &uc.FastUse.Rule
	case Timer:
		return &uc.Timer.Rule
	case Menu:
		return &uc.Menu.Rule
	}
	panic("unknown check " + c.String())
}

// DefaultUserConfig returns the [AntiCheat] table of a new config.toml.
func DefaultUserConfig() UserConfig {
	uc := UserConfig{
		Enabled:         true,
		LogInterval:     20,
		MaxLatencyTicks: 40,
		Grace:           Grace{Dismount: 10, GameMode: 10, Impulse: 10, Join: 60, Respawn: 20, Teleport: 10, WorldChange: 40},

		MovedTooQuickly: MovedTooQuicklyTable{MovedTooQuicklySettings: MovedTooQuicklySettings{Limit: 10, LimitGliding: 17.32}},
		Speed:           SpeedTable{SpeedSettings: SpeedSettings{Tolerance: 0.3, Epsilon: 0.05}},
		Fly:             FlyTable{FlySettings: FlySettings{HoverTicks: 80, HoverDescent: 0.03125, ResendSupport: true}},
		Ascend:          AscendTable{AscendSettings: AscendSettings{Epsilon: 0.05, StepHeight: 0.6, GroundMargin: 0.02, GroundDepth: 0.25}},
		NoClip:          NoClipTable{NoClipSettings: NoClipSettings{MinDepth: 0.1, ResendFirst: true}},
		Phase:           PhaseTable{PhaseSettings: PhaseSettings{Tolerance: 0.25}},
		EntityReach:     ReachTable{ReachSettings: ReachSettings{Measure: "box", Survival: 3, Creative: 5, Buffer: 3}},
		BlockReach:      ReachTable{ReachSettings: ReachSettings{Measure: "centre", Survival: 8, Creative: 14}},
		BreakTime:       BreakTimeTable{BreakTimeSettings: BreakTimeSettings{Tolerance: 0.7, InstantPerSecond: 20, InstantBurst: 5}},
		Place:           PlaceTable{PlaceSettings: PlaceSettings{PerSecond: 20, Burst: 10, FaceEpsilon: 0.5}},
		Attack:          AttackTable{AttackSettings: AttackSettings{PerSecond: 20, Burst: 10, TargetsPerTick: 1}},
		AttackAim:       AttackAimTable{AttackAimSettings: AttackAimSettings{MinCos: 0.85, HitboxMargin: 0.4, LineOfSight: true}},
		FastUse:         FastUseTable{FastUseSettings: FastUseSettings{MinTicks: 4, Burst: 2}},
		Timer:           TimerTable{TimerSettings: TimerSettings{Tolerance: 10, MaxBalance: 40, MaxQueue: 40}},
		Menu:            MenuTable{MenuSettings: MenuSettings{Buffer: 4, RequestsPerTick: 64}},
	}
	for _, c := range Checks() {
		r := uc.rule(c)
		r.Enabled, r.Action, r.Decay, r.ExemptGameModes = true, defaultActions[c].String(), 1, []string{}
	}
	for _, c := range []Check{Speed, Fly, Ascend, NoClip, Phase} {
		uc.rule(c).ExemptGameModes = []string{"spectator"}
	}
	// Java does not judge how creative players move through blocks either.
	uc.rule(Phase).ExemptGameModes = []string{"creative", "spectator"}
	uc.rule(NoClip).ActAt = 1
	return uc
}

// defaultActions are the actions of the checks by default. Speed only logs:
// the physics of Bedrock clients is not Java's, and no recorded Bedrock
// traces calibrate the envelope yet.
var defaultActions = [checkCount]Action{
	MovedTooQuickly: Setback, Speed: Log, Fly: Setback, Ascend: Log, NoClip: Setback, Phase: Log,
	EntityReach: Cancel, BlockReach: Cancel, BreakTime: Cancel, Place: Log, Attack: Log, AttackAim: Log,
	FastUse: Log, Timer: Throttle, Menu: Cancel,
}

// Policy is the Rule of a check, parsed.
type Policy struct {
	// Enabled switches the check on.
	Enabled bool
	// Action is what the game does once the level passed ActAt.
	Action Action
	// ActAt, KickAt and Decay are those of the Rule.
	ActAt, KickAt, Decay float64
	// KickMessage is the reason a kick shows, the default of the check if
	// the Rule had none.
	KickMessage text.Text
	// ExemptGameModes are the game modes the check skips.
	ExemptGameModes []world.GameMode
}

// Exempts reports whether the policy exempts a player in the game mode
// passed.
func (p Policy) Exempts(mode world.GameMode) bool {
	return slices.Contains(p.ExemptGameModes, mode)
}

// Config is the anti-cheat as the game runs it: the [AntiCheat] table of
// config.toml, parsed and checked.
type Config struct {
	// Enabled switches every check on or off.
	Enabled bool
	// ExemptOperators has operators skip every check.
	ExemptOperators bool
	// ExemptPlayers are the keys of the players who skip every check.
	ExemptPlayers []identity.Key
	// LogInterval is the ticks between two lines a check logs of one
	// player.
	LogInterval int
	// MaxLatencyTicks caps the round trip the latency windows assume.
	MaxLatencyTicks int
	// Grace holds the grace periods of the movement checks.
	Grace Grace

	policies [checkCount]Policy

	MovedTooQuickly MovedTooQuicklySettings
	Speed           SpeedSettings
	Fly             FlySettings
	Ascend          AscendSettings
	NoClip          NoClipSettings
	Phase           PhaseSettings
	EntityReach     ReachSettings
	BlockReach      ReachSettings
	BreakTime       BreakTimeSettings
	Place           PlaceSettings
	Attack          AttackSettings
	AttackAim       AttackAimSettings
	FastUse         FastUseSettings
	Timer           TimerSettings
	Menu            MenuSettings
}

// Policy returns the policy of the check passed.
func (c *Config) Policy(ch Check) Policy { return c.policies[ch] }

// SetPolicy sets the policy of the check passed, as a plugin or a test
// configures the anti-cheat without a file.
func (c *Config) SetPolicy(ch Check, p Policy) { c.policies[ch] = p }

// Active reports whether the check passed runs: the anti-cheat and the
// check are enabled.
func (c *Config) Active(ch Check) bool { return c.Enabled && c.policies[ch].Enabled }

// Default returns the anti-cheat of DefaultUserConfig.
func Default() *Config {
	c, err := DefaultUserConfig().Config(nil)
	if err != nil {
		panic("anticheat: the default config is invalid: " + err.Error())
	}
	return c
}

// defaultKickMessages are the reasons kicks show by default: those Java
// writes, in English.
var defaultKickMessages = map[Check]string{
	Fly:         "Flying is not enabled on this server",
	EntityReach: "Attempted to attack an invalid entity",
	Attack:      "Attempted to attack an invalid entity",
	AttackAim:   "Attempted to attack an invalid entity",
}

// DefaultKickMessage returns the reason a kick of the check shows when its
// Rule has none: Java's English texts.
func DefaultKickMessage(c Check) string {
	if m, ok := defaultKickMessages[c]; ok {
		return m
	}
	if c.Movement() {
		return "Invalid move player packet received"
	}
	return "Kicked by the anti-cheat"
}

// Config parses and checks the table: an error names the first key whose
// value the game cannot run, so that a typo does not switch a protection
// off unnoticed. A rule that kicks before it acts is logged to log, a
// warning, if log is not nil.
func (uc UserConfig) Config(log *slog.Logger) (*Config, error) {
	c := &Config{
		Enabled:         uc.Enabled,
		ExemptOperators: uc.ExemptOperators,
		LogInterval:     uc.LogInterval,
		MaxLatencyTicks: uc.MaxLatencyTicks,
		Grace:           uc.Grace,
		MovedTooQuickly: uc.MovedTooQuickly.MovedTooQuicklySettings,
		Speed:           uc.Speed.SpeedSettings,
		Fly:             uc.Fly.FlySettings,
		Ascend:          uc.Ascend.AscendSettings,
		NoClip:          uc.NoClip.NoClipSettings,
		Phase:           uc.Phase.PhaseSettings,
		EntityReach:     uc.EntityReach.ReachSettings,
		BlockReach:      uc.BlockReach.ReachSettings,
		BreakTime:       uc.BreakTime.BreakTimeSettings,
		Place:           uc.Place.PlaceSettings,
		Attack:          uc.Attack.AttackSettings,
		AttackAim:       uc.AttackAim.AttackAimSettings,
		FastUse:         uc.FastUse.FastUseSettings,
		Timer:           uc.Timer.TimerSettings,
		Menu:            uc.Menu.MenuSettings,
	}
	for _, s := range uc.ExemptPlayers {
		k, err := identity.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("AntiCheat.ExemptPlayers: %q: %w", s, err)
		}
		c.ExemptPlayers = append(c.ExemptPlayers, k)
	}
	if err := errors.Join(
		nonNegative("AntiCheat.LogInterval", float64(uc.LogInterval)),
		nonNegative("AntiCheat.MaxLatencyTicks", float64(uc.MaxLatencyTicks)),
		nonNegative("AntiCheat.Grace.Dismount", float64(uc.Grace.Dismount)),
		nonNegative("AntiCheat.Grace.GameMode", float64(uc.Grace.GameMode)),
		nonNegative("AntiCheat.Grace.Impulse", float64(uc.Grace.Impulse)),
		nonNegative("AntiCheat.Grace.Join", float64(uc.Grace.Join)),
		nonNegative("AntiCheat.Grace.Respawn", float64(uc.Grace.Respawn)),
		nonNegative("AntiCheat.Grace.Teleport", float64(uc.Grace.Teleport)),
		nonNegative("AntiCheat.Grace.WorldChange", float64(uc.Grace.WorldChange)),
	); err != nil {
		return nil, firstError(err)
	}
	for _, ch := range Checks() {
		p, err := parseRule(ch, *uc.rule(ch), log)
		if err != nil {
			return nil, err
		}
		c.policies[ch] = p
	}
	measures := []string{"box", "centre", "position"}
	var err error
	if c.EntityReach.Measure, err = parseMeasure("AntiCheat.EntityReach.Measure", c.EntityReach.Measure, []string{"box", "position"}); err != nil {
		return nil, err
	}
	if c.BlockReach.Measure, err = parseMeasure("AntiCheat.BlockReach.Measure", c.BlockReach.Measure, measures); err != nil {
		return nil, err
	}
	if err := errors.Join(
		positive("AntiCheat.MovedTooQuickly.Limit", c.MovedTooQuickly.Limit),
		positive("AntiCheat.MovedTooQuickly.LimitGliding", c.MovedTooQuickly.LimitGliding),
		nonNegative("AntiCheat.Speed.Tolerance", c.Speed.Tolerance),
		nonNegative("AntiCheat.Speed.Epsilon", c.Speed.Epsilon),
		positive("AntiCheat.Fly.HoverTicks", float64(c.Fly.HoverTicks)),
		nonNegative("AntiCheat.Fly.HoverDescent", c.Fly.HoverDescent),
		nonNegative("AntiCheat.Ascend.Epsilon", c.Ascend.Epsilon),
		nonNegative("AntiCheat.Ascend.StepHeight", c.Ascend.StepHeight),
		nonNegative("AntiCheat.Ascend.GroundMargin", c.Ascend.GroundMargin),
		nonNegative("AntiCheat.Ascend.GroundDepth", c.Ascend.GroundDepth),
		nonNegative("AntiCheat.NoClip.MinDepth", c.NoClip.MinDepth),
		nonNegative("AntiCheat.Phase.Tolerance", c.Phase.Tolerance),
		positive("AntiCheat.EntityReach.Survival", c.EntityReach.Survival),
		positive("AntiCheat.EntityReach.Creative", c.EntityReach.Creative),
		nonNegative("AntiCheat.EntityReach.Buffer", c.EntityReach.Buffer),
		positive("AntiCheat.BlockReach.Survival", c.BlockReach.Survival),
		positive("AntiCheat.BlockReach.Creative", c.BlockReach.Creative),
		nonNegative("AntiCheat.BlockReach.Buffer", c.BlockReach.Buffer),
		within("AntiCheat.BreakTime.Tolerance", c.BreakTime.Tolerance, 0, 1, false),
		positive("AntiCheat.BreakTime.InstantPerSecond", c.BreakTime.InstantPerSecond),
		positive("AntiCheat.BreakTime.InstantBurst", float64(c.BreakTime.InstantBurst)),
		positive("AntiCheat.Place.PerSecond", c.Place.PerSecond),
		positive("AntiCheat.Place.Burst", float64(c.Place.Burst)),
		nonNegative("AntiCheat.Place.FaceEpsilon", c.Place.FaceEpsilon),
		positive("AntiCheat.Attack.PerSecond", c.Attack.PerSecond),
		positive("AntiCheat.Attack.Burst", float64(c.Attack.Burst)),
		positive("AntiCheat.Attack.TargetsPerTick", float64(c.Attack.TargetsPerTick)),
		within("AntiCheat.AttackAim.MinCos", c.AttackAim.MinCos, -1, 1, true),
		nonNegative("AntiCheat.AttackAim.HitboxMargin", c.AttackAim.HitboxMargin),
		nonNegative("AntiCheat.FastUse.MinTicks", float64(c.FastUse.MinTicks)),
		positive("AntiCheat.FastUse.Burst", float64(c.FastUse.Burst)),
		nonNegative("AntiCheat.Timer.Tolerance", float64(c.Timer.Tolerance)),
		nonNegative("AntiCheat.Timer.MaxBalance", float64(c.Timer.MaxBalance)),
		positive("AntiCheat.Timer.MaxQueue", float64(c.Timer.MaxQueue)),
		nonNegative("AntiCheat.Menu.Buffer", c.Menu.Buffer),
		positive("AntiCheat.Menu.RequestsPerTick", float64(c.Menu.RequestsPerTick)),
	); err != nil {
		return nil, firstError(err)
	}
	return c, nil
}

// parseRule parses the Rule of the check passed.
func parseRule(c Check, r Rule, log *slog.Logger) (Policy, error) {
	key := "AntiCheat." + c.String()
	p := Policy{Enabled: r.Enabled, ActAt: r.ActAt, KickAt: r.KickAt, Decay: r.Decay}
	name := strings.ToLower(strings.TrimSpace(r.Action))
	found := false
	var names []string
	for _, a := range c.actions() {
		names = append(names, fmt.Sprintf("%q", a.String()))
		if a.String() == name {
			p.Action, found = a, true
		}
	}
	if !found {
		return p, fmt.Errorf("%s.Action %q: use %s or %s", key, r.Action, strings.Join(names[:len(names)-1], ", "), names[len(names)-1])
	}
	if err := errors.Join(
		nonNegative(key+".ActAt", r.ActAt),
		nonNegative(key+".KickAt", r.KickAt),
		nonNegative(key+".Decay", r.Decay),
	); err != nil {
		return p, firstError(err)
	}
	for _, s := range r.ExemptGameModes {
		mode, ok := ParseGameMode(s)
		if !ok {
			return p, fmt.Errorf("%s.ExemptGameModes: unknown game mode %q", key, s)
		}
		p.ExemptGameModes = append(p.ExemptGameModes, mode)
	}
	msg := r.KickMessage
	if strings.TrimSpace(msg) == "" {
		msg = DefaultKickMessage(c)
	}
	p.KickMessage = text.ParseLegacy(msg)
	if log != nil && r.Enabled && r.KickAt > 0 && r.KickAt <= r.ActAt && p.Action != Kick {
		logging.Sub(log, logging.AntiCheat).Warn("An anti-cheat check kicks before it acts: its KickAt is at most its ActAt.", "check", c.String(), "KickAt", r.KickAt, "ActAt", r.ActAt, "Action", p.Action.String())
	}
	return p, nil
}

// ParseGameMode returns the game mode of a name as /gamemode takes it:
// "survival", "creative", "adventure" or "spectator", their short forms or
// numbers, in any case.
func ParseGameMode(s string) (world.GameMode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0", "s", "survival":
		return world.GameModeSurvival, true
	case "1", "c", "creative":
		return world.GameModeCreative, true
	case "2", "a", "adventure":
		return world.GameModeAdventure, true
	case "3", "sp", "spectator", "spectate":
		return world.GameModeSpectator, true
	}
	return nil, false
}

// parseMeasure returns the measure of a reach, trimmed and lower-cased, if
// it is one of those passed. "center" reads as "centre".
func parseMeasure(key, s string, valid []string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(s))
	if m == "center" {
		m = "centre"
	}
	if slices.Contains(valid, m) {
		return m, nil
	}
	quoted := make([]string, len(valid))
	for i, v := range valid {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return "", fmt.Errorf("%s %q: use %s or %s", key, s, strings.Join(quoted[:len(quoted)-1], ", "), quoted[len(quoted)-1])
}

func nonNegative(key string, v float64) error {
	if v < 0 || math.IsNaN(v) {
		return fmt.Errorf("%s %v: must not be negative", key, v)
	}
	return nil
}

func positive(key string, v float64) error {
	if !(v > 0) {
		return fmt.Errorf("%s %v: must be above 0", key, v)
	}
	return nil
}

// within checks that v is in (low, high], or [low, high] if closed.
func within(key string, v, low, high float64, closed bool) error {
	if math.IsNaN(v) || v > high || v < low || (!closed && v == low) {
		if closed {
			return fmt.Errorf("%s %v: must be from %v to %v", key, v, low, high)
		}
		return fmt.Errorf("%s %v: must be above %v and at most %v", key, v, low, high)
	}
	return nil
}

// firstError returns the first error errors.Join joined.
func firstError(err error) error {
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		return j.Unwrap()[0]
	}
	return err
}
