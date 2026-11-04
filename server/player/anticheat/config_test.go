package anticheat

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/pelletier/go-toml"
)

func TestCheckNames(t *testing.T) {
	want := []string{"MovedTooQuickly", "Speed", "Fly", "Ascend", "NoClip", "Phase", "EntityReach", "BlockReach",
		"BreakTime", "Place", "Attack", "AttackAim", "FastUse", "Timer", "Menu"}
	checks := Checks()
	if len(checks) != len(want) {
		t.Fatalf("%d checks, want %d", len(checks), len(want))
	}
	for i, c := range checks {
		if c.String() != want[i] {
			t.Errorf("check %d is %s, want %s", i, c, want[i])
		}
	}
	if All.String() != "All" {
		t.Errorf("All is %s", All)
	}
}

// TestDefault checks the defaults the design settled: what each check does
// by default, and its thresholds.
func TestDefault(t *testing.T) {
	c := Default()
	if !c.Enabled || c.ExemptOperators || len(c.ExemptPlayers) != 0 || c.LogInterval != 20 || c.MaxLatencyTicks != 40 {
		t.Errorf("global settings: %+v", c)
	}
	if c.Grace != (Grace{Dismount: 10, GameMode: 10, Impulse: 10, Join: 60, Respawn: 20, Teleport: 10, WorldChange: 40}) {
		t.Errorf("grace: %+v", c.Grace)
	}
	actions := map[Check]Action{
		MovedTooQuickly: Setback, Speed: Log, Fly: Setback, Ascend: Log, NoClip: Setback, Phase: Log,
		EntityReach: Cancel, BlockReach: Cancel, BreakTime: Cancel, Place: Log, Attack: Log, AttackAim: Log,
		FastUse: Log, Timer: Throttle, Menu: Cancel,
	}
	for _, ch := range Checks() {
		p := c.Policy(ch)
		if !p.Enabled || p.Action != actions[ch] || p.Decay != 1 || p.KickAt != 0 || !c.Active(ch) {
			t.Errorf("%s: %+v, want enabled, %s, decay 1", ch, p, actions[ch])
		}
		spectator := p.Exempts(world.GameModeSpectator)
		if want := ch == Speed || ch == Fly || ch == Ascend || ch == NoClip || ch == Phase; spectator != want {
			t.Errorf("%s exempts spectators: %v, want %v", ch, spectator, want)
		}
		if p.Exempts(world.GameModeSurvival) {
			t.Errorf("%s exempts survival", ch)
		}
	}
	if c.Policy(NoClip).ActAt != 1 || c.Policy(Speed).ActAt != 0 {
		t.Errorf("ActAt of NoClip %v, of Speed %v", c.Policy(NoClip).ActAt, c.Policy(Speed).ActAt)
	}
	if c.EntityReach != (ReachSettings{Measure: "box", Survival: 3, Creative: 5, Buffer: 3}) {
		t.Errorf("entity reach: %+v", c.EntityReach)
	}
	if c.BlockReach != (ReachSettings{Measure: "centre", Survival: 8, Creative: 14}) {
		t.Errorf("block reach: %+v", c.BlockReach)
	}
	if got := c.Policy(Fly).KickMessage.Plain(); got != "Flying is not enabled on this server" {
		t.Errorf("Fly kicks with %q", got)
	}
	if got := c.Policy(Speed).KickMessage.Plain(); got != "Invalid move player packet received" {
		t.Errorf("Speed kicks with %q", got)
	}
	if got := c.Policy(Attack).KickMessage.Plain(); got != "Attempted to attack an invalid entity" {
		t.Errorf("Attack kicks with %q", got)
	}
	if got := c.Policy(Menu).KickMessage.Plain(); got != "Kicked by the anti-cheat" {
		t.Errorf("Menu kicks with %q", got)
	}
}

// readTable reads an [AntiCheat] table over the defaults, as the server
// reads config.toml.
func readTable(t *testing.T, file string) (UserConfig, error) {
	t.Helper()
	var f struct{ AntiCheat UserConfig }
	f.AntiCheat = DefaultUserConfig()
	err := toml.Unmarshal([]byte(file), &f)
	return f.AntiCheat, err
}

// TestTableKeepsDefaults checks that a table setting one key of a check
// keeps the other keys of that check at their defaults: the Rule and the
// settings embedded in its table are read as keys of the table.
func TestTableKeepsDefaults(t *testing.T) {
	uc, err := readTable(t, "[AntiCheat.Speed]\nAction = \"setback\"\n")
	if err != nil {
		t.Fatal(err)
	}
	c, err := uc.Config(nil)
	if err != nil {
		t.Fatal(err)
	}
	p := c.Policy(Speed)
	if p.Action != Setback || !p.Enabled || p.Decay != 1 || !p.Exempts(world.GameModeSpectator) {
		t.Errorf("Speed after setting its Action: %+v", p)
	}
	if c.Speed != (SpeedSettings{Tolerance: 0.3, Epsilon: 0.05}) {
		t.Errorf("Speed settings: %+v", c.Speed)
	}
	uc, err = readTable(t, "[AntiCheat]\nExemptPlayers = [\"Steve\", \"xuid:2535400000000001\"]\n[AntiCheat.Fly]\nHoverTicks = 100\nExemptGameModes = [\"Creative\", \" sp \"]\nKickMessage = \"§cNo flying\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if c, err = uc.Config(nil); err != nil {
		t.Fatal(err)
	}
	if c.Fly.HoverTicks != 100 || c.Fly.HoverDescent != 0.03125 || !c.Fly.ResendSupport {
		t.Errorf("Fly settings: %+v", c.Fly)
	}
	p = c.Policy(Fly)
	if !p.Exempts(world.GameModeCreative) || !p.Exempts(world.GameModeSpectator) || p.Exempts(world.GameModeSurvival) {
		t.Errorf("Fly exemptions: %v", p.ExemptGameModes)
	}
	if got := p.KickMessage.Plain(); got != "No flying" {
		t.Errorf("Fly kick message %q", got)
	}
	steve, _ := identity.OfName("steve")
	xuid, _ := identity.OfXUID("2535400000000001")
	if len(c.ExemptPlayers) != 2 || c.ExemptPlayers[0] != steve || c.ExemptPlayers[1] != xuid {
		t.Errorf("exempt players: %v", c.ExemptPlayers)
	}
}

// TestDefaultTableRoundTrips checks that the default table, written and
// read back, means the defaults.
func TestDefaultTableRoundTrips(t *testing.T) {
	var f struct{ AntiCheat UserConfig }
	f.AntiCheat = DefaultUserConfig()
	data, err := toml.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var g struct{ AntiCheat UserConfig }
	if err := toml.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	c, err := g.AntiCheat.Config(nil)
	if err != nil {
		t.Fatalf("the default table read back: %v\n%s", err, data)
	}
	d := Default()
	for _, ch := range Checks() {
		if a, b := c.Policy(ch), d.Policy(ch); a.Action != b.Action || a.Enabled != b.Enabled || a.ActAt != b.ActAt || a.Decay != b.Decay || len(a.ExemptGameModes) != len(b.ExemptGameModes) {
			t.Errorf("%s read back as %+v, want %+v", ch, a, b)
		}
	}
	if c.Speed != d.Speed || c.Fly != d.Fly || c.Timer != d.Timer || c.EntityReach != d.EntityReach || c.Grace != d.Grace {
		t.Errorf("settings read back differ:\n%+v\n%+v", c, d)
	}
	if !strings.Contains(string(data), "[AntiCheat.Speed]") || strings.Contains(string(data), "Rule") || strings.Contains(string(data), "Settings") {
		t.Errorf("the table is not written flat:\n%s", data)
	}
}

func TestConfigErrors(t *testing.T) {
	for file, want := range map[string]string{
		"[AntiCheat.Speed]\nAction = \"throttle\"\n":           `AntiCheat.Speed.Action "throttle": use "log", "setback" or "kick"`,
		"[AntiCheat.Place]\nAction = \"setback\"\n":            `AntiCheat.Place.Action "setback": use "log", "cancel" or "kick"`,
		"[AntiCheat.Timer]\nAction = \"cancel\"\n":             `AntiCheat.Timer.Action "cancel": use "log", "throttle", "setback" or "kick"`,
		"[AntiCheat.Ascend]\nAction = \"throttle\"\n":          `AntiCheat.Ascend.Action "throttle": use "log", "setback", "cancel" or "kick"`,
		"[AntiCheat.Place]\nExemptGameModes = [\"creativ\"]\n": `AntiCheat.Place.ExemptGameModes: unknown game mode "creativ"`,
		"[AntiCheat.Speed]\nTolerance = -0.1\n":                `AntiCheat.Speed.Tolerance -0.1: must not be negative`,
		"[AntiCheat.Speed]\nDecay = -1.0\n":                    `AntiCheat.Speed.Decay -1: must not be negative`,
		"[AntiCheat.BreakTime]\nTolerance = 0.0\n":             `AntiCheat.BreakTime.Tolerance 0: must be above 0 and at most 1`,
		"[AntiCheat.BreakTime]\nTolerance = 1.5\n":             `AntiCheat.BreakTime.Tolerance 1.5: must be above 0 and at most 1`,
		"[AntiCheat.AttackAim]\nMinCos = 1.5\n":                `AntiCheat.AttackAim.MinCos 1.5: must be from -1 to 1`,
		"[AntiCheat.EntityReach]\nMeasure = \"centre\"\n":      `AntiCheat.EntityReach.Measure "centre": use "box" or "position"`,
		"[AntiCheat.BlockReach]\nMeasure = \"eyes\"\n":         `AntiCheat.BlockReach.Measure "eyes": use "box", "centre" or "position"`,
		"[AntiCheat.MovedTooQuickly]\nLimit = 0.0\n":           `AntiCheat.MovedTooQuickly.Limit 0: must be above 0`,
		"[AntiCheat.Grace]\nJoin = -5\n":                       `AntiCheat.Grace.Join -5: must not be negative`,
		"[AntiCheat]\nExemptPlayers = [\" \"]\n":               `AntiCheat.ExemptPlayers: " ": player name must not be empty`,
	} {
		uc, err := readTable(t, file)
		if err != nil {
			t.Fatalf("%q: %v", file, err)
		}
		if _, err := uc.Config(nil); err == nil || err.Error() != want {
			t.Errorf("%q: error %v, want %q", file, err, want)
		}
	}
}

// TestConfigTrimsValues checks that actions and measures are read trimmed
// and in any case, as IdentityKey is.
func TestConfigTrimsValues(t *testing.T) {
	uc, err := readTable(t, "[AntiCheat.Speed]\nAction = \" SetBack \"\n[AntiCheat.BlockReach]\nMeasure = \" Center \"\n")
	if err != nil {
		t.Fatal(err)
	}
	c, err := uc.Config(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Policy(Speed).Action != Setback || c.BlockReach.Measure != "centre" {
		t.Errorf("Speed %s, BlockReach measure %q", c.Policy(Speed).Action, c.BlockReach.Measure)
	}
}

// TestConfigWarnsOfKickBeforeAct checks that a rule that kicks before it
// acts is a warning, not an error.
func TestConfigWarnsOfKickBeforeAct(t *testing.T) {
	uc, err := readTable(t, "[AntiCheat.NoClip]\nKickAt = 1.0\n")
	if err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	if _, err := uc.Config(slog.New(slog.NewTextHandler(&logged, nil))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logged.String(), "kicks before it acts") || !strings.Contains(logged.String(), "check=NoClip") {
		t.Errorf("logged %q", logged.String())
	}
}
