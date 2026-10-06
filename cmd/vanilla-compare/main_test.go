package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCompareReorderedAndValidRuntimeBoundaries(t *testing.T) {
	a := snapshot{ProtocolVersion: 2193, WorldSeed: -1, Dimension: -1, WorldGameMode: 1, Difficulty: 3, Items: []item{{Identifier: "z", RuntimeID: 0}, {Identifier: "a", RuntimeID: -32768}}}
	b := a
	b.Items = []item{a.Items[1], a.Items[0]}
	if got := compare(a, b); !got.Equal {
		t.Fatalf("reordered equivalent snapshots differ: %+v", got)
	}
}

func TestCompareReportsEveryMismatchCategory(t *testing.T) {
	a := snapshot{ProtocolVersion: 1, WorldSeed: 2, Dimension: 3, WorldGameMode: 4, Difficulty: 5, Items: []item{{Identifier: "same", RuntimeID: 1}, {Identifier: "removed", RuntimeID: 2}}}
	b := snapshot{ProtocolVersion: 2, WorldSeed: 9, Dimension: 8, WorldGameMode: 7, Difficulty: 6, Items: []item{{Identifier: "same", RuntimeID: 10, ComponentBased: true, Version: 11}, {Identifier: "added", RuntimeID: 12}}}
	r := compare(a, b)
	if r.Equal || !r.Differences.ProtocolVersion || !r.Differences.WorldSeed || !r.Differences.Dimension || !r.Differences.WorldGameMode || !r.Differences.Difficulty {
		t.Fatalf("settings differences missing: %+v", r)
	}
	if len(r.Differences.Items) != 3 {
		t.Fatalf("expected add/remove/change, got %+v", r.Differences.Items)
	}
	if r.Differences.Items[0].Identifier != "added" || r.Differences.Items[1].Identifier != "removed" || r.Differences.Items[2].Identifier != "same" || !reflect.DeepEqual(r.Differences.Items[2].Changed, []string{"runtime_id", "component_based", "version"}) {
		t.Fatalf("unexpected item differences: %+v", r.Differences.Items)
	}
}

func TestLoadRejectsInvalidSnapshots(t *testing.T) {
	withItems := func(items string) string {
		return `{"protocol_version":2193,"library_game_version":"ignored","world_seed":1,"dimension":0,"world_game_mode":0,"difficulty":1,"biomes":[],"items":` + items + `}`
	}
	valid := withItems(`[{"identifier":"negative","runtime_id":-32768,"component_based":true,"version":2},{"identifier":"zero","runtime_id":0,"component_based":false,"version":2},{"identifier":"positive","runtime_id":32767,"component_based":false,"version":2}]`)
	cases := map[string]string{
		"trailing":               valid + ` {}`,
		"duplicate identifier":   withItems(`[{"identifier":"x","runtime_id":0},{"identifier":"x","runtime_id":1}]`),
		"duplicate runtime":      withItems(`[{"identifier":"x","runtime_id":0},{"identifier":"y","runtime_id":0}]`),
		"runtime overflow":       withItems(`[{"identifier":"x","runtime_id":32768}]`),
		"fractional protocol":    `{"protocol_version":2193.5,"items":[]}`,
		"protocol overflow":      `{"protocol_version":2147483648,"items":[]}`,
		"unknown field":          `{"items":[],"unexpected":true}`,
		"missing world settings": `{"protocol_version":2193,"items":[]}`,
		"null world seed":        `{"protocol_version":2193,"world_seed":null,"dimension":0,"world_game_mode":0,"difficulty":1,"items":[]}`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "snapshot.json")
			if err := os.WriteFile(p, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := load(p); err == nil {
				t.Fatal("expected invalid snapshot error")
			}
		})
	}
	p := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(p, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := load(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []item{
		{Identifier: "negative", RuntimeID: -32768, ComponentBased: true, Version: 2},
		{Identifier: "zero", RuntimeID: 0, Version: 2},
		{Identifier: "positive", RuntimeID: 32767, Version: 2},
	}
	if !reflect.DeepEqual(got.Items, want) || got.ProtocolVersion != 2193 || got.WorldSeed != 1 {
		t.Fatalf("snapshot values changed while decoding: %+v", got)
	}
}
