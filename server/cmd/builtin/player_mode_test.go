package builtin

import (
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// withPlayers runs f in a synchronous world holding a survival player for
// each name.
func withPlayers(t *testing.T, names []string, f func(tx *world.Tx, players []*player.Player)) {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		players := make([]*player.Player, len(names))
		for i, name := range names {
			players[i] = tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: name, GameMode: world.GameModeSurvival})).(*player.Player)
		}
		f(tx, players)
	})
}

// GameModeCommand sets the game mode of the targets, the source itself by
// default. Bedrock clients also send the numeric and one-letter aliases,
// which the parser accepts beside Java's names.
func TestGamemodeCommand(t *testing.T) {
	withPlayers(t, []string{"Alex", "Steve"}, func(tx *world.Tx, players []*player.Player) {
		alex, steve := players[0], players[1]
		for _, c := range []struct {
			args string
			want world.GameMode
		}{
			{"creative Alex", world.GameModeCreative},
			{"adventure alex", world.GameModeAdventure},
			{"0 Alex", world.GameModeSurvival},
			{"c Alex", world.GameModeCreative},
			{"SPECTATOR Alex", world.GameModeSpectator},
			{"s Alex", world.GameModeSurvival},
			{"2 Alex", world.GameModeAdventure},
			{"sp Alex", world.GameModeSpectator},
		} {
			out := runTestCommand(newGamemodeCommand(), c.args, tx)
			if out.ErrorCount() != 0 || alex.GameMode() != c.want {
				t.Errorf("gamemode %s: mode %v, errors %v", c.args, alex.GameMode(), out.Errors())
			}
		}
		if steve.GameMode() != world.GameModeSurvival {
			t.Fatal("gamemode changed a player it did not target")
		}

		out := runTestCommand(newGamemodeCommand(), "creative @a", tx)
		if out.ErrorCount() != 0 || alex.GameMode() != world.GameModeCreative || steve.GameMode() != world.GameModeCreative {
			t.Fatalf("gamemode creative @a: errors %v", out.Errors())
		}
		if msg := out.Messages()[0].String(); !strings.Contains(msg, "Alex, Steve") {
			t.Errorf("message %q does not name both players", msg)
		}

		for _, args := range []string{"hardcore Alex", "creative Nobody", "creative", "5 Alex", "creative Alex extra"} {
			if out := runTestCommand(newGamemodeCommand(), args, tx); out.ErrorCount() == 0 {
				t.Errorf("gamemode %s succeeded", args)
			}
		}
	})
}

// Commands that change players need operator permission (permission level
// 2 in Java's Commands registration): a player who is not an operator gets
// an unknown command, and nothing changes. The console is trusted.
func TestPlayerCommandsNeedOperator(t *testing.T) {
	// Without a server, no player is an operator.
	previous := commandServer
	commandServer = nil
	t.Cleanup(func() { commandServer = previous })
	withPlayers(t, []string{"Alex"}, func(tx *world.Tx, players []*player.Player) {
		alex := players[0]
		_ = alex.Inventory().SetItem(0, item.NewStack(item.Diamond{}, 3))
		for _, c := range []struct {
			cmd  cmd.Command
			args string
		}{
			{newGamemodeCommand(), "creative"},
			{newClearCommand(), ""},
			{newKickCommand(), "Alex"},
		} {
			c.cmd.Execute(c.args, alex, tx)
		}
		if alex.GameMode() != world.GameModeSurvival {
			t.Error("a non-operator changed their game mode")
		}
		if it, _ := alex.Inventory().Item(0); it.Count() != 3 {
			t.Error("a non-operator cleared their inventory")
		}
		if !slices.Contains(slices.Collect(tx.Players()), world.Entity(alex)) {
			t.Error("a non-operator kicked a player")
		}
	})
}

// ClearCommand without an item removes everything the targets carry: the
// inventory, armour and both hands.
func TestClearCommand(t *testing.T) {
	withPlayers(t, []string{"Alex", "Steve"}, func(tx *world.Tx, players []*player.Player) {
		alex, steve := players[0], players[1]
		for _, p := range players {
			_ = p.Inventory().SetItem(5, item.NewStack(item.Diamond{}, 10))
			p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
			p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), item.NewStack(item.Arrow{}, 8))
		}
		out := runTestCommand(newClearCommand(), "Alex", tx)
		if out.ErrorCount() != 0 {
			t.Fatalf("clear Alex: %v", out.Errors())
		}
		main, off := alex.HeldItems()
		if !main.Empty() || !off.Empty() || !alex.Armour().Helmet().Empty() || len(alex.Inventory().Items()) != 0 {
			t.Fatalf("clear left items on Alex: %v %v %v %v", main, off, alex.Armour().Helmet(), alex.Inventory().Items())
		}
		if len(steve.Inventory().Items()) == 0 || steve.Armour().Helmet().Empty() {
			t.Fatal("clear Alex cleared Steve")
		}
		if out := runTestCommand(newClearCommand(), "", tx); out.ErrorCount() == 0 {
			t.Fatal("clear from the console without a target succeeded")
		}
	})
}

// KickCommand disconnects the targets with the reason given, or a default.
func TestKickCommand(t *testing.T) {
	withPlayers(t, []string{"Alex", "Steve"}, func(tx *world.Tx, players []*player.Player) {
		out := runTestCommand(newKickCommand(), "Alex being rude", tx)
		if out.ErrorCount() != 0 || !strings.Contains(out.Messages()[0].String(), "Alex") {
			t.Fatalf("kick: errors %v, messages %v", out.Errors(), out.Messages())
		}
		remaining := slices.Collect(tx.Players())
		if slices.Contains(remaining, world.Entity(players[0])) || !slices.Contains(remaining, world.Entity(players[1])) {
			t.Fatalf("players after kick: %v", remaining)
		}
		if out := runTestCommand(newKickCommand(), "Nobody", tx); out.ErrorCount() == 0 {
			t.Fatal("kicking an absent player succeeded")
		}
	})
}
