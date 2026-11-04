package server

import (
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/item/recipe"
)

// TestNewRegistersVanillaContentOnce checks that a second server in the same
// process leaves the recipes and creative items the first registered as they
// were, in the same order, so that their network IDs stay the same.
func TestNewRegistersVanillaContentOnce(t *testing.T) {
	conf := Config{Log: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))}

	closeWorlds(t, conf.New())
	recipes, dynamic := recipe.Recipes(), recipe.DynamicRecipes()
	groups, items := creative.Groups(), creative.Items()
	if len(recipes) == 0 || len(groups) == 0 || len(items) == 0 {
		t.Fatalf("New registered %d recipes, %d creative groups and %d creative items, want vanilla content", len(recipes), len(groups), len(items))
	}

	closeWorlds(t, conf.New())
	if got := recipe.Recipes(); !reflect.DeepEqual(got, recipes) {
		t.Errorf("a second New changed the recipes: %d registered, %d before", len(got), len(recipes))
	}
	if got := recipe.DynamicRecipes(); !reflect.DeepEqual(got, dynamic) {
		t.Errorf("a second New changed the dynamic recipes: %d registered, %d before", len(got), len(dynamic))
	}
	if got := creative.Groups(); !reflect.DeepEqual(got, groups) {
		t.Errorf("a second New changed the creative groups: %d registered, %d before", len(got), len(groups))
	}
	if got := creative.Items(); !reflect.DeepEqual(got, items) {
		t.Errorf("a second New changed the creative items: %d registered, %d before", len(got), len(items))
	}
}
