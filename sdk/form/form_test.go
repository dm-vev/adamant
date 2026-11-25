package form_test

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/sdk/form"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

func TestMenuModalAndCustomForm(t *testing.T) {
	var clicked string
	mf := form.NewMenu("Warp").
		Body("Pick destination").
		Button("Spawn", "", func(*player.Player, *world.Tx) { clicked = "spawn" }).
		Button("Arena", "", func(*player.Player, *world.Tx) { clicked = "arena" }).
		Build()
	if err := mf.SubmitJSON([]byte("1"), nil, nil); err != nil {
		t.Fatalf("Menu SubmitJSON: %v", err)
	}
	if clicked != "arena" {
		t.Fatalf("clicked = %q, want arena", clicked)
	}

	var confirmed bool
	mod := form.NewModal("Delete?", "Cannot undo").
		Confirm("Yes", func(*player.Player, *world.Tx) { confirmed = true }).
		Cancel("No", nil).
		Build()
	if err := mod.SubmitJSON([]byte("true"), nil, nil); err != nil {
		t.Fatalf("Modal SubmitJSON: %v", err)
	}
	if !confirmed {
		t.Fatal("expected modal confirm callback")
	}

	var got form.Values
	var closed bool
	cf := form.NewCustom("Settings").
		Header("Profile").
		Label("Configure options").
		Divider().
		Input("nick", "Nickname", "Steve", "Enter name").
		Toggle("pvp", "Enable PvP", true).
		Slider("fov", "FOV", 60, 110, 5, 80).
		Dropdown("role", "Role", []string{"Warrior", "Mage"}, 0).
		StepSlider("diff", "Difficulty", []string{"Easy", "Hard"}, 1).
		OnSubmit(func(_ *player.Player, v form.Values, _ *world.Tx) { got = v }).
		OnClose(func(*player.Player, *world.Tx) { closed = true }).
		Build()

	raw, err := json.Marshal(cf)
	if err != nil || len(raw) == 0 {
		t.Fatalf("MarshalJSON custom form: %v", err)
	}

	payload := `[null,null,null,"Alex",false,95,1,0]`
	if err := cf.SubmitJSON([]byte(payload), nil, nil); err != nil {
		t.Fatalf("Custom SubmitJSON: %v", err)
	}
	if got.String("nick") != "Alex" || got.Bool("pvp") || got.Float("fov") != 95 || got.Int("fov") != 95 {
		t.Fatalf("unexpected parsed values: nick=%q pvp=%v fov=%v", got.String("nick"), got.Bool("pvp"), got.Float("fov"))
	}
	if got.String("role") != "Mage" || got.Index("role") != 1 || got.String("diff") != "Easy" {
		t.Fatalf("unexpected dropdown/stepslider values: role=%q(%d) diff=%q", got.String("role"), got.Index("role"), got.String("diff"))
	}

	// Out-of-bounds slider must error.
	if err := cf.SubmitJSON([]byte(`[null,null,null,"Alex",false,999,1,0]`), nil, nil); err == nil {
		t.Fatal("expected error for out-of-bounds slider")
	}

	// Nil payload invokes OnClose.
	if err := cf.SubmitJSON(nil, nil, nil); err != nil || !closed {
		t.Fatalf("OnClose: err=%v closed=%v", err, closed)
	}
}
