package scoreboard

import (
	"encoding/json"
	"testing"
)

func TestObjectiveScoreOperations(t *testing.T) {
	m := NewModel()
	if err := m.AddObjective("points", "Points", Dummy); err != nil {
		t.Fatal(err)
	}
	if err := m.SetScore("points", "Ada", 7); err != nil {
		t.Fatal(err)
	}
	if got, err := m.AddScore("points", "Ada", -2); err != nil || got != 5 {
		t.Fatalf("AddScore() = %d, %v; want 5, nil", got, err)
	}
	if got, ok := m.Score("points", "Ada"); !ok || got != 5 {
		t.Fatalf("Score() = %d, %v; want 5, true", got, ok)
	}
}

func TestTeamJoinLeave(t *testing.T) {
	m := NewModel()
	if err := m.AddTeam("red", "Red"); err != nil {
		t.Fatal(err)
	}
	if err := m.AddTeam("blue", "Blue"); err != nil {
		t.Fatal(err)
	}
	if err := m.JoinTeam("red", "Alex"); err != nil {
		t.Fatal(err)
	}
	if err := m.JoinTeam("blue", "Alex"); err != nil {
		t.Fatal(err)
	}
	if team, ok := m.TeamFor("Alex"); !ok || team.Name != "blue" {
		t.Fatalf("TeamFor() = %q, %v; want blue, true", team.Name, ok)
	}
	if removed := m.LeaveTeam("Alex"); removed != 1 {
		t.Fatalf("LeaveTeam() = %d; want 1", removed)
	}
}

func TestTriggerEnableAndUse(t *testing.T) {
	m := NewModel()
	if err := m.AddObjective("triggered", "Triggered", Trigger); err != nil {
		t.Fatal(err)
	}
	if err := m.TriggerUse("triggered", "Ada"); err == nil {
		t.Fatal("TriggerUse succeeded before enable")
	}
	m.SetTriggerEnabled("Ada", true)
	if err := m.TriggerUse("triggered", "Ada"); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.Score("triggered", "Ada"); !ok || got != 1 {
		t.Fatalf("Score() = %d, %v; want 1, true", got, ok)
	}
	if m.TriggerIsEnabled("Ada") {
		t.Fatal("trigger remained enabled after use")
	}
}

func TestModelJSONRoundTrip(t *testing.T) {
	m := NewModel()
	if err := m.AddObjective("points", "Points", Dummy); err != nil {
		t.Fatal(err)
	}
	if err := m.SetScore("points", "fake", 11); err != nil {
		t.Fatal(err)
	}
	if err := m.AddTeam("red", "Red"); err != nil {
		t.Fatal(err)
	}
	if err := m.JoinTeam("red", "fake"); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var reopened Model
	if err := json.Unmarshal(data, &reopened); err != nil {
		t.Fatal(err)
	}
	if score, ok := reopened.Score("points", "fake"); !ok || score != 11 {
		t.Fatalf("reopened score = %d, %v; want 11, true", score, ok)
	}
	if team, ok := reopened.TeamFor("fake"); !ok || team.Name != "red" {
		t.Fatalf("reopened team = %q, %v; want red, true", team.Name, ok)
	}
}

func TestScoreOperation(t *testing.T) {
	m := NewModel()
	for _, name := range []string{"a", "b"} {
		if err := m.AddObjective(name, name, Dummy); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.SetScore("a", "Ada", 9); err != nil {
		t.Fatal(err)
	}
	if err := m.SetScore("b", "Ada", 4); err != nil {
		t.Fatal(err)
	}
	if err := m.Operate("a", "Ada", "-=", "b", "Ada"); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.Score("a", "Ada"); !ok || got != 5 {
		t.Fatalf("score after operation = %d, %v; want 5, true", got, ok)
	}
}
